// The incremental prefetch keeps evidence current without the owner: it prepares packets for the
// series a scan created and refreshes the packets a claim found no longer current. What it has to
// do lives in requests, so a launch that joins a queued run, and a run resumed after a restart,
// lose nothing.

package identification

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"yomifolio/internal/metadata"
	"yomifolio/internal/runhandle"
	"yomifolio/internal/task"
)

// PacketMaxAge is how long a packet stays current after it was fetched. A claim does not hand out
// a series whose packet is older; it asks the incremental prefetch to refresh it instead.
const PacketMaxAge = 30 * 24 * time.Hour

// currentPacket is the SQL condition, on the evidence row aliased e, that the packet is current at
// the bound cutoff (see packetCutoff): fetched since the cutoff, and after every source enabled
// since the series' not-found verdict, so the packet of a reopened series covers the new source.
const currentPacket = `(e.fetched_at_ms >= ? AND NOT EXISTS (SELECT 1 FROM identification_not_found n
	JOIN identification_enabled_sources x ON x.enabled_at_ms > n.concluded_at_ms AND x.enabled_at_ms > e.fetched_at_ms
	WHERE n.series_id = e.series_id))`

// packetCutoff is the fetch time a packet must reach to be current at now.
func packetCutoff(now time.Time) int64 { return now.Add(-PacketMaxAge).UnixMilli() }

// Request asks the incremental prefetch to prepare packets for these series. Series that no
// longer exist are left out; a series already requested stays requested once.
func (p *Prefetcher) Request(ctx context.Context, seriesIDs []int64) error {
	if len(seriesIDs) == 0 {
		return nil
	}
	in, args := libraryArgs(seriesIDs)
	_, err := p.db.ExecContext(ctx, `INSERT OR IGNORE INTO identification_prefetch_requests (series_id, requested_at_ms)
		SELECT id, ? FROM series WHERE id IN (`+in+`)`, append([]any{p.now().UnixMilli()}, args...)...)
	return err
}

// ChainIncremental launches the incremental prefetch a finished scan of the library asks for. A
// failed launch is logged on the scan's context and never reaches the scan.
func (p *Prefetcher) ChainIncremental(ctx context.Context, libraryID int64) {
	if _, err := p.StartIncremental(context.Background(), libraryID, task.TriggerChained); err != nil && !errors.Is(err, task.ErrRunAlreadyActive) {
		slog.WarnContext(ctx, "Incremental evidence prefetch not started", "library_id", libraryID, "error", err)
	}
}

// StartIncremental launches the incremental prefetch of one library when any of its requested
// series still needs a packet. The run reads its series when it starts, not at launch.
func (p *Prefetcher) StartIncremental(ctx context.Context, libraryID int64, trigger task.Trigger) (PrefetchLaunch, error) {
	var name string
	err := p.db.QueryRowContext(ctx, `SELECT name FROM libraries WHERE id = ?`, libraryID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return PrefetchLaunch{}, ErrLibraryNotFound
	}
	if err != nil {
		return PrefetchLaunch{}, err
	}
	requested, err := p.requested(ctx, libraryID)
	if err != nil || len(requested) == 0 {
		return PrefetchLaunch{}, err
	}
	spec := task.RunSpec{
		Identity: task.Identity{Type: PrefetchTask, Scope: task.ScopeLibrary, ScopeID: libraryID, Variant: prefetchIncremental},
		Trigger:  trigger, LibraryIDs: []int64{libraryID}, Key: fmt.Sprintf("evidence_prefetch_incremental_%d", libraryID), ScopeName: name,
		CanCancel: true, CanPause: true, Total: len(requested), StartParams: map[string]string{"series": strconv.Itoa(len(requested))},
		StartCode: "task.msg.evidence_prefetch.start", CompleteCode: "task.msg.evidence_prefetch.complete",
		CancelCode: "task.msg.evidence_prefetch.cancelled", FailCode: "task.msg.evidence_prefetch.failed",
	}
	launched, err := p.start(ctx, spec, func(ctx context.Context, h *runhandle.Handle) (task.Result, error) {
		series, err := p.requested(ctx, libraryID)
		if err != nil {
			return task.Result{}, err
		}
		return p.run(ctx, h, series)
	})
	if err != nil {
		return PrefetchLaunch{}, err
	}
	return PrefetchLaunch{RunID: launched.Run.ID, Series: len(requested)}, nil
}

