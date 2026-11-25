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
