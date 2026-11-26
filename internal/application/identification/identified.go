package identification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"yomifolio/internal/metadata"
)

// SourceEvidence lists, in source priority order, the stored source snapshot behind each entry the
// series is identified to: the snapshot its evidence packet kept for that candidate; for a
// conclusion derived from history, the source result of the applied proposal it came from; else
// the snapshot the conclusion itself kept when it was confirmed, as the owner's pick from a manual
// search outside the packet does. A conclusion with none of them is left out. It reads in the caller's transaction, so a write that
// relies on the evidence sees the identification it commits against.
func SourceEvidence(ctx context.Context, tx *sql.Tx, seriesID int64) ([]metadata.FieldEvidence, error) {
	list, err := conclusions(ctx, tx, seriesID)
	if err != nil {
		return nil, err
	}
	_, facts, _, err := storedEvidence(ctx, tx, seriesID)
	if err != nil {
		return nil, err
	}
	out := []metadata.FieldEvidence{}
	for _, c := range list {
		id := candidateID(c.Source, c.EntryID)
		fact, found := facts[id]
		if !found && c.ProposalID > 0 {
			if fact, found, err = proposalResult(ctx, tx, c.ProposalID); err != nil {
				return nil, err
			}
		}
		if !found {
			if fact, found, err = conclusionFacts(ctx, tx, seriesID, c.Source); err != nil {
				return nil, err
			}
		}
		if !found {
			continue
		}
		raw, err := json.Marshal(fact)
		if err != nil {
			return nil, err
		}
		var snapshot metadata.SourceFacts
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return nil, err
		}
		out = append(out, metadata.FieldEvidence{CandidateID: id, Source: c.Source, Metadata: snapshot})
	}
	return out, nil
}

// proposalResult reads the source result a proposal was built from. A damaged payload has none.
func proposalResult(ctx context.Context, q queryer, proposalID int64) (metadata.SeriesMetadata, bool, error) {
	var payload string
	err := q.QueryRowContext(ctx, `SELECT raw_payload FROM metadata_reviews WHERE id = ?`, proposalID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return metadata.SeriesMetadata{}, false, nil
	}
	if err != nil {
		return metadata.SeriesMetadata{}, false, err
	}
	var result metadata.SeriesMetadata
	if json.Unmarshal([]byte(payload), &result) != nil {
		return metadata.SeriesMetadata{}, false, nil
	}
	return result, true, nil
}

// conclusionFacts reads the source snapshot a conclusion kept when it was confirmed. A conclusion
// derived from history kept none.
func conclusionFacts(ctx context.Context, q queryer, seriesID int64, source string) (metadata.SeriesMetadata, bool, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT facts FROM identification_conclusions WHERE series_id = ? AND source = ?`, seriesID, source).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return metadata.SeriesMetadata{}, false, nil
	}
	if err != nil {
		return metadata.SeriesMetadata{}, false, err
	}
	var facts metadata.SeriesMetadata
	if raw == "" || raw == "{}" || json.Unmarshal([]byte(raw), &facts) != nil {
		return metadata.SeriesMetadata{}, false, nil
	}
	return facts, true, nil
}
