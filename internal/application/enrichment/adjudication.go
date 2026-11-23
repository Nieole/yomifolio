package enrichment

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/proposal"
	"yomifolio/internal/seriesmeta"
)

// Adjudication explicitly opts into proposal consumption, unlike ordinary enrichment.
// Legacy proposals without confirmed identity/edition evidence may only be rejected.
type Adjudication struct {
	Mode   string `json:"mode"`
	Reason string `json:"reason"`
}
type AdjudicationResult struct {
	Accepted   bool   `json:"accepted"`
	ProposalID int64  `json:"proposal_id"`
	Mode       string `json:"mode"`
	Reason     string `json:"reason"`
	Status     string `json:"status"`
}

func validAdjudication(in Input) bool {
	a := in.Adjudication
	if a == nil {
		return len(in.Fields) > 0
	}
	for _, f := range in.Fields {
		if f == "cover" || f == "source_links" {
			return false
		}
	}
	if len(in.ProposalIDs) != 1 || in.ProposalIDs[0] <= 0 || in.Cover != nil || strings.TrimSpace(a.Reason) == "" || len(a.Reason) > 1000 {
		return false
	}
	switch a.Mode {
	case "reject":
		return len(in.Fields) == 0 && len(in.AuthorCorrections) == 0
	case "partial":
		return len(in.Fields) > 0
	case "supplement_and_close":
		return true
	default:
		return false
	}
}
func adjudicationGuard(ctx context.Context, q *database.Queries, in Input, evidence map[string][]metadata.FieldEvidence) (database.MetadataReview, bool, error) {
	row, ok, err := proposal.AdjudicationPending(ctx, q, in.ProposalIDs[0], in.SeriesID, in.Fields)
	if err == sql.ErrNoRows {
		return row, false, delegated.ErrDenied
	}
	if err != nil || !ok || in.Adjudication.Mode == "reject" {
		return row, ok, err
	}
	var raw metadata.SeriesMetadata
	if json.Unmarshal([]byte(row.RawPayload), &raw) != nil || raw.MatchEvidence == nil || !raw.MatchEvidence.IdentityConfirmed || !raw.MatchEvidence.EditionConfirmed {
		return row, false, nil
	}
	for _, f := range in.Fields {
		saved, exists := raw.MatchEvidence.Fields[f]
		if !exists {
			return row, false, nil
		}
		b, _ := json.Marshal(evidence[f])
		expected, _ := json.Marshal([]metadata.FieldEvidence{saved})
		if string(b) != string(expected) {
			return row, false, nil
		}
	}
	return row, true, nil
}
func finishAdjudication(ctx context.Context, tx *sql.Tx, q *database.Queries, row database.MetadataReview, in Input, out *Operation) error {
	for _, f := range out.Fields {
		if !f.Changed {
			continue
		}
		stored, err := json.Marshal(value(out.After, f.Field))
		if err != nil {
			return err
		}
		text := string(stored)
		if v, ok := value(out.After, f.Field).(*string); ok && v != nil {
			text = *v
		}
		if err = seriesmeta.RecordEnrichmentProvenance(ctx, q, row.SeriesID, row.ID, f.Field, text, f.Sources[0]); err != nil {
			return err
		}
	}
	status, err := proposal.ConsumeAdjudication(ctx, tx, q, row, changedFields(out.Fields), in.Adjudication.Mode != "partial")
	if err != nil {
		return err
	}
	row, err = q.GetMetadataReview(ctx, row.ID)
	if err != nil {
		return err
	}
	accepted, err := proposal.Accepted(ctx, tx, row)
	if err != nil {
		return err
	}
	out.Adjudication = &AdjudicationResult{ProposalID: row.ID, Mode: in.Adjudication.Mode, Reason: in.Adjudication.Reason, Status: status, Accepted: accepted}
	return nil
}

// Ordinary supplementation uses confirmed source evidence, not proposal field
// replacement. It must still honor terminal decisions in the actual transaction.
func pendingEvidence(ctx context.Context, q *database.Queries, in Input) (bool, error) {
	for _, id := range in.ProposalIDs {
		_, ok, err := proposal.AdjudicationPending(ctx, q, id, in.SeriesID, nil)
		if err == sql.ErrNoRows {
			return false, delegated.ErrDenied
		}
		if err != nil || !ok {
			return ok, err
		}
	}
	return true, nil
}
