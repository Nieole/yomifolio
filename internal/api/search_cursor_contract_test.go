package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"yomifolio/internal/database"
)

func cursorPage(t *testing.T, f *normalizedSearchHTTP, q, target string, limit int, cursor string) SearchResult {
	t.Helper()
	var r SearchResult
	if err := json.Unmarshal(f.request(t, "GET", fmt.Sprintf("/api/search?q=%s&target=%s&limit=%d&cursor=%s", url.QueryEscape(q), target, limit, url.QueryEscape(cursor))), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func cursorStatus(t *testing.T, f *normalizedSearchHTTP, path string, cookie *http.Cookie, status int, code string) {
	t.Helper()
	req, err := http.NewRequest("GET", f.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != status {
		t.Fatalf("status %d want %d: %s", resp.StatusCode, status, raw)
	}
	if code != "" {
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		normalizedEqual(t, body["code"], code)
	}
}

func cursorLogin(t *testing.T, f *normalizedSearchHTTP, name string) *http.Cookie {
	t.Helper()
	resp, err := f.server.Client().Post(f.server.URL+"/api/auth/login", "application/json", strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"password1"}`, name)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("login %d", resp.StatusCode)
	}
	return resp.Cookies()[0]
}

func TestGlobalCursorHTTPContract(t *testing.T) {
	c, store, _, _ := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one')`)
	normalizedExec(t, store, `INSERT INTO series(id,library_id,name,path) VALUES(1,1,'Needle','/s1'),(2,1,'Needle','/s2'),(3,1,'Needle','/s3')`)
	f := normalizedSearchServer(t, c, store)
	for _, q := range []string{"", "  ", "！—", "missing"} {
		raw := f.request(t, "GET", "/api/search?q="+url.QueryEscape(q))
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		normalizedEqual(t, body["hits"], []any{})
		normalizedEqual(t, body["total_hits"], float64(0))
		normalizedEqual(t, body["max_score"], float64(0))
		normalizedEqual(t, body["has_more"], false)
		normalizedEqual(t, body["next_cursor"], "")
	}
	for _, params := range []string{"limit=0", "limit=-1", "limit=101", "limit=abc", "limit=1.0", "limit=", "limit=+1", "limit=9999999999999999999999", "limit=1&limit=2", "target=nope"} {
		cursorStatus(t, f, "/api/search?q=&"+params, f.cookie, 400, "")
	}
	raw := f.request(t, "GET", "/api/search?q=needle&limit=1")
	var body struct {
		Hits []struct {
			Fields map[string]any `json:"fields"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "title", "series_name", "type", "cover_path"} {
		if _, ok := body.Hits[0].Fields[key]; !ok {
			t.Fatalf("missing compatible field %s", key)
		}
	}
	first := cursorPage(t, f, "needle", "series", 1, "")
	normalizedEqual(t, first.HasMore, true)
	normalizedEqual(t, cursorPage(t, f, "Ｎｅｅｄ—ｌｅ", "series", 1, first.NextCursor).Hits[0].ID, "s_2")
	for _, params := range []string{"q=other&target=series", "q=needle&target=all", "q=&target=series"} {
		cursorStatus(t, f, "/api/search?"+params+"&cursor="+first.NextCursor, f.cookie, 400, "invalid_search_cursor")
	}
	for _, cursor := range []string{"!", "e30", first.NextCursor + "=", strings.Repeat("A", 8193)} {
		cursorStatus(t, f, "/api/search?q=needle&target=series&cursor="+url.QueryEscape(cursor), f.cookie, 400, "invalid_search_cursor")
	}
	for _, suffix := range []string{"cursor=%zz", "cursor=" + first.NextCursor + "&cursor=bad"} {
		cursorStatus(t, f, "/api/search?q=needle&target=series&"+suffix, f.cookie, 400, "invalid_search_cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise wire versions, enum/range validation, and exact structure size.
	for _, index := range []int{0, 1, 34, 75, 76, 80, 84, 85} {
		corrupt := append([]byte{}, decoded...)
		corrupt[index] = 255
		cursorStatus(t, f, "/api/search?q=needle&target=series&cursor="+base64.RawURLEncoding.EncodeToString(corrupt), f.cookie, 400, "invalid_search_cursor")
	}

	// Impossible rank: nonzero position with no excess characters.
	impossible := append([]byte{}, decoded...)
	impossible[75] = 1
	impossible[79] = 1
	cursorStatus(t, f, "/api/search?q=needle&target=series&cursor="+base64.RawURLEncoding.EncodeToString(impossible), f.cookie, 400, "invalid_search_cursor")
	cursorStatus(t, f, "/api/search?q=needle&target=series&cursor="+first.NextCursor, nil, 401, "")
	mkTestUser(t, store, "search-other", database.RoleRegular)
	other := cursorLogin(t, f, "search-other")
	cursorStatus(t, f, "/api/search?q=needle&target=series&cursor="+first.NextCursor, other, 400, "invalid_search_cursor")
	// Permission context comes from the newly authenticated account, not cursor claims.
	normalizedExec(t, store, `UPDATE users SET role='regular' WHERE username='search-admin'`)
	cursorStatus(t, f, "/api/search?q=needle&target=series&cursor="+first.NextCursor, f.cookie, 400, "invalid_search_cursor")
	refreshed := cursorPage(t, f, "needle", "series", 1, "")
	normalizedEqual(t, refreshed.Hits[0].ID, "s_1")
	normalizedExec(t, store, `UPDATE users SET must_change_password=1 WHERE username='search-admin'`)
	cursorStatus(t, f, "/api/search?q=needle&target=series&cursor="+refreshed.NextCursor, f.cookie, 403, "")
}

func TestGlobalCursorHTTPLongMetadataAndQuery(t *testing.T) {
	c, store, _, _ := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one')`)
	normalizedExec(t, store, `INSERT INTO series(id,library_id,name,path) VALUES(1,1,'other','/s')`)
	long := "Needle" + strings.Repeat("界", 10000)
	for i := 1; i <= 3; i++ {
		_, err := store.UpsertBookByPath(context.Background(), database.UpsertBookByPathParams{SeriesID: 1, LibraryID: 1, Name: "other", Title: sql.NullString{String: long, Valid: true}, Path: fmt.Sprintf("/b%d.cbz", i), FileModifiedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
	}
	f := normalizedSearchServer(t, c, store)
	for _, query := range []string{"needle", long} {
		cursor := ""
		for i := 1; i <= 3; i++ {
			page := cursorPage(t, f, query, "title", 1, cursor)
			normalizedEqual(t, len(page.Hits), 1)
			normalizedEqual(t, page.Hits[0].ID, fmt.Sprintf("b_%d", i))
			normalizedEqual(t, page.Hits[0].Fields.Title, long)
			normalizedEqual(t, page.HasMore, i < 3)
			if len(page.NextCursor) > 8192 {
				t.Fatalf("oversize cursor: %d", len(page.NextCursor))
			}
			cursor = page.NextCursor
			if i == 1 { // title and book are the same bound target; batch size may change.
				normalizedEqual(t, len(cursorPage(t, f, query, "book", 100, cursor).Hits), 2)
			}
		}
	}
	first := cursorPage(t, f, "needle", "book", 1, "")
	// Seek does not require the boundary row to survive.
	normalizedExec(t, store, `DELETE FROM books WHERE id=1`)
	normalizedEqual(t, cursorPage(t, f, "needle", "book", 1, first.NextCursor).Hits[0].ID, "b_2")
	normalizedExec(t, store, `DELETE FROM books`)
	last := cursorPage(t, f, "needle", "book", 1, first.NextCursor)
	normalizedEqual(t, len(last.Hits), 0)
	normalizedEqual(t, last.HasMore, false)
	normalizedEqual(t, last.NextCursor, "")
}
