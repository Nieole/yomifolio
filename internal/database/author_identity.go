package database

import "context"

// EnsureAuthor reuses an exact name/role identity without updating shared rows.
// It never deletes old identities, including those retained by undo receipts.
func (q *Queries) EnsureAuthor(ctx context.Context, name, role string) (int64, error) {
	if _, err := q.db.ExecContext(ctx, `INSERT INTO authors(name,role) VALUES(?,?) ON CONFLICT(name,role) DO NOTHING`, name, role); err != nil {
		return 0, err
	}
	var id int64
	err := q.db.QueryRowContext(ctx, `SELECT id FROM authors WHERE name=? AND role=?`, name, role).Scan(&id)
	return id, err
}
