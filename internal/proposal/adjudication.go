package proposal

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"yomifolio/internal/database"
)

// Accepted projects durable acceptance facts in the caller's authorized transaction.
// Manual partial Apply records provenance, not applied_at; protected/no-op writes
// and values merely matching a proposal are not evidence of acceptance.
func Accepted(ctx context.Context, tx *sql.Tx, row database.MetadataReview) (bool, error) {
	if row.AppliedAt.Valid || row.Status == "applied" || row.Status == "supplemented_closed" {
		return true, nil
	}
	var accepted bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM series_metadata_provenance WHERE review_id=?)`, row.ID).Scan(&accepted)
	return accepted, err
}

// AdjudicationPending checks the actual remaining fields in the caller's write
// transaction. False is a public conflict, never permission to reuse consumed evidence.
func AdjudicationPending(ctx context.Context, q *database.Queries, id, series int64, selected []string) (database.MetadataReview, bool, error) {
	row, err := q.GetMetadataReview(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, sql.ErrNoRows
	}
	if err != nil {
		return row, false, err
	}
	if row.SeriesID != series {
		return row, false, sql.ErrNoRows
	}
	if !isPending(row.Status) {
		return row, false, nil
	}
	fields, err := q.ListMetadataReviewFieldsByReviews(ctx, []int64{id})
	if err != nil {
		return row, false, err
	}
	for _, name := range selected {
		if !slices.ContainsFunc(fields, func(f database.MetadataReviewField) bool { return f.FieldName == name }) {
			return row, false, nil
		}
	}
	return row, true, nil
}

// ConsumeAdjudication consumes only actual writes and advances the pending CAS in
// the same transaction. applied_at also records a successful partial supplement;
// closure never clears it or restores content. Ordinary manual Apply is unchanged.
func ConsumeAdjudication(ctx context.Context, tx *sql.Tx, q *database.Queries, row database.MetadataReview, changed []string, closeRemaining bool) (string, error) {
	for _, name := range changed {
		if err := q.DeleteMetadataReviewField(ctx, database.DeleteMetadataReviewFieldParams{ReviewID: row.ID, FieldName: name}); err != nil {
			return "", err
		}
	}
	if len(changed) > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE metadata_reviews SET applied_at=COALESCE(applied_at,CURRENT_TIMESTAMP),updated_at=CURRENT_TIMESTAMP WHERE id=? AND lower(status)='pending'`, row.ID); err != nil {
			return "", err
		}
	}
	status := "pending"
	remaining, err := q.ListMetadataReviewFieldsByReviews(ctx, []int64{row.ID})
	if err != nil {
		return "", err
	}
	if !closeRemaining && len(remaining) == 0 && len(changed) > 0 {
		n, err := q.ResolvePendingMetadataReview(ctx, database.ResolvePendingMetadataReviewParams{ID: row.ID, Status: "applied"})
		if err != nil {
			return "", err
		}
		if n != 1 {
			return "", errPreempted
		}
		return "applied", nil
	}
	if closeRemaining {
		status = "rejected"
		accepted, err := Accepted(ctx, tx, row)
		if err != nil {
			return "", err
		}
		if len(changed) > 0 || accepted {
			status = "supplemented_closed"
		}

		n, err := q.ResolvePendingMetadataReview(ctx, database.ResolvePendingMetadataReviewParams{ID: row.ID, Status: status})
		if err != nil {
			return "", err
		}
		if n != 1 {
			return "", errPreempted
		}
	}
	return status, nil
}
