package api

import (
	"os"
	"path/filepath"
	"testing"

	"yomifolio/internal/database"
)

// Independent post-maintenance identity audit of the persisted installation.
// This does not substitute for the HTTP completion/oracle in the maintenance run.
func TestSearchRealArchiveIdentityAudit(t *testing.T) {
	path, files := os.Getenv("SEARCH_REAL_VERIFY_DB"), os.Getenv("SEARCH_REAL_VERIFY_FILES")
	if path == "" || files == "" {
		t.Skip("opt-in persisted real archive audit")
	}
	store, err := database.NewStore(path)
	acceptanceCheck(t, err)
	defer store.Close()
	for _, table := range []string{"series", "books"} {
		rows, err := store.DB().Query("SELECT id,library_id,path FROM " + table + " ORDER BY id")
		acceptanceCheck(t, err)
		count := 0
		for rows.Next() {
			var id, lib int
			var path string
			acceptanceCheck(t, rows.Scan(&id, &lib, &path))
			count++
			normalizedEqual(t, id, count)
			normalizedEqual(t, lib, 1)
			expected := filepath.Join(files, acceptanceName(id, false))
			if table == "books" {
				expected = filepath.Join(files, acceptanceName((id-1)/10+1, false), acceptanceName(id, true)+".cbz")
			}
			normalizedEqual(t, path, expected)
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
		}
		acceptanceCheck(t, rows.Err())
		acceptanceCheck(t, rows.Close())
		want := 20000
		if table == "books" {
			want = 200000
		}
		normalizedEqual(t, count, want)
		t.Logf("audited %s: %d stable IDs, library and accessible exact paths", table, count)
	}
	var invalid int
	acceptanceCheck(t, store.DB().QueryRow(`SELECT count(*) FROM books WHERE series_id != ((id-1)/10)+1 OR library_id != 1`).Scan(&invalid))
	normalizedEqual(t, invalid, 0)
	t.Logf("all 200000 book-to-series identities preserved; db=%s", path)
}
