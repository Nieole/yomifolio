package delegated

import (
	"context"
	"database/sql"
)

// AuthorizeTx must be called inside the SAME SQLite transaction that writes the
// protected content. A concurrent revoke either precedes that transaction (denied)
// or follows its commit. SQLITE_BUSY/snapshot conflicts must abort the entire
// write; retry only by starting a fresh transaction and checking again.
// It does not commit, retry, compensate, or authorize an external network request.
func AuthorizeTx(ctx context.Context, tx *sql.Tx, subject Subject, library int64, capability string) error {
	if tx == nil {
		return ErrDenied
	}
	authority, err := loadAuthority(ctx, tx, subject)
	if err != nil {
		return ErrDenied
	}
	return authorizeAuthority(authority, library, capability)
}
