package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
)

func TestGlobalCursorHTTPWalk(t *testing.T) {
	c, store, _, _ := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one'),(2,'two','/two')`)
	for i := 1; i <= 45; i++ {
		normalizedExec(t, store, `INSERT INTO series(id,library_id,name,path) VALUES(?,?,'Needle',?)`, i, 1+i%2, fmt.Sprintf("/s%d", i))
		normalizedExec(t, store, `INSERT INTO books(id,series_id,library_id,name,path,size,file_modified_at) VALUES(?,?,?,'Needle',?,1,CURRENT_TIMESTAMP)`, i, i, 1+i%2, fmt.Sprintf("/b%d", i))
	}
	f := normalizedSearchServer(t, c, store)
	for _, target := range []string{"all", "series", "book", "title"} {
		want := []string{}
		if target != "series" {
			for i := 1; i <= 45; i++ {
				want = append(want, fmt.Sprintf("b_%d", i))
			}
		}
		if target == "all" || target == "series" {
			for i := 1; i <= 45; i++ {
				want = append(want, fmt.Sprintf("s_%d", i))
			}
		}
		for _, limit := range []int{1, 7, 20, 100} {
			got := []string{}
			cursor := ""
			for page := 0; page < 100; page++ {
				var r struct {
					Hits   []SearchHit `json:"hits"`
					Total  uint64      `json:"total_hits"`
					More   bool        `json:"has_more"`
					Cursor string      `json:"next_cursor"`
				}
				body := f.request(t, "GET", fmt.Sprintf("/api/search?q=Ｎｅｅｄ—ｌｅ&target=%s&limit=%d&cursor=%s", target, limit, url.QueryEscape(cursor)))
				if err := json.Unmarshal(body, &r); err != nil {
					t.Fatal(err)
				}
				if len(r.Hits) > limit {
					t.Fatalf("mixed batch exceeds limit: %d > %d", len(r.Hits), limit)
				}
				normalizedEqual(t, r.Total, uint64(len(r.Hits)))
				for _, h := range r.Hits {
					got = append(got, h.ID)
				}
				if !r.More {
					normalizedEqual(t, r.Cursor, "")
					break
				}
				if r.Cursor == "" || len(r.Cursor) > 8192 {
					t.Fatalf("unusable cursor %q", r.Cursor)
				}
				cursor = r.Cursor
			}
			normalizedEqual(t, got, want)
		}
	}
}
