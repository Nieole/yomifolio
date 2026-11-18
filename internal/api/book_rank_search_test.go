package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"yomifolio/internal/database"
	"yomifolio/internal/scanner"
)

func TestBookNormalizedHTTPOracle(t *testing.T) {
	c, store, _, _ := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one')`)
	normalizedExec(t, store, `INSERT INTO series(id,library_id,name,path) VALUES(1,1,'unrelated','/s')`)
	for _, row := range []struct {
		id          int
		name, title string
	}{
		{1, "Ａ—Straße", ""}, {2, "other", "E\u0301！"}, {3, "Ｃ＋＋", ""}, {4, "①", ""}, {5, "alpha", "beta"}, {6, "进击的巨人", ""}, {7, "孤 星", ""},
	} {
		normalizedExec(t, store, `INSERT INTO books(id,series_id,library_id,name,title,path,size,file_modified_at) VALUES(?,1,1,?,?,?,1,CURRENT_TIMESTAMP)`, row.id, row.name, row.title, fmt.Sprintf("/b%d", row.id))
	}
	f := normalizedSearchServer(t, c, store)
	for _, tc := range []struct {
		q   string
		ids []string
	}{
		{"A STRASSE", []string{"b_1"}}, {"ß", []string{"b_1"}}, {"é", []string{"b_2"}}, {"C++", []string{"b_3"}}, {"1", []string{}}, {"①", []string{"b_4"}}, {"alphabeta", []string{}}, {"进击 巨人", []string{}}, {"孤—星", []string{"b_7"}}, {"！—", []string{}},
	} {
		t.Run(tc.q, func(t *testing.T) { normalizedEqual(t, f.global(t, tc.q, "book"), tc.ids) })
	}
}

