package delegated

import (
	"context"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/proposal"
)

// confirmedProposalStore fences proposal creation, not metadata application.
// The authority read and current series/lock read share the actual write transaction.
type confirmedProposalStore struct {
	database.Store
	subject Subject
	library int64
	series  int64
}

func (d confirmedProposalStore) ExecTx(ctx context.Context, fn func(proposal.Queries) error) error {
	tx, err := d.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := database.New(tx)
	current, err := q.GetSeries(ctx, d.series)
	if err != nil || current.LibraryID != d.library {
		return ErrDenied
	}
	if err = AuthorizeTx(ctx, tx, d.subject, current.LibraryID, MetadataWrite); err != nil {
		return err
	}
	if err = fn(q); err != nil {
		return err
	}
	return tx.Commit()
}
func (a *MatchingAccess) queue(ctx context.Context, subject Subject, library, series int64, result *metadata.SeriesMetadata) (proposal.QueueResult, error) {
	if err := bounded(result); err != nil {
		return proposal.QueueResult{}, err
	}
	db := confirmedProposalStore{Store: a.Store, subject: subject, library: library, series: series}
	service := proposal.NewService(db)
	return service.Queue(ctx, database.Series{ID: series}, result, "delegated", result.MatchEvidence.Query, proposal.QueueOptions{RefreshSeries: true, MatchEvidence: result.MatchEvidence})
}
