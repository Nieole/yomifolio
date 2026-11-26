package identification

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"slices"
	"time"

	"yomifolio/internal/database"
	"yomifolio/internal/proposal"
)

type historicalProposal struct {
	id, series int64
	status     string
	entry      Entry
	title      string
	original   string
	decidedAt  time.Time
}

// DeriveFromHistory turns the proposal history into the initial identification state, once per
// database: an applied proposal, or a pending one the owner already accepted in part, becomes the
// owner's conclusion for its source, and the entry of a rejected proposal becomes an excluded
// candidate. Every pending proposal of a series identified this way closes as superseded, the rest
// of a partly accepted one included; pending proposals of a series that stays unprocessed are left
// alone. Proposals without a source entry (model output, multi-source agent proposals) carry no
// identity and are skipped. Per series and source the most recently applied entry wins, and an
// entry the owner applied is never also excluded.
//
// The derivation and its completion marker commit together, so an interrupted run leaves nothing
// behind and the next start runs it again from the same history.
func (s *Service) DeriveFromHistory(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var done bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identification_history_derivation)`).Scan(&done); err != nil || done {
		return err
	}
	proposals, err := historicalProposals(ctx, tx)
	if err != nil {
		return err
	}
	// Newest decision first: the first applied entry seen per series and source is the winner.
	slices.SortFunc(proposals, func(a, b historicalProposal) int {
		return cmp.Or(b.decidedAt.Compare(a.decidedAt), cmp.Compare(b.id, a.id))
	})
	type key struct {
		series int64
		source string
	}
	chosen := map[key]bool{}
	applied := map[key]map[string]bool{}
	var conclusions, exclusions, superseded int
	for _, p := range proposals {
		k := key{p.series, p.entry.Source}
		if p.status != "applied" {
			continue
		}
		if applied[k] == nil {
			applied[k] = map[string]bool{}
		}
		applied[k][p.entry.ID] = true
		if chosen[k] {
			continue
		}
		chosen[k] = true
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO identification_conclusions
			(series_id, source, entry_id, entry_url, title, original_title, aliases, confirmer, user_id, credential_id, confirmed_at_ms, reason, proposal_id)
			VALUES (?, ?, ?, ?, ?, ?, '[]', ?, 0, '', ?, '', ?)`,
			p.series, p.entry.Source, p.entry.ID, p.entry.URL, p.title, p.original, ConfirmedByOwner, p.decidedAt.UnixMilli(), p.id); err != nil {
			return err
		}
		conclusions++
	}
	// Every series the history identifies has its remaining backlog superseded, the rest of a partly
	// accepted proposal included.
	var identified []int64
	for k := range chosen {
		if !slices.Contains(identified, k.series) {
			identified = append(identified, k.series)
		}
	}
	slices.Sort(identified)
	for _, series := range identified {
		closed, err := proposal.SupersedePending(ctx, database.New(tx), series)
		if err != nil {
			return err
		}
		superseded += len(closed)
	}
	for _, p := range proposals {
		if p.status != "rejected" || applied[key{p.series, p.entry.Source}][p.entry.ID] {
			continue
		}
		result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO identification_exclusions
			(series_id, source, entry_id, entry_url, title, user_id, excluded_at_ms, proposal_id)
			VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
			p.series, p.entry.Source, p.entry.ID, p.entry.URL, p.title, p.decidedAt.UnixMilli(), p.id)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n > 0 {
			exclusions++
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identification_history_derivation (id, completed_at_ms) VALUES (1, ?)`, s.now().UnixMilli()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	slog.InfoContext(ctx, "Derived series identification from proposal history", "conclusions", conclusions, "excluded_candidates", exclusions, "superseded_proposals", superseded)
	return nil
}

func historicalProposals(ctx context.Context, tx *sql.Tx) ([]historicalProposal, error) {
	type row struct {
		historicalProposal
		appliedAt, rejectedAt sql.NullTime
		updatedAt             time.Time
		acceptedMs            sql.NullInt64
	}
	// accepted_ms is when the earliest field still attributed to the proposal was written: a
	// partial review-page apply leaves provenance behind but no applied_at.
	rows, err := tx.QueryContext(ctx, `SELECT r.id, r.series_id, r.provider, r.source_id, r.source_url, lower(r.status), r.raw_payload,
			r.applied_at, r.rejected_at, r.updated_at,
			(SELECT CAST(strftime('%s', MIN(p.updated_at)) AS INTEGER) * 1000 FROM series_metadata_provenance p WHERE p.review_id = r.id)
		FROM metadata_reviews r WHERE lower(r.status) IN ('applied', 'rejected', 'pending')`)
	if err != nil {
		return nil, err
	}
	var candidates []row
	for rows.Next() {
		var r row
		var provider, link, payload string
		var sourceID int64
		if err := rows.Scan(&r.id, &r.series, &provider, &sourceID, &link, &r.status, &payload, &r.appliedAt, &r.rejectedAt, &r.updatedAt, &r.acceptedMs); err != nil {
			rows.Close()
			return nil, err
		}
		// The raw payload is the source result the proposal was built from; a damaged one still
		// leaves the row's own source identity usable.
		var result struct {
			Title, OriginalTitle, Provider string
		}
		_ = json.Unmarshal([]byte(payload), &result)
		name := provider
		if name == "" {
			name = result.Provider
		}
		source, ok := SourceKey(name)
		if !ok {
			continue
		}
		if r.entry, ok = EntryOf(source, sourceID, link); !ok {
			continue
		}
		r.title, r.original = result.Title, result.OriginalTitle
		candidates = append(candidates, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []historicalProposal
	for _, r := range candidates {
		p := r.historicalProposal
		p.decidedAt = r.updatedAt
		switch p.status {
		case "applied":
			if r.appliedAt.Valid {
				p.decidedAt = r.appliedAt.Time
			}
		case "rejected":
			if r.rejectedAt.Valid {
				p.decidedAt = r.rejectedAt.Time
			}
		case "pending":
			// A pending proposal the owner already accepted in part counts as applied; acceptance is
			// the proposal module's own projection, not a guess from matching values.
			accepted, err := proposal.Accepted(ctx, tx, database.MetadataReview{ID: p.id, Status: "pending", AppliedAt: r.appliedAt})
			if err != nil {
				return nil, err
			}
			if !accepted {
				continue
			}
			p.status = "applied"
			switch {
			case r.appliedAt.Valid:
				p.decidedAt = r.appliedAt.Time
			case r.acceptedMs.Valid:
				p.decidedAt = time.UnixMilli(r.acceptedMs.Int64)
			}
		}
		out = append(out, p)
	}
	return out, nil
}
