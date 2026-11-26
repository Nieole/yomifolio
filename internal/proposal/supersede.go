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

// Supersede closes one pending proposal as superseded, in the caller's write transaction. It is the
// pending CAS, so a proposal another decision ended first keeps that decision (ADR 0011); it reports
// whether this call closed it.
func Supersede(ctx context.Context, q *database.Queries, id int64) (bool, error) {
	n, err := q.ResolvePendingMetadataReview(ctx, database.ResolvePendingMetadataReviewParams{ID: id, Status: StatusSuperseded})
	return n == 1, err
}

// SupersedePending supersedes every pending proposal of the series and returns those it closed, in
// id order.
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
		ok, err := Supersede(ctx, q, row.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			closed = append(closed, row.ID)
		}
	}
	slices.Sort(closed)
	return closed, nil
}
