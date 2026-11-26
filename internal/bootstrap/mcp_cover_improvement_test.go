// These tests guard the cover improvement primitive: an agent replaces only an unlocked,
// archive-generated cover with a page of the same series, the switch and its receipt commit
// together, and the receipt is read, undone and cleaned up with the existing operation tools.

package bootstrap_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/config"
	"yomifolio/internal/covermaterial"
	"yomifolio/internal/database"
)

// coverWorld is one series whose single two-page book shows a red first page as its automatic
// cover and a blue second page an agent may prefer.
type coverWorld struct {
	t       *testing.T
	cfg     *config.Manager
	store   database.Store
	web     *identificationWeb
	agent   *mainLoopAgent
	library database.Library
	series  int64
	book    int64
	cover   string
}

func solidPNG(t *testing.T, c color.Color) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 64, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 64; x++ {
			m.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func openCoverWorld(t *testing.T, capabilities ...string) *coverWorld {
	t.Helper()
	ctx := t.Context()
	store, cfg := fixture(t)
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Covers", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Covered", Path: filepath.Join(lib.Path, "Covered")})
	if err != nil {
		t.Fatal(err)
	}
	red, blue := solidPNG(t, color.RGBA{R: 220, A: 255}), solidPNG(t, color.RGBA{B: 220, A: 255})
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	for i, page := range [][]byte{red, blue} {
		w, e := z.Create(fmt.Sprintf("%02d.png", i+1))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(page); e != nil {
			t.Fatal(e)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(lib.Path, "book.cbz")
	if err = os.WriteFile(path, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	modified := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	book, err := store.CreateBook(ctx, database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID, Name: "book", Path: path, Size: int64(archive.Len()), FileModifiedAt: modified, PageCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	// The cover key the scanner derives from the archive, holding the archive's first page.
	cover := automaticCoverPath(path, modified, int64(archive.Len()))
	file := filepath.Join(config.ThumbnailDir(cfg.Snapshot()), cover)
	if err = os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, red, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().Exec(`UPDATE books SET cover_path = ? WHERE id = ?`, cover, book.ID); err != nil {
		t.Fatal(err)
	}
	web := openClaimWeb(t, store, cfg, &recordingSources{answer: candidatesBySeries}, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if len(capabilities) == 0 {
		capabilities = []string{delegated.Read, delegated.MetadataWrite}
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{lib.ID}, capabilities)
	return &coverWorld{t: t, cfg: cfg, store: store, web: web, agent: agent, library: lib, series: series.ID, book: book.ID, cover: cover}
}

// blue reports whether the public cover of the book currently shows the blue page.
func (w *coverWorld) blue() bool {
	w.t.Helper()
	rec := w.web.raw("GET", fmt.Sprintf("/api/covers/%d", w.book), "")
	if rec.Code != http.StatusOK {
		w.t.Fatalf("public cover %d %s", rec.Code, rec.Body)
	}
	decoded, _, err := image.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		w.t.Fatal(err)
	}
	r, _, b, _ := decoded.At(20, 20).RGBA()
	return b > r
}

func (w *coverWorld) improve(key string, args map[string]any) receipt {
	w.t.Helper()
	input := map[string]any{"request_key": key, "series_id": w.series, "book_id": w.book, "page": 2, "reason": "page 2 is the full cover art; page 1 is an advertisement"}
	for k, v := range args {
		input[k] = v
	}
	var out receipt
	if result := w.agent.call("library_improve_cover", input, &out); result.IsError {
		w.t.Fatalf("cover %s refused: %s", key, resultText(result))
	}
	return out
}

func (w *coverWorld) refused(key string, args map[string]any) {
	w.t.Helper()
	input := map[string]any{"request_key": key, "series_id": w.series, "book_id": w.book, "page": 2, "reason": "page 2 is the full cover art"}
	for k, v := range args {
		input[k] = v
	}
	var out receipt
	if result := w.agent.call("library_improve_cover", input, &out); !result.IsError {
		w.t.Fatalf("cover %s accepted: %+v", key, out)
	}
}

func TestAgentImprovesAnAutomaticCoverWithAnArchivePage(t *testing.T) {
	w := openCoverWorld(t)
	if w.blue() {
		t.Fatal("fixture cover already blue")
	}
	applied := w.improve("cover-1", nil)
	if applied.Kind != "enrich" || applied.Outcome != "changed" || !applied.Applied || applied.CoverSelection == nil ||
		applied.CoverSelection.BookID != w.book || applied.CoverSelection.Page != 2 || applied.NewCoverMaterial == "" || applied.OldCoverMaterial == "" {
		t.Fatalf("cover receipt %+v", applied)
	}
	if len(applied.Fields) != 1 || applied.Fields[0].Field != "cover" || applied.Fields[0].Reason != "page 2 is the full cover art; page 1 is an advertisement" {
		t.Fatalf("cover audit %+v", applied.Fields)
	}
	if !w.blue() {
		t.Fatal("public cover still shows the red page")
	}
	// Readers that resolve the cover by file name find the committed material too.
	if rec := w.web.raw("GET", "/api/thumbnails/"+applied.NewCoverMaterial, ""); rec.Code != http.StatusOK {
		t.Fatalf("new cover material %d", rec.Code)
	}
	// The agent's own cover read shows the new page too.
	result := w.agent.call("library_get_series_cover", map[string]any{"series_id": w.series}, &map[string]any{})
	shown := false
	for _, block := range result.Content {
		if picture, ok := block.(*mcp.ImageContent); ok {
			decoded, _, err := image.Decode(bytes.NewReader(picture.Data))
			if err != nil {
				t.Fatal(err)
			}
			r, _, b, _ := decoded.At(20, 20).RGBA()
			shown = b > r
		}
	}
	if result.IsError || !shown {
		t.Fatalf("agent cover read does not show the blue page: %s", resultText(result))
	}

	var undone receipt
	if result := w.agent.call("library_undo_metadata_operation", map[string]any{"request_key": "cover-1-undo", "operation_id": applied.ID}, &undone); result.IsError || undone.Outcome != "changed" {
		t.Fatalf("undo %+v %s", undone, resultText(result))
	}
	if w.blue() {
		t.Fatal("undo kept the blue page")
	}
}

// operations lists the receipts of the series as the agent reads them.
func (w *coverWorld) operations() string {
	w.t.Helper()
	var page map[string]any
	if result := w.agent.call("library_list_metadata_operations", map[string]any{"series_id": w.series, "limit": 50}, &page); result.IsError {
		w.t.Fatalf("list operations %s", resultText(result))
	}
	raw, _ := json.Marshal(page)
	return string(raw)
}

func (w *coverWorld) exec(query string, args ...any) {
	w.t.Helper()
	if _, err := w.store.DB().Exec(query, args...); err != nil {
		w.t.Fatal(err)
	}
}

func TestCoverImprovementLeavesManualAndLockedCoversAlone(t *testing.T) {
	w := openCoverWorld(t)
	ctx := t.Context()
	other, err := w.store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: w.library.ID, Name: "Other", Path: filepath.Join(w.library.Path, "Other")})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := w.store.CreateBook(ctx, database.CreateBookParams{LibraryID: w.library.ID, SeriesID: other.ID, Name: "foreign", Path: filepath.Join(w.library.Path, "foreign.cbz"), PageCount: 4})
	if err != nil {
		t.Fatal(err)
	}

	// Malformed or foreign selections are refused before anything is recorded.
	before := w.operations()
	w.refused("foreign-book", map[string]any{"book_id": foreign.ID})
	w.refused("missing-book", map[string]any{"book_id": 999999})
	w.refused("page-beyond", map[string]any{"page": 3})
	w.refused("page-zero", map[string]any{"page": 0})
	w.refused("no-reason", map[string]any{"reason": "  "})
	w.refused("url", map[string]any{"url": "http://127.0.0.1/private"})
	// The page's content is read, so a credential without read may not pick one.
	blind := w.web.mainLoopAgent(w.cfg, 1, []int64{w.library.ID}, []string{delegated.MetadataWrite})
	if result := blind.call("library_improve_cover", map[string]any{"request_key": "blind", "series_id": w.series, "book_id": w.book, "page": 2, "reason": "page 2 is the full cover art"}, &receipt{}); !result.IsError {
		t.Fatal("cover chosen without read authority")
	}
	if after := w.operations(); after != before || w.blue() {
		t.Fatalf("refused selections left receipts or changed the cover: %s", after)
	}

	// A protected cover does not turn a malformed selection into a recorded conflict.
	w.exec(`UPDATE books SET cover_locked = 1 WHERE id = ?`, w.book)
	w.refused("locked-page-beyond", map[string]any{"page": 3})
	w.refused("locked-foreign-book", map[string]any{"book_id": foreign.ID})
	if after := w.operations(); after != before {
		t.Fatalf("refused selections on a locked cover left receipts: %s", after)
	}
	w.exec(`UPDATE books SET cover_locked = 0 WHERE id = ?`, w.book)

	// A protected cover is a recorded conflict: locked on the book, locked on the series, a manual
	// file, or a page the owner chose.
	for _, protect := range []struct{ name, query string }{
		{"book-lock", `UPDATE books SET cover_locked = 1 WHERE id = ?`},
		{"series-lock", `UPDATE series SET locked_fields = 'cover' WHERE id = (SELECT series_id FROM books WHERE id = ?)`},
		{"manual-file", `UPDATE books SET cover_path = 'manual.png' WHERE id = ?`},
	} {
		w.exec(protect.query, w.book)
		got := w.improve("protected-"+protect.name, nil)
		if got.Outcome != "conflict" || got.Applied || got.NewCoverMaterial != "" {
			t.Fatalf("%s: %+v", protect.name, got)
		}
		w.exec(`UPDATE books SET cover_locked = 0, cover_path = ? WHERE id = ?`, w.cover, w.book)
		w.exec(`UPDATE series SET locked_fields = '' WHERE id = ?`, w.series)
		if w.blue() {
			t.Fatalf("%s: protected cover replaced", protect.name)
		}
	}
	if rec := w.web.raw("POST", fmt.Sprintf("/api/books/%d/cover", w.book), `{"page":1}`); rec.Code != http.StatusOK {
		t.Fatalf("owner page cover %d %s", rec.Code, rec.Body)
	}
	if got := w.improve("owner-chosen", nil); got.Outcome != "conflict" {
		t.Fatalf("owner-chosen cover %+v", got)
	}
	if w.blue() {
		t.Fatal("owner-chosen cover replaced")
	}
}

func TestCoverImprovementCommitsWithItsReceiptOrNotAtAll(t *testing.T) {
	w := openCoverWorld(t, delegated.Read, delegated.MetadataWrite, delegated.Cleanup)

	// A failing cover switch leaves the old cover and a failed receipt.
	w.exec(`CREATE TRIGGER fail_cover_write BEFORE UPDATE OF cover_path ON books BEGIN SELECT RAISE(ABORT, 'controlled cover write'); END`)
	if got := w.improve("write-failure", nil); got.Outcome != "failed" || got.Applied {
		t.Fatalf("failed switch %+v", got)
	}
	w.exec(`DROP TRIGGER fail_cover_write`)
	// Failing to record the receipt's material references undoes the switch as well.
	w.exec(`CREATE TRIGGER fail_cover_reference BEFORE INSERT ON metadata_cover_materials BEGIN SELECT RAISE(ABORT, 'controlled publication'); END`)
	w.refused("publication-failure", nil)
	w.exec(`DROP TRIGGER fail_cover_reference`)
	// A commit that fails at the end publishes nothing either.
	w.exec(`CREATE TABLE cover_commit_fault(id INTEGER REFERENCES users(id) DEFERRABLE INITIALLY DEFERRED);
		CREATE TRIGGER fail_cover_commit AFTER INSERT ON metadata_cover_materials BEGIN INSERT INTO cover_commit_fault VALUES(-999); END`)
	w.refused("commit-failure", nil)
	w.exec(`DROP TRIGGER fail_cover_commit; DROP TABLE cover_commit_fault`)
	// Neither does a cover material that cannot be prepared.
	materials := filepath.Join(covermaterial.RootDir(config.ThumbnailDir(w.cfg.Snapshot())), "metadata-covers")
	if err := os.MkdirAll(materials, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(materials, materials+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materials, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.refused("prepare-failure", nil)
	if err := os.Remove(materials); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(materials+"-saved", materials); err != nil {
		t.Fatal(err)
	}
	if w.blue() {
		t.Fatal("a failed operation published the new cover")
	}

	applied := w.improve("applied", nil)
	if applied.Outcome != "changed" || !w.blue() {
		t.Fatalf("cover %+v", applied)
	}
	replay := w.improve("applied", nil)
	if replay.ID != applied.ID || !replay.Replayed || replay.Applied {
		t.Fatalf("replay %+v", replay)
	}
	w.refused("applied", map[string]any{"reason": "another comparison"})
	w.refused("applied", map[string]any{"page": 1})

	// Undo is blocked once the owner picks a cover or locks it, and allowed again after.
	if rec := w.web.raw("POST", fmt.Sprintf("/api/books/%d/cover", w.book), `{"page":1}`); rec.Code != http.StatusOK {
		t.Fatalf("owner page cover %d %s", rec.Code, rec.Body)
	}
	var undo receipt
	if result := w.agent.call("library_undo_metadata_operation", map[string]any{"request_key": "undo-owner-cover", "operation_id": applied.ID}, &undo); result.IsError || undo.Outcome != "conflict" {
		t.Fatalf("undo over the owner's cover %+v %s", undo, resultText(result))
	}
	w.exec(`UPDATE books SET cover_path = ?, cover_locked = 1 WHERE id = ?`, applied.NewCoverMaterial, w.book)
	var locked receipt
	if result := w.agent.call("library_undo_metadata_operation", map[string]any{"request_key": "undo-locked-cover", "operation_id": applied.ID}, &locked); result.IsError || locked.Outcome != "conflict" {
		t.Fatalf("undo over a locked cover %+v %s", locked, resultText(result))
	}
	w.exec(`UPDATE books SET cover_locked = 0 WHERE id = ?`, w.book)

	// Cleanup discards the undo snapshot: the cover stays and undo is no longer possible.
	var cleaned map[string]any
	if result := w.agent.call("library_cleanup_metadata_operation", map[string]any{"request_key": "clean-cover", "operation_id": applied.ID}, &cleaned); result.IsError || cleaned["undo_unavailable"] != true {
		t.Fatalf("cleanup %+v %s", cleaned, resultText(result))
	}
	var unavailable receipt
	if result := w.agent.call("library_undo_metadata_operation", map[string]any{"request_key": "undo-cleaned", "operation_id": applied.ID}, &unavailable); result.IsError || unavailable.Outcome != "unavailable" {
		t.Fatalf("undo after cleanup %+v %s", unavailable, resultText(result))
	}
	if !w.blue() {
		t.Fatal("cleanup changed the current cover")
	}
}
