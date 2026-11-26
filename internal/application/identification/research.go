// Research searches one series again with a term the owner or an agent chose and merges what the
// sources return into its evidence packet. The search is a run, so it outlives the request that
// started it; the caller waits for it a bounded time.

package identification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/runhandle"
	"yomifolio/internal/task"
)

// ResearchTask is the task type of a search for one series with a custom term. Each researcher
// (the owner, or one credential) has one task per series: a second search launched while one is
// queued joins it. It has no restart function; the researcher launches it again.
const ResearchTask task.Type = "evidence_research"

// Research outcomes.
const (
	// ResearchCompleted: the search ran and what it found joined the packet.
	ResearchCompleted = "completed"
	// ResearchAlreadySearched: the packet already covers the term, so nothing was searched.
	ResearchAlreadySearched = "already_searched"
	// ResearchRunning: the search has not finished within the wait; its run keeps going.
	ResearchRunning = "running"
	// ResearchFailed: no source answered the term; the packet holds what it held.
	ResearchFailed = "failed"
	// ResearchCancelled: the run was cancelled before it stored anything.
	ResearchCancelled = "cancelled"
)

const (
	defaultResearchWaitMS = 20000
	maxResearchWaitMS     = 30000
)

var (
	// ErrInvalidResearch means the term or the wait is out of bounds. It is a delegated.ErrInvalid,
	// so agent adapters report it as invalid input.
	ErrInvalidResearch = fmt.Errorf("%w: research term or wait out of bounds", delegated.ErrInvalid)
	// ErrOwnerReviewed means an agent asked to work on a series the owner keeps.
	ErrOwnerReviewed = errors.New("series is owner-reviewed")

	errResearchUnavailable = errors.New("research is not assembled")
	errNoSourceAnswered    = errors.New("no source answered")
)

// ResearchInput names the series and the term to search it with.
type ResearchInput struct {
	SeriesID int64  `json:"series_id" jsonschema:"series to search again"`
	Query    string `json:"query" jsonschema:"your own search term, 1-100 characters with a letter or digit, such as a title followed by an author"`
	WaitMS   *int   `json:"wait_ms,omitempty" jsonschema:"milliseconds to wait for the search, 0-30000, default 20000; the search goes on after the wait"`
}

// ResearchResult is the outcome of one research. Added lists, in packet order, the candidates the
// search added; Packet is the series' packet after it, absent while the search is running.
type ResearchResult struct {
	SeriesID int64  `json:"series_id"`
	Query    string `json:"query"`
	Status   string `json:"status"`
	// RunID is the run that searched, or is searching, for the term; 0 when nothing was searched.
	RunID   int64           `json:"run_id,omitempty"`
	Added   []string        `json:"added"`
	Dropped int             `json:"dropped,omitempty"`
	Packet  *EvidencePacket `json:"packet,omitempty"`
}

// researchTerm bounds a custom term: valid text without control or format characters, whitespace
// collapsed, 1-maxTermRunes runes, and at least one letter or digit.
func researchTerm(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", ErrInvalidResearch
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", ErrInvalidResearch
		}
	}
	term := strings.Join(strings.Fields(raw), " ")
	if n := utf8.RuneCountInString(term); n == 0 || n > maxTermRunes || database.NormalizeSearchText(term) == "" {
		return "", ErrInvalidResearch
	}
	return term, nil
}

// researcher is who asked for a research: the owner, or an agent under its delegated authority.
type researcher struct {
	agent *delegated.Subject
	actor int64
}

// Research searches one series again with the agent's own term. It needs current metadata_write
// authority over the series' library and refuses a series the owner keeps.
func (s *Service) Research(ctx context.Context, subject delegated.Subject, in ResearchInput) (ResearchResult, error) {
	return s.research(ctx, researcher{agent: &subject}, in)
}

// ResearchAsOwner searches one series again with the owner's term, whatever its status.
func (s *Service) ResearchAsOwner(ctx context.Context, actor int64, in ResearchInput) (ResearchResult, error) {
	return s.research(ctx, researcher{actor: actor}, in)
}

type researchTarget struct {
	id, library       int64
	name, libraryName string
	status            string
}

