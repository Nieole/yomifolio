package identification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"yomifolio/internal/bookcontent"
	"yomifolio/internal/config"
	"yomifolio/internal/diskwork"
	"yomifolio/internal/metadata"
	"yomifolio/internal/parser"
	"yomifolio/internal/runhandle"
	"yomifolio/internal/storageio"
	"yomifolio/internal/task"
)

// PrefetchTask is the task type of the evidence prefetch. The owner's backfill of the whole
// collection runs on the system scope and of one library on that library; both are retried by hand
// only, since an unattended restart would resume a long stream of requests to outside sites nobody
// asked for. The incremental prefetch of a library is a third identity, so it may run beside a
// backfill instead of waiting days behind it; it resumes after a restart, since it is launched
// unattended to begin with and only finishes the requests it was given.
const PrefetchTask task.Type = "evidence_prefetch"

const (
	prefetchAllLibraries task.Variant = "all_libraries"
	prefetchOneLibrary   task.Variant = "one_library"
	prefetchIncremental  task.Variant = "incremental"
)

// ErrLibraryNotFound means the library to prefetch does not exist.
var ErrLibraryNotFound = errors.New("library not found")

// RestartDefinition declares one retryable prefetch identity.
type RestartDefinition struct {
	Type      task.Type
	Variant   task.Variant
	Resumable bool
}

// Restarts declares the prefetch identities that can be retried; only the incremental prefetch
// resumes automatically.
func Restarts() []RestartDefinition {
	return []RestartDefinition{{Type: PrefetchTask, Variant: prefetchAllLibraries}, {Type: PrefetchTask, Variant: prefetchOneLibrary},
		{Type: PrefetchTask, Variant: prefetchIncremental, Resumable: true}}
}

// Prefetcher prepares evidence packets in background runs: the prefetch of the unprocessed series
// and the research of one series with a custom term. Every backfill run picks the unprocessed series
// that have no packet yet, and every incremental run the requested series without a current one, so
// launching again after a pause, cancel or failure continues where the last run stopped. Requests to
// each source are spaced by one pacer shared by all these runs.
type Prefetcher struct {
	db     *sql.DB
	start  func(context.Context, task.RunSpec, task.Body) (task.Launched, error)
	await  func(context.Context, int64) (task.Run, error)
	config func() config.Config
	open   func(config.Config, string) metadata.Provider
	pacer  *metadata.SourcePacer
	now    func() time.Time
}

// NewPrefetcher binds the prefetch to the process database, run launcher and waiter, configuration
// and catalogue source factory.
func NewPrefetcher(db *sql.DB, start func(context.Context, task.RunSpec, task.Body) (task.Launched, error), await func(context.Context, int64) (task.Run, error), cfg func() config.Config, open func(config.Config, string) metadata.Provider) *Prefetcher {
	return &Prefetcher{db: db, start: start, await: await, config: cfg, open: open, pacer: metadata.NewSourcePacer(), now: time.Now}
}

// WithClock replaces the current time packets are stamped with. It must be set before any run starts.
func (p *Prefetcher) WithClock(now func() time.Time) *Prefetcher { p.now = now; return p }

// PrefetchLaunch reports a launched prefetch. Series is how many series it will search; when it
// is zero nothing needed a packet and no run was started.
type PrefetchLaunch struct {
	RunID  int64 `json:"run_id,omitempty"`
	Series int   `json:"series"`
}

