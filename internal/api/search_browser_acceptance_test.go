package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"yomifolio/internal/database"
)

// TestSearchAcceptanceBrowser serves the built application and real authenticated
// SQLite HTTP routes for the external Playwright interaction runner. Opt-in only.
func TestSearchAcceptanceBrowser(t *testing.T) {
	ready := os.Getenv("SEARCH_BROWSER_READY")
	if ready == "" {
		t.Skip("opt-in real browser fixture")
	}
	c, store, _, root := newTestController(t)
	normalizedExec(t, store, `INSERT INTO libraries(id,name,path) VALUES(1,'Browser library','/browser')`)
	for id := 1; id <= 45; id++ {
		normalizedExec(t, store, `INSERT INTO series(id,library_id,name,path) VALUES(?,1,?,?)`, id, fmt.Sprintf("Browser Needle %02d", id), fmt.Sprintf("/browser/s%d", id))
		normalizedExec(t, store, `INSERT INTO books(id,series_id,library_id,name,path,size,file_modified_at) VALUES(?,?,1,?,?,1,CURRENT_TIMESTAMP)`, id, id, fmt.Sprintf("Browser Needle Book %02d", id), fmt.Sprintf("/browser/b%d.cbz", id))
	}
	longLibrary, longSeries, longBook := seedBookFixture(t, store, root, "browser-real", "Long Needle", "Readable.cbz", 1)
	_ = longLibrary
	normalizedExec(t, store, `UPDATE series SET title=? WHERE id=?`, "Long Needle "+strings.Repeat("界Wide", 600), longSeries.ID)
	var page bytes.Buffer
	acceptanceCheck(t, png.Encode(&page, image.NewRGBA(image.Rect(0, 0, 32, 32))))
	seedBookArchivePages(t, c, longBook, page.Bytes())
	mkTestUser(t, store, "browser-admin", database.RoleAdmin)
	mkTestUser(t, store, "browser-reader", database.RoleRegular)
	router := chi.NewRouter()
	c.SetupRoutes(router)
	dist, err := filepath.Abs(os.Getenv("SEARCH_BROWSER_DIST"))
	acceptanceCheck(t, err)
	router.Handle("/*", http.FileServer(http.Dir(dist)))
	server := httptest.NewServer(router)
	defer func() {
		c.ShutdownNotify()
		server.CloseClientConnections()
		server.Close()
	}()
	payload, _ := json.Marshal(map[string]string{"url": server.URL, "username": "browser-admin", "password": "password1", "reader": "browser-reader", "series": fmt.Sprint(longSeries.ID), "book": fmt.Sprint(longBook.ID)})
	acceptanceCheck(t, os.WriteFile(ready, payload, 0644))
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready + ".done"); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("browser runner did not signal completion")
}
