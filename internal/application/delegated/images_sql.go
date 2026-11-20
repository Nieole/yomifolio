package delegated

import "context"

func (s *SQLStore) SeriesCoverBook(ctx context.Context, series int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT b.id FROM books b JOIN series s ON s.id=b.series_id AND s.library_id=b.library_id WHERE b.series_id=? AND coalesce(b.cover_path,'')<>'' ORDER BY b.sort_number,b.name,b.id LIMIT 1`, series).Scan(&id)
	return id, err
}