// research launches the search and waits for it. A term the packet already covers returns the
// packet without searching, so repeating a call after its run finished reads what that run stored.
func (s *Service) research(ctx context.Context, who researcher, in ResearchInput) (ResearchResult, error) {
	query, err := researchTerm(in.Query)
	if err != nil {
		return ResearchResult{}, err
	}
	wait := defaultResearchWaitMS
	if in.WaitMS != nil {
		wait = *in.WaitMS
	}
	if wait < 0 || wait > maxResearchWaitMS {
		return ResearchResult{}, ErrInvalidResearch
	}
	if s.prefetch == nil {
		return ResearchResult{}, errResearchUnavailable
	}
	target, err := s.researchTarget(ctx, who, in.SeriesID)
	if err != nil {
		return ResearchResult{}, err
	}
	out := ResearchResult{SeriesID: target.id, Query: query, Added: []string{}}
	packet, err := currentEvidence(ctx, s.db, target.id)
	if err != nil {
		return ResearchResult{}, err
	}
	if search, done := packet.searched(query); done {
		return out.settled(ResearchAlreadySearched, search, packet), nil
	}
	launched, err := s.launchResearch(ctx, who, target, query)
	if err != nil {
		return ResearchResult{}, err
	}
	out.Status, out.RunID = ResearchRunning, launched.Run.ID
	// A queued search of the same researcher took the launch in; its own term is the one searched.
	if launched.Coalesced {
		return out, nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(wait)*time.Millisecond)
	defer cancel()
	run, err := s.prefetch.await(waitCtx, launched.Run.ID)
	switch {
	case ctx.Err() != nil:
		return ResearchResult{}, ctx.Err()
	case errors.Is(err, context.DeadlineExceeded):
		return out, nil
	case err != nil:
		return ResearchResult{}, err
	}
	if err := s.authorizeResearch(ctx, who, target.library); err != nil {
		return ResearchResult{}, err
	}
	if packet, err = currentEvidence(ctx, s.db, target.id); err != nil {
		return ResearchResult{}, err
	}
	search, _ := packet.searched(query)
	switch run.Status {
	case task.StatusCompleted:
		return out.settled(ResearchCompleted, search, packet), nil
	case task.StatusCancelled:
		return out.settled(ResearchCancelled, nil, packet), nil
	default:
		return out.settled(ResearchFailed, nil, packet), nil
	}
}

func (r ResearchResult) settled(status string, search *EvidenceSearch, packet *EvidencePacket) ResearchResult {
	r.Status, r.Packet = status, packet
	if search != nil {
		r.Added, r.Dropped = slices.Clone(search.Added), search.Dropped
	}
	return r
}

// researchTarget finds the series and checks the researcher may search it. A series an agent may
// not reach is a denial, never a missing series.
func (s *Service) researchTarget(ctx context.Context, who researcher, seriesID int64) (researchTarget, error) {
	if who.agent == nil {
		if err := requireAdmin(ctx, s.db, who.actor); err != nil {
			return researchTarget{}, err
		}
	}
	target := researchTarget{id: seriesID}
	err := s.db.QueryRowContext(ctx, `SELECT s.library_id, s.name, l.name, st.status FROM series s
		JOIN libraries l ON l.id = s.library_id JOIN series_identification_status st ON st.series_id = s.id WHERE s.id = ?`, seriesID).
		Scan(&target.library, &target.name, &target.libraryName, &target.status)
	switch {
	case errors.Is(err, sql.ErrNoRows) && who.agent != nil:
		return researchTarget{}, delegated.ErrDenied
	case errors.Is(err, sql.ErrNoRows):
		return researchTarget{}, ErrSeriesNotFound
	case err != nil:
		return researchTarget{}, err
	}
	if who.agent == nil {
		return target, nil
	}
	if err := s.access.Authorize(ctx, *who.agent, target.library, delegated.MetadataWrite); err != nil {
		return researchTarget{}, err
	}
	if target.status == StatusOwnerReviewed {
		return researchTarget{}, ErrOwnerReviewed
	}
	return target, nil
}

func (s *Service) authorizeResearch(ctx context.Context, who researcher, library int64) error {
	if who.agent == nil {
		return requireAdmin(ctx, s.db, who.actor)
	}
	return s.access.Authorize(ctx, *who.agent, library, delegated.MetadataWrite)
}

