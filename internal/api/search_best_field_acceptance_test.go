package api

import (
	"fmt"
	"testing"
)

// Keep best-field selection observable at the real HTTP seam while replacing
// field-expansion/window ranking with one bounded rank per candidate identity.
func TestSearchBestFieldNumericProjectionHTTP(t *testing.T) {
	c, store, _, _ := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one')`)
	fields := [][2]string{{"q", "xq"}, {"xq", "q"}, {"xq", "qxx"}, {"xxq", "xq"}, {"界q", "a界q"}, {"xqz", "yq"}, {"q", "q"}}
	for i, f := range fields {
		normalizedExec(t, store, `INSERT INTO series(id,library_id,name,title,path) VALUES(?,1,?,?,?)`, i+1, f[0], f[1], fmt.Sprintf("/s%d", i+1))
		normalizedExec(t, store, `INSERT INTO books(id,series_id,library_id,name,title,path,size,file_modified_at) VALUES(?,?,1,?,?,?,1,CURRENT_TIMESTAMP)`, i+1, i+1, f[0], f[1], fmt.Sprintf("/b%d", i+1))
	}
	f := normalizedSearchServer(t, c, store)
	for _, target := range []string{"series", "book"} {
		prefix := "s_"
		if target == "book" {
			prefix = "b_"
		}
		want := []string{}
		for _, id := range []int{1, 2, 7, 3, 4, 5, 6} {
			want = append(want, fmt.Sprintf("%s%d", prefix, id))
		}
		for _, limit := range []int{1, 3, 7} {
			got := []string{}
			cursor := ""
			for {
				r := cursorPage(t, f, "q", target, limit, cursor)
				for _, h := range r.Hits {
					got = append(got, h.ID)
				}
				if !r.HasMore {
					break
				}
				cursor = r.NextCursor
			}
			normalizedEqual(t, got, want)
		}
	}
}

// More than a thousand matches are traversed entirely using only server-issued
// cursors. The independent literal oracle has three groups: own exact books,
// exact series, then inherited books; equal keys are broken by persistent ID.
func TestSearchBroadCompleteOrderedHTTP(t *testing.T) {
	c, store, _, _ := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one'),(2,'two','/two')`)
	for sid := 1; sid <= 150; sid++ {
		normalizedExec(t, store, `INSERT INTO series(id,library_id,name,path) VALUES(?,?,'Needle',?)`, sid, 1+sid%2, fmt.Sprintf("/s%d", sid))
		for j := 1; j <= 20; j++ {
			bid := (sid-1)*20 + j
			name := "unrelated"
			if bid%2 == 1 {
				name = "Needle"
			}
			normalizedExec(t, store, `INSERT INTO books(id,series_id,library_id,name,path,size,file_modified_at) VALUES(?,?,?,?,?,1,CURRENT_TIMESTAMP)`, bid, sid, 1+sid%2, name, fmt.Sprintf("/b%d", bid))
		}
	}
	f := normalizedSearchServer(t, c, store)
	for _, target := range []string{"all", "series", "book"} {
		want := []string{}
		if target != "series" {
			for bid := 1; bid <= 3000; bid += 2 {
				want = append(want, fmt.Sprintf("b_%d", bid))
			}
		}
		if target != "book" {
			for sid := 1; sid <= 150; sid++ {
				want = append(want, fmt.Sprintf("s_%d", sid))
			}
		}
		if target != "series" {
			for bid := 2; bid <= 3000; bid += 2 {
				want = append(want, fmt.Sprintf("b_%d", bid))
			}
		}
		got := []string{}
		cursor := ""
		for {
			page := cursorPage(t, f, "Needle", target, 100, cursor)
			for _, hit := range page.Hits {
				got = append(got, hit.ID)
			}
			if !page.HasMore {
				normalizedEqual(t, page.NextCursor, "")
				break
			}
			if page.NextCursor == "" || page.NextCursor == cursor {
				t.Fatal("nonadvancing cursor")
			}
			cursor = page.NextCursor
		}
		normalizedEqual(t, got, want)
		t.Logf("full HTTP %s: %d independently ordered matches", target, len(got))
	}
}

// Literal HTTP order covers disjoint own/inherited sources, a secondary field
// beyond a consumed cursor, Unicode character positions, and heap replacement.
// The performance red is the recorded acceptance budget, not a fabricated
// claim that this behavior was broken before the query optimization.
func TestSearchBestFieldDisjointSourcesHTTP(t *testing.T) {
	c, store, _, _ := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one')`)
	normalizedExec(t, store, `INSERT INTO series(id,library_id,name,title,path) VALUES(1,1,'xq','q','/s1'),(2,1,'other','','/s2')`)
	fields := [][3]string{
		{"q", "xxq", "1"}, {"界q", "a界q", "1"}, {"qxx", "xq", "1"}, {"none", "", "1"}, {"none", "", "1"}, {"xxq", "q", "2"}, {"a界q", "界q", "2"}, {"q", "q", "2"},
	}
	for i, f := range fields {
		normalizedExec(t, store, `INSERT INTO books(id,series_id,library_id,name,title,path,size,file_modified_at) VALUES(?,?,1,?,?,?,1,CURRENT_TIMESTAMP)`, i+1, f[2], f[0], f[1], fmt.Sprintf("/b%d", i+1))
	}
	f := normalizedSearchServer(t, c, store)
	want := []string{"b_1", "b_6", "b_8", "b_3", "b_2", "b_7", "b_4", "b_5"}
	for _, limit := range []int{1, 2, 3, 7, 100} {
		got := []string{}
		cursor := ""
		for {
			page := cursorPage(t, f, "q", "book", limit, cursor)
			for _, hit := range page.Hits {
				got = append(got, hit.ID)
			}
			if !page.HasMore {
				break
			}
			cursor = page.NextCursor
		}
		normalizedEqual(t, got, want)
	}
}
