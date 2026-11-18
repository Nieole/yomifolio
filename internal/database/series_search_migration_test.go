package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

//go:embed testdata/series_search_v4.sql
var seriesSearchV4Triggers string

func searchMigrationDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "search.db")
	if err := Migrate(path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mustSearchExec(t, db, `INSERT INTO libraries(id,name,path) VALUES(1,'one','/one'),(2,'two','/two')`)
	return path, db
}

func mustSearchExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func assertSearchProjection(t *testing.T, db *sql.DB, name, title string, library int) {
	t.Helper()
	var n, tt string
	var l int
	if err := db.QueryRow(`SELECT name,title,library_id FROM series_search_text WHERE series_id=1`).Scan(&n, &tt, &l); err != nil {
		t.Fatal(err)
	}
	if n != name || tt != title || l != library {
		t.Fatalf("projection = (%q,%q,%d), want (%q,%q,%d)", n, tt, l, name, title, library)
	}
	for _, table := range []string{"series_search_fts", "series_gram_fts"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table + ` WHERE rowid=1`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s row count = %d", table, count)
		}
	}
}

func TestSeriesNormalizedIndexLifecycle(t *testing.T) {
	_, db := searchMigrationDB(t)
	mustSearchExec(t, db, `INSERT INTO series(id,library_id,name,title,path) VALUES(1,1,' Ａ—Straße ','É！','/one/series')`)
	assertSearchProjection(t, db, "astrasse", "é", 1)
	mustSearchExec(t, db, `UPDATE series SET name='Ｃ＋＋', title=NULL, library_id=2 WHERE id=1`)
	assertSearchProjection(t, db, "c++", "", 2)
	if err := rebuildSeriesSearchIndexContext(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertSearchProjection(t, db, "c++", "", 2)
	mustSearchExec(t, db, `DELETE FROM series WHERE id=1`)
	for _, table := range []string{"series_search_text", "series_search_fts", "series_gram_fts"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s retains %d deleted rows", table, count)
		}
	}
}

func TestSeriesNormalizedUpgradeFromV4RetriesAfterFailure(t *testing.T) {
	path, db := searchMigrationDB(t)
	for _, kind := range []string{"search", "gram"} {
		for _, suffix := range []string{"ai", "ad", "au"} {
			mustSearchExec(t, db, "DROP TRIGGER trg_series_"+kind+"_fts_"+suffix)
		}
	}
	mustSearchExec(t, db, `DROP TABLE series_search_text`)
	mustSearchExec(t, db, seriesSearchV4Triggers)
	mustSearchExec(t, db, `PRAGMA user_version=4`)
	mustSearchExec(t, db, `INSERT INTO series(id,library_id,name,title,path) VALUES(1,1,'Ａ—Straße','É！','/one/series')`)
	// Unrelated derived data must not be rebackfilled for a series-only upgrade.
	mustSearchExec(t, db, `INSERT INTO tags(id,name,series_count) VALUES(1,'sentinel',73)`)
	mustSearchExec(t, db, `INSERT INTO books(id,series_id,library_id,name,path,size,file_modified_at) VALUES(1,1,1,'Old Book','/one/series/book.cbz',1,CURRENT_TIMESTAMP)`)
	mustSearchExec(t, db, `DELETE FROM book_search_fts`)
	mustSearchExec(t, db, seriesSearchProjectionDDL)
	mustSearchExec(t, db, `CREATE TRIGGER fail_search_backfill BEFORE INSERT ON series_search_text BEGIN SELECT RAISE(ABORT,'injected search failure'); END`)
	if err := Migrate(path); err == nil || !strings.Contains(err.Error(), "injected search failure") {
		t.Fatalf("migration error = %v, want injected search failure", err)
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 4 {
		t.Fatalf("failed migration marked version %d", version)
	}
	// The failed rebuild must not leave the original indexes empty.
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM series_search_fts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("failed rebuild lost old index: count=%d", count)
	}
	mustSearchExec(t, db, `DROP TRIGGER fail_search_backfill`)
	if err := Migrate(path); err != nil {
		t.Fatal(err)
	}
	assertSearchProjection(t, db, "astrasse", "é", 1)
	// Verify the backfill changed both actual candidate indexes, not only the projection.
	assertNormalizedCandidates(t, db)
	mustSearchExec(t, db, `UPDATE series SET name='Ａ—Straße', title='É！', library_id=2 WHERE id=1`)
	assertSearchProjection(t, db, "astrasse", "é", 2)
	assertNormalizedCandidates(t, db)
	if err := Migrate(path); err != nil {
		t.Fatal(err)
	}
	assertSearchProjection(t, db, "astrasse", "é", 2)
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("version=%d", version)
	}
	if err := db.QueryRow(`SELECT series_count FROM tags WHERE id=1`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 73 {
		t.Fatalf("unrelated tag backfill ran: %d", count)
	}
	if err := db.QueryRow(`SELECT count(*) FROM book_search_fts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("unrelated book rebuild ran")
	}
	// All six replaced definitions must handle deletion after the upgrade.
	mustSearchExec(t, db, `DELETE FROM series WHERE id=1`)
	for _, table := range []string{"series_search_text", "series_search_fts", "series_gram_fts"} {
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s retains deleted rows", table)
		}
	}
	// The upgraded insert triggers, not just update triggers, must be normalized.
	mustSearchExec(t, db, `INSERT INTO series(id,library_id,name,title,path) VALUES(1,1,'Ａ—Straße','É！','/one/new')`)
	assertSearchProjection(t, db, "astrasse", "é", 1)
	assertNormalizedCandidates(t, db)
}

func assertNormalizedCandidates(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, tc := range []struct{ table, match string }{
		{"series_search_fts", `"astrasse"`},
		{"series_gram_fts", `"6173"`}, // "as" as UTF-8 hex
		{"series_gram_fts", `"C3A9"`}, // composed é, not stripped e
	} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM `+tc.table+` WHERE `+tc.table+` MATCH ?`, tc.match).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s MATCH %q = %d, want 1", tc.table, tc.match, count)
		}
	}
}

