package database

import (
	"container/heap"
	"context"
	"strings"
	"unicode/utf8"
)

// normalizedFieldRank reads indexed candidates' already-normalized full text.
func normalizedFieldRank(name, title, keyword string, inherited bool) (SearchMatchRank, bool) {
	var best SearchMatchRank
	found := false
	qlen := utf8.RuneCountInString(keyword)
	for _, field := range []string{name, title} {
		p := strings.Index(field, keyword)
		if p < 0 {
			continue
		}
		r := SearchMatchRank{Tier: 1, Position: uint32(utf8.RuneCountInString(field[:p])), Extra: uint32(utf8.RuneCountInString(field) - qlen)}
		if inherited {
			r.Tier = 2
		} else if r.Extra == 0 {
			r.Tier = 0
		}
		if !found || rankLess(r, best) {
			best = r
			found = true
		}
	}
	return best, found
}

func rankLess(a, b SearchMatchRank) bool {
	if a.Tier != b.Tier {
		return a.Tier < b.Tier
	}
	if a.Position != b.Position {
		return a.Position < b.Position
	}
	return a.Extra < b.Extra
}

func keyLess(a, b GlobalSearchKey) bool {
	if a.SearchMatchRank != b.SearchMatchRank {
		return rankLess(a.SearchMatchRank, b.SearchMatchRank)
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.ID < b.ID
}

type globalRankHeap []GlobalSearchKey

func (h globalRankHeap) Len() int           { return len(h) }
func (h globalRankHeap) Less(i, j int) bool { return keyLess(h[j], h[i]) }
func (h globalRankHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *globalRankHeap) Push(x any)        { *h = append(*h, x.(GlobalSearchKey)) }
func (h *globalRankHeap) Pop() any          { a := *h; x := a[len(a)-1]; *h = a[:len(a)-1]; return x }

// collectBookRanks keeps only the best limit successors, not a search
// snapshot. Every indexed candidate is considered before this bounded heap
// discards a worse rank. The enclosing read transaction covers hydration too.
func collectBookRanks(ctx context.Context, db globalSearchReader, query string, args []any, keyword string, limit int32, after *GlobalSearchKey) ([]GlobalSearchKey, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	h := make(globalRankHeap, 0, int(limit))
	for rows.Next() {
		var id int64
		var name, title, sname, stitle string
		err = rows.Scan(&id, &name, &title, &sname, &stitle)
		if err != nil {
			return nil, err
		}
		rank, ok := normalizedFieldRank(name, title, keyword, false)
		if !ok {
			rank, ok = normalizedFieldRank(sname, stitle, keyword, true)
		}
		if !ok {
			continue
		}
		k := GlobalSearchKey{SearchMatchRank: rank, Kind: 0, ID: id}
		if after != nil && !keyLess(*after, k) {
			continue
		}
		if len(h) < int(limit) {
			heap.Push(&h, k)
		} else if keyLess(k, h[0]) {
			h[0] = k
			heap.Fix(&h, 0)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	keys := make([]GlobalSearchKey, len(h))
	for i := len(keys) - 1; i >= 0; i-- {
		keys[i] = heap.Pop(&h).(GlobalSearchKey)
	}
	return keys, nil
}

func hydrateBookRanks(keys []GlobalSearchKey) (string, []any) {
	var values []string
	args := make([]any, 0, len(keys)*5)
	for i, k := range keys {
		values = append(values, "(?,?,?,?,?)")
		args = append(args, k.ID, k.Tier, k.Position, k.Extra, i)
	}
	query := `WITH top(id,tier,position,extra,ordinal) AS (VALUES ` + strings.Join(values, ",") + `)`
	query += ` SELECT b.id, b.series_id, b.library_id, b.name, b.path, b.size, b.file_modified_at, b.volume, b.title, b.summary, b.number, b.sort_number, b.page_count, b.cover_path, b.last_read_page, b.last_read_at, b.is_read, b.file_hash, b.quick_hash, b.path_fingerprint, b.path_fingerprint_no_ext, b.filename_fingerprint, b.created_at, b.updated_at,
 s.name, s.title, r.tier,r.position,r.extra
 FROM top r JOIN books b ON b.id=r.id JOIN series s ON s.id=b.series_id
 ORDER BY r.ordinal`
	return query, args
}
