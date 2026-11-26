package identification

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/task"
)

// Claim bounds. A batch holds at most MaxClaim series and, packets included, at most claimBudget
// bytes of JSON, so a model with a small context can take a whole batch in. One packet is bounded
// by packetBudget, or by researchedBudget once a research added to it, so a batch always fits at
// least seven prefetched or three researched packets; past the budget the batch is cut short rather
// than any packet being trimmed further.
const (
	MaxClaim      = 20
	LeaseDuration = 30 * time.Minute
	claimBudget   = 30 << 10
)

// Why a claim handed out no series.
const (
	// EmptyAllProcessed: no unprocessed series is left in the claimed libraries.
	EmptyAllProcessed = "all_processed"
	// EmptyPrefetchPending: unprocessed series remain, but their evidence packets are not ready:
	// none yet, or one being refreshed.
	EmptyPrefetchPending = "prefetch_pending"
	// EmptyAllClaimed: every ready series is under a live lease, the caller's own included.
	EmptyAllClaimed = "all_claimed"
)

// ClaimInput narrows a claim to some of the credential's libraries and a smaller batch.
type ClaimInput struct {
	LibraryIDs []int64 `json:"library_ids,omitempty" jsonschema:"libraries to claim from, default every library of this credential"`
	Limit      int     `json:"limit,omitempty" jsonschema:"at most this many series, 1-20, default 20"`
}

// Claim is a batch of series handed to one session under one lease. EmptyReason is set exactly
// when Series is empty.
type Claim struct {
	LeaseID        string          `json:"lease_id,omitempty"`
	LeaseExpiresAt string          `json:"lease_expires_at,omitempty"`
	Series         []ClaimedSeries `json:"series"`
	EmptyReason    string          `json:"empty_reason,omitempty"`
}

// ClaimedSeries is one claimed series with its evidence packet. OpenNotes lists the categories of
// the notes already raised on it and still open.
type ClaimedSeries struct {
	SeriesID  int64          `json:"series_id"`
	LibraryID int64          `json:"library_id"`
	OpenNotes []string       `json:"open_notes,omitempty"`
	Evidence  EvidencePacket `json:"evidence"`
}