func rankedHTTP(t *testing.T, f *normalizedSearchHTTP, q, target string) SearchResult {
	t.Helper()
	var result SearchResult
	if err := json.Unmarshal(f.request(t, "GET", "/api/search?q="+url.QueryEscape(q)+"&target="+target), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestGlobalRankHTTPIndependentPrefix(t *testing.T) {
	c, store, _, _ := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one'),(2,'two','/two')`)
	// Exact keys tie by kind (book before series), then numeric persistent ID.
	normalizedExec(t, store, `INSERT INTO series(id,library_id,name,title,path) VALUES
 (1,1,'Ｎｅｅｄｌｅ','','/s1'),(2,2,'unrelated','','/s2'),
 (3,1,'NeedleX','','/s3'),(4,2,'xNeedle','','/s4'),(5,1,'NeedleXX','','/s5'),
 (6,2,'Needle','','/s6'),(7,1,'renamed','Needle','/s7')`)
	for _, b := range []struct {
		id, series  int
		name, title string
	}{
		{10, 2, "Needle", "Needle"}, {2, 2, "Ｎｅｅｄｌｅ", ""},
		{11, 1, "NeedleX", ""}, {12, 2, "xNeedle", ""}, {13, 2, "NeedleXX", ""},
		{14, 2, "unrelated", "Needle"},
	} {
		normalizedExec(t, store, `INSERT INTO books(id,series_id,library_id,name,title,path,size,file_modified_at) VALUES(?,?,1,?,?,?,1,CURRENT_TIMESTAMP)`, b.id, b.series, b.name, b.title, fmt.Sprintf("/b%d", b.id))
	}
	for i := 100; i < 140; i++ {
		normalizedExec(t, store, `INSERT INTO books(id,series_id,library_id,name,path,size,file_modified_at) VALUES(?,1,1,'volume',?,1,CURRENT_TIMESTAMP)`, i, fmt.Sprintf("/b%d", i))
	}
	f := normalizedSearchServer(t, c, store)
	want := []string{"b_2", "b_10", "b_14", "s_1", "s_6", "s_7", "b_11", "s_3", "b_13", "s_5", "b_12", "s_4"}
	for id := 100; id < 140; id++ {
		want = append(want, fmt.Sprintf("b_%d", id))
	}
	result := rankedHTTP(t, f, "needle", "all")
	ids := []string{}
	for i, hit := range result.Hits {
		ids = append(ids, hit.ID)
		score := 0.25
		if i < 6 {
			score = 1
		} else if i < 12 {
			score = 0.5
		}
		normalizedEqual(t, hit.Score, score)
	}
	normalizedEqual(t, ids, want[:20])
	for _, limit := range []int{1, 7, 20, 100} {
		walked := []string{}
		cursor := ""
		for page := 0; page < 100; page++ {
			batch := cursorPage(t, f, "needle", "all", limit, cursor)
			for _, hit := range batch.Hits {
				walked = append(walked, hit.ID)
			}
			if !batch.HasMore {
				break
			}
			cursor = batch.NextCursor
		}
		normalizedEqual(t, walked, want)
	}

	normalizedEqual(t, result.Total, uint64(20))
	normalizedEqual(t, result.MaxScore, float64(1))
	for _, target := range []string{"book", "title"} {
		r := rankedHTTP(t, f, "needle", target)
		normalizedEqual(t, len(r.Hits), 20)
		normalizedEqual(t, r.Hits[0].ID, "b_2")
	}
	// A batch consisting only of substring hits must not be scaled to 1.
	r := rankedHTTP(t, f, "eedl", "series")
	for _, hit := range r.Hits {
		normalizedEqual(t, hit.Score, 0.5)
	}
	normalizedEqual(t, r.MaxScore, 0.5)
	normalizedEqual(t, rankedHTTP(t, f, "missing", "all").MaxScore, float64(0))
}

func TestBookNormalizedHTTPLifecycleRepairAndLongMetadata(t *testing.T) {
	c, store, _, _ := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one')`)
	normalizedExec(t, store, `INSERT INTO series(id,library_id,name,path) VALUES(1,1,'unrelated','/s')`)
	f := normalizedSearchServer(t, c, store)
	// Use the scanner's existing metadata write entry, not a search-only writer.
	params := database.UpsertBookByPathParams{SeriesID: 1, LibraryID: 1, Name: "Ｓｔｒａ—ße", Path: "/long.cbz", FileModifiedAt: time.Now()}
	b, err := store.UpsertBookByPath(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("b_%d", b.ID)
	normalizedEqual(t, f.global(t, "strasse", "book"), []string{id})
	params.Name = "Ｎｅｗ—Name"
	params.Title = sql.NullString{String: "Needle" + strings.Repeat("界", 10000), Valid: true}
	if _, err := store.UpsertBookByPath(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	normalizedEqual(t, f.global(t, "strasse", "book"), []string{})
	normalizedEqual(t, f.global(t, "newname", "book"), []string{id})
	r := rankedHTTP(t, f, "needle", "book")
	normalizedEqual(t, len(r.Hits), 1)
	normalizedEqual(t, r.Hits[0].Score, 0.5)
	normalizedEqual(t, r.Hits[0].Fields.Title, params.Title.String)
	normalizedEqual(t, f.global(t, params.Title.String, "book"), []string{id})
	for _, table := range []string{"book_search_fts", "book_gram_fts"} {
		normalizedExec(t, store, "DELETE FROM "+table)
	}
	for _, q := range []string{"newname", "ne"} {
		normalizedEqual(t, f.global(t, q, "book"), []string{})
	}
	f.rebuild(t)
	for _, q := range []string{"newname", "ne", "needle"} {
		normalizedEqual(t, f.global(t, q, "book"), []string{id})
	}
	normalizedExec(t, store, `DELETE FROM books WHERE id=?`, b.ID)
	normalizedEqual(t, f.global(t, "newname", "book"), []string{})
	normalizedEqual(t, f.global(t, "needle", "book"), []string{})
}

func TestGlobalRankHTTPCountsCharactersAndChoosesBestField(t *testing.T) {
	c, store, _, _ := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one')`)
	normalizedExec(t, store, `INSERT INTO series(id,library_id,name,title,path) VALUES
 (1,1,'龙needle','','/s1'),(2,1,'abneedle','','/s2'),
 (3,1,'xneedle','needle龙','/s3'),(4,1,'needleXX','','/s4')`)
	f := normalizedSearchServer(t, c, store)
	r := rankedHTTP(t, f, "needle", "all")
	ids := []string{}
	for _, h := range r.Hits {
		ids = append(ids, h.ID)
	}
	normalizedEqual(t, ids, []string{"s_3", "s_4", "s_1", "s_2"})
}

func TestBookNormalizedHTTPUpgradeV4AndV5(t *testing.T) {
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			c, store, _, _ := newTestController(t)
			cfg := c.currentConfig()
			c.Close()
			for _, kind := range []string{"search", "gram"} {
				for _, suffix := range []string{"ai", "ad", "au"} {
					normalizedExec(t, store, "DROP TRIGGER trg_book_"+kind+"_fts_"+suffix)
				}
			}
			normalizedExec(t, store, `DROP TABLE book_search_text`)
			old, err := os.ReadFile("../database/testdata/book_search_v5.sql")
			if err != nil {
				t.Fatal(err)
			}
			normalizedExec(t, store, string(old))
			normalizedExec(t, store, fmt.Sprintf("PRAGMA user_version=%d", version))
			normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one')`)
			normalizedExec(t, store, `INSERT INTO series(id,library_id,name,path) VALUES(1,1,'unrelated','/s')`)
			normalizedExec(t, store, `INSERT INTO books(id,series_id,library_id,name,title,path,size,file_modified_at) VALUES
 (1,1,1,'Ａ—Straße','É！','/b1',1,CURRENT_TIMESTAMP),
 (2,1,1,'Ｃ＋＋',NULL,'/b2',1,CURRENT_TIMESTAMP),
 (3,1,1,'①',NULL,'/b3',1,CURRENT_TIMESTAMP)`)
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
				q   string
				ids []string
			}{
				{"astrasse", []string{"b_1"}}, {"ß", []string{"b_1"}}, {"é", []string{"b_1"}},
				{"C++", []string{"b_2"}}, {"①", []string{"b_3"}}, {"1", []string{}},
			} {
				normalizedEqual(t, f.global(t, tc.q, "book"), tc.ids)
			}
			normalizedExec(t, reopened, `UPDATE books SET name='Ｎｅｗ—Name',title=NULL WHERE id=1`)
			normalizedEqual(t, f.global(t, "newname", "book"), []string{"b_1"})
			normalizedEqual(t, f.global(t, "astrasse", "book"), []string{})
			f.rebuild(t)
			normalizedEqual(t, f.global(t, "newname", "book"), []string{"b_1"})
		})
	}
}
