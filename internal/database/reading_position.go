package database

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ReadingPositionState is the durable state locked for one reader and book.
type ReadingPositionState struct {
	BasisPresent bool
	IsRead       bool
	Present      bool
	Cleared      bool
	FactID       string
	PageCount    int64
	Page         int64
	LastReadAt   sql.NullTime
	ChangedAtNS  int64
	Duplicate    bool
}

// ReadingPositionMutation is the decision applied within the locked transaction.
type ReadingPositionMutation struct {
	Snapshot   ReadingPositionState
	Duplicate  bool
	IsRead     bool
	Apply      bool
	Clear      bool
	Page       int64
	OccurredAt time.Time
}

// MutateReadingPosition applies one decision with the same transaction guarantees as a batch.
func (s *SqlStore) MutateReadingPosition(ctx context.Context, userID, bookID int64, factID string, decide func(ReadingPositionState) ReadingPositionMutation) (ReadingPositionMutation, error) {
	results, err := s.MutateReadingPositions(ctx, userID, []int64{bookID}, factID, func(_ int64, state ReadingPositionState) ReadingPositionMutation { return decide(state) })
	if err != nil {
		return ReadingPositionMutation{}, err
	}
	result, found := results[bookID]
	if !found {
		return result, sql.ErrNoRows
	}
	return result, nil
}