// Claim hands out up to limit unprocessed series whose evidence packet is current and that no live
// lease holds, in library and name order, and leases them to the caller for LeaseDuration.
// Owner-reviewed and otherwise processed series are never handed out. A series the claim would
// hand out but for a packet no longer current (older than PacketMaxAge, or predating the source
// that reopened it) is held back until the incremental prefetch, launched after the claim,
// refreshes the packet. Every library needs current metadata_write authority, checked again inside
// the claiming transaction; claims are serialized by that transaction, so two claims never hand
// out the same series.
func (s *Service) Claim(ctx context.Context, subject delegated.Subject, in ClaimInput) (Claim, error) {
	limit := in.Limit
	if limit == 0 {
		limit = MaxClaim
	}
	if limit < 0 || limit > MaxClaim {
		return Claim{}, delegated.ErrInvalid
	}
	libraries, err := s.access.Libraries(ctx, subject, in.LibraryIDs, delegated.MetadataWrite)
	if err != nil {
		return Claim{}, err
	}
	out := Claim{Series: []ClaimedSeries{}}
	if len(libraries) == 0 {
		out.EmptyReason = EmptyAllProcessed
		return out, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Claim{}, err
	}
	defer tx.Rollback()
	for _, library := range libraries {
		if err := delegated.AuthorizeTx(ctx, tx, subject, library, delegated.MetadataWrite); err != nil {
			return Claim{}, err
		}
	}
	now := s.now()
	ready, err := claimable(ctx, tx, libraries, now, limit)
	if err != nil {
		return Claim{}, err
	}
	stale, err := requestRefresh(ctx, tx, libraries, now, limit)
	if err != nil {
		return Claim{}, err
	}
	if len(ready) == 0 {
		if out.EmptyReason, err = emptyReason(ctx, tx, libraries, now); err != nil {
			return Claim{}, err
		}
		if err := tx.Commit(); err != nil {
			return Claim{}, err
		}
		s.refresh(ctx, stale)
		return out, nil
	}
	lease, err := newLeaseID()
	if err != nil {
		return Claim{}, err
	}
	expires := now.Add(LeaseDuration)
	out.LeaseID, out.LeaseExpiresAt = lease, expires.UTC().Format(time.RFC3339)
	base, _ := json.Marshal(out)
	size := len(base)
	for _, candidate := range ready {
		item, err := claimedSeries(ctx, tx, candidate)
		if err != nil {
			return Claim{}, err
		}
		encoded, _ := json.Marshal(item)
		if len(out.Series) > 0 && size+len(encoded)+1 > claimBudget {
			break
		}
		size += len(encoded) + 1
		out.Series = append(out.Series, item)
	}
	for _, item := range out.Series {
		if _, err := tx.ExecContext(ctx, `INSERT INTO identification_leases (series_id, lease_id, user_id, credential_id, claimed_at_ms, expires_at_ms)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(series_id) DO UPDATE SET lease_id = excluded.lease_id, user_id = excluded.user_id,
			credential_id = excluded.credential_id, claimed_at_ms = excluded.claimed_at_ms, expires_at_ms = excluded.expires_at_ms`,
			item.SeriesID, lease, subject.UserID, subject.CredentialID, now.UnixMilli(), expires.UnixMilli()); err != nil {
			return Claim{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Claim{}, err
	}
	s.refresh(ctx, stale)
	return out, nil
}

// requestRefresh asks the incremental prefetch to refresh, in claim order, up to limit packets the
// claim holds back as no longer current, and returns the libraries they belong to.
func requestRefresh(ctx context.Context, tx *sql.Tx, libraries []int64, now time.Time, limit int) ([]int64, error) {
	in, args := libraryArgs(libraries)
	rows, err := tx.QueryContext(ctx, `SELECT s.id, s.library_id FROM series_identification_status st
		JOIN series s ON s.id = st.series_id
		JOIN identification_evidence e ON e.series_id = st.series_id
		LEFT JOIN identification_leases l ON l.series_id = st.series_id
		WHERE st.status = 'unprocessed' AND st.library_id IN (`+in+`) AND (l.series_id IS NULL OR l.expires_at_ms <= ?) AND NOT `+currentPacket+`
		ORDER BY s.library_id, s.name, s.id LIMIT ?`, append(args, now.UnixMilli(), packetCutoff(now), limit)...)
	if err != nil {
		return nil, err
	}
	var series, touched []int64
	for rows.Next() {
		var id, library int64
		if err := rows.Scan(&id, &library); err != nil {
			rows.Close()
			return nil, err
		}
		series = append(series, id)
		if !slices.Contains(touched, library) {
			touched = append(touched, library)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, id := range series {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO identification_prefetch_requests (series_id, requested_at_ms) VALUES (?, ?)`, id, now.UnixMilli()); err != nil {
			return nil, err
		}
	}
	return touched, nil
}

// refresh launches the incremental prefetch of each library a claim asked to refresh packets in.
// A failed launch is logged and leaves the claim as it was; the next claim asks again.
func (s *Service) refresh(ctx context.Context, libraries []int64) {
	for _, library := range libraries {
		if _, err := s.prefetch.StartIncremental(context.Background(), library, task.TriggerChained); err != nil && !errors.Is(err, task.ErrRunAlreadyActive) {
			slog.WarnContext(ctx, "Evidence packet refresh not started", "library_id", library, "error", err)
		}
	}
}

type readySeries struct {
	id, library int64
	packet      string
}

func libraryArgs(libraries []int64) (string, []any) {
	args := make([]any, len(libraries))
	for i, id := range libraries {
		args[i] = id
	}
	return "?" + strings.Repeat(", ?", len(libraries)-1), args
}

// claimable lists the series with a current packet that no live lease holds, in claim order.
func claimable(ctx context.Context, tx *sql.Tx, libraries []int64, now time.Time, limit int) ([]readySeries, error) {
	in, args := libraryArgs(libraries)
	rows, err := tx.QueryContext(ctx, `SELECT s.id, s.library_id, e.packet FROM series_identification_status st
		JOIN series s ON s.id = st.series_id
		JOIN identification_evidence e ON e.series_id = st.series_id
		LEFT JOIN identification_leases l ON l.series_id = st.series_id
		WHERE st.status = 'unprocessed' AND st.library_id IN (`+in+`) AND (l.series_id IS NULL OR l.expires_at_ms <= ?) AND `+currentPacket+`
		ORDER BY s.library_id, s.name, s.id LIMIT ?`, append(args, now.UnixMilli(), packetCutoff(now), limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []readySeries
	for rows.Next() {
		var r readySeries
		if err := rows.Scan(&r.id, &r.library, &r.packet); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// claimedSeries builds the claimed view of one ready series, leaving out the candidates excluded
// since its packet was fetched.
func claimedSeries(ctx context.Context, tx *sql.Tx, ready readySeries) (ClaimedSeries, error) {
	item := ClaimedSeries{SeriesID: ready.id, LibraryID: ready.library}
	if err := json.Unmarshal([]byte(ready.packet), &item.Evidence); err != nil {
		return ClaimedSeries{}, err
	}
	excluded, err := excludedIDs(ctx, tx, ready.id)
	if err != nil {
		return ClaimedSeries{}, err
	}
	item.Evidence.Candidates = slices.DeleteFunc(item.Evidence.Candidates, func(c EvidenceCandidate) bool { return excluded[c.ID] })
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT category FROM identification_escalations
		WHERE series_id = ? AND class = ? AND resolved_at_ms IS NULL ORDER BY category`, ready.id, EscalationNote)
	if err != nil {
		return ClaimedSeries{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var category string
		if err := rows.Scan(&category); err != nil {
			return ClaimedSeries{}, err
		}
		item.OpenNotes = append(item.OpenNotes, category)
	}
	return item, rows.Err()
}

// emptyReason says why no series could be claimed from the libraries. A packet that is not current
// counts as not ready.
func emptyReason(ctx context.Context, tx *sql.Tx, libraries []int64, now time.Time) (string, error) {
	in, args := libraryArgs(libraries)
	var pending, ready int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(NOT ready), 0), COALESCE(SUM(ready), 0) FROM (
			SELECT COALESCE(`+currentPacket+`, 0) AS ready FROM series_identification_status st
			LEFT JOIN identification_evidence e ON e.series_id = st.series_id
			WHERE st.status = 'unprocessed' AND st.library_id IN (`+in+`))`, append([]any{packetCutoff(now)}, args...)...).Scan(&pending, &ready)
	switch {
	case err != nil:
		return "", err
	case pending > 0:
		return EmptyPrefetchPending, nil
	case ready > 0:
		return EmptyAllClaimed, nil
	}
	return EmptyAllProcessed, nil
}

func newLeaseID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
