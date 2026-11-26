package proposal

import (
	"context"
	"database/sql"

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
