package enrichment

import (
	"context"
	"database/sql"
	"slices"
	"sort"
	"strings"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/proposal"
)

// collectProposalEvidence gathers field evidence from persisted proposals with
// explicit identity and edition confirmation. Every field's source also supplies a
// source link. A rejection consults no evidence.
func (s *Service) collectProposalEvidence(ctx context.Context, actor delegated.Subject, in Input) (fieldEvidence, error) {
	out := fieldEvidence{sources: map[string][]metadata.FieldEvidence{}}
	if in.Adjudication != nil && in.Adjudication.Mode == "reject" {
		return out, nil
	}
	for _, id := range in.ProposalIDs {
		proposal, err := s.evidence.GetMatchProposal(ctx, actor, in.SeriesID, id)
		if err != nil {
			return fieldEvidence{}, err
		}
		e := proposal.Evidence
		if e == nil || !e.IdentityConfirmed || !e.EditionConfirmed || strings.TrimSpace(e.Summary) == "" {
			return fieldEvidence{}, delegated.ErrInvalid
		}
		if out.reason == "" {
			out.reason = e.Summary
		}
		keys := make([]string, 0, len(e.Fields))
		for f := range e.Fields {
			keys = append(keys, f)
		}
		sort.Strings(keys)
		for _, f := range keys {
			out.sources[f] = append(out.sources[f], e.Fields[f])
			source := e.Fields[f]
			if !slices.ContainsFunc(out.sources["source_links"], func(existing metadata.FieldEvidence) bool {
				return existing.Source == source.Source && existing.Metadata.SourceURL == source.Metadata.SourceURL
			}) {
				out.sources["source_links"] = append(out.sources["source_links"], source)
			}
		}
	}
	return out, nil
}

// proposalDecision confirms evidence through persisted proposals. Ordinary
// supplementation only requires every proposal to stay pending and records no
// provenance; adjudication consumes its one proposal and links provenance to it.
type proposalDecision struct {
	in       Input
	evidence fieldEvidence
	row      database.MetadataReview
}

func (p *proposalDecision) authorize(ctx context.Context, tx *sql.Tx, actor delegated.Subject, library int64) error {
	if p.in.Adjudication == nil {
		return nil
	}
	return authorize(ctx, tx, actor, library, false)
}

func (p *proposalDecision) guard(ctx context.Context, tx *sql.Tx, q *database.Queries, out *Operation) (bool, string, error) {
	if p.in.Adjudication == nil {
		pending, err := pendingEvidence(ctx, q, p.in)
		return pending, "proposal is no longer pending", err
	}
	row, pending, err := adjudicationGuard(ctx, q, p.in, p.evidence.sources)
	if err != nil {
		return false, "", err
	}
	accepted, err := proposal.Accepted(ctx, tx, row)
	if err != nil {
		return false, "", err
	}
	p.row = row
	out.Adjudication = &AdjudicationResult{ProposalID: p.in.ProposalIDs[0], Mode: p.in.Adjudication.Mode, Reason: p.in.Adjudication.Reason, Status: row.Status, Accepted: accepted}
	return pending, "proposal terminal or selected field already consumed", nil
}

func (p *proposalDecision) settle(ctx context.Context, tx *sql.Tx, q *database.Queries, out *Operation) (bool, error) {
	if p.in.Adjudication == nil {
		return false, nil
	}
	if err := recordProvenance(ctx, q, *out, p.row.ID); err != nil {
		return false, err
	}
	status, err := proposal.ConsumeAdjudication(ctx, tx, q, p.row, changedFields(out.Fields), p.in.Adjudication.Mode != "partial")
	if err != nil {
		return false, err
	}
	row, err := q.GetMetadataReview(ctx, p.row.ID)
	if err != nil {
		return false, err
	}
	accepted, err := proposal.Accepted(ctx, tx, row)
	if err != nil {
		return false, err
	}
	out.Adjudication = &AdjudicationResult{ProposalID: row.ID, Mode: p.in.Adjudication.Mode, Reason: p.in.Adjudication.Reason, Status: status, Accepted: accepted}
	return status != "pending", nil
}

func (p *proposalDecision) abandon(out *Operation) {
	if out.Adjudication != nil {
		out.Adjudication.Status = "pending"
	}
}

// pendingEvidence lets ordinary supplementation use confirmed source evidence without
// consuming proposal fields, while still honoring terminal decisions in the actual
// write transaction.
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
