package seriesmeta

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"fmt"
	"path"
	"strings"

	"yomifolio/internal/covermaterial"
	"yomifolio/internal/database"
)

// CoverState identifies the catalogue-selected book cover, including protection and origin.
type CoverState struct {
	BookID    int64  `json:"book_id"`
	Path      string `json:"path"`
	Locked    bool   `json:"locked"`
	Automatic bool   `json:"automatic"`
}

// ArchiveCover recognizes the scanner's path/mtime/size key, not arbitrary unlocked images.
// Manual page choices and uploads use content hashes and remain protected even if unlocked.
func ArchiveCover(book database.Book) bool {
	hash := fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", book.Path, book.FileModifiedAt.Unix(), book.Size))))
	key := strings.ReplaceAll(book.CoverPath.String, "\\", "/")
	return covermaterial.IsPath(key) || strings.TrimSuffix(key, path.Ext(key)) == hash[:2]+"/"+hash
}

// WriteEnrichmentCover switches an unlocked cover in the caller's checked transaction.
// Expected-value, origin and current authorization checks belong to that transaction;
// refreshing statistics and committing text, references and receipts remain the caller's job.
func WriteEnrichmentCover(ctx context.Context, tx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, current database.Series, state *CoverState) error {
	if state == nil {
		return ErrConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE books SET cover_path=? WHERE id=? AND series_id=? AND library_id=? AND cover_locked=0`, state.Path, state.BookID, current.ID, current.LibraryID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}