func TestSeriesNormalizedMaintenanceRebuildRollsBackOnFailure(t *testing.T) {
	path, db := searchMigrationDB(t)
	mustSearchExec(t, db, `INSERT INTO series(id,library_id,name,title,path) VALUES(1,1,'Ａ—Straße','É！','/one/series')`)
	// Use the same public maintenance entrypoint as the host.
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mustSearchExec(t, db, `CREATE TRIGGER fail_search_rebuild BEFORE INSERT ON series_search_text BEGIN SELECT RAISE(ABORT,'injected maintenance failure'); END`)
	if err := store.RebuildSeriesSearchIndex(context.Background()); err == nil || !strings.Contains(err.Error(), "injected maintenance failure") {
		t.Fatalf("rebuild error = %v, want injected maintenance failure", err)
	}
	assertSearchProjection(t, db, "astrasse", "é", 1)
	assertNormalizedCandidates(t, db)
	mustSearchExec(t, db, `DROP TRIGGER fail_search_rebuild`)
	// Repair deliberate missing derived rows from the actual persisted facts.
	mustSearchExec(t, db, `DELETE FROM series_search_text`)
	mustSearchExec(t, db, `DELETE FROM series_search_fts`)
	mustSearchExec(t, db, `DELETE FROM series_gram_fts`)
	if err := store.RebuildSeriesSearchIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertSearchProjection(t, db, "astrasse", "é", 1)
	assertNormalizedCandidates(t, db)
}

func TestSeriesNormalizedWritesOnStorePooledConnections(t *testing.T) {
	path, db := searchMigrationDB(t)
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	var held []*sql.Conn
	defer func() {
		for _, c := range held {
			c.Close()
		}
	}()
	for i := range 4 {
		c, err := store.DB().Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
		if _, err := c.ExecContext(ctx, `INSERT INTO series(id,library_id,name,title,path) VALUES(?,1,'Ａ—Straße','É！',?)`, i+1, fmt.Sprintf("/one/series%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM series_search_text WHERE name='astrasse' AND title='é'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("normalized pooled writes = %d, want 4", count)
	}
}
