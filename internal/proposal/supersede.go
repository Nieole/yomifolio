package proposal

import (
	"context"
	"slices"

	"yomifolio/internal/database"
)

// StatusSuperseded is the terminal status of a proposal closed because its series was identified.
// It is neither a rejection nor an acceptance: the remaining differences stay unapplied, and fields
// accepted from the proposal before stay accepted.
const StatusSuperseded = "superseded"

// SupersedePending closes every pending proposal of the series as superseded, in the caller's write
// transaction, and returns those it closed in id order. Each closure is the pending CAS, so a
// proposal another decision ended first keeps that decision (ADR 0011).
func SupersedePending(ctx context.Context, q *database.Queries, series int64) ([]int64, error) {
	rows, err := q.ListMetadataReviewsBySeries(ctx, series)
	if err != nil {
		return nil, err
	}
	closed := []int64{}
	for _, row := range rows {
		if !isPending(row.Status) {
			continue
		}
		n, err := q.ResolvePendingMetadataReview(ctx, database.ResolvePendingMetadataReviewParams{ID: row.ID, Status: StatusSuperseded})
		if err != nil {
			return nil, err
		}
		if n == 1 {
			closed = append(closed, row.ID)
		}
	}
	slices.Sort(closed)
	return closed, nil
}
