package identification

import (
	"context"
	"database/sql"

	"yomifolio/internal/task"
)

// SetOwnerReviewed records that the owner takes over the series' metadata. Setting it again
// keeps the original decision. The result is the series' identification after the change.
func (s *Service) SetOwnerReviewed(ctx context.Context, actor, seriesID int64) (SeriesIdentification, error) {
	return s.ownerDecision(ctx, actor, seriesID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO identification_owner_reviews (series_id, user_id, reviewed_at_ms) VALUES (?, ?, ?)`,
			seriesID, actor, s.now().UnixMilli())
		return err
	})
}

// WithdrawOwnerReviewed hands the series back to the identification flow. Withdrawing a series
// that is not owner-reviewed changes nothing.
func (s *Service) WithdrawOwnerReviewed(ctx context.Context, actor, seriesID int64) (SeriesIdentification, error) {
	return s.ownerDecision(ctx, actor, seriesID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM identification_owner_reviews WHERE series_id = ?`, seriesID)
		return err
	})
}

// ownerDecision checks the actor and the series, applies change and reads the result, all in one
// transaction.
func (s *Service) ownerDecision(ctx context.Context, actor, seriesID int64, change func(*sql.Tx) error) (SeriesIdentification, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SeriesIdentification{}, err
	}
	defer tx.Rollback()
	if err := requireAdmin(ctx, tx, actor); err != nil {
		return SeriesIdentification{}, err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM series WHERE id = ?)`, seriesID).Scan(&exists); err != nil {
		return SeriesIdentification{}, err
	}
	if !exists {
		return SeriesIdentification{}, ErrSeriesNotFound
	}
	if err := change(tx); err != nil {
		return SeriesIdentification{}, err
	}
	out, err := read(ctx, tx, seriesID)
	if err != nil {
		return SeriesIdentification{}, err
	}
	if err := tx.Commit(); err != nil {
		return SeriesIdentification{}, err
	}
	return out, nil
}

// StartPrefetch launches the evidence prefetch the owner asked for, over one library or over every
// library when libraryID is 0.
func (s *Service) StartPrefetch(ctx context.Context, actor, libraryID int64) (PrefetchLaunch, error) {
	if err := requireAdmin(ctx, s.db, actor); err != nil {
		return PrefetchLaunch{}, err
	}
	if s.prefetch == nil {
		return PrefetchLaunch{}, ErrDenied
	}
	return s.prefetch.Start(ctx, libraryID, task.TriggerManual)
}
