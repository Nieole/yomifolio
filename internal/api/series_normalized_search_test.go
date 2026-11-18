package api

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"yomifolio/internal/database"
	"yomifolio/internal/scanner"
)

// The agreed seam is registered HTTP routes over real SQLite. SQL is used only
// to arrange catalogue facts, old installations and deliberate index damage.
type normalizedSearchHTTP struct {
	c      *apiFixture
	store  database.Store
	server *httptest.Server
	cookie *http.Cookie
	csrf   string
}

func normalizedSearchServer(t *testing.T, c *apiFixture, store database.Store) *normalizedSearchHTTP {
	t.Helper()
	cfg := c.currentConfig()
	cfg.Protocols.OPDS.Enabled = true
	cfg.Protocols.Mihon.Enabled = true
	c.config.Replace(&cfg)
	mkTestUser(t, store, "search-admin", database.RoleAdmin)
	r := chi.NewRouter()
	c.SetupRoutes(r)
	c.SetupOPDSRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	f := &normalizedSearchHTTP{c: c, store: store, server: srv}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", strings.NewReader(`{"username":"search-admin","password":"password1"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("login: %d %s", resp.StatusCode, b)
	}
	var session authSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	f.cookie = resp.Cookies()[0]
	f.csrf = session.CSRFToken
	return f
}

