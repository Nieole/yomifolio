package delegated

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
)

// SQLStore is constructed only by the composition root. All credential management
// decisions and writes share a transaction; authentication joins current account state.
type SQLStore struct{ db *sql.DB }

func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{db: db} }

type sqlWrite struct{ tx *sql.Tx }

func (s *SQLStore) Write(ctx context.Context, apply func(Write) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := apply(sqlWrite{tx}); err != nil {
		return err
	}
	return tx.Commit()
}
func (w sqlWrite) Admin(ctx context.Context, id int64) error {
	var ok bool
	err := w.tx.QueryRowContext(ctx, `SELECT role = 'admin' AND NOT must_change_password FROM users WHERE id=?`, id).Scan(&ok)
	if err != nil || !ok {
		return ErrDenied
	}
	return nil
}
func (w sqlWrite) UserExists(ctx context.Context, id int64) error {
	var n int
	err := w.tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id=?`, id).Scan(&n)
	if err != nil {
		return ErrInvalid
	}
	return nil
}
func (w sqlWrite) LibraryExists(ctx context.Context, id int64) error {
	var n int
	err := w.tx.QueryRowContext(ctx, `SELECT 1 FROM libraries WHERE id=?`, id).Scan(&n)
	if err != nil {
		return ErrInvalid
	}
	return nil
}
func restrictionsJSON(c Credential) (string, string) {
	libs, _ := json.Marshal(c.LibraryIDs)
	caps, _ := json.Marshal(c.Capabilities)
	return string(libs), string(caps)
}
func (w sqlWrite) Insert(ctx context.Context, c Credential, hash string) error {
	libs, caps := restrictionsJSON(c)
	_, err := w.tx.ExecContext(ctx, `INSERT INTO mcp_credentials(id,user_id,name,token_hash,library_ids,capabilities,revoked) VALUES(?,?,?,?,?,?,?)`, c.ID, c.UserID, c.Name, hash, libs, caps, c.Revoked)
	return err
}

const credentialColumns = `id,user_id,name,library_ids,capabilities,revoked`

type scanner interface{ Scan(...any) error }

func scanCredential(row scanner) (Credential, error) {
	var c Credential
	var libs, caps string
	err := row.Scan(&c.ID, &c.UserID, &c.Name, &libs, &caps, &c.Revoked)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal([]byte(libs), &c.LibraryIDs); err != nil {
		return c, err
	}
	err = json.Unmarshal([]byte(caps), &c.Capabilities)
	return c, err
}
func (w sqlWrite) Get(ctx context.Context, id string) (Credential, error) {
	return scanCredential(w.tx.QueryRowContext(ctx, `SELECT `+credentialColumns+` FROM mcp_credentials WHERE id=?`, id))
}
func (w sqlWrite) List(ctx context.Context) ([]Credential, error) {
	rows, err := w.tx.QueryContext(ctx, `SELECT `+credentialColumns+` FROM mcp_credentials ORDER BY rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Credential{}
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (w sqlWrite) Update(ctx context.Context, c Credential) error {
	libs, caps := restrictionsJSON(c)
	_, err := w.tx.ExecContext(ctx, `UPDATE mcp_credentials SET library_ids=?,capabilities=?,revoked=? WHERE id=?`, libs, caps, c.Revoked, c.ID)
	return err
}
func (s *SQLStore) Authenticate(ctx context.Context, hash string) (Subject, error) {
	var subject Subject
	err := s.db.QueryRowContext(ctx, `SELECT c.id,c.user_id FROM mcp_credentials c JOIN users u ON u.id=c.user_id WHERE c.token_hash=? AND NOT c.revoked AND NOT u.must_change_password`, hash).Scan(&subject.CredentialID, &subject.UserID)
	if err != nil {
		return Subject{}, ErrDenied
	}
	return subject, nil
}
func (s *SQLStore) Authority(ctx context.Context, subject Subject) (Authority, error) {
	return loadAuthority(ctx, s.db, subject)
}

type authorityQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadAuthority(ctx context.Context, q authorityQuery, subject Subject) (Authority, error) {
	var out Authority
	var libs, caps string
	err := q.QueryRowContext(ctx, `SELECT c.id,c.user_id,c.name,c.library_ids,c.capabilities,c.revoked,u.role='admin',u.must_change_password FROM mcp_credentials c JOIN users u ON u.id=c.user_id WHERE c.id=? AND c.user_id=?`, subject.CredentialID, subject.UserID).Scan(&out.Credential.ID, &out.Credential.UserID, &out.Credential.Name, &libs, &caps, &out.Credential.Revoked, &out.Admin, &out.MustChangePassword)
	if err != nil {
		return out, ErrDenied
	}
	if err = json.Unmarshal([]byte(libs), &out.Credential.LibraryIDs); err != nil {
		return out, err
	}
	err = json.Unmarshal([]byte(caps), &out.Credential.Capabilities)
	return out, err
}
func (s *SQLStore) Series(ctx context.Context, id int64) (Series, error) {
	var out Series
	var path string
	err := s.db.QueryRowContext(ctx, `SELECT id,library_id,name,coalesce(title,''),path FROM series WHERE id=?`, id).Scan(&out.ID, &out.LibraryID, &out.Name, &out.Title, &path)
	out.DirectoryName = filepath.Base(path)
	return out, err
}
