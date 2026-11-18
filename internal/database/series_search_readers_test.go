package database

import (
	"context"
	"testing"
)

func TestNormalizedSeriesReaders(t *testing.T) {
	store := newExecTxTestStore(t)
	ctx := context.Background()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{Name: "normalized", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"ONE PIECE", "Été", "ab", "Straße", "Ａ－Ｂ", "Σ猫", "C++", "cafe", "café"} {
		series, err := store.UpsertSeriesByPath(ctx, UpsertSeriesByPathParams{LibraryID: lib.ID, Name: name, Path: "/normalized/" + itoa(i)})
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.DB().ExecContext(ctx, `INSERT INTO books(series_id,library_id,name,path,size,file_modified_at) VALUES(?,?,?, ?,0,CURRENT_TIMESTAMP)`, series.ID, lib.ID, "unrelated", "/book/"+itoa(i))
		if err != nil {
			t.Fatal(err)
		}

	}
	for _, tc := range []struct {
		q    string
		want int
	}{{"One Piece", 1}, {"É", 2}, {"Ｏｎｅ－ＰＩＥＣＥ", 1}, {"STRASSE", 1}, {"ß", 1}, {"a - b", 2}, {"σ猫", 1}, {"ÉTÉ", 1}, {"cafe", 1}, {"café", 1}, {"---", 0}, {"C++", 1}} {
		t.Run(tc.q, func(t *testing.T) {
			hits, err := store.SearchGlobalSeries(ctx, tc.q, 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) != tc.want {
				t.Fatalf("global %q: got %d want %d", tc.q, len(hits), tc.want)
			}
			rows, total, err := store.SearchSeriesPaged(ctx, lib.ID, SeriesListFilters{Keyword: tc.q}, 100, 0, "name_asc")
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != tc.want || total != tc.want {
				t.Fatalf("list %q: rows=%d total=%d want=%d", tc.q, len(rows), total, tc.want)
			}
			protocol, total, err := store.SearchProtocolSeries(ctx, tc.q, 100, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(protocol) != tc.want || total != tc.want {
				t.Fatalf("protocol %q: rows=%d total=%d want=%d", tc.q, len(protocol), total, tc.want)
			}

			books, err := store.SearchGlobalBooks(ctx, tc.q, 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(books) != tc.want {
				t.Fatalf("books via series %q: got=%d want=%d", tc.q, len(books), tc.want)
			}

			count, err := store.CountMihonSeries(ctx, CountMihonSeriesParams{LibraryID: lib.ID, Query: tc.q})
			if err != nil {
				t.Fatal(err)
			}
			if int(count) != tc.want {
				t.Fatalf("Mihon count %q: got=%d want=%d", tc.q, count, tc.want)
			}
			mihon, err := store.ListMihonSeries(ctx, ListMihonSeriesParams{LibraryID: lib.ID, Query: tc.q, Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			if len(mihon) != tc.want {
				t.Fatalf("Mihon list %q: got=%d want=%d", tc.q, len(mihon), tc.want)
			}
			updated, err := store.ListMihonSeriesByUpdated(ctx, ListMihonSeriesByUpdatedParams{LibraryID: lib.ID, Query: tc.q, Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			if len(updated) != tc.want {
				t.Fatalf("Mihon updated %q: got=%d want=%d", tc.q, len(updated), tc.want)
			}
			byBooks, err := store.ListMihonSeriesByBooks(ctx, ListMihonSeriesByBooksParams{LibraryID: lib.ID, Query: tc.q, Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			if len(byBooks) != tc.want {
				t.Fatalf("Mihon books %q: got=%d want=%d", tc.q, len(byBooks), tc.want)
			}

		})
	}
}

func TestNormalizedSeriesEligibilityBeforePagination(t *testing.T) {
	store := newExecTxTestStore(t)
	ctx := context.Background()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{Name: "page", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range []struct{ name, title, status string }{
		{"a", "bc", "ongoing"}, {"abc decoy", "", "ended"},
		{"abc first", "", "ongoing"}, {"abc second", "", "ongoing"},
		{"unrelated", "Ａ－ＢＣ title", "ongoing"},
	} {
		series, err := store.UpsertSeriesByPath(ctx, UpsertSeriesByPathParams{LibraryID: lib.ID, Name: item.name, Path: "/page/" + itoa(i)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.DB().ExecContext(ctx, `UPDATE series SET title=?,status=? WHERE id=?`, item.title, item.status, series.ID); err != nil {
			t.Fatal(err)
		}
	}
	f := SeriesListFilters{Keyword: "a-b-c", Status: "ongoing"}
	rows, total, err := store.SearchSeriesPaged(ctx, lib.ID, f, 1, 1, "name_asc")
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(rows) != 1 || rows[0].Name != "abc second" {
		t.Fatalf("filtered page: total=%d rows=%+v", total, rows)
	}
	rows, cursor, more, err := store.SearchSeriesCursor(ctx, lib.ID, f, 1, "name_asc", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "abc first" || !more {
		t.Fatalf("cursor first: %+v more=%v", rows, more)
	}
	rows, _, _, err = store.SearchSeriesCursor(ctx, lib.ID, f, 1, "name_asc", cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "abc second" {
		t.Fatalf("cursor second: %+v", rows)
	}
	protocol, n, err := store.SearchProtocolSeries(ctx, "a-b-c", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 || len(protocol) != 1 {
		t.Fatalf("protocol page total=%d rows=%+v", n, protocol)
	}
}

func TestBookOwnSearchUsesNormalizedFields(t *testing.T) {
	store := newExecTxTestStore(t)
	ctx := context.Background()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{Name: "own", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.UpsertSeriesByPath(ctx, UpsertSeriesByPathParams{LibraryID: lib.ID, Name: "unrelated", Path: "/own"})
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"É", "A-B", "Straße", "ＡＢ"} {
		if _, err := store.DB().ExecContext(ctx, `INSERT INTO books(series_id,library_id,name,path,size,file_modified_at) VALUES(?,?,?,?,0,CURRENT_TIMESTAMP)`, series.ID, lib.ID, name, "/own/"+itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		q    string
		want int
	}{{"É", 1}, {"é", 1}, {"AB", 2}, {"A-B", 2}, {"STRASSE", 1}, {"ＡＢ", 2}} {
		hits, err := store.SearchGlobalBooks(ctx, tc.q, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != tc.want {
			t.Errorf("own %q: got=%d want=%d", tc.q, len(hits), tc.want)
		}
	}
}
