package database

import (
	"context"
	"os"
	"testing"
)

// Explain the exact statements used by production on a disposable upgraded
// copy of the frozen mother database. Virtual-table MATCH scans and scans of
// materialized candidate sets are not full catalogue scans.
func TestSearchAcceptanceQueryPlan(t *testing.T) {
	path := os.Getenv("SEARCH_ACCEPTANCE_PLAN_DB")
	if path == "" {
		t.Skip("opt-in frozen database query plan")
	}
	if err := Migrate(path); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, q := range []string{"海", "海贼", "海贼王", "孤星Atlas"} {
		for _, kind := range []string{"series", "book"} {
			for _, deep := range []bool{false, true} {
				var after *GlobalSearchKey
				if deep {
					after = &GlobalSearchKey{SearchMatchRank: SearchMatchRank{Tier: 1, Extra: 12}, ID: 19000, Kind: 1}
					if kind == "book" {
						after.Kind = 0
						after.ID = 190000
					}
				}
				query, args := globalSeriesStatement(q, 21, after)
				if kind == "book" {
					query, args = globalBooksStatement(q, 21, after)
				}
				rows, err := s.DB().QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("query=%q target=%s deep=%v", q, kind, deep)
				for rows.Next() {
					var id, parent, unused int
					var detail string
					if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
						t.Fatal(err)
					}
					t.Log(detail)
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				rows.Close()
				if kind == "book" {
					query, args = hydrateBookRanks([]GlobalSearchKey{{ID: 1, Kind: 0, SearchMatchRank: SearchMatchRank{Tier: 1, Extra: 12}}})
					rows, err = s.DB().QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
					if err != nil {
						t.Fatal(err)
					}
					for rows.Next() {
						var id, parent, unused int
						var detail string
						if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
							t.Fatal(err)
						}
						t.Log("hydrate:", detail)
					}
					if err := rows.Err(); err != nil {
						t.Fatal(err)
					}
					rows.Close()
				}
			}
		}
	}
}
