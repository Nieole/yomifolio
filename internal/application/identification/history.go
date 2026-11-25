package identification

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"slices"
	"time"
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
// database: an applied proposal becomes the owner's conclusion for its source, and the entry of a
// rejected proposal becomes an excluded candidate. Pending proposals are left alone. Proposals
// without a source entry (model output, multi-source agent proposals) carry no identity and are
// skipped. Per series and source the most recently applied entry wins, and an entry the owner
// applied is never also excluded.
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
	var conclusions, exclusions int
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
	slog.InfoContext(ctx, "Derived series identification from proposal history", "conclusions", conclusions, "excluded_candidates", exclusions)
	return nil
}

func historicalProposals(ctx context.Context, tx *sql.Tx) ([]historicalProposal, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, series_id, provider, source_id, source_url, status, raw_payload, applied_at, rejected_at, updated_at
		FROM metadata_reviews WHERE status IN ('applied', 'rejected')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []historicalProposal
	for rows.Next() {
		var p historicalProposal
		var provider, link, payload string
		var sourceID int64
		var appliedAt, rejectedAt sql.NullTime
		var updatedAt time.Time
		if err := rows.Scan(&p.id, &p.series, &provider, &sourceID, &link, &p.status, &payload, &appliedAt, &rejectedAt, &updatedAt); err != nil {
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
		if p.entry, ok = EntryOf(source, sourceID, link); !ok {
			continue
		}
		p.title, p.original = result.Title, result.OriginalTitle
		p.decidedAt = updatedAt
		if p.status == "applied" && appliedAt.Valid {
			p.decidedAt = appliedAt.Time
		}
		if p.status == "rejected" && rejectedAt.Valid {
			p.decidedAt = rejectedAt.Time
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
