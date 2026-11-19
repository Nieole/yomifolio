package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"
)

// The agreed seam is the real maintenance HTTP launch and terminal run response.
// Removing an actual library root injects a disk failure, not a mocked scanner.
func TestSearchMaintenanceMissingLibraryFailsHTTP(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "missing-library", "Needle", "Needle.cbz", 1)
	var page bytes.Buffer
	acceptanceCheck(t, png.Encode(&page, image.NewRGBA(image.Rect(0, 0, 32, 32))))
	seedBookArchivePages(t, c, book, page.Bytes())
	f := normalizedSearchServer(t, c, store)
	acceptanceCheck(t, os.RemoveAll(lib.Path))
	f.request(t, "POST", "/api/system/rebuild-index")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var runs []RunSnapshot
		acceptanceCheck(t, json.Unmarshal(f.request(t, "GET", "/api/system/tasks?type=rebuild_index"), &runs))
		for _, run := range runs {
			switch run.Status {
			case "failed":
				if !strings.Contains(fmt.Sprint(run), "missing-library") {
					t.Fatalf("terminal failure lost library context: %+v", run)
				}
				return
			case "completed", "cancelled":
				t.Fatalf("missing library must fail maintenance, got %+v", run)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("maintenance did not reach terminal failure")
}

// A completed historical run must not satisfy the wait for a newly launched
// rebuild whose real library root has become inaccessible.
func TestSearchMaintenanceWaitIgnoresHistoricalTerminalHTTP(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "stale-terminal-library", "Needle", "Needle.cbz", 1)
	var page bytes.Buffer
	acceptanceCheck(t, png.Encode(&page, image.NewRGBA(image.Rect(0, 0, 32, 32))))
	seedBookArchivePages(t, c, book, page.Bytes())
	f := normalizedSearchServer(t, c, store)
	first := f.rebuild(t)
	acceptanceCheck(t, os.RemoveAll(lib.Path))
	second := f.rebuildTerminal(t, "failed")
	if second.RunID == 0 || second.RunID == first.RunID {
		t.Fatalf("wait returned historical run %d instead of new failed run: %+v", first.RunID, second)
	}
	if !strings.Contains(fmt.Sprint(second), "stale-terminal-library") {
		t.Fatalf("new failure lost library context: %+v", second)
	}
}

// Unlike synthetic performance paths, these archives are real and remain
// accessible throughout the complete asynchronous maintenance + forced scan.
func TestSearchFullMaintenanceWithRealArchiveHTTP(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, book := seedBookFixture(t, store, root, "real-library", "Ｎeedle—Series", "NeedleBook.cbz", 1)
	var page bytes.Buffer
	acceptanceCheck(t, png.Encode(&page, image.NewRGBA(image.Rect(0, 0, 32, 32))))
	seedBookArchivePages(t, c, book, page.Bytes())
	f := normalizedSearchServer(t, c, store)
	var firstRunID int64
	for pass := 0; pass < 2; pass++ {
		run := f.rebuild(t)
		if run.RunID == 0 || run.RunID == firstRunID {
			t.Fatalf("maintenance must complete a new run: first=%d, got %+v", firstRunID, run)
		}
		if pass == 0 {
			firstRunID = run.RunID
		}
		normalizedEqual(t, f.global(t, "needleseries", "series"), []string{fmt.Sprintf("s_%d", series.ID)})
		normalizedEqual(t, f.global(t, "needlebook", "book"), []string{fmt.Sprintf("b_%d", book.ID)})
		normalizedEqual(t, normalizedSeriesIDs(f.series(t, fmt.Sprintf("libraryId=%d&q=needleseries", lib.ID))), []int64{series.ID})
		var count int
		acceptanceCheck(t, store.DB().QueryRow(`SELECT count(*) FROM books WHERE library_id=?`, lib.ID).Scan(&count))
		normalizedEqual(t, count, 1)
	}
}
