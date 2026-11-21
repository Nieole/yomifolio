package database

import (
	"context"
	"database/sql"
)

// MetadataReceipt stores an immutable operation, separate from latest field provenance.
// Payload contains the complete versioned application receipt and reversal material.
type MetadataReceipt struct {
	Sequence     int64
	ID           string
	UserID       int64
	CredentialID string
	RequestKey   string
	RequestHash  string
	SeriesID     int64
	LibraryID    int64
	ParentID     string
	Outcome      string
	Payload      string
}

// MetadataJournal binds all receipt reads and inserts to the actual metadata transaction.
type MetadataJournal struct{ Tx *sql.Tx }

// TryChange isolates metadata writes while retaining the outer authorization and
// journal transaction. failed means a write error was fully rolled back; only then
// may the caller commit a failed receipt. Transaction/savepoint failures return an
// error instead: durability is not guaranteed and the outer transaction must abort.
// The callback must not commit, roll back, or otherwise control the transaction.
func (j MetadataJournal) TryChange(ctx context.Context, write func() error) (failed bool, err error) {
	if _, err = j.Tx.ExecContext(ctx, `SAVEPOINT metadata_change`); err != nil {
		return false, err
	}
	if writeErr := write(); writeErr != nil {
		if _, err = j.Tx.ExecContext(ctx, `ROLLBACK TO metadata_change`); err != nil {
			return false, err
		}
		failed = true
	}
	_, err = j.Tx.ExecContext(ctx, `RELEASE metadata_change`)
	return failed, err
}

const metadataReceiptColumns = `sequence,id,user_id,credential_id,request_key,request_hash,series_id,library_id,parent_id,outcome,payload`

func scanMetadataReceipt(row interface{ Scan(...any) error }) (MetadataReceipt, error) {
	var r MetadataReceipt
	err := row.Scan(&r.Sequence, &r.ID, &r.UserID, &r.CredentialID, &r.RequestKey, &r.RequestHash, &r.SeriesID, &r.LibraryID, &r.ParentID, &r.Outcome, &r.Payload)
	return r, err
}
func (j MetadataJournal) Get(ctx context.Context, id string) (MetadataReceipt, error) {
	return scanMetadataReceipt(j.Tx.QueryRowContext(ctx, `SELECT `+metadataReceiptColumns+` FROM metadata_operations WHERE id=?`, id))
}
func (j MetadataJournal) Retry(ctx context.Context, user int64, credential, key string) (MetadataReceipt, error) {
	return scanMetadataReceipt(j.Tx.QueryRowContext(ctx, `SELECT `+metadataReceiptColumns+` FROM metadata_operations WHERE user_id=? AND credential_id=? AND request_key=?`, user, credential, key))
}
func (j MetadataJournal) UndoneBy(ctx context.Context, id string) (string, error) {
	var out string
	err := j.Tx.QueryRowContext(ctx, `SELECT id FROM metadata_operations WHERE parent_id=? AND outcome='changed'`, id).Scan(&out)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return out, err
}
func (j MetadataJournal) Insert(ctx context.Context, r MetadataReceipt) error {
	_, err := j.Tx.ExecContext(ctx, `INSERT INTO metadata_operations(id,user_id,credential_id,request_key,request_hash,series_id,library_id,parent_id,outcome,payload) VALUES(?,?,?,?,?,?,?,?,?,?)`, r.ID, r.UserID, r.CredentialID, r.RequestKey, r.RequestHash, r.SeriesID, r.LibraryID, r.ParentID, r.Outcome, r.Payload)
	return err
}

// List returns stable descending keyset pages. Zero series selects the administrator view.
func (j MetadataJournal) List(ctx context.Context, series, before int64, limit int) ([]MetadataReceipt, error) {
	rows, err := j.Tx.QueryContext(ctx, `SELECT `+metadataReceiptColumns+` FROM metadata_operations WHERE (?=0 OR series_id=?) AND (?=0 OR sequence<?) ORDER BY sequence DESC LIMIT ?`, series, series, before, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetadataReceipt{}
	for rows.Next() {
		r, e := scanMetadataReceipt(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RequireMetadataAdmin checks the current web actor inside the content transaction.
func RequireMetadataAdmin(ctx context.Context, tx *sql.Tx, user int64) error {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT role='admin' AND NOT must_change_password FROM users WHERE id=?`, user).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return sql.ErrNoRows
	}
	return nil
}
