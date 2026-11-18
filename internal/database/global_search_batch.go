package database

import (
	"context"
	"database/sql"
)

// GlobalSearchKey is a bounded, text-free seek position shared by both types.
type GlobalSearchKey struct {
	SearchMatchRank
	Kind uint8
	ID   int64
}

type globalSearchReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func globalSearchSeek(after *GlobalSearchKey, kind uint8) (string, []any) {
	if after == nil {
		return "", nil
	}
	return `AND (r.tier,r.position,r.extra,?,r.id) > (?,?,?,?,?)`, []any{kind, after.Tier, after.Position, after.Extra, after.Kind, after.ID}
}

// SearchGlobalBatch reads each type's ordered successors in one read transaction.
// The authenticated catalogue policy grants every reader all libraries; no
// per-library ACL exists. Any future ACL must filter candidates before LIMIT.
func (s *SqlStore) SearchGlobalBatch(ctx context.Context, keyword, target string, limit int32, after *GlobalSearchKey) ([]BookSearchHit, []SeriesSearchHit, error) {
	keyword = NormalizeSearchText(keyword)
	if keyword == "" {
		return []BookSearchHit{}, []SeriesSearchHit{}, nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var books []BookSearchHit
	var series []SeriesSearchHit
	if target != "series" {
		books, err = s.searchGlobalBooksSubstring(ctx, tx, keyword, limit, after)
		if err != nil {
			return nil, nil, err
		}
	}
	if target != "book" {
		series, err = s.searchGlobalSeriesSubstring(ctx, tx, keyword, limit, after)
		if err != nil {
			return nil, nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return books, series, nil
}
