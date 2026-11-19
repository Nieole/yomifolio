package api

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/scanner"
)

// Installation data is produced by separately compiled archived releases, not
// by resetting user_version on a current schema. Only a private copy is opened.
func TestSearchArchivedUpgradeHTTPMatrix(t *testing.T) {
	input := os.Getenv("SEARCH_UPGRADE_HTTP_INPUT")
	if input == "" {
		t.Skip("opt-in archived installation")
	}
	root := t.TempDir()
	path := filepath.Join(root, "upgrade.db")
	src, err := os.Open(input)
	acceptanceCheck(t, err)
	dst, err := os.Create(path)
	acceptanceCheck(t, err)
	_, err = io.Copy(dst, src)
	acceptanceCheck(t, err)
	acceptanceCheck(t, src.Close())
	acceptanceCheck(t, dst.Close())
	acceptanceCheck(t, database.Migrate(path))
	store, err := database.NewStore(path)
	acceptanceCheck(t, err)
	defer store.Close()
	cfg := &config.Config{}
	cfg.Database.Path = path
	cfg.Cache.Dir = filepath.Join(root, "cache")
	config.NormalizeConfig(cfg)
	manager := config.NewManager(cfg)
	c := newAPIFixture(store, scanner.NewScanner(store, manager), manager, filepath.Join(root, "config.yaml"), defaultControllerCacheSizes())
	defer c.Close()
	f := normalizedSearchServer(t, c, store)
	adminPage := cursorPage(t, f, "星", "series", 1, "")
	cursorStatus(t, f, "/api/search?q=星&target=series&cursor="+adminPage.NextCursor, nil, 401, "")
	for _, role := range []string{database.RoleAdmin, database.RoleRegular} {
		normalizedExec(t, store, `UPDATE users SET role=? WHERE username='search-admin'`, role)
		if role == database.RoleRegular {
			cursorStatus(t, f, "/api/search?q=星&target=series&cursor="+adminPage.NextCursor, f.cookie, 400, "invalid_search_cursor")
		}
		for _, query := range []string{"星", "孤星", "孤星Atlas", "孤—星 Ａｔｌａｓ"} {
			oracleQuery := query
			if query == "孤—星 Ａｔｌａｓ" {
				oracleQuery = "孤星Atlas"
			}
			for _, target := range []string{"all", "series", "book", "title"} {
				keys := map[string]acceptanceOracleKey{}
				for sid := 1; sid <= 20000; sid++ {
					sk, sm := acceptanceOracleRank(acceptanceName(sid, false), oracleQuery, 1, int64(sid))
					if sm && target != "book" && target != "title" {
						keys[fmt.Sprintf("s_%d", sid)] = sk
					}
					if target == "series" {
						continue
					}
					for j := 1; j <= 10; j++ {
						bid := (sid-1)*10 + j
						bk, bm := acceptanceOracleRank(acceptanceName(bid, true), oracleQuery, 0, int64(bid))
						if !bm && sm {
							bk = sk
							bk.tier = 2
							bk.kind = 0
							bk.id = int64(bid)
						}
						if bm || sm {
							keys[fmt.Sprintf("b_%d", bid)] = bk
						}
					}
				}
				want := []string{}
				for id := range keys {
					want = append(want, id)
				}
				sort.Slice(want, func(i, j int) bool { return keys[want[i]].less(keys[want[j]]) })
				got := []string{}
				cursor := ""
				for {
					r := cursorPage(t, f, query, target, 100, cursor)
					for _, hit := range r.Hits {
						got = append(got, hit.ID)
					}
					if !r.HasMore {
						break
					}
					cursor = r.NextCursor
				}
				normalizedEqual(t, got, want)
			}
			for _, lib := range []int{1, 999} {
				want := []int64{}
				if lib == 1 {
					for sid := 1000; sid <= 20000; sid += 1000 {
						want = append(want, int64(sid))
					}
				}
				for _, order := range []string{"name_asc", "name_desc"} {
					ordered := append([]int64{}, want...)
					if order == "name_desc" {
						sort.Slice(ordered, func(i, j int) bool { return ordered[i] > ordered[j] })
					}
					body := f.series(t, fmt.Sprintf("libraryId=%d&q=%s&sortBy=%s&limit=100", lib, url.QueryEscape(query), order))
					normalizedEqual(t, normalizedSeriesIDs(body), ordered)
					normalizedEqual(t, body.Total, len(want))
				}
				for page := 1; page <= 4; page++ {
					start := min((page-1)*7, len(want))
					end := min(start+7, len(want))
					expected := want[start:end]
					var mihon MihonSeriesPageResponse
					acceptanceCheck(t, json.Unmarshal(f.request(t, "GET", fmt.Sprintf("/api/mihon/v1/series?libraryId=%d&q=%s&limit=7&page=%d", lib, url.QueryEscape(query), page)), &mihon))
					ids := []int64{}
					for _, row := range mihon.Items {
						ids = append(ids, row.ID)
					}
					normalizedEqual(t, ids, expected)
					normalizedEqual(t, mihon.Total, int64(len(want)))
					normalizedEqual(t, mihon.HasNext, end < len(want))
					// Cross-library OPDS has the same 20-series independent expected set.
					if lib == 1 {
						var feed OPDSFeed
						acceptanceCheck(t, xml.Unmarshal(f.request(t, "GET", fmt.Sprintf("/opds/v1.2/search?q=%s&limit=7&page=%d", url.QueryEscape(query), page)), &feed))
						entries := []string{}
						for _, entry := range feed.Entries {
							entries = append(entries, entry.ID)
						}
						wantEntries := []string{}
						for _, id := range expected {
							wantEntries = append(wantEntries, fmt.Sprintf("urn:manga-manager:opds:search:series:%d", id))
						}
						normalizedEqual(t, entries, wantEntries)
						hasNext := false
						for _, link := range feed.Links {
							if link.Rel == "next" {
								hasNext = true
							}
						}
						normalizedEqual(t, hasNext, end < len(want))
					}
				}
			}
			t.Logf("archive HTTP role=%s %q: all/series/book/title full ordered oracle; library 1/999 asc/desc; Mihon+OPDS 4 pages and totals", role, query)
		}
	}
	normalizedExec(t, store, `UPDATE series SET status='completed' WHERE id%2000=0`)
	filtered := f.series(t, "libraryId=1&q=%E5%AD%A4%E6%98%9F&status=completed&sortBy=name_asc&limit=100")
	wantFiltered := []int64{}
	for id := 2000; id <= 20000; id += 2000 {
		wantFiltered = append(wantFiltered, int64(id))
	}
	normalizedEqual(t, normalizedSeriesIDs(filtered), wantFiltered)
	normalizedEqual(t, filtered.Total, 10)
	regularPage := cursorPage(t, f, "星", "series", 1, "")
	normalizedExec(t, store, `UPDATE users SET must_change_password=1 WHERE username='search-admin'`)
	cursorStatus(t, f, "/api/search?q=星&target=series&cursor="+regularPage.NextCursor, f.cookie, 403, "")
}
