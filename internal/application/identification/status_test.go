// The status view is the only definition of the identification status, and later use cases
// write the facts it reads before any of them can be driven end to end. These cases pin its
// precedence so a writer added later cannot silently reorder what the owner and agents see.

package identification_test

import (
	"context"
	"path/filepath"
	"testing"

	"yomifolio/internal/application/identification"
	"yomifolio/internal/database"
)

func TestStatusPrecedence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "status.db")
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
	facts := map[string]string{
		"conclusion":     `INSERT INTO identification_conclusions (series_id, source, entry_id, confirmer, confirmed_at_ms) VALUES (?, 'bangumi', '1', 'agent', 1)`,
		"not found":      `INSERT INTO identification_not_found (series_id, searched_sources, confirmer, concluded_at_ms) VALUES (?, '["bangumi"]', 'agent', 1)`,
		"identification": `INSERT INTO identification_escalations (series_id, category, class, raised_by, raised_at_ms) VALUES (?, 'ambiguous', 'identification', 'agent', 1)`,
		"recheck":        `INSERT INTO identification_escalations (series_id, category, class, raised_by, raised_at_ms) VALUES (?, 'contradiction', 'recheck', 'system', 1)`,
		"note":           `INSERT INTO identification_escalations (series_id, category, class, raised_by, raised_at_ms) VALUES (?, 'duplicate', 'note', 'agent', 1)`,
		"resolved":       `INSERT INTO identification_escalations (series_id, category, class, raised_by, raised_at_ms, resolved_at_ms) VALUES (?, 'ambiguous', 'identification', 'agent', 1, 2)`,
		"owner reviewed": `INSERT INTO identification_owner_reviews (series_id, user_id, reviewed_at_ms) VALUES (?, 1, 1)`,
	}
	cases := []struct {
		facts []string
		want  string
	}{
		{nil, identification.StatusUnprocessed},
		{[]string{"note"}, identification.StatusUnprocessed},
		{[]string{"resolved"}, identification.StatusUnprocessed},
		{[]string{"not found"}, identification.StatusNotFound},
		{[]string{"conclusion"}, identification.StatusIdentified},
		{[]string{"conclusion", "not found"}, identification.StatusIdentified},
		{[]string{"conclusion", "note"}, identification.StatusIdentified},
		{[]string{"identification"}, identification.StatusAwaitingOwner},
		{[]string{"not found", "identification"}, identification.StatusAwaitingOwner},
		{[]string{"conclusion", "identification"}, identification.StatusAwaitingOwner},
		{[]string{"conclusion", "recheck"}, identification.StatusNeedsRecheck},
		{[]string{"conclusion", "recheck", "identification"}, identification.StatusNeedsRecheck},
		{[]string{"owner reviewed"}, identification.StatusOwnerReviewed},
		{[]string{"conclusion", "recheck", "identification", "not found", "owner reviewed"}, identification.StatusOwnerReviewed},
	}
	service := identification.New(db, nil)
	for i, tc := range cases {
		series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "series", Path: filepath.Join(lib.Path, string(rune('a'+i)))})
		if err != nil {
			t.Fatal(err)
		}
		for _, fact := range tc.facts {
			if _, err := db.ExecContext(ctx, facts[fact], series.ID); err != nil {
				t.Fatalf("%s: %v", fact, err)
			}
		}
		got, err := service.Series(ctx, admin.ID, series.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != tc.want {
			t.Errorf("facts %v: status %s want %s", tc.facts, got.Status, tc.want)
		}
	}
}