// Start launches the prefetch for one library, or for every library when libraryID is 0.
func (p *Prefetcher) Start(ctx context.Context, libraryID int64, trigger task.Trigger) (PrefetchLaunch, error) {
	spec := task.RunSpec{
		Trigger: trigger, CanCancel: true, CanPause: true,
		StartCode: "task.msg.evidence_prefetch.start", CompleteCode: "task.msg.evidence_prefetch.complete",
		CancelCode: "task.msg.evidence_prefetch.cancelled", FailCode: "task.msg.evidence_prefetch.failed",
	}
	if libraryID == 0 {
		spec.Identity = task.Identity{Type: PrefetchTask, Scope: task.ScopeSystem, Variant: prefetchAllLibraries}
		spec.LibraryIDs = []int64{task.AllLibraries}
		spec.Key = "evidence_prefetch_all"
	} else {
		var name string
		err := p.db.QueryRowContext(ctx, `SELECT name FROM libraries WHERE id = ?`, libraryID).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			return PrefetchLaunch{}, ErrLibraryNotFound
		}
		if err != nil {
			return PrefetchLaunch{}, err
		}
		spec.Identity = task.Identity{Type: PrefetchTask, Scope: task.ScopeLibrary, ScopeID: libraryID, Variant: prefetchOneLibrary}
		spec.LibraryIDs = []int64{libraryID}
		spec.Key = fmt.Sprintf("evidence_prefetch_library_%d", libraryID)
		spec.ScopeName = name
	}
	pending, err := p.pending(ctx, libraryID)
	if err != nil || len(pending) == 0 {
		return PrefetchLaunch{}, err
	}
	spec.Total = len(pending)
	spec.StartParams = map[string]string{"series": strconv.Itoa(len(pending))}
	launched, err := p.start(ctx, spec, func(ctx context.Context, h *runhandle.Handle) (task.Result, error) {
		return p.run(ctx, h, pending)
	})
	if err != nil {
		return PrefetchLaunch{}, err
	}
	return PrefetchLaunch{RunID: launched.Run.ID, Series: len(pending)}, nil
}

// Restart launches a new run of a finished prefetch identity.
func (p *Prefetcher) Restart(ctx context.Context, identity task.Identity, _ map[string]string, trigger task.Trigger) error {
	switch {
	case identity.Type == PrefetchTask && identity.Variant == prefetchIncremental && identity.Scope == task.ScopeLibrary && identity.ScopeID > 0:
		_, err := p.StartIncremental(ctx, identity.ScopeID, trigger)
		return err
	case trigger == task.TriggerResumed:
		return fmt.Errorf("evidence prefetch does not resume automatically: %+v", identity)
	case identity.Type == PrefetchTask && identity.Variant == prefetchAllLibraries && identity.Scope == task.ScopeSystem:
		_, err := p.Start(ctx, 0, trigger)
		return err
	case identity.Type == PrefetchTask && identity.Variant == prefetchOneLibrary && identity.Scope == task.ScopeLibrary && identity.ScopeID > 0:
		_, err := p.Start(ctx, identity.ScopeID, trigger)
		return err
	}
	return fmt.Errorf("unsupported prefetch identity: %+v", identity)
}

type pendingSeries struct {
	id   int64
	name string
}

