package database

import "context"

// CoverMaterialVisible gates the otherwise filename-addressed public thumbnail route.
// Prepared but uncommitted resources have no references and must never be served.
func (s *SqlStore) CoverMaterialVisible(ctx context.Context, key string) (bool, error) {
	var visible bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM books WHERE cover_path=? UNION ALL SELECT 1 FROM metadata_cover_materials WHERE path=?)`, key, key).Scan(&visible)
	return visible, err
}
