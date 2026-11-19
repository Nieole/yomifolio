package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/parser"
	"yomifolio/internal/scanner"
)

// Opt-in complete maintenance at the agreed HTTP seam. The frozen mother is
// never opened for writes. Every catalogue path is backed by an accessible CBZ.
func TestSearchLargeRealMaintenanceHTTP(t *testing.T) {
	input, root := os.Getenv("SEARCH_REAL_INPUT"), os.Getenv("SEARCH_REAL_ROOT")
	if input == "" || root == "" {
		t.Skip("opt-in 200000 real archives")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("real archive destination must be new")
	}
	acceptanceCheck(t, os.MkdirAll(root, 0755))
	path := filepath.Join(root, "catalogue.db")
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
	var page, archive bytes.Buffer
	acceptanceCheck(t, png.Encode(&page, image.NewRGBA(image.Rect(0, 0, 32, 32))))
	z := zip.NewWriter(&archive)
	entry, err := z.Create("001.png")
	acceptanceCheck(t, err)
	_, err = entry.Write(page.Bytes())
	acceptanceCheck(t, err)
	acceptanceCheck(t, z.Close())
	template := filepath.Join(root, "template.cbz")
	acceptanceCheck(t, os.WriteFile(template, archive.Bytes(), 0644))
	library := filepath.Join(root, "library")
	existingFiles := os.Getenv("SEARCH_REAL_FILES")
	if existingFiles != "" {
		library = existingFiles
	}
	acceptanceCheck(t, os.MkdirAll(library, 0755))
	start := time.Now()
	tx, err := store.DB().Begin()
	acceptanceCheck(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`UPDATE libraries SET path=? WHERE id=1`, library)
	acceptanceCheck(t, err)
	for sid := 1; sid <= 20000; sid++ {
		dir := filepath.Join(library, acceptanceName(sid, false))
		acceptanceCheck(t, os.MkdirAll(dir, 0755))
		_, err = tx.Exec(`UPDATE series SET path=? WHERE id=?`, dir, sid)
		acceptanceCheck(t, err)
		for j := 1; j <= 10; j++ {
			bid := (sid-1)*10 + j
			// NTFS limits hard links to one inode; use disjoint 1000-link
			// template groups without changing any catalogue identity/path.
			if (bid-1)%1000 == 0 {
				template = filepath.Join(root, fmt.Sprintf("template-%d.cbz", (bid-1)/1000))
				acceptanceCheck(t, os.WriteFile(template, archive.Bytes(), 0644))
			}
			file := filepath.Join(dir, acceptanceName(bid, true)+".cbz")
			if existingFiles == "" {
				acceptanceCheck(t, os.Link(template, file))
			} else {
				_, err = os.Stat(file)
				acceptanceCheck(t, err)
			}
			_, err = tx.Exec(`UPDATE books SET path=? WHERE id=?`, file, bid)
			acceptanceCheck(t, err)
		}
	}
	acceptanceCheck(t, tx.Commit())
	t.Logf("prepared 200000 accessible valid CBZ / 20000 directories in %s; archive bytes=%d reused_files=%q", time.Since(start), archive.Len(), existingFiles)
	cfg := &config.Config{}
	cfg.Database.Path = path
	cfg.Cache.Dir = filepath.Join(root, "cache")
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Scanner.ArchivePoolSize = 5
	cfg.Scanner.ScanProfile = os.Getenv("SEARCH_REAL_PROFILE")
	config.NormalizeConfig(cfg)
	acceptanceCheck(t, os.MkdirAll(cfg.Cache.Dir, 0755))
	manager := config.NewManager(cfg)
	parser.InitPool(5)
	defer parser.ResetArchivePool()
	c := newAPIFixture(store, scanner.NewScanner(store, manager), manager, filepath.Join(root, "config.yaml"), defaultControllerCacheSizes())
	defer c.Close()
	c.runBackground(func() { c.sse.run(c.lifecycleDone()) })
	f := normalizedSearchServer(t, c, store)
	start = time.Now()
	f.request(t, "POST", "/api/system/rebuild-index")
	deadline := time.Now().Add(60 * time.Minute)
	completed := false
	for time.Now().Before(deadline) && !completed {
		var runs []RunSnapshot
		acceptanceCheck(t, json.Unmarshal(f.request(t, "GET", "/api/system/tasks?type=rebuild_index"), &runs))
		for _, run := range runs {
			if run.Status == "completed" {
				completed = true
			}
			if run.Status == "failed" || run.Status == "cancelled" {
				t.Fatalf("complete real maintenance failed: %+v", run)
			}
		}
		if !completed {
			time.Sleep(time.Second)
		}
	}
	if !completed {
		t.Fatal("full real maintenance did not complete")
	}
	t.Logf("HTTP full maintenance elapsed=%s scan_profile=%s", time.Since(start), cfg.Scanner.ScanProfile)
	for table, want := range map[string]int{"books": 200000, "series": 20000} {
		var count int
		acceptanceCheck(t, store.DB().QueryRow("SELECT count(*) FROM "+table).Scan(&count))
		normalizedEqual(t, count, want)
	}
	// Path and ID identity must survive, not merely aggregate counts.
	rows, err := store.DB().Query(`SELECT id,path FROM books ORDER BY id`)
	acceptanceCheck(t, err)
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id int
		var got string
		acceptanceCheck(t, rows.Scan(&id, &got))
		sid := (id-1)/10 + 1
		want := filepath.Join(library, acceptanceName(sid, false), acceptanceName(id, true)+".cbz")
		normalizedEqual(t, got, want)
		if _, err := os.Stat(got); err != nil {
			t.Fatal(err)
		}
		count++
	}
	acceptanceCheck(t, rows.Err())
	acceptanceCheck(t, rows.Close())
	normalizedEqual(t, count, 200000)
	expected := map[string]bool{}
	for sid := 1; sid <= 20000; sid++ {
		if sid%1000 == 0 {
			expected[fmt.Sprintf("s_%d", sid)] = true
		}
		for j := 1; j <= 10; j++ {
			bid := (sid-1)*10 + j
			if sid%1000 == 0 || bid%1000 == 0 {
				expected[fmt.Sprintf("b_%d", bid)] = true
			}
		}
	}
	cursor := ""
	seen := map[string]bool{}
	for {
		r := cursorPage(t, f, "孤星", "all", 100, cursor)
		for _, h := range r.Hits {
			if !expected[h.ID] || seen[h.ID] {
				t.Fatalf("oracle unexpected/duplicate %s", h.ID)
			}
			seen[h.ID] = true
		}
		if !r.HasMore {
			break
		}
		cursor = r.NextCursor
	}
	normalizedEqual(t, len(seen), len(expected))
	ids := normalizedSeriesIDs(f.series(t, "libraryId=1&q=%E5%AD%A4%E6%98%9F"))
	normalizedEqual(t, len(ids), 20)
	t.Logf("post-scan identities/files=%d; independent full HTTP sparse oracle=%d; library series oracle=20", count, len(seen))
}
