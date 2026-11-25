package delegated

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }
func (s *SQLStore) FindSeries(ctx context.Context, in FindSeriesInput) ([]Series, error) {
	out := []Series{}
	if len(in.LibraryIDs) == 0 {
		return out, nil
	}
	query := `SELECT id,library_id,name,coalesce(title,''),path FROM series WHERE id>? AND library_id IN (` + placeholders(len(in.LibraryIDs)) + `)`
	args := []any{in.AfterID}
	for _, id := range in.LibraryIDs {
		args = append(args, id)
	}
	if len(in.SeriesIDs) > 0 {
		query += ` AND id IN (` + placeholders(len(in.SeriesIDs)) + `)`
		for _, id := range in.SeriesIDs {
			args = append(args, id)
		}
	}
	if in.AddedFrom != "" {
		query += ` AND julianday(created_at)>=julianday(?)`
		args = append(args, in.AddedFrom)
	}
	if in.AddedBefore != "" {
		query += ` AND julianday(created_at)<julianday(?)`
		args = append(args, in.AddedBefore)
	}
	// A series matches when any one listed field is missing.
	missing := make([]string, 0, len(in.MissingFields))
	for _, field := range in.MissingFields {
		switch field {
		case "authors":
			missing = append(missing, `NOT EXISTS (SELECT 1 FROM series_authors WHERE series_id=series.id)`)
		case "tags":
			missing = append(missing, `NOT EXISTS (SELECT 1 FROM series_tags WHERE series_id=series.id)`)
		case "source_links":
			missing = append(missing, `NOT EXISTS (SELECT 1 FROM series_links WHERE series_id=series.id)`)
		case "rating":
			missing = append(missing, `rating IS NULL`)
		case "title", "summary", "publisher", "status", "language":
			missing = append(missing, fmt.Sprintf(`trim(coalesce(%s,''))=''`, field))
		default:
			return nil, ErrInvalid
		}
	}
	if len(missing) > 0 {
		query += ` AND (` + strings.Join(missing, ` OR `) + `)`
	}
	query += ` ORDER BY id ASC LIMIT ?`
	args = append(args, in.Limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v Series
		var path string
		if err = rows.Scan(&v.ID, &v.LibraryID, &v.Name, &v.Title, &path); err != nil {
			return nil, err
		}
		v.DirectoryName = filepath.Base(path)
		out = append(out, v)
	}
	return out, rows.Err()
}

const bookColumns = `b.id,b.series_id,b.library_id,b.name,b.path,coalesce(b.title,''),coalesce(b.number,''),b.volume,b.page_count`

func scanBook(row scanner) (Book, error) {
	var b Book
	var path string
	err := row.Scan(&b.ID, &b.SeriesID, &b.LibraryID, &b.Name, &path, &b.Title, &b.Number, &b.Volume, &b.PageCount)
	b.Filename = filepath.Base(path)
	b.DirectoryName = filepath.Base(filepath.Dir(path))
	return b, err
}
func (s *SQLStore) Books(ctx context.Context, in BooksInput) ([]Book, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+bookColumns+` FROM books b JOIN series s ON s.id=b.series_id AND s.library_id=b.library_id WHERE b.series_id=? AND b.id>? ORDER BY b.id LIMIT ?`, in.SeriesID, in.AfterID, in.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Book{}
	for rows.Next() {
		b, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (s *SQLStore) Book(ctx context.Context, id int64) (Book, error) {
	return scanBook(s.db.QueryRowContext(ctx, `SELECT `+bookColumns+` FROM books b JOIN series s ON s.id=b.series_id AND s.library_id=b.library_id WHERE b.id=?`, id))
}
