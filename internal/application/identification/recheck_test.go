// These cases pin the contradiction criterion of RecheckNewBooks: which ComicInfo series names
// count as naming the identified work. Loosened, wrong identifications stay hidden; tightened,
// every translated or re-punctuated series name sends the owner a false recheck.

package identification_test

import (
	"context"
	"path/filepath"
	"testing"

	"yomifolio/internal/application/identification"
	"yomifolio/internal/database"
)

func TestRecheckComparesTheSeriesNameWithEveryIdentifiedName(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "recheck.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	db := store.DB()
	admin, err := store.CreateUser(ctx, database.CreateUserParams{Username: "admin", PasswordHash: "unused", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	type names struct{ title, original, aliases string }
	cases := []struct {
		name        string
		conclusions []names
		clue        string
		want        string
	}{
		{"equal after folding case, width, spaces and punctuation", []names{{"进击的巨人", "Shingeki no Kyojin", "[]"}}, "ＳＨＩＮＧＥＫＩ-NO-KYOJIN", identification.StatusIdentified},
		{"an alias", []names{{"进击的巨人", "進撃の巨人", `["Attack on Titan"]`}}, "Attack on Titan", identification.StatusIdentified},
		{"any source's names", []names{{"怪物", "MONSTER", "[]"}, {"Pluto", "", "[]"}}, "PLUTO", identification.StatusIdentified},
		{"a name inside the series name", []names{{"Monster", "", "[]"}}, "Monster 完全版", identification.StatusIdentified},
		{"the series name inside a name", []names{{"Monster Perfect Edition", "", "[]"}}, "Monster", identification.StatusIdentified},
		{"another work", []names{{"怪物", "MONSTER", "[]"}}, "Pluto", identification.StatusNeedsRecheck},
		{"a single character is no containment", []names{{"K", "", "[]"}}, "Kamen", identification.StatusNeedsRecheck},
		{"conclusions without names", []names{{"", "", "[]"}}, "Pluto", identification.StatusIdentified},
		{"a series name of punctuation only", []names{{"Monster", "", "[]"}}, "--", identification.StatusIdentified},
		{"no conclusion", nil, "Pluto", identification.StatusUnprocessed},
	}
	service := identification.New(db, nil)
	for i, tc := range cases {
		series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: tc.name, Path: filepath.Join(lib.Path, string(rune('a'+i)))})
		if err != nil {
			t.Fatal(err)
		}
		for j, c := range tc.conclusions {
			if _, err := db.ExecContext(ctx, `INSERT INTO identification_conclusions (series_id, source, entry_id, title, original_title, aliases, confirmer, confirmed_at_ms)
				VALUES (?, ?, '1', ?, ?, ?, 'agent', 1)`, series.ID, identification.Sources[j], c.title, c.original, c.aliases); err != nil {
				t.Fatal(err)
			}
		}
		if err := service.RecheckNewBooks(ctx, []identification.NewBook{{SeriesID: series.ID, Name: "02.cbz", ComicInfoSeries: tc.clue}}); err != nil {
			t.Fatal(err)
		}
		got, err := service.Series(ctx, admin.ID, series.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != tc.want {
			t.Errorf("%s: status %s want %s", tc.name, got.Status, tc.want)
		}
	}
}