// MutateReadingPositions atomically persists a bounded batch, receipts and affected series projections.
// Missing books are omitted. Decisions run while SQLite holds the write lock; a failed batch returns no commits.
func (s *SqlStore) MutateReadingPositions(ctx context.Context, userID int64, bookIDs []int64, factID string, decide func(int64, ReadingPositionState) ReadingPositionMutation) (map[int64]ReadingPositionMutation, error) {
	results := make(map[int64]ReadingPositionMutation)
	err := s.ExecTx(ctx, func(q *Queries) error {
		affected := make(map[int64]struct{})
		for _, bookID := range bookIDs {
			book, err := q.GetBook(ctx, bookID)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			state, err := loadReadingPosition(ctx, q, userID, book, factID)
			if err != nil {
				return err
			}
			mutation := decide(bookID, state)
			mutation.Duplicate = state.Duplicate
			mutation.Snapshot = state
			results[bookID] = mutation
			if !mutation.Apply {
				continue
			}
			if err := persistReadingPosition(ctx, q, userID, bookID, factID, mutation); err != nil {
				return err
			}
			snapshot, err := loadReadingPositionSnapshot(ctx, q.db, userID, bookID)
			if err != nil {
				return err
			}
			mutation.Snapshot = snapshot
			results[bookID] = mutation
			affected[book.SeriesID] = struct{}{}
		}
		for seriesID := range affected {
			if userID > 0 {
				if err := refreshUserSeriesProgressTx(ctx, q, userID, seriesID); err != nil {
					return err
				}
			} else {
				if _, err := q.db.ExecContext(ctx, refreshSeriesStatsStatement("s.id = ?"), seriesID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

func loadReadingPosition(ctx context.Context, q *Queries, userID int64, book Book, factID string) (ReadingPositionState, error) {
	state, err := loadReadingPositionSnapshot(ctx, q.db, userID, book.ID)
	if err != nil {
		return state, err
	}
	if factID != "" {
		var exists int
		err := q.db.QueryRowContext(ctx, `SELECT 1 FROM reading_position_receipts WHERE user_id=? AND book_id=? AND fact_id=?`, userID, book.ID, factID).Scan(&exists)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return state, err
		}
		state.Duplicate = err == nil
	}
	return state, nil
}

// GetReadingPosition returns one consistent reader/book snapshot, including clear tombstones.
func (s *SqlStore) GetReadingPosition(ctx context.Context, userID, bookID int64) (ReadingPositionState, error) {
	return loadReadingPositionSnapshot(ctx, s.db, userID, bookID)
}

func loadReadingPositionSnapshot(ctx context.Context, db DBTX, userID, bookID int64) (ReadingPositionState, error) {
	var state ReadingPositionState
	var basis sql.NullInt64
	var fact sql.NullString
	var present bool
	var bookAt, userAt sql.NullTime
	err := db.QueryRowContext(ctx, `SELECT b.page_count,
 CASE WHEN ? > 0 THEN COALESCE(p.last_read_page,0) ELSE COALESCE(b.last_read_page,0) END,
 b.last_read_at, p.last_read_at,
 CASE WHEN ? > 0 THEN COALESCE(p.is_read,0) ELSE b.is_read END,
 CASE WHEN ? > 0 THEN p.book_id IS NOT NULL ELSE (b.last_read_at IS NOT NULL OR COALESCE(b.last_read_page,0)>0 OR b.is_read) END,
 x.changed_at_ns,x.fact_id
 FROM books b LEFT JOIN user_book_progress p ON p.book_id=b.id AND p.user_id=?
 LEFT JOIN reading_position_basis x ON x.book_id=b.id AND x.user_id=? WHERE b.id=?`, userID, userID, userID, userID, userID, bookID).Scan(&state.PageCount, &state.Page, &bookAt, &userAt, &state.IsRead, &present, &basis, &fact)
	if err != nil {
		return state, err
	}
	state.LastReadAt = bookAt
	if userID > 0 {
		state.LastReadAt = userAt
	}
	state.BasisPresent = basis.Valid
	state.Present = present || basis.Valid
	state.Cleared = basis.Valid && !present
	state.ChangedAtNS = basis.Int64
	state.FactID = fact.String
	if basis.Valid && present {
		state.LastReadAt = sql.NullTime{Time: time.Unix(0, basis.Int64).UTC(), Valid: true}
	}
	return state, nil
}

func persistReadingPosition(ctx context.Context, q *Queries, userID, bookID int64, factID string, m ReadingPositionMutation) error {
	if userID > 0 {
		if m.Clear {
			if _, err := q.db.ExecContext(ctx, `DELETE FROM user_book_progress WHERE user_id=? AND book_id=?`, userID, bookID); err != nil {
				return err
			}
		} else {
			if _, err := q.db.ExecContext(ctx, `INSERT INTO user_book_progress(user_id,book_id,last_read_page,last_read_at,is_read) VALUES(?,?,?,?,?) ON CONFLICT(user_id,book_id) DO UPDATE SET last_read_page=excluded.last_read_page,last_read_at=excluded.last_read_at,is_read=excluded.is_read,updated_at=CURRENT_TIMESTAMP`, userID, bookID, sql.NullInt64{Int64: m.Page, Valid: m.Page > 0}, LocalWallClock(m.OccurredAt), m.IsRead); err != nil {
				return err
			}
		}
	} else {
		page := sql.NullInt64{Int64: m.Page, Valid: !m.Clear && m.Page > 0}
		at := sql.NullTime{Time: LocalWallClock(m.OccurredAt), Valid: !m.Clear}
		if err := q.UpdateBookProgress(ctx, UpdateBookProgressParams{ID: bookID, LastReadPage: page, LastReadAt: at, IsRead: m.IsRead}); err != nil {
			return err
		}
	}
	if _, err := q.db.ExecContext(ctx, `INSERT INTO reading_position_basis(user_id,book_id,changed_at_ns,fact_id) VALUES(?,?,?,?) ON CONFLICT(user_id,book_id) DO UPDATE SET changed_at_ns=excluded.changed_at_ns,fact_id=excluded.fact_id`, userID, bookID, m.OccurredAt.UnixNano(), factID); err != nil {
		return err
	}
	if factID != "" {
		_, err := q.db.ExecContext(ctx, `INSERT INTO reading_position_receipts(user_id,book_id,fact_id) VALUES(?,?,?)`, userID, bookID, factID)
		return err
	}
	return nil
}

// LogUserReadingActivityAt projects a proven reading date, retaining daily maximum absolute page semantics.
func (s *SqlStore) LogUserReadingActivityAt(ctx context.Context, userID, bookID, pages int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_reading_activity(user_id,book_id,date,pages_read) VALUES(?,?,?,?) ON CONFLICT(user_id,book_id,date) DO UPDATE SET pages_read=MAX(user_reading_activity.pages_read,excluded.pages_read)`, userID, bookID, ActivityDayKey(LocalWallClock(at)), pages)
	return err
}

// ResolveReadingOperationTime retains the first proven occurrence for an identified operation.
func (s *SqlStore) ResolveReadingOperationTime(ctx context.Context, userID int64, operationID string, at time.Time) (time.Time, error) {
	var ns int64
	err := s.ExecTx(ctx, func(q *Queries) error {
		if _, err := q.db.ExecContext(ctx, `INSERT INTO reading_operations(user_id,operation_id,occurred_at_ns) VALUES(?,?,?) ON CONFLICT(user_id,operation_id) DO NOTHING`, userID, operationID, at.UnixNano()); err != nil {
			return err
		}
		return q.db.QueryRowContext(ctx, `SELECT occurred_at_ns FROM reading_operations WHERE user_id=? AND operation_id=?`, userID, operationID).Scan(&ns)
	})
	return time.Unix(0, ns), err
}
