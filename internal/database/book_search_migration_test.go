package database

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"testing"
)

//go:embed testdata/book_search_v5.sql
var bookSearchV5Triggers string

func TestBookNormalizedUpgradeRetriesFromV4AndV5(t *testing.T) {
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path, db := searchMigrationDB(t)
			mustSearchExec(t, db, `INSERT INTO series(id,library_id,name,path) VALUES(1,1,'unrelated','/s')`)
			// Restore the actual pre-normalization book triggers and remove all new data.
			for _, kind := range []string{"search", "gram"} {
				for _, suffix := range []string{"ai", "ad", "au"} {
					mustSearchExec(t, db, "DROP TRIGGER trg_book_"+kind+"_fts_"+suffix)
				}
			}
			mustSearchExec(t, db, `DROP TABLE book_search_text`)
			mustSearchExec(t, db, `DELETE FROM book_search_fts; DELETE FROM book_gram_fts`)
			mustSearchExec(t, db, bookSearchV5Triggers)
			mustSearchExec(t, db, fmt.Sprintf(`PRAGMA user_version=%d`, version))
			mustSearchExec(t, db, `INSERT INTO books(id,series_id,library_id,name,title,path,size,file_modified_at) VALUES(1,1,1,'Ａ—Straße','É！','/b',1,CURRENT_TIMESTAMP)`)
			mustSearchExec(t, db, bookSearchProjectionDDL)
			mustSearchExec(t, db, `CREATE TRIGGER fail_book_backfill BEFORE INSERT ON book_search_text BEGIN SELECT RAISE(ABORT,'injected book failure'); END`)
			if err := Migrate(path); err == nil || !strings.Contains(err.Error(), "injected book failure") {
				t.Fatalf("migration: %v", err)
			}
			var got int
			if err := db.QueryRow(`PRAGMA user_version`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != version {
				t.Fatalf("failed migration marked %d", got)
			}
			if err := db.QueryRow(`SELECT count(*) FROM book_search_fts`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != 1 {
				t.Fatal("failed rebuild lost old index")
			}
			mustSearchExec(t, db, `DROP TRIGGER fail_book_backfill`)
			if err := Migrate(path); err != nil {
				t.Fatal(err)
			}
			if err := Migrate(path); err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			check := func(q string, want int) {
				t.Helper()
				hits, err := store.SearchGlobalBooks(context.Background(), q, 20)
				if err != nil {
					t.Fatal(err)
				}
				if len(hits) != want {
					t.Fatalf("%q hits=%d want=%d", q, len(hits), want)
				}
			}
			for _, q := range []string{"astrasse", "as", "é"} {
				check(q, 1)
			}
			mustSearchExec(t, db, `UPDATE books SET name='Ｎｅｗ—Name',title=NULL,library_id=2 WHERE id=1`)
			check("newname", 1)
			check("astrasse", 0)
			check("é", 0)
			mustSearchExec(t, db, `CREATE TRIGGER fail_book_rebuild BEFORE INSERT ON book_search_text BEGIN SELECT RAISE(ABORT,'injected maintenance failure'); END`)
			if err := store.RebuildBookSearchIndex(context.Background()); err == nil {
				t.Fatal("expected maintenance failure")
			}
			check("newname", 1)
			mustSearchExec(t, db, `DROP TRIGGER fail_book_rebuild`)
			mustSearchExec(t, db, `DELETE FROM book_search_text; DELETE FROM book_search_fts; DELETE FROM book_gram_fts`)
			if err := store.RebuildBookSearchIndex(context.Background()); err != nil {
				t.Fatal(err)
			}
			check("newname", 1)
			check("ne", 1)
			mustSearchExec(t, db, `DELETE FROM books WHERE id=1`)
			for _, table := range []string{"book_search_text", "book_search_fts", "book_gram_fts"} {
				if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&got); err != nil {
					t.Fatal(err)
				}
				if got != 0 {
					t.Fatalf("%s retains rows", table)
				}
			}
			mustSearchExec(t, db, `INSERT INTO books(id,series_id,library_id,name,path,size,file_modified_at) VALUES(1,1,1,'Ａ—Straße','/new',1,CURRENT_TIMESTAMP)`)
			check("astrasse", 1)
		})
	}
}
