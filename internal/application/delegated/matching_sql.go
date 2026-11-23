package delegated

import (
	"context"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/proposal"
)

// confirmedProposalStore keeps Queue inside the caller's authorized transaction.
type confirmedProposalStore struct {
	database.Store
	queries *database.Queries
}

func (d confirmedProposalStore) ExecTx(_ context.Context, fn func(proposal.Queries) error) error {
	return fn(d.queries)
}

func (a *MatchingAccess) queue(ctx context.Context, subject Subject, library, series int64, result *metadata.SeriesMetadata) (proposal.QueueResult, bool, error) {
	if err := bounded(result); err != nil {
		return proposal.QueueResult{}, false, err
	}
	tx, err := a.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return proposal.QueueResult{}, false, err
	}
	defer tx.Rollback()
	q := database.New(tx)
	current, err := q.GetSeries(ctx, series)
	if err != nil || current.LibraryID != library {
		return proposal.QueueResult{}, false, ErrDenied
	}
	if err = AuthorizeTx(ctx, tx, subject, current.LibraryID, MetadataWrite); err != nil {
		return proposal.QueueResult{}, false, err
	}
	service := proposal.NewService(confirmedProposalStore{Store: a.Store, queries: q})
	out, err := service.Queue(ctx, database.Series{ID: series}, result, "delegated", result.MatchEvidence.Query, proposal.QueueOptions{RefreshSeries: true, MatchEvidence: result.MatchEvidence})
	if err != nil {
		return proposal.QueueResult{}, false, err
	}
	accepted := false
	if out.Proposal.ID > 0 {
		accepted, err = proposal.Accepted(ctx, tx, out.Proposal)
		if err != nil {
			return proposal.QueueResult{}, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return proposal.QueueResult{}, false, err
	}
	return out, accepted, nil
}
