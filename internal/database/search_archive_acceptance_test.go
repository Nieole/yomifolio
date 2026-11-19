package database

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// Shared unchanged with independent release archives by Go overlay. Never opens
// the input installation for writes; output must be a new standalone database.
func TestSearchArchiveUpgrade(t *testing.T) {
	input, output := os.Getenv("SEARCH_ARCHIVE_INPUT"), os.Getenv("SEARCH_ARCHIVE_OUTPUT")
	if input == "" || output == "" {
		t.Skip("opt-in independent release upgrade")
	}
	src, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err = dst.Close(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("SEARCH_ARCHIVE_FAIL_BOOK") == "1" {
		store, err := NewStore(output)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		_, err = store.DB().Exec(`CREATE TABLE IF NOT EXISTS book_search_text (book_id INTEGER PRIMARY KEY,series_id INTEGER NOT NULL,library_id INTEGER NOT NULL,name TEXT NOT NULL,title TEXT NOT NULL); CREATE TRIGGER acceptance_fail_book BEFORE INSERT ON book_search_text BEGIN SELECT RAISE(ABORT,'archive injected backfill failure'); END`)
		if err != nil {
			t.Fatal(err)
		}
		if err := Migrate(output); err == nil || !strings.Contains(err.Error(), "archive injected backfill failure") {
			t.Fatalf("expected injected failure, got %v", err)
		}
		var version int
		if err := store.DB().QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
			t.Fatal(err)
		}
		if version != 5 {
			t.Fatalf("failed upgrade advanced version to %d", version)
		}
		t.Log("real v5 backfill failure left version=5; removing fault and retrying")
		if _, err := store.DB().Exec(`DROP TRIGGER acceptance_fail_book`); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for pass := 0; pass < 2; pass++ {
		start := time.Now()
		if err = Migrate(output); err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(start)
		store, err := NewStore(output)
		if err != nil {
			t.Fatal(err)
		}
		var version, books, series int
		for query, dest := range map[string]*int{"PRAGMA user_version": &version, "SELECT count(*) FROM books": &books, "SELECT count(*) FROM series": &series} {
			if err = store.DB().QueryRow(query).Scan(dest); err != nil {
				t.Fatal(err)
			}
		}
		if books != 200000 || series != 20000 {
			t.Fatalf("lost catalogue: %d/%d", books, series)
		}
		if _, err = store.DB().Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			t.Fatal(err)
		}
		if err = store.Close(); err != nil {
			t.Fatal(err)
		}
		t.Logf("pass=%d version=%d books=%d series=%d migrate=%s", pass, version, books, series, elapsed)
	}
}