// pending lists the unprocessed series without a packet, library by library and by name.
func (p *Prefetcher) pending(ctx context.Context, libraryID int64) ([]pendingSeries, error) {
	query := `SELECT s.id, s.name FROM series_identification_status st JOIN series s ON s.id = st.series_id
		WHERE st.status = 'unprocessed' AND NOT EXISTS (SELECT 1 FROM identification_evidence e WHERE e.series_id = s.id)`
	var args []any
	if libraryID != 0 {
		query += ` AND s.library_id = ?`
		args = append(args, libraryID)
	}
	rows, err := p.db.QueryContext(ctx, query+` ORDER BY s.library_id, s.name, s.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pendingSeries
	for rows.Next() {
		var s pendingSeries
		if err := rows.Scan(&s.id, &s.name); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type prefetchOutcome int

const (
	prefetchReady prefetchOutcome = iota
	// prefetchSkipped: the series was deleted, processed or given a packet since the run began.
	prefetchSkipped
	// prefetchUnanswered: no source answered, so the series stays pending for a later run.
	prefetchUnanswered
)

func (p *Prefetcher) run(ctx context.Context, h *runhandle.Handle, series []pendingSeries) (task.Result, error) {
	ready, unanswered, skipped := 0, 0, 0
	result := func() task.Result {
		return task.Result{Params: map[string]string{"ready": strconv.Itoa(ready), "unanswered": strconv.Itoa(unanswered), "total": strconv.Itoa(len(series))}}
	}
	for i, s := range series {
		if err := h.Checkpoint(ctx); err != nil {
			return result(), err
		}
		outcome, err := p.prefetch(ctx, h, s.id)
		if err != nil {
			return result(), err
		}
		switch outcome {
		case prefetchReady:
			ready++
		case prefetchUnanswered:
			unanswered++
			h.ItemFailed(s.name, "no source answered")
		case prefetchSkipped:
			skipped++
		}
		current, total := i+1, len(series)
		h.Report(runhandle.Frame{Current: &current, Total: &total, Item: s.name, Code: "task.msg.evidence_prefetch.progress", Params: map[string]string{"name": s.name},
			Metrics: map[string]int64{"ready_series": int64(ready), "unanswered_series": int64(unanswered), "skipped_series": int64(skipped)}})
	}
	slog.InfoContext(ctx, "Evidence prefetch finished", "ready", ready, "unanswered", unanswered, "skipped", skipped, "total", len(series))
	return result(), nil
}

// prefetch searches one unprocessed series that has no current packet and stores its packet.
// Refreshing a packet searches again the custom terms recorded in it, so a research survives the
// refresh. A series a newly enabled source reopened is searched on the reopened sources only.
// Errors are returned only when the run must stop: cancellation, or a database that cannot be read
// or written.
func (p *Prefetcher) prefetch(ctx context.Context, h *runhandle.Handle, seriesID int64) (prefetchOutcome, error) {
	var status string
	var prior sql.NullString
	var current bool
	err := p.db.QueryRowContext(ctx, `SELECT st.status, e.packet, COALESCE(`+currentPacket+`, 0) FROM series_identification_status st
		LEFT JOIN identification_evidence e ON e.series_id = st.series_id WHERE st.series_id = ?`, packetCutoff(p.now()), seriesID).Scan(&status, &prior, &current)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (status != StatusUnprocessed || current)) {
		return prefetchSkipped, nil
	}
	if err != nil {
		return prefetchSkipped, err
	}
	sources, reopened, err := reopenedSources(ctx, p.db, seriesID)
	if err != nil || (reopened && len(sources) == 0) {
		return prefetchSkipped, err
	}
	var custom []string
	if prior.Valid {
		var stored EvidencePacket
		if err := json.Unmarshal([]byte(prior.String), &stored); err == nil {
			for _, search := range stored.Searches {
				custom = append(custom, search.Query)
			}
		}
	}
	local, found, err := p.localClues(ctx, h, seriesID)
	if err != nil || !found {
		return prefetchSkipped, err
	}
	queries := searchTerms(local.DirectoryName, local.ComicInfo)
	fanout := p.fanout()
	fanout.Only = sources
	search := func(term string) ([]metadata.SourcePage, error) {
		var pages []metadata.SourcePage
		err := fanout.Search(ctx, metadata.SourceQuery{Text: term, Limit: searchLimit}, h.Checkpoint, func(page metadata.SourcePage) {
			pages = append(pages, page)
		})
		return pages, err
	}
	var pages []metadata.SourcePage
	for _, query := range queries {
		found, err := search(query)
		if err != nil {
			return prefetchSkipped, err
		}
		pages = append(pages, found...)
	}
	searches := make([]customSearch, 0, len(custom))
	for _, term := range custom {
		found, err := search(term)
		if err != nil {
			return prefetchSkipped, err
		}
		searches = append(searches, customSearch{term, found})
	}
	return p.save(ctx, seriesID, prior, local, queries, pages, searches)
}

// customSearch is one recorded custom term searched again by a refresh, with its pages.
type customSearch struct {
	term  string
	pages []metadata.SourcePage
}

// fanout searches the catalogue sources of the current configuration, spacing the requests to
// each source by its configured interval.
func (p *Prefetcher) fanout() metadata.SourceFanout {
	return metadata.SourceFanout{Config: p.config(), Open: p.open, Pace: func(ctx context.Context, source string) error {
		return p.pacer.Wait(ctx, source, p.config().Identification.SourceIntervalsMS.For(source))
	}}
}

// localClues reads what the library says about the series, including the first readable ComicInfo
// document of its first books, read as the run's disk work. found is false when the series is gone.
func (p *Prefetcher) localClues(ctx context.Context, h *runhandle.Handle, seriesID int64) (EvidenceLocalClues, bool, error) {
	local, books, found, err := p.libraryClues(ctx, seriesID)
	if err != nil || !found {
		return EvidenceLocalClues{}, false, err
	}
	for _, path := range books {
		var info *parser.ComicInfo
		if err := h.Disk(ctx, diskwork.Work{Kind: storageio.WorkKindMetadataScan, Path: path}, func() error {
			info, _ = bookcontent.ArchiveComicInfo(ctx, path)
			return nil
		}); err != nil {
			return EvidenceLocalClues{}, false, err
		}
		if info != nil {
			local.ComicInfo = comicInfoClues(info)
			break
		}
	}
	return local, true, nil
}

// libraryClues reads the series' directory name and book count, and the names and paths of its
// first books.
func (p *Prefetcher) libraryClues(ctx context.Context, seriesID int64) (EvidenceLocalClues, []string, bool, error) {
	var local EvidenceLocalClues
	err := p.db.QueryRowContext(ctx, `SELECT s.name, (SELECT COUNT(*) FROM books b WHERE b.series_id = s.id) FROM series s WHERE s.id = ?`, seriesID).Scan(&local.DirectoryName, &local.BookCount)
	if errors.Is(err, sql.ErrNoRows) {
		return EvidenceLocalClues{}, nil, false, nil
	}
	if err != nil {
		return EvidenceLocalClues{}, nil, false, err
	}
	rows, err := p.db.QueryContext(ctx, `SELECT path FROM books WHERE series_id = ? ORDER BY volume, sort_number, name, id LIMIT ?`, seriesID, listedFiles)
	if err != nil {
		return EvidenceLocalClues{}, nil, false, err
	}
	defer rows.Close()
	var books []string
	local.FileNames = []string{}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return EvidenceLocalClues{}, nil, false, err
		}
		books = append(books, path)
		local.FileNames = append(local.FileNames, cut(filepath.Base(path), textRunes))
	}
	local.DirectoryName = cut(local.DirectoryName, 2*textRunes)
	return local, books, true, rows.Err()
}

// comicInfoClues keeps the identity fields of a ComicInfo document; writers come before artists.
func comicInfoClues(info *parser.ComicInfo) *EvidenceComicInfo {
	clues := &EvidenceComicInfo{Series: cut(info.Series, textRunes), Year: int(info.Year)}
	for _, field := range []string{info.Writer, info.Penciller} {
		for _, name := range strings.FieldsFunc(field, func(r rune) bool { return strings.ContainsRune(",，、;；/&", r) }) {
			if name = cut(name, nameRunes); name != "" && !slices.Contains(clues.Authors, name) && len(clues.Authors) < listedAuthors {
				clues.Authors = append(clues.Authors, name)
			}
		}
	}
	if clues.Year < 1800 || clues.Year > 2199 {
		clues.Year = 0
	}
	if clues.Series == "" && len(clues.Authors) == 0 && clues.Year == 0 {
		return nil
	}
	return clues
}

// save writes the packet of a series that still exists, dropping the entries a person excluded,
// including any excluded while its sources were being searched, and merging the custom searches
// a refresh repeated. The packet replaces prior, the one the search started from; a packet stored
// in the meantime, by a research or another run, is kept. Storing ends the series' request.
func (p *Prefetcher) save(ctx context.Context, seriesID int64, prior sql.NullString, local EvidenceLocalClues, queries []string, pages []metadata.SourcePage, searches []customSearch) (prefetchOutcome, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return prefetchSkipped, err
	}
	defer tx.Rollback()
	var stored sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT e.packet FROM series s LEFT JOIN identification_evidence e ON e.series_id = s.id WHERE s.id = ?`, seriesID).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && stored != prior) {
		return prefetchSkipped, nil
	}
	if err != nil {
		return prefetchSkipped, err
	}
	excluded, err := excludedIDs(ctx, tx, seriesID)
	if err != nil {
		return prefetchSkipped, err
	}
	fetchedAt := p.now()
	packet, facts, answered := assemble(local, queries, pages, excluded, fetchedAt)
	if !answered {
		return prefetchUnanswered, nil
	}
	for _, search := range searches {
		packet, facts, _, _ = merge(packet, facts, search.term, search.pages, excluded, fetchedAt)
	}
	if err := writeEvidence(ctx, tx, seriesID, &fetchedAt, packet, facts); err != nil {
		return prefetchSkipped, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM identification_prefetch_requests WHERE series_id = ?`, seriesID); err != nil {
		return prefetchSkipped, err
	}
	return prefetchReady, tx.Commit()
}