// requested lists, by name, the requested series of the library that still need a packet: still
// unprocessed, with no current packet. It first drops the requests that no longer need one.
func (p *Prefetcher) requested(ctx context.Context, libraryID int64) ([]prefetchTarget, error) {
	cutoff := packetCutoff(p.now())
	const needs = `st.status = 'unprocessed' AND (e.series_id IS NULL OR NOT ` + currentPacket + `)`
	const from = ` FROM identification_prefetch_requests r JOIN series s ON s.id = r.series_id
		JOIN series_identification_status st ON st.series_id = s.id
		LEFT JOIN identification_evidence e ON e.series_id = s.id WHERE s.library_id = ?`
	if _, err := p.db.ExecContext(ctx, `DELETE FROM identification_prefetch_requests WHERE series_id IN (SELECT r.series_id`+from+` AND NOT (`+needs+`))`,
		libraryID, cutoff); err != nil {
		return nil, err
	}
	rows, err := p.db.QueryContext(ctx, `SELECT s.id, s.name`+from+` AND `+needs+` ORDER BY s.name, s.id`, libraryID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []prefetchTarget
	for rows.Next() {
		var s prefetchTarget
		if err := rows.Scan(&s.id, &s.name); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SyncSources records which catalogue sources the current configuration enables. A source seen
// enabled for the first time is stamped with the current time, or with 0 while nothing is recorded
// yet, so the sources enabled at upgrade count as enabled all along. A disabled source is
// forgotten, so enabling it again counts as new.
func (p *Prefetcher) SyncSources(ctx context.Context) error {
	enabled := metadata.CatalogueSources(p.config())
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var recorded int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM identification_enabled_sources`).Scan(&recorded); err != nil {
		return err
	}
	at := p.now().UnixMilli()
	if recorded == 0 {
		at = 0
	}
	in, args := placeholders(enabled)
	if _, err := tx.ExecContext(ctx, `DELETE FROM identification_enabled_sources WHERE source NOT IN (`+in+`)`, args...); err != nil {
		return err
	}
	for _, source := range enabled {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO identification_enabled_sources (source, enabled_at_ms) VALUES (?, ?)`, source, at); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// reopenedSources lists the sources a not-found verdict of the series is reopened for: enabled
// since the verdict and not searched by it. reopened is false when the series has no such verdict.
func reopenedSources(ctx context.Context, q queryer, seriesID int64) (sources []string, reopened bool, err error) {
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identification_not_found WHERE series_id = ?)`, seriesID).Scan(&reopened); err != nil || !reopened {
		return nil, false, err
	}
	rows, err := q.QueryContext(ctx, `SELECT x.source FROM identification_not_found n
		JOIN identification_enabled_sources x ON x.enabled_at_ms > n.concluded_at_ms
		WHERE n.series_id = ? AND x.source NOT IN (SELECT value FROM json_each(n.searched_sources)) ORDER BY x.source`, seriesID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err != nil {
			return nil, false, err
		}
		sources = append(sources, source)
	}
	return sources, true, rows.Err()
}

// recordNotFound records a not-found verdict on the series. A verdict on a series a new source
// reopened replaces the earlier one and keeps the sources that one searched, so it covers every
// source searched so far.
func recordNotFound(ctx context.Context, tx *sql.Tx, seriesID int64, searched []string, confirmer string, userID int64, credentialID string, at int64, reason string) error {
	var earlier string
	err := tx.QueryRowContext(ctx, `SELECT searched_sources FROM identification_not_found WHERE series_id = ?`, seriesID).Scan(&earlier)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var covered []string
	_ = json.Unmarshal([]byte(earlier), &covered)
	for _, source := range searched {
		if !slices.Contains(covered, source) {
			covered = append(covered, source)
		}
	}
	slices.SortFunc(covered, func(a, b string) int { return cmp.Compare(defaultRank(a), defaultRank(b)) })
	raw, _ := json.Marshal(append([]string{}, covered...))
	_, err = tx.ExecContext(ctx, `INSERT INTO identification_not_found (series_id, searched_sources, confirmer, user_id, credential_id, concluded_at_ms, reason)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(series_id) DO UPDATE SET searched_sources = excluded.searched_sources, confirmer = excluded.confirmer,
		user_id = excluded.user_id, credential_id = excluded.credential_id, concluded_at_ms = excluded.concluded_at_ms, reason = excluded.reason`,
		seriesID, string(raw), confirmer, userID, credentialID, at, reason)
	return err
}

func placeholders(values []string) (string, []any) {
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}
	if len(args) == 0 {
		return "NULL", nil
	}
	in := "?"
	for range args[1:] {
		in += ", ?"
	}
	return in, args
}
