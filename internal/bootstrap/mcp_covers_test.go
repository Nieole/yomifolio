package bootstrap_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"yomifolio/internal/config"
	"yomifolio/internal/covermaterial"
	"yomifolio/internal/database"
	"yomifolio/internal/task"
)

func verifyAtomicCovers(t *testing.T, store database.Store, cfg *config.Manager, session *mcp.ClientSession, router http.Handler, engine *task.Engine, token string, gate func() *enrichmentBarrier, cleanup func() error, call func(string, any) map[string]any, web func(string, string, any, int) []byte, propose func(int64, string) any) func(func(string, string, any, int) []byte) func(func(string, string, any, int) []byte) {
	t.Helper()
	ctx := t.Context()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Cover archive", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	// The credential in the caller is intentionally limited to its first library.
	_ = lib
	var library int64
	if err = store.DB().QueryRow(`SELECT id FROM libraries ORDER BY id LIMIT 1`).Scan(&library); err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{Name: "Cover series", Path: lib.Path + "/s", LibraryID: library})
	if err != nil {
		t.Fatal(err)
	}
	img := func(c color.Color) []byte {
		m := image.NewRGBA(image.Rect(0, 0, 64, 96))
		for y := 0; y < 96; y++ {
			for x := 0; x < 64; x++ {
				m.Set(x, y, c)
			}
		}
		var b bytes.Buffer
		if e := png.Encode(&b, m); e != nil {
			t.Fatal(e)
		}
		return b.Bytes()
	}
	old, newImage := img(color.RGBA{R: 220, A: 255}), img(color.RGBA{B: 220, A: 255})
	path := filepath.Join(lib.Path, "book.cbz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for i, b := range [][]byte{old, newImage} {
		w, e := z.Create(fmt.Sprintf("%02d.png", i+1))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(b); e != nil {
			t.Fatal(e)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	modified := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	book, err := store.CreateBook(ctx, database.CreateBookParams{LibraryID: library, SeriesID: series.ID, Name: "book", Path: path, Size: 123, FileModifiedAt: modified, PageCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	// Match the ordinary scanner's archive-derived cover key.
	oldPath := automaticCoverPath(path, modified, 123)
	if err = os.MkdirAll(filepath.Dir(filepath.Join(config.ThumbnailDir(cfg.Snapshot()), oldPath)), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(config.ThumbnailDir(cfg.Snapshot()), oldPath), old, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().Exec(`UPDATE books SET cover_path=? WHERE id=?`, oldPath, book.ID); err != nil {
		t.Fatal(err)
	}
	proposal := propose(series.ID, "bangumi")
	input := map[string]any{"request_key": "cover-success", "series_id": series.ID, "proposal_ids": []any{proposal}, "fields": []string{"summary", "cover"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": series.ID}), "cover": map[string]any{"book_id": book.ID, "page": 2, "quality_compared": true, "reason": "Compared pages: page 2 is upright complete artwork without the red advertisement on page 1"}}
	out := call("library_enrich_metadata", input)
	if out["outcome"] != "changed" {
		t.Fatalf("cover and text not applied: %+v", out)
	}
	assertSummary := func(web func(string, string, any, int) []byte, expected string) {
		t.Helper()
		var catalogue database.Series
		if err := json.Unmarshal(web("GET", fmt.Sprintf("/series/info/%d", series.ID), nil, 200), &catalogue); err != nil {
			t.Fatal(err)
		}
		if catalogue.Summary.String != expected {
			t.Fatalf("ordinary series projection: got %q, want %q", catalogue.Summary.String, expected)
		}
	}
	verifyCoverTransportCancellation(t, cfg, store, propose, router, engine, library, path, token, input, out, call)
	assertSummary(web, "Space journey")
	state := call("library_get_enrichment_state", map[string]any{"series_id": series.ID})
	if state["summary"] != "Space journey" {
		t.Fatal(state)
	}
	cover := state["cover"].(map[string]any)
	if cover["path"] == oldPath {
		t.Fatal("cover did not switch")
	}
	got := web("GET", fmt.Sprintf("/covers/%d", book.ID), nil, 200)
	decoded, _, err := image.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	r, _, b, _ := decoded.At(20, 20).RGBA()
	if b <= r {
		t.Fatal("public cover still shows old red image")
	}
	evidence, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_series_cover", Arguments: map[string]any{"series_id": series.ID}})
	if e != nil || evidence.IsError {
		t.Fatalf("new cover evidence unavailable: %+v %v", evidence, e)
	}
	var imageBlock *mcp.ImageContent
	for _, block := range evidence.Content {
		if img, ok := block.(*mcp.ImageContent); ok {
			imageBlock = img
		}
	}
	if imageBlock == nil {
		t.Fatal(evidence.Content)
	}
	preview, _, e := image.Decode(bytes.NewReader(imageBlock.Data))
	if e != nil {
		t.Fatal(e)
	}
	pr, _, pb, _ := preview.At(20, 20).RGBA()
	if pb <= pr {
		t.Fatal("MCP still shows old image")
	}
	// Filename-based readers (web, OPDS and Mihon) must resolve committed material too.
	web("GET", "/thumbnails/"+url.PathEscape(out["new_cover_material"].(string)[:len("metadata-covers")])+"/"+filepath.Base(out["new_cover_material"].(string)), nil, 200)
	undo := call("library_undo_metadata_operation", map[string]any{"request_key": "cover-undo", "operation_id": out["id"]})
	if undo["outcome"] != "changed" {
		t.Fatal(undo)
	}
	assertSummary(web, "")
	got = web("GET", fmt.Sprintf("/covers/%d", book.ID), nil, 200)
	decoded, _, err = image.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	r, _, b, _ = decoded.At(20, 20).RGBA()
	if r <= b {
		t.Fatal("undo did not restore old public image")
	}
	reset := func() {
		t.Helper()
		if _, err := store.DB().Exec(`UPDATE series SET summary=NULL,locked_fields='' WHERE id=?`, series.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.DB().Exec(`UPDATE books SET cover_path=?,cover_locked=0 WHERE id=?`, oldPath, book.ID); err != nil {
			t.Fatal(err)
		}
	}
	makeInput := func(key string) map[string]any {
		return map[string]any{"request_key": key, "series_id": series.ID, "proposal_ids": []any{proposal}, "fields": []string{"summary", "cover"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": series.ID}), "cover": map[string]any{"book_id": book.ID, "page": 2, "quality_compared": true, "reason": "Page two contains complete upright artwork, unlike page one"}}
	}
	reject := func(in any) {
		t.Helper()
		result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_enrich_metadata", Arguments: in})
		if e != nil || !result.IsError {
			t.Fatalf("expected safe rejection: %+v %v", result, e)
		}
	}
	unchanged := func() {
		t.Helper()
		assertSummary(web, "")
		state := call("library_get_enrichment_state", map[string]any{"series_id": series.ID})
		if state["summary"] != nil || state["cover"].(map[string]any)["path"] != oldPath {
			t.Fatalf("half-applied: %+v", state)
		}
		decoded, _, err := image.Decode(bytes.NewReader(web("GET", fmt.Sprintf("/covers/%d", book.ID), nil, 200)))
		if err != nil {
			t.Fatal(err)
		}
		r, _, b, _ := decoded.At(20, 20).RGBA()
		if r <= b {
			t.Fatal("failed operation published cover")
		}
	}

	// Combined 09+10 command: author correction, cover and text are one unit.
	execSQL := func(query string, args ...any) {
		t.Helper()
		if _, e := store.DB().Exec(query, args...); e != nil {
			t.Fatal(e)
		}
	}
	execSQL(`INSERT INTO authors(name,role) VALUES('Cover Alias','Artist'),('Canonical','Writer')`)
	execSQL(`INSERT INTO series_authors SELECT ?,id FROM authors WHERE name='Cover Alias'`, series.ID)
	combined := func(key string) map[string]any {
		in := makeInput(key)
		in["fields"] = []string{"cover", "summary", "authors"}
		in["author_corrections"] = []any{map[string]any{"from": map[string]string{"name": "Cover Alias", "role": "Artist"}, "to": map[string]string{"name": "Canonical", "role": "Writer"}, "reason": "Printed canonical credit"}}
		return in
	}
	assertAuthors := func(name, role string) {
		t.Helper()
		state := call("library_get_enrichment_state", map[string]any{"series_id": series.ID})
		raw, _ := json.Marshal(state["authors"])
		want, _ := json.Marshal([]map[string]string{{"name": name, "role": role}})
		if !bytes.Equal(raw, want) {
			t.Fatalf("combined author group: %s != %s", raw, want)
		}
	}

	assertCombinedImage := func(blue bool) {
		t.Helper()
		decoded, _, e := image.Decode(bytes.NewReader(web("GET", fmt.Sprintf("/covers/%d", book.ID), nil, 200)))
		if e != nil {
			t.Fatal(e)
		}
		red, _, b, _ := decoded.At(20, 20).RGBA()
		if (b > red) != blue {
			t.Fatalf("combined public cover: blue=%v, want %v", b > red, blue)
		}
	}
	// Current whole-author group and related text CAS each reject without publishing cover.
	for _, field := range []string{"authors", "summary"} {
		reset()
		in := combined("combined-cas-" + field)
		if field == "authors" {
			execSQL(`INSERT INTO series_authors SELECT ?,id FROM authors WHERE name='Canonical'`, series.ID)
		} else {
			execSQL(`UPDATE series SET summary='Later text' WHERE id=?`, series.ID)
		}
		result := call("library_enrich_metadata", in)
		if result["outcome"] != "conflict" {
			t.Fatal(result)
		}
		execSQL(`DELETE FROM series_authors WHERE series_id=? AND author_id IN (SELECT id FROM authors WHERE name='Canonical')`, series.ID)
		reset()
		unchanged()
		assertAuthors("Cover Alias", "Artist")
	}
	// Force the last association write to fail after cover and scalar writes.
	reset()
	execSQL(`CREATE TRIGGER combined_author_failure BEFORE INSERT ON series_authors BEGIN SELECT RAISE(ABORT,'controlled author failure'); END`)
	failedCombined := call("library_enrich_metadata", combined("combined-author-failure"))
	execSQL(`DROP TRIGGER combined_author_failure`)
	if failedCombined["outcome"] != "failed" {
		t.Fatal(failedCombined)
	}
	unchanged()
	assertAuthors("Cover Alias", "Artist")
	in := combined("combined-success")
	execSQL(`UPDATE series SET publisher='Unrelated combined edit' WHERE id=?`, series.ID)
	applied := call("library_enrich_metadata", in)
	if applied["outcome"] != "changed" {
		t.Fatal(applied)
	}
	assertAuthors("Canonical", "Writer")
	assertSummary(web, "Space journey")
	assertCombinedImage(true)
	// Same key with genuinely different author/cover intention cannot replay.
	for _, field := range []string{"cover", "author_corrections"} {
		raw, _ := json.Marshal(in)
		altered := map[string]any{}
		_ = json.Unmarshal(raw, &altered)
		if field == "cover" {
			altered[field].(map[string]any)["reason"] = "Different quality choice"
		} else {
			altered[field].([]any)[0].(map[string]any)["reason"] = "Different correction choice"
		}
		reject(altered)
	}
	for _, field := range []string{"authors", "summary", "cover"} {
		if field == "authors" {
			execSQL(`INSERT INTO series_authors SELECT ?,id FROM authors WHERE name='Cover Alias'`, series.ID)
		}
		if field == "summary" {
			execSQL(`UPDATE series SET summary='Later text' WHERE id=?`, series.ID)
		}
		if field == "cover" {
			execSQL(`UPDATE books SET cover_locked=1 WHERE id=?`, book.ID)
		}
		denied := call("library_undo_metadata_operation", map[string]any{"operation_id": applied["id"], "request_key": "combined-undo-conflict-" + field})
		if denied["outcome"] != "conflict" {
			t.Fatal(denied)
		}
		execSQL(`DELETE FROM series_authors WHERE series_id=? AND author_id IN (SELECT id FROM authors WHERE name='Cover Alias')`, series.ID)
		execSQL(`UPDATE series SET summary='Space journey' WHERE id=?`, series.ID)
		execSQL(`UPDATE books SET cover_locked=0 WHERE id=?`, book.ID)
	}
	restored := call("library_undo_metadata_operation", map[string]any{"operation_id": applied["id"], "request_key": "combined-undo"})
	if restored["outcome"] != "changed" {
		t.Fatal(restored)
	}
	assertAuthors("Cover Alias", "Artist")
	assertSummary(web, "")
	assertCombinedImage(false)
	if call("library_get_enrichment_state", map[string]any{"series_id": series.ID})["publisher"] != "Unrelated combined edit" {
		t.Fatal("undo lost unrelated edit")
	}
	// Undo restores retained bytes, not the original mutable cache key.
	reset()
	unchanged()
	execSQL(`DELETE FROM series_authors WHERE series_id=?`, series.ID)
	execSQL(`UPDATE series SET publisher=NULL WHERE id=?`, series.ID)
	reset()
	// Exercise the ordinary human page-cover command, then the delegated refusal.
	web("POST", fmt.Sprintf("/books/%d/cover", book.ID), map[string]int{"page": 2}, 200)
	reject(makeInput("human-page-cover"))
	reset()
	// A cover-only item needs no remote proposal; another series can fail independently.
	empty, e := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: library, Name: "No cover", Path: lib.Path + "/empty"})
	if e != nil {
		t.Fatal(e)
	}
	coverOnly := makeInput("cover-only-batch")
	coverOnly["fields"] = []string{"cover"}
	coverOnly["proposal_ids"] = []int64{}
	batch := call("library_enrich_metadata_batch", map[string]any{"operations": []any{coverOnly, map[string]any{"request_key": "empty-cover", "series_id": empty.ID, "proposal_ids": []int64{}, "fields": []string{"cover"}, "cover": map[string]any{"book_id": book.ID, "page": 2, "quality_compared": true, "reason": "Compared"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": empty.ID})}}})
	batchOps := batch["operations"].([]any)
	if batchOps[0].(map[string]any)["outcome"] != "changed" || batchOps[1].(map[string]any)["outcome"] != "failed" {
		t.Fatal(batch)
	}
	call("library_undo_metadata_operation", map[string]any{"operation_id": batchOps[0].(map[string]any)["id"], "request_key": "batch-cover-undo"})
	reset()
	// Current role is rechecked after real image preparation, at the commit boundary.
	roleInput := makeInput("cover-current-role")
	barrier := gate()
	done := make(chan *mcp.CallToolResult, 1)
	failed := make(chan error, 1)
	go func() {
		result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_enrich_metadata", Arguments: roleInput})
		if e != nil {
			failed <- e
		} else {
			done <- result
		}
	}()
	<-barrier.entered
	if _, err = store.DB().Exec(`UPDATE users SET role='regular' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	close(barrier.release)
	select {
	case e := <-failed:
		t.Fatal(e)
	case result := <-done:
		if !result.IsError {
			t.Fatal("current user intersection was bypassed", result)
		}
	}
	if _, err = store.DB().Exec(`UPDATE users SET role='admin' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	unchanged()
	foreignSeries, e := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Foreign", Path: lib.Path + "/foreign"})
	if e != nil {
		t.Fatal(e)
	}
	foreignBook, e := store.CreateBook(ctx, database.CreateBookParams{LibraryID: lib.ID, SeriesID: foreignSeries.ID, Name: "Foreign", Path: lib.Path + "/foreign.cbz"})
	if e != nil {
		t.Fatal(e)
	}
	foreign := makeInput("foreign-source")
	foreign["cover"].(map[string]any)["book_id"] = foreignBook.ID
	reject(foreign)
	unchanged()
	call("library_get_page_thumbnails", map[string]any{"series_id": series.ID, "book_id": book.ID, "pages": []int{2}})
	invalid := makeInput("invalid-page")
	invalid["cover"].(map[string]any)["page"] = 900
	reject(invalid)
	unchanged()
	invalid = makeInput("uncompared")
	invalid["cover"].(map[string]any)["quality_compared"] = false
	reject(invalid)
	unchanged()
	invalid = makeInput("no-url")
	invalid["cover"].(map[string]any)["url"] = "http://127.0.0.1/private"
	reject(invalid)
	unchanged()
	// A file at the reserved directory forces the actual immutable publisher to fail.
	directory := filepath.Join(covermaterial.RootDir(config.ThumbnailDir(cfg.Snapshot())), "metadata-covers")
	if err = os.Rename(directory, directory+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(directory, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	reject(makeInput("prepare-write-failure"))
	unchanged()
	if err = os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(directory+"-saved", directory); err != nil {
		t.Fatal(err)
	}
	// Manual provenance remains protected even if someone clears its lock.
	for _, locked := range []bool{true, false} {
		path := oldPath
		if !locked {
			path = "manual.png"
			if err = os.WriteFile(filepath.Join(config.ThumbnailDir(cfg.Snapshot()), path), old, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = store.DB().Exec(`UPDATE books SET cover_path=?,cover_locked=? WHERE id=?`, path, locked, book.ID); err != nil {
			t.Fatal(err)
		}
		reject(makeInput(fmt.Sprintf("manual-%t", locked)))
	}
	reset()
	conflict := makeInput("cover-text-conflict")
	if _, err = store.DB().Exec(`UPDATE series SET summary='Human text' WHERE id=?`, series.ID); err != nil {
		t.Fatal(err)
	}
	result := call("library_enrich_metadata", conflict)
	if result["outcome"] != "conflict" {
		t.Fatal(result)
	}
	decoded, _, err = image.Decode(bytes.NewReader(web("GET", fmt.Sprintf("/covers/%d", book.ID), nil, 200)))
	if err != nil {
		t.Fatal(err)
	}
	r, _, b, _ = decoded.At(20, 20).RGBA()
	if r <= b {
		t.Fatal("conflict published image")
	}
	reset()
	if _, err = store.DB().Exec(`CREATE TRIGGER fail_cover_write BEFORE UPDATE OF cover_path ON books BEGIN SELECT RAISE(ABORT,'controlled cover write'); END`); err != nil {
		t.Fatal(err)
	}
	result = call("library_enrich_metadata", makeInput("cover-write-failure"))
	if result["outcome"] != "failed" {
		t.Fatal(result)
	}
	unchanged()
	if _, err = store.DB().Exec(`DROP TRIGGER fail_cover_write`); err != nil {
		t.Fatal(err)
	}
	// Fail text after the cover UPDATE: the same savepoint must restore both.
	if _, err = store.DB().Exec(`CREATE TRIGGER fail_cover_text BEFORE UPDATE OF summary ON series BEGIN SELECT RAISE(ABORT,'controlled text write'); END`); err != nil {
		t.Fatal(err)
	}
	textFailureInput := makeInput("cover-text-write-failure")
	textFailure := call("library_enrich_metadata", textFailureInput)
	if textFailure["outcome"] != "failed" || textFailure["applied"] != false {
		t.Fatal(textFailure)
	}
	unchanged()
	if _, err = store.DB().Exec(`DROP TRIGGER fail_cover_text`); err != nil {
		t.Fatal(err)
	}
	replayedFailure := call("library_enrich_metadata", textFailureInput)
	if replayedFailure["id"] != textFailure["id"] || replayedFailure["replayed"] != true || replayedFailure["applied"] != false {
		t.Fatal(replayedFailure)
	}
	unchanged()
	if _, err = store.DB().Exec(`CREATE TRIGGER fail_cover_reference BEFORE INSERT ON metadata_cover_materials BEGIN SELECT RAISE(ABORT,'controlled publication'); END`); err != nil {
		t.Fatal(err)
	}
	reject(makeInput("publication-failure"))
	unchanged()
	if _, err = store.DB().Exec(`DROP TRIGGER fail_cover_reference`); err != nil {
		t.Fatal(err)
	}
	// A deferred FK violation is an actual COMMIT failure, not a failed statement.
	if _, err = store.DB().Exec(`CREATE TABLE cover_commit_fault(id INTEGER REFERENCES users(id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER fail_cover_commit AFTER INSERT ON metadata_cover_materials BEGIN INSERT INTO cover_commit_fault VALUES(-999); END`); err != nil {
		t.Fatal(err)
	}
	reject(makeInput("commit-failure"))
	unchanged()
	if _, err = store.DB().Exec(`DROP TRIGGER fail_cover_commit; DROP TABLE cover_commit_fault`); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	hidden := 0
	for _, entry := range entries {
		var referenced int
		if err = store.DB().QueryRow(`SELECT count(*) FROM metadata_cover_materials WHERE path=?`, "metadata-covers/"+entry.Name()).Scan(&referenced); err != nil {
			t.Fatal(err)
		}
		if referenced == 0 {
			web("GET", "/thumbnails/metadata-covers/"+entry.Name(), nil, 404)
			if runtime.GOOS == "windows" {
				web("GET", "/thumbnails/METADATA-COVERS/"+strings.ToUpper(entry.Name()), nil, 404)
				web("GET", "/thumbnails/metadata-covers./"+entry.Name(), nil, 404)
			}
			hidden++
		}
	}
	if hidden == 0 {
		t.Fatal("no prepared orphan tested")
	}
	before := makeInput("cover-reopen")
	var stdioCredential struct{ Token string }
	if e := json.Unmarshal(web("POST", "/mcp/credentials", map[string]any{"user_id": 1, "name": "cover-stdio", "library_ids": []int64{library}, "capabilities": []string{"read", "metadata_write"}}, 200), &stdioCredential); e != nil {
		t.Fatal(e)
	}
	result = coverStdio(t, cfg, router, stdioCredential.Token, before)
	if result["outcome"] != "changed" {
		t.Fatal(result)
	}
	if err = cleanup(); err != nil {
		t.Fatal(err)
	}
	web("GET", "/thumbnails/"+result["old_cover_material"].(string), nil, 200)
	web("GET", "/thumbnails/"+result["new_cover_material"].(string), nil, 200)
	web("POST", fmt.Sprintf("/books/%d/cover", book.ID), map[string]int{"page": 1}, 200)
	manualUndo := call("library_undo_metadata_operation", map[string]any{"operation_id": result["id"], "request_key": "cover-undo-manual"})
	if manualUndo["outcome"] != "conflict" {
		t.Fatal(manualUndo)
	}
	if _, err = store.DB().Exec(`UPDATE books SET cover_path=?,cover_locked=0 WHERE id=?`, result["new_cover_material"], book.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().Exec(`UPDATE books SET cover_locked=1 WHERE id=?`, book.ID); err != nil {
		t.Fatal(err)
	}
	deniedUndo := call("library_undo_metadata_operation", map[string]any{"operation_id": result["id"], "request_key": "cover-undo-lock"})
	if deniedUndo["outcome"] != "conflict" {
		t.Fatal(deniedUndo)
	}
	if _, err = store.DB().Exec(`UPDATE books SET cover_locked=0 WHERE id=?`, book.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().Exec(`UPDATE series SET summary='Later edit' WHERE id=?`, series.ID); err != nil {
		t.Fatal(err)
	}
	deniedUndo = call("library_undo_metadata_operation", map[string]any{"operation_id": result["id"], "request_key": "cover-undo-text"})
	if deniedUndo["outcome"] != "conflict" {
		t.Fatal(deniedUndo)
	}
	if _, err = store.DB().Exec(`UPDATE series SET summary='Space journey',publisher='Unrelated publisher' WHERE id=?`, series.ID); err != nil {
		t.Fatal(err)
	}
	// The mutable ordinary cache key is not the retained undo material.
	if err = os.Remove(filepath.Join(config.ThumbnailDir(cfg.Snapshot()), oldPath)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return func(web func(string, string, any, int) []byte) func(func(string, string, any, int) []byte) {
		t.Helper()
		assertColor := func(blue bool) {
			t.Helper()
			im, _, e := image.Decode(bytes.NewReader(web("GET", fmt.Sprintf("/covers/%d", book.ID), nil, 200)))
			if e != nil {
				t.Fatal(e)
			}
			r, _, b, _ := im.At(20, 20).RGBA()
			if (b > r) != blue {
				t.Fatal("reopened public cover bytes disagree")
			}
		}
		assertColor(true)
		assertSummary(web, "Space journey")
		var persistedFailure map[string]any
		if e := json.Unmarshal(web("GET", fmt.Sprintf("/mcp/operations/%s", textFailure["id"]), nil, 200), &persistedFailure); e != nil || persistedFailure["outcome"] != "failed" {
			t.Fatal(persistedFailure, e)
		}
		web("GET", "/thumbnails/"+result["old_cover_material"].(string), nil, 200)
		raw := web("POST", fmt.Sprintf("/mcp/operations/%s/undo", result["id"]), map[string]string{"request_key": "cover-reopen-undo"}, 200)
		var restored map[string]any
		if e := json.Unmarshal(raw, &restored); e != nil {
			t.Fatal(e)
		}
		if restored["outcome"] != "changed" || restored["after"].(map[string]any)["summary"] != nil || restored["after"].(map[string]any)["publisher"] != "Unrelated publisher" {
			t.Fatal(string(raw))
		}
		assertColor(false)
		assertSummary(web, "")
		return func(_ func(string, string, any, int) []byte) { t.Helper(); assertColor(false) }
	}

}

func automaticCoverPath(path string, modified time.Time, size int64) string {
	h := fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", path, modified.Unix(), size))))
	return h[:2] + "/" + h + ".png"
}

func coverStdio(t *testing.T, cfg *config.Manager, router http.Handler, token string, input any) map[string]any {
	t.Helper()
	ctx := t.Context()
	binary := filepath.Join(t.TempDir(), "mcp-proxy.exe")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/mcp-proxy")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build stdio: %v %s", e, out)
	}
	server := httptest.NewServer(router)
	defer server.Close()
	previous := cfg.Snapshot()
	defer cfg.Replace(&previous)
	snapshot := previous
	snapshot.MCP.PublicURL = server.URL
	snapshot.MCP.AllowLANHTTP = true
	cfg.Replace(&snapshot)
	command := exec.CommandContext(ctx, binary, "--url", server.URL, "--allow-lan-http")
	command.Dir = t.TempDir()
	command.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN="+token)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	session, err := mcp.NewClient(&mcp.Implementation{Name: "atomic-cover-stdio", Version: "1"}, nil).Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatalf("stdio: %v %s", err, stderr.String())
	}
	defer session.Close()
	call := func() map[string]any {
		t.Helper()
		result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_enrich_metadata", Arguments: input})
		if e != nil || result.IsError {
			t.Fatalf("stdio cover: %+v %v %s", result, e, stderr.String())
		}
		raw, _ := json.Marshal(result.StructuredContent)
		var op map[string]any
		if e = json.Unmarshal(raw, &op); e != nil {
			t.Fatal(e)
		}
		return op
	}
	result := call()
	replay := call()
	if result["id"] != replay["id"] || replay["replayed"] != true || replay["applied"] != false {
		t.Fatal(replay)
	}
	return result
}