func (f *normalizedSearchHTTP) request(t *testing.T, method, path string) []byte {
	t.Helper()
	req, err := http.NewRequest(method, f.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/api/mihon/") {
		req.AddCookie(f.cookie)
		req.Header.Set("X-CSRF-Token", f.csrf)
	} else {
		req.SetBasicAuth("search-admin", "password1")
	}
	resp, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, b)
	}
	return b
}
func normalizedExec(t *testing.T, store database.Store, query string, args ...any) {
	t.Helper()
	if _, err := store.DB().Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
func (f *normalizedSearchHTTP) global(t *testing.T, q, target string) []string {
	t.Helper()
	var body struct {
		Hits []struct {
			ID string `json:"id"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(f.request(t, "GET", "/api/search?q="+url.QueryEscape(q)+"&target="+target), &body); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(body.Hits))
	for _, hit := range body.Hits {
		ids = append(ids, hit.ID)
	}
	sort.Strings(ids)
	return ids
}
func (f *normalizedSearchHTTP) series(t *testing.T, query string) seriesSearchBody {
	t.Helper()
	var body seriesSearchBody
	if err := json.Unmarshal(f.request(t, "GET", "/api/series/search?"+query), &body); err != nil {
		t.Fatal(err)
	}
	return body
}
func normalizedSeriesIDs(body seriesSearchBody) []int64 {
	ids := make([]int64, 0, len(body.Items))
	for _, row := range body.Items {
		ids = append(ids, row.ID)
	}
	return ids
}
func normalizedEqual(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v; independent oracle wants %v", got, want)
	}
}

func TestSeriesNormalizedSearchHTTPOracle(t *testing.T) {
	c, store, _, _ := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one'),(2,'two','/two')`)
	for _, s := range []struct {
		id, lib     int
		name, title string
	}{
		{1, 1, "Ａ—Straße", ""}, {2, 2, "a strasse", ""},
		{3, 1, "Other", "E\u0301！"}, {4, 1, "Éclair", ""}, {5, 1, "Eclair", ""},
		{6, 1, "Ｃ＋＋", ""}, {7, 1, "C", ""}, {8, 1, "① Adventure", ""}, {9, 1, "1 Adventure", ""},
		{10, 1, "alpha", "beta"}, {11, 1, "进击的巨人", ""}, {12, 1, "进击 巨人", ""},
		{13, 1, "ß", ""}, {14, 1, "ßa", ""}, {15, 1, "孤 星", ""},
	} {
		normalizedExec(t, store, `INSERT INTO series(id,library_id,name,title,path) VALUES(?,?,?,?,?)`, s.id, s.lib, s.name, s.title, fmt.Sprintf("/series/%d", s.id))
		normalizedExec(t, store, `INSERT INTO books(id,series_id,library_id,name,path,size,file_modified_at) VALUES(?,?,?,'unrelated volume',?,1,CURRENT_TIMESTAMP)`, 100+s.id, s.id, s.lib, fmt.Sprintf("/books/%d", s.id))
	}
	f := normalizedSearchServer(t, c, store)
	for _, tc := range []struct {
		q             string
		global, local []int64
	}{
		{"A STRASSE", []int64{1, 2}, []int64{1}},
		{"ＳＴＲＡＳＳＥ", []int64{1, 2}, []int64{1}},
		{"É", []int64{3, 4}, []int64{3, 4}},
		{"e\u0301", []int64{3, 4}, []int64{3, 4}},
		{"eclair", []int64{5}, []int64{5}},
		{"C++", []int64{6}, []int64{6}},
		{"①", []int64{8}, []int64{8}},
		{"1", []int64{9}, []int64{9}},
		{"alphabeta", []int64{}, []int64{}},
		{"巨人进击", []int64{}, []int64{}},
		{"进击 巨人", []int64{12}, []int64{12}},
		{"孤", []int64{15}, []int64{15}},
		{"孤星", []int64{15}, []int64{15}},
		{"孤 \t—星", []int64{15}, []int64{15}},
		{"ß", []int64{1, 2, 13, 14}, []int64{1, 13, 14}},
		{"ßa", []int64{14}, []int64{14}},
		{" —！\t　", []int64{}, []int64{}},
		{"", []int64{}, []int64{}},
	} {
		t.Run(tc.q, func(t *testing.T) {
			wantSeries := []string{}
			wantAll := []string{}
			for _, id := range tc.global {
				wantSeries = append(wantSeries, fmt.Sprintf("s_%d", id))
				wantAll = append(wantAll, fmt.Sprintf("s_%d", id), fmt.Sprintf("b_%d", 100+id))
			}
			sort.Strings(wantSeries)
			sort.Strings(wantAll)
			normalizedEqual(t, f.global(t, tc.q, "series"), wantSeries)
			normalizedEqual(t, f.global(t, tc.q, "all"), wantAll)
			for _, scope := range []struct {
				lib  int
				want []int64
			}{{0, tc.global}, {1, tc.local}} {
				body := f.series(t, fmt.Sprintf("libraryId=%d&limit=100&sortBy=created_asc&q=%s", scope.lib, url.QueryEscape(tc.q)))
				// Empty raw input is the list's established unfiltered browse, unlike a normalized-empty nonempty query.
				if tc.q == "" {
					continue
				}
				ids := normalizedSeriesIDs(body)
				sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
				normalizedEqual(t, ids, scope.want)
				normalizedEqual(t, body.Total, len(scope.want))
			}
		})
	}
}

func (f *normalizedSearchHTTP) rebuild(t *testing.T) {
	t.Helper()
	f.request(t, "POST", "/api/system/rebuild-index")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var runs []RunSnapshot
		if err := json.Unmarshal(f.request(t, "GET", "/api/system/tasks?type=rebuild_index"), &runs); err != nil {
			t.Fatal(err)
		}
		for _, run := range runs {
			if run.Status == "completed" {
				return
			}
			if run.Status == "failed" || run.Status == "cancelled" {
				t.Fatalf("rebuild terminated: %+v", run)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("HTTP rebuild did not complete")
}

func TestSeriesNormalizedSearchHTTPLifecycleAndRepair(t *testing.T) {
	c, store, _, _ := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one'),(2,'two','/two')`)
	f := normalizedSearchServer(t, c, store)
	normalizedEqual(t, f.global(t, "strasse", "series"), []string{})
	normalizedExec(t, store, `INSERT INTO series(id,library_id,name,title,path) VALUES(21,1,'Ｓｔｒａ—ße','First！Title','/one/s')`)
	normalizedEqual(t, f.global(t, "strasse", "series"), []string{"s_21"})
	normalizedEqual(t, f.global(t, "firsttitle", "series"), []string{"s_21"})
	normalizedExec(t, store, `INSERT INTO books(id,series_id,library_id,name,path,size,file_modified_at) VALUES(121,21,1,'unrelated volume','/book121',1,CURRENT_TIMESTAMP)`)
	normalizedEqual(t, f.global(t, "strasse", "all"), []string{"b_121", "s_21"})
	normalizedExec(t, store, `UPDATE series SET name='Ｎｅｗ—Name',title='Ｅ́－Toile',library_id=2 WHERE id=21`)
	normalizedEqual(t, f.global(t, "strasse", "series"), []string{})
	normalizedEqual(t, f.global(t, "firsttitle", "series"), []string{})
	normalizedEqual(t, f.global(t, "newname", "series"), []string{"s_21"})
	normalizedEqual(t, f.global(t, "étoile", "series"), []string{"s_21"})
	normalizedEqual(t, normalizedSeriesIDs(f.series(t, "libraryId=1&q=newname")), []int64{})
	normalizedEqual(t, normalizedSeriesIDs(f.series(t, "libraryId=2&q=newname")), []int64{21})
	// Both candidate paths are damaged. A zero-result HTTP response must be
	// distinguishable from the independent expected set, not blessed as healthy.
	normalizedExec(t, store, `DELETE FROM series_search_fts WHERE rowid=21`)
	normalizedExec(t, store, `DELETE FROM series_gram_fts WHERE rowid=21`)
	for _, q := range []string{"newname", "ne"} {
		damaged := f.global(t, q, "series")
		if reflect.DeepEqual(damaged, []string{"s_21"}) {
			t.Fatalf("fault injection did not expose omission for %q", q)
		}
		normalizedEqual(t, damaged, []string{})
	}
	f.rebuild(t)
	for _, q := range []string{"newname", "ne", "étoile"} {
		normalizedEqual(t, f.global(t, q, "series"), []string{"s_21"})
	}
	normalizedEqual(t, normalizedSeriesIDs(f.series(t, "libraryId=2&q=newname")), []int64{21})
	normalizedEqual(t, f.global(t, "newname", "all"), []string{"b_121", "s_21"})
	normalizedEqual(t, f.global(t, "étoile", "all"), []string{"b_121", "s_21"})
	normalizedExec(t, store, `DELETE FROM series WHERE id=21`)
	normalizedEqual(t, f.global(t, "newname", "series"), []string{})
	normalizedEqual(t, f.global(t, "étoile", "series"), []string{})
	normalizedEqual(t, f.global(t, "newname", "all"), []string{})
	normalizedEqual(t, normalizedSeriesIDs(f.series(t, "libraryId=2&q=newname")), []int64{})
}

func TestSeriesNormalizedSearchHTTPFiltersBeforePagination(t *testing.T) {
	c, store, _, _ := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one'),(2,'two','/two')`)
	normalizedExec(t, store, `INSERT INTO series(id,library_id,name,path,status) VALUES
 (31,1,'Ａ—Match','/31','ongoing'),(32,2,'Ｂ—Match','/32','completed'),
 (33,1,'Ｃ—Match','/33','completed'),(34,1,'Ｄ—Match','/34','completed'),
 (35,1,'Ｅ—Other','/35','completed')`)
	f := normalizedSearchServer(t, c, store)
	// The matching but excluded records precede the desired pages in name order.
	base := "libraryId=1&q=" + url.QueryEscape("ｍａｔｃｈ") + "&status=completed&sortBy=name_asc&limit=1"
	first := f.series(t, base+"&page=1")
	normalizedEqual(t, normalizedSeriesIDs(first), []int64{33})
	normalizedEqual(t, first.Total, 2)
	if !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page: %+v", first)
	}
	second := f.series(t, base+"&page=2")
	normalizedEqual(t, normalizedSeriesIDs(second), []int64{34})
	normalizedEqual(t, second.Total, 2)
	if second.HasMore {
		t.Fatalf("offset final page has more: %+v", second)
	}
	cursor := f.series(t, base+"&page=2&cursor="+url.QueryEscape(first.NextCursor))
	normalizedEqual(t, normalizedSeriesIDs(cursor), []int64{34})
	if cursor.HasMore {
		t.Fatalf("cursor final page has more: %+v", cursor)
	}
}

func TestSeriesNormalizedSearchHTTPUpgradeV4(t *testing.T) {
	c, store, _, _ := newTestController(t)
	cfg := c.currentConfig()
	c.Close()
	// Install the actual old six triggers BEFORE inserting data. No normalized
	// projection or normalized index data is allowed to survive this fixture.
	for _, kind := range []string{"search", "gram"} {
		for _, suffix := range []string{"ai", "ad", "au"} {
			normalizedExec(t, store, "DROP TRIGGER trg_series_"+kind+"_fts_"+suffix)
		}
	}
	normalizedExec(t, store, `DROP TABLE series_search_text`)
	old, err := os.ReadFile("../database/testdata/series_search_v4.sql")
	if err != nil {
		t.Fatal(err)
	}
	normalizedExec(t, store, string(old))
	normalizedExec(t, store, `PRAGMA user_version=4`)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one')`)
	normalizedExec(t, store, `INSERT INTO series(id,library_id,name,title,path) VALUES
 (41,1,'Ａ—Straße','É！','/41'),(42,1,'Ｃ＋＋','','/42'),(43,1,'①','','/43')`)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(cfg.Database.Path); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(cfg.Database.Path); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.NewStore(cfg.Database.Path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	upgraded := newAPIFixture(reopened, scanner.NewScanner(reopened, c.config), c.config, c.configPath, defaultControllerCacheSizes())
	t.Cleanup(upgraded.Close)
	f := normalizedSearchServer(t, upgraded, reopened)
	for _, tc := range []struct {
		q    string
		want []string
		ids  []int64
	}{
		{"astrasse", []string{"s_41"}, []int64{41}}, {"ß", []string{"s_41"}, []int64{41}},
		{"é", []string{"s_41"}, []int64{41}}, {"C++", []string{"s_42"}, []int64{42}},
		{"1", []string{}, []int64{}}, {"①", []string{"s_43"}, []int64{43}},
	} {
		normalizedEqual(t, f.global(t, tc.q, "series"), tc.want)
		normalizedEqual(t, normalizedSeriesIDs(f.series(t, "libraryId=1&q="+url.QueryEscape(tc.q))), tc.ids)
	}
	normalizedExec(t, reopened, `UPDATE series SET name='Ｎｅｗ—Name',title=NULL WHERE id=41`)
	normalizedEqual(t, f.global(t, "newname", "series"), []string{"s_41"})
	normalizedEqual(t, f.global(t, "astrasse", "series"), []string{})
	f.rebuild(t)
	normalizedEqual(t, f.global(t, "newname", "series"), []string{"s_41"})
}

func TestSeriesNormalizedSearchHTTPProtocols(t *testing.T) {
	c, store, _, _ := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one'),(2,'two','/two')`)
	// Equal display names and equal normalized text make the existing protocol
	// name/ID tie-break deterministic, independently of FTS rank magnitudes.
	normalizedExec(t, store, `INSERT INTO series(id,library_id,name,path) VALUES
 (51,1,'Ａ—ß','/51'),(52,2,'Ａ—ß','/52'),(53,1,'Ａ—ß','/53'),
 (54,2,'Ａ—ß','/54'),(55,1,'Ａ—ß','/55'),(56,1,'xyz','/56')`)
	f := normalizedSearchServer(t, c, store)
	for _, q := range []string{"ａ ｓｓ", "ß", "Ａ"} {
		t.Run(q, func(t *testing.T) {
			for page, want := range [][]int64{{51, 52}, {53, 54}, {55}, {}} {
				path := fmt.Sprintf("/api/mihon/v1/series?q=%s&limit=2&page=%d", url.QueryEscape(q), page+1)
				var body MihonSeriesPageResponse
				if err := json.Unmarshal(f.request(t, "GET", path), &body); err != nil {
					t.Fatal(err)
				}
				ids := make([]int64, 0, len(body.Items))
				for _, row := range body.Items {
					ids = append(ids, row.ID)
				}
				normalizedEqual(t, ids, want)
				normalizedEqual(t, body.Total, int64(5))
				normalizedEqual(t, body.Limit, 2)
				normalizedEqual(t, body.Page, page+1)
				normalizedEqual(t, body.HasNext, page < 2)
				var feed OPDSFeed
				if err := xml.Unmarshal(f.request(t, "GET", fmt.Sprintf("/opds/v1.2/search?q=%s&limit=2&page=%d", url.QueryEscape(q), page+1)), &feed); err != nil {
					t.Fatal(err)
				}
				entries := make([]string, 0, len(feed.Entries))
				for _, entry := range feed.Entries {
					entries = append(entries, entry.ID)
				}
				expected := []string{}
				for _, id := range want {
					expected = append(expected, fmt.Sprintf("urn:manga-manager:opds:search:series:%d", id))
				}
				normalizedEqual(t, entries, expected)
				hasNext := false
				for _, link := range feed.Links {
					if link.Rel == "next" {
						hasNext = true
						u, err := url.Parse(link.Href)
						if err != nil {
							t.Fatal(err)
						}
						normalizedEqual(t, u.Query().Get("page"), fmt.Sprint(page+2))
						normalizedEqual(t, u.Query().Get("limit"), "2")
					}
				}
				// OPDS exposes total-dependent navigation, not a numeric total field.
				normalizedEqual(t, hasNext, page < 2)
			}
		})
	}
	// Library-scoped Mihon uses the filtered catalogue path, not cross-library relevance.
	for page, want := range [][]int64{{51, 53}, {55}, {}} {
		var scoped MihonSeriesPageResponse
		path := fmt.Sprintf("/api/mihon/v1/series?libraryId=1&q=%s&limit=2&page=%d", url.QueryEscape("ａ ｓｓ"), page+1)
		if err := json.Unmarshal(f.request(t, "GET", path), &scoped); err != nil {
			t.Fatal(err)
		}
		ids := make([]int64, 0, len(scoped.Items))
		for _, row := range scoped.Items {
			ids = append(ids, row.ID)
		}
		normalizedEqual(t, ids, want)
		normalizedEqual(t, scoped.Total, int64(3))
		normalizedEqual(t, scoped.HasNext, page == 0)
	}
	var capped MihonSeriesPageResponse
	if err := json.Unmarshal(f.request(t, "GET", "/api/mihon/v1/series?q=ass&limit=999"), &capped); err != nil {
		t.Fatal(err)
	}
	normalizedEqual(t, capped.Limit, 100)
	normalizedEqual(t, capped.Total, int64(5))
	var cappedFeed OPDSFeed
	if err := xml.Unmarshal(f.request(t, "GET", "/opds/v1.2/search?q=ass&limit=999"), &cappedFeed); err != nil {
		t.Fatal(err)
	}
	normalizedEqual(t, len(cappedFeed.Entries), 5)
	for _, link := range cappedFeed.Links {
		if link.Rel == "self" {
			u, err := url.Parse(link.Href)
			if err != nil {
				t.Fatal(err)
			}
			normalizedEqual(t, u.Query().Get("limit"), "100")
		}
	}
	for _, q := range []string{"—！", "1"} {
		var empty MihonSeriesPageResponse
		if err := json.Unmarshal(f.request(t, "GET", "/api/mihon/v1/series?q="+url.QueryEscape(q)), &empty); err != nil {
			t.Fatal(err)
		}
		normalizedEqual(t, empty.Total, int64(0))
		normalizedEqual(t, len(empty.Items), 0)
		normalizedEqual(t, empty.HasNext, false)
		var feed OPDSFeed
		if err := xml.Unmarshal(f.request(t, "GET", "/opds/v1.2/search?q="+url.QueryEscape(q)), &feed); err != nil {
			t.Fatal(err)
		}
		normalizedEqual(t, len(feed.Entries), 0)
	}
}
