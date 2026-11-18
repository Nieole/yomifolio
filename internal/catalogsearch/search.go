// Package catalogsearch owns catalogue result ranking, identity and mixed batches.
package catalogsearch

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"yomifolio/internal/database"
	"yomifolio/internal/reading"
)

type Store interface {
	SearchGlobalBatch(context.Context, string, string, int32, *database.GlobalSearchKey) ([]database.BookSearchHit, []database.SeriesSearchHit, error)
}
type Search struct{ store Store }

func New(store Store) *Search { return &Search{store: store} }

type Result struct {
	Hits       []*Hit
	Total      uint64
	MaxScore   float64
	HasMore    bool
	NextCursor string
}
type Hit struct {
	ID                                 string
	Score                              float64
	Title, SeriesName, Kind, CoverPath string
	Key                                SortKey
}

// SortKey is the complete total order. Kind is 0 for book and 1 for series;
// it breaks ties only, never groups results. No unbounded text enters the key.
type SortKey database.GlobalSearchKey

func (k SortKey) Less(other SortKey) bool {
	if k.Tier != other.Tier {
		return k.Tier < other.Tier
	}
	if k.Position != other.Position {
		return k.Position < other.Position
	}
	if k.Extra != other.Extra {
		return k.Extra < other.Extra
	}
	if k.Kind != other.Kind {
		return k.Kind < other.Kind
	}
	return k.ID < other.ID
}

var ErrTarget = errors.New("invalid search target")

func (c *Search) Query(ctx context.Context, subject reading.Subject, query, target string, limit int32, cursor, authority string) (*Result, error) {
	reader, err := subject.UserID()
	if err != nil {
		return nil, err
	}
	if target == "" {
		target = "all"
	}
	if target == "title" {
		target = "book"
	}
	if target != "all" && target != "book" && target != "series" {
		return nil, ErrTarget
	}
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return nil, ErrLimit
	}
	prefix := cursorPrefix(query, target, reader, authority)
	var after *SortKey
	if cursor != "" {
		after, err = decodeCursor(cursor, prefix)
		if err != nil {
			return nil, err
		}
	}
	books, series, err := c.store.SearchGlobalBatch(ctx, query, target, limit+1, (*database.GlobalSearchKey)(after))
	if err != nil {
		return nil, err
	}
	res := &Result{Hits: make([]*Hit, 0)}
	if target != "series" {
		for _, r := range books {
			title := r.Name
			if r.Title.Valid && r.Title.String != "" {
				title = r.Title.String
			}
			series := r.SeriesName
			if r.SeriesTitle.Valid && r.SeriesTitle.String != "" {
				series = r.SeriesTitle.String
			}
			res.Hits = append(res.Hits, &Hit{ID: "b_" + strconv.FormatInt(r.ID, 10), Score: r.Rank.Score(), Title: title, SeriesName: series, Kind: "book", CoverPath: r.CoverPath.String, Key: SortKey{SearchMatchRank: r.Rank, Kind: 0, ID: r.ID}})
		}
	}
	if target != "book" {
		for _, r := range series {
			title := r.Name
			if r.Title.Valid && r.Title.String != "" {
				title = r.Title.String
			}
			res.Hits = append(res.Hits, &Hit{ID: "s_" + strconv.FormatInt(r.ID, 10), Score: r.Rank.Score(), Title: title, SeriesName: r.Name, Kind: "series", CoverPath: r.CoverPath.String, Key: SortKey{SearchMatchRank: r.Rank, Kind: 1, ID: r.ID}})
		}
	}
	sort.Slice(res.Hits, func(i, j int) bool { return res.Hits[i].Key.Less(res.Hits[j].Key) })
	// Stores reduce duplicate field/source matches before their ordered LIMIT.
	// Each type's top N therefore contains every possible mixed top-N member.
	if len(res.Hits) > int(limit) {
		res.HasMore = true
		res.Hits = res.Hits[:limit]
		res.NextCursor = encodeCursor(prefix, res.Hits[len(res.Hits)-1].Key)
	}
	res.Total = uint64(len(res.Hits))
	for _, hit := range res.Hits {
		if hit.Score > res.MaxScore {
			res.MaxScore = hit.Score
		}
	}
	return res, nil
}