// launchResearch starts the research run. An agent's run carries its credential, rechecks its
// authority at every source access and writes only while the authority still holds.
func (s *Service) launchResearch(ctx context.Context, who researcher, target researchTarget, query string) (task.Launched, error) {
	series := strconv.FormatInt(target.id, 10)
	spec := task.RunSpec{
		Identity:   task.Identity{Type: ResearchTask, Scope: task.ScopeLibrary, ScopeID: target.library, Variant: task.Variant("series:" + series)},
		LibraryIDs: []int64{target.library}, Trigger: task.TriggerManual, Key: "evidence_research_series_" + series, ScopeName: target.libraryName,
		CanCancel: true, Args: map[string]string{"series_id": series, "query": query},
		StartCode: "task.msg.evidence_research.start", StartParams: map[string]string{"series": target.name, "query": query},
		CompleteCode: "task.msg.evidence_research.complete", CancelCode: "task.msg.evidence_research.cancelled", FailCode: "task.msg.evidence_research.failed",
	}
	fence := func(ctx context.Context, tx *sql.Tx) error { return requireAdmin(ctx, tx, who.actor) }
	if who.agent != nil {
		subject := *who.agent
		fence = func(ctx context.Context, tx *sql.Tx) error {
			if err := delegated.AuthorizeTx(ctx, tx, subject, target.library, delegated.MetadataWrite); err != nil {
				return err
			}
			var status string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM series_identification_status WHERE series_id = ?`, target.id).Scan(&status); err != nil {
				return err
			}
			if status == StatusOwnerReviewed {
				return ErrOwnerReviewed
			}
			return nil
		}
	}
	body := func(ctx context.Context, h *runhandle.Handle) (task.Result, error) {
		return s.prefetch.research(ctx, h, target, query, fence)
	}
	if who.agent != nil {
		var err error
		if spec, body, err = s.access.PrepareRun(ctx, *who.agent, target.library, delegated.MetadataWrite, spec, body); err != nil {
			return task.Launched{}, err
		}
	}
	return s.prefetch.start(ctx, spec, body)
}

// research searches the series with the term and merges what the sources return into its packet.
// A series without a packet is first searched with its derived terms, as the prefetch would, so
// the packet it gets is complete. fence runs inside the write transaction.
func (p *Prefetcher) research(ctx context.Context, h *runhandle.Handle, target researchTarget, query string, fence func(context.Context, *sql.Tx) error) (task.Result, error) {
	if err := h.Checkpoint(ctx); err != nil {
		return task.Result{}, err
	}
	params := map[string]string{"series": target.name, "query": query, "added": "0"}
	stored, _, found, err := storedEvidence(ctx, p.db, target.id)
	if err != nil {
		return task.Result{}, err
	}
	var local EvidenceLocalClues
	var derived []string
	if found {
		if _, done := stored.searched(query); done {
			return task.Result{Params: params}, nil
		}
		local = stored.Local
	} else {
		if local, found, err = p.localClues(ctx, h, target.id); err != nil {
			return task.Result{}, err
		}
		if !found {
			return task.Result{}, ErrSeriesNotFound
		}
		derived = searchTerms(local.DirectoryName, local.ComicInfo)
	}
	terms := derived
	if !slices.ContainsFunc(derived, func(term string) bool {
		return database.NormalizeSearchText(term) == database.NormalizeSearchText(query)
	}) {
		terms = append(slices.Clone(derived), query)
	}
	fanout := p.fanout()
	total := len(terms) * len(metadata.CatalogueSources(fanout.Config))
	var derivedPages, pages []metadata.SourcePage
	for i, term := range terms {
		visit := func(page metadata.SourcePage) {
			if i < len(derived) {
				derivedPages = append(derivedPages, page)
			} else {
				pages = append(pages, page)
			}
			h.Advance(len(derivedPages)+len(pages), total, "", nil)
		}
		if err := fanout.Search(ctx, metadata.SourceQuery{Text: term, Limit: searchLimit}, h.Checkpoint, visit); err != nil {
			return task.Result{}, err
		}
	}
	added, err := p.saveResearch(ctx, target.id, query, local, derived, derivedPages, pages, fence)
	params["added"] = strconv.Itoa(added)
	return task.Result{Params: params}, err
}

// saveResearch merges the term's pages into the series' packet in one transaction. A series that
// still has no packet gets one built from the derived terms' pages first; that packet is stored even
// when no source answered the custom term, which still fails the run.
func (p *Prefetcher) saveResearch(ctx context.Context, seriesID int64, query string, local EvidenceLocalClues, derived []string, derivedPages, pages []metadata.SourcePage, fence func(context.Context, *sql.Tx) error) (int, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := fence(ctx, tx); err != nil {
		return 0, err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM series WHERE id = ?)`, seriesID).Scan(&exists); err != nil {
		return 0, err
	}
	if !exists {
		return 0, ErrSeriesNotFound
	}
	excluded, err := excludedIDs(ctx, tx, seriesID)
	if err != nil {
		return 0, err
	}
	stored, facts, found, err := storedEvidence(ctx, tx, seriesID)
	if err != nil {
		return 0, err
	}
	now := p.now()
	if found {
		merged, mergedFacts, search, answered := merge(stored, facts, query, pages, excluded, now)
		if !answered {
			// A research of the same term may have stored it while this one searched.
			if _, done := stored.searched(query); done {
				return 0, nil
			}
			return 0, errNoSourceAnswered
		}
		return len(search.Added), commitEvidence(ctx, tx, seriesID, nil, merged, mergedFacts)
	}
	base, baseFacts, baseAnswered := assemble(local, derived, derivedPages, excluded, now)
	merged, mergedFacts, search, answered := merge(base, baseFacts, query, pages, excluded, now)
	switch {
	case answered:
		return len(search.Added), commitEvidence(ctx, tx, seriesID, &now, merged, mergedFacts)
	case !baseAnswered:
		return 0, errNoSourceAnswered
	}
	if err := commitEvidence(ctx, tx, seriesID, &now, base, baseFacts); err != nil {
		return 0, err
	}
	// A term that is one of the derived terms was searched with them.
	if _, done := base.searched(query); done {
		return 0, nil
	}
	return 0, errNoSourceAnswered
}

func commitEvidence(ctx context.Context, tx *sql.Tx, seriesID int64, fetchedAt *time.Time, packet EvidencePacket, facts map[string]metadata.SeriesMetadata) error {
	if err := writeEvidence(ctx, tx, seriesID, fetchedAt, packet, facts); err != nil {
		return err
	}
	return tx.Commit()
}

// searched reports whether the packet already covers the term: one of its derived queries or a
// recorded search, compared after database.NormalizeSearchText. The recorded search is returned
// when the term matches one.
func (p *EvidencePacket) searched(query string) (*EvidenceSearch, bool) {
	if p == nil {
		return nil, false
	}
	key := database.NormalizeSearchText(query)
	for i := range p.Searches {
		if database.NormalizeSearchText(p.Searches[i].Query) == key {
			return &p.Searches[i], true
		}
	}
	return nil, slices.ContainsFunc(p.Queries, func(q string) bool { return database.NormalizeSearchText(q) == key })
}

// merge adds what one custom search found to a packet. Per source it adds at most
// candidatesPerSource entries the packet does not hold and nobody excluded, ranked as assemble
// ranks them, with signals against the packet's own local clues. What the packet held stays as it
// was; a source that answered becomes available. It reports false, changing nothing, when no source
// answered. A term the packet already covers is never merged.
func merge(packet EvidencePacket, facts map[string]metadata.SeriesMetadata, query string, pages []metadata.SourcePage, excluded map[string]bool, at time.Time) (EvidencePacket, map[string]metadata.SeriesMetadata, EvidenceSearch, bool) {
	if _, done := packet.searched(query); done {
		return packet, facts, EvidenceSearch{}, false
	}
	held := map[string]bool{}
	for _, c := range packet.Candidates {
		held[c.ID] = true
	}
	for id := range facts {
		held[id] = true
	}
	titles, authors := localTitleKeys(packet.Local), localAuthorKeys(packet.Local)
	sources := slices.Clone(packet.Sources)
	found := map[string][]EvidenceCandidate{}
	newFacts := map[string]metadata.SeriesMetadata{}
	answered := false
	for _, page := range pages {
		at := slices.IndexFunc(sources, func(s EvidenceSource) bool { return s.Source == page.Source })
		if at < 0 {
			sources = append(sources, EvidenceSource{Source: page.Source, Status: SourceUnavailable})
			at = len(sources) - 1
		}
		if page.Status != metadata.SourceAvailable {
			continue
		}
		answered = true
		sources[at].Status = SourceAvailable
		for _, result := range page.Results {
			entry, ok := EntryOf(page.Source, int64(result.SourceID), result.SourceURL)
			if !ok {
				continue
			}
			id := candidateID(page.Source, entry.ID)
			if _, dup := newFacts[id]; dup || held[id] || excluded[id] {
				continue
			}
			newFacts[id] = result
			found[page.Source] = append(found[page.Source], candidate(id, entry, result, titles, authors, packet.Local.BookCount))
		}
	}
	if !answered {
		return packet, facts, EvidenceSearch{}, false
	}
	search := EvidenceSearch{Query: query, SearchedAt: stamp(at.UnixMilli()), Added: []string{}}
	candidates := make([]EvidenceCandidate, 0, len(packet.Candidates))
	for _, source := range sources {
		for _, c := range packet.Candidates {
			if c.Source == source.Source {
				candidates = append(candidates, c)
			}
		}
		for _, c := range rankHits(found[source.Source]) {
			candidates = append(candidates, c)
			search.Added = append(search.Added, c.ID)
		}
	}
	for _, c := range packet.Candidates {
		if !slices.ContainsFunc(sources, func(s EvidenceSource) bool { return s.Source == c.Source }) {
			candidates = append(candidates, c)
		}
	}
	packet.Sources, packet.Candidates = sources, candidates
	packet.Searches = append(slices.Clone(packet.Searches), search)
	if len(packet.Searches) > listedSearches {
		packet.Searches = packet.Searches[len(packet.Searches)-listedSearches:]
	}
	packet = fitResearch(packet)
	search = packet.Searches[len(packet.Searches)-1]
	merged := make(map[string]metadata.SeriesMetadata, len(facts)+len(search.Added))
	for id, fact := range facts {
		merged[id] = fact
	}
	for _, id := range search.Added {
		merged[id] = newFacts[id]
	}
	return packet, merged, search, true
}

// fitResearch keeps a packet within researchedBudget without touching what it held before its last
// search: it sheds the summaries of the candidates that search added, last first, then those
// candidates, lowest rank of the last source first, and finally the oldest recorded searches.
func fitResearch(packet EvidencePacket) EvidencePacket {
	over := func() bool {
		raw, _ := json.Marshal(packet)
		return len(raw) > researchedBudget
	}
	last := func() *EvidenceSearch { return &packet.Searches[len(packet.Searches)-1] }
	added := func(id string) bool { return slices.Contains(last().Added, id) }
	for i := len(packet.Candidates) - 1; i >= 0 && over(); i-- {
		if added(packet.Candidates[i].ID) {
			packet.Candidates[i].Summary = ""
		}
	}
	for len(last().Added) > 0 && over() {
		rank := map[string]int{}
		worst, worstRank := -1, -1
		for i, c := range packet.Candidates {
			if !added(c.ID) {
				continue
			}
			r := rank[c.Source]
			rank[c.Source]++
			if r >= worstRank {
				worst, worstRank = i, r
			}
		}
		id := packet.Candidates[worst].ID
		packet.Candidates = slices.Delete(packet.Candidates, worst, worst+1)
		search := last()
		search.Added = slices.DeleteFunc(slices.Clone(search.Added), func(a string) bool { return a == id })
		search.Dropped++
	}
	for len(packet.Searches) > 1 && over() {
		packet.Searches = slices.Delete(packet.Searches, 0, 1)
	}
	return packet
}

// currentEvidence reads the series' packet without the candidates excluded since it was fetched.
func currentEvidence(ctx context.Context, q queryer, seriesID int64) (*EvidencePacket, error) {
	excluded, err := exclusions(ctx, q, seriesID)
	if err != nil {
		return nil, err
	}
	return readEvidence(ctx, q, seriesID, excluded)
}
