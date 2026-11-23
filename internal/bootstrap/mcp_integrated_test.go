package bootstrap_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/api"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/task"
)

// One lineage crosses discovery, model admission, publication, adjudication and
// irreversible cleanup; transport-specific tests cannot substitute for it.
func TestMCPIntegratedAcceptance(t *testing.T) {
	for _, mode := range []string{"http", "stdio"} {
		t.Run(mode, func(t *testing.T) {
			for _, capability := range []string{"read", "scan_control", "cleanup"} {
				t.Run(capability, func(t *testing.T) { runIntegratedAcceptance(t, mode == "stdio", capability) })
			}
		})
	}
}

func runIntegratedAcceptance(t *testing.T, stdio bool, finalCapability string) {
	ctx := t.Context()
	store, cfg := fixture(t)
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Integrated", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	makeSeries := func(name string, library int64) int64 {
		s, e := store.CreateSeries(ctx, database.CreateSeriesParams{Name: name, LibraryID: library, Path: filepath.Join(lib.Path, name)})
		if e != nil {
			t.Fatal(e)
		}
		return s.ID
	}
	target, shared, ambiguous, failed := makeSeries("Identity", lib.ID), makeSeries("Shared", lib.ID), makeSeries("Ambiguous", lib.ID), makeSeries("Failed", lib.ID)
	outside, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Outside", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	hidden := makeSeries("Hidden", outside.ID)
	if _, err = store.DB().Exec(`INSERT INTO authors(name,role) VALUES('Alias','Artist')`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{target, shared} {
		if _, err = store.DB().Exec(`INSERT INTO series_authors SELECT ?,id FROM authors WHERE name='Alias'`, id); err != nil {
			t.Fatal(err)
		}
	}
	picture := func(c color.Color) []byte {
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
	red, blue := picture(color.RGBA{R: 220, A: 255}), picture(color.RGBA{B: 220, A: 255})
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	for i, data := range [][]byte{red, blue, []byte(`<ComicInfo><Series>Identity</Series><Title>Original edition</Title><Writer>Canonical</Writer></ComicInfo>`)} {
		name := fmt.Sprintf("%02d.png", i+1)
		if i == 2 {
			name = "ComicInfo.xml"
		}
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(lib.Path, "Identity.cbz")
	if err = os.WriteFile(path, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	modified := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	book, err := store.CreateBook(ctx, database.CreateBookParams{LibraryID: lib.ID, SeriesID: target, Name: "Identity.cbz", Path: path, Size: int64(archive.Len()), FileModifiedAt: modified, PageCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	oldPath := automaticCoverPath(path, modified, int64(archive.Len()))
	coverFile := filepath.Join(config.ThumbnailDir(cfg.Snapshot()), oldPath)
	if err = os.MkdirAll(filepath.Dir(coverFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(coverFile, red, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().Exec(`UPDATE books SET cover_path=? WHERE id=?`, oldPath, book.ID); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var hold atomic.Bool
	entered := make(chan struct{}, 1)
	type sourceBarrier struct{ entered, release chan struct{} }
	barriers := map[string]sourceBarrier{}
	for _, name := range []string{"active-permission", "active-password", "eof-cancel"} {
		barriers[name] = sourceBarrier{make(chan struct{}, 1), make(chan struct{})}
	}
	options := bootstrap.Options{Sources: api.ExternalSources{
		AI: func(string, string, string, string, string, string, int) metadata.AIProvider {
			return budgetAI{calls: &calls, hold: &hold, entered: entered}
		},
		Metadata: func(_ config.Config, name string) metadata.Provider {
			return &controlledSource{search: func(ctx context.Context, query string, _, _ int) ([]*metadata.SeriesMetadata, int, error) {
				if barrier, ok := barriers[query]; ok && name == "bangumi" {
					barrier.entered <- struct{}{}
					select {
					case <-barrier.release:
					case <-ctx.Done():
						return nil, 0, ctx.Err()
					}
				}
				if query == "Failed" {
					return nil, 0, fmt.Errorf("controlled upstream failure")
				}
				return []*metadata.SeriesMetadata{{Title: "Identity", Summary: "Verified journey", Publisher: "Edition press", Authors: []metadata.SeriesAuthor{{Name: "Canonical", Role: "Writer"}}, Tags: []string{"Adventure"}, SourceID: 1, SourceURL: "https://example.org/" + name}, {Title: "Identity alternate edition", SourceID: 2}}, 2, nil
			}}
		},
	}}
	process, err := bootstrap.Open(ctx, store, cfg, options)
	if err != nil {
		t.Fatal(err)
	}
	var server *httptest.Server
	var session *mcp.ClientSession
	defer func() {
		if session != nil {
			session.Close()
		}
		if server != nil {
			server.Close()
		}
		process.Close()
	}()
	var cookie *http.Cookie
	var csrf string
	web := func(method, path string, body any, status int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, "/api"+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
			req.Header.Set("X-CSRF-Token", csrf)
		}
		r := httptest.NewRecorder()
		router := chi.NewRouter()
		process.HTTP.SetupRoutes(router)
		router.ServeHTTP(r, req)
		if r.Code != status {
			t.Fatalf("web %s %s: %d %s", method, path, r.Code, r.Body.String())
		}
		if path == "/auth/setup" {
			cookie = r.Result().Cookies()[0]
		}
		var out map[string]any
		_ = json.Unmarshal(r.Body.Bytes(), &out)
		return out
	}
	login := web("POST", "/auth/setup", map[string]string{"username": "admin", "password": "correct horse battery staple"}, 200)
	csrf = login["csrf_token"].(string)
	caps := []string{"read", "metadata_write", "internal_llm", "cleanup", "scan_control"}
	issued := web("POST", "/mcp/credentials", map[string]any{"user_id": 1, "name": "integrated", "library_ids": []int64{lib.ID}, "capabilities": caps}, 200)
	var binary string
	if stdio {
		binary = filepath.Join(t.TempDir(), "mcp-proxy.exe")
		if out, e := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/mcp-proxy").CombinedOutput(); e != nil {
			t.Fatalf("build: %v %s", e, out)
		}
	}
	startServer := func() {
		router := chi.NewRouter()
		process.HTTP.SetupRoutes(router)
		if stdio {
			server = httptest.NewServer(router)
		} else {
			server = httptest.NewTLSServer(router)
		}
		snapshot := cfg.Snapshot()
		snapshot.Library.IOPolicy.ArchiveOpenConcurrency = 1
		snapshot.MCP.PublicURL = server.URL
		snapshot.MCP.AllowLANHTTP = stdio
		cfg.Replace(&snapshot)
	}
	connect := func() {
		var transport mcp.Transport = &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: issued["token"].(string)}}}
		if stdio {
			cmd := exec.CommandContext(ctx, binary, "--url", server.URL, "--allow-lan-http")
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN="+issued["token"].(string))
			transport = &mcp.CommandTransport{Command: cmd}
		}
		var e error
		session, e = mcp.NewClient(&mcp.Implementation{Name: "integrated", Version: "1"}, nil).Connect(ctx, transport, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
		if e != nil {
			t.Fatal(e)
		}
		if session.InitializeResult().ProtocolVersion != "2025-11-25" {
			t.Fatal(session.InitializeResult())
		}
	}
	startServer()
	connect()
	call := func(name string, args any, wantError bool) map[string]any {
		t.Helper()
		r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil || r.IsError != wantError {
			t.Fatalf("%s: %+v %v", name, r, e)
		}
		raw, _ := json.Marshal(r.StructuredContent)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return out
	}
	state := func(id int64) map[string]any {
		return call("library_get_enrichment_state", map[string]any{"series_id": id}, false)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	for _, name := range []string{"library_find_series", "library_get_book_clues", "library_assess_match", "library_enrich_metadata", "library_cleanup_metadata_operation"} {
		if !names[name] {
			t.Fatal("missing tool", name)
		}
	}
	page := call("library_find_series", map[string]any{"library_ids": []int64{lib.ID}, "series_ids": []int64{target}, "missing_fields": []string{"summary", "publisher"}, "added_from": "2000-01-01T00:00:00Z", "limit": 1}, false)
	if items := page["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != float64(target) {
		t.Fatal(page)
	}
	call("library_get_series", map[string]any{"series_id": hidden}, true)
	clues := call("library_get_book_clues", map[string]any{"book_id": book.ID}, false)
	if ci := clues["comic_info"].(map[string]any); ci["status"] != "available" || ci["series"] != "Identity" {
		t.Fatal(clues)
	}
	images, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_page_thumbnails", Arguments: map[string]any{"series_id": target, "book_id": book.ID, "pages": []int{2}}})
	if e != nil || images.IsError {
		t.Fatal(images, e)
	}
	imageCount := 0
	for _, block := range images.Content {
		if img, ok := block.(*mcp.ImageContent); ok {
			imageCount++
			decoded, _, err := image.Decode(bytes.NewReader(img.Data))
			if err != nil {
				t.Fatal(err)
			}
			red, _, blue, _ := decoded.At(20, 20).RGBA()
			if blue <= red {
				t.Fatal("selected page is not blue artwork")
			}
		}
	}
	if imageCount != 1 {
		t.Fatal(images)
	}
	// Real RAR clues share the same request-owned admission and volume IO lease.
	rarPath := filepath.Join(lib.Path, "clues.cbr")
	rarBytes, err := os.ReadFile("../parser/testdata/vol.cbr")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rarPath, rarBytes, 0600); err != nil {
		t.Fatal(err)
	}
	rarBook, err := store.CreateBook(ctx, database.CreateBookParams{LibraryID: lib.ID, SeriesID: target, Name: "clues.cbr", Path: rarPath, Size: int64(len(rarBytes)), FileModifiedAt: modified, PageCount: 4})
	if err != nil {
		t.Fatal(err)
	}
	assertClueTransportDisconnect(t, cfg.Snapshot(), server, binary, issued["token"].(string), process.Application.Engine, rarPath, lib.ID, rarBook.ID, stdio)
	if clue := call("library_get_book_clues", map[string]any{"book_id": rarBook.ID}, false); clue["comic_info"].(map[string]any)["status"] != "available" {
		t.Fatal(clue)
	}
	// Real compressed solid stream with a CRC-valid undersized first declaration:
	// public clues must say unavailable, and images must not decode or fall back.
	hostilePath := filepath.Join(lib.Path, "solid-undersized.cbr")
	hostileBytes, err := os.ReadFile("../parser/testdata/solid-undersized.cbr")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(hostilePath, hostileBytes, 0600); err != nil {
		t.Fatal(err)
	}
	hostileBook, err := store.CreateBook(ctx, database.CreateBookParams{LibraryID: lib.ID, SeriesID: target, Name: "solid-undersized.cbr", Path: hostilePath, Size: int64(len(hostileBytes)), FileModifiedAt: modified, PageCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if clue := call("library_get_book_clues", map[string]any{"book_id": hostileBook.ID}, false); clue["comic_info"].(map[string]any)["status"] != "unavailable" {
		t.Fatal(clue)
	}
	rejected, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_page_thumbnails", Arguments: map[string]any{"series_id": target, "book_id": hostileBook.ID, "pages": []int{1}}})
	if err != nil || !rejected.IsError {
		t.Fatalf("hostile solid image: %+v %v", rejected, err)
	}
	for _, content := range rejected.Content {
		if _, ok := content.(*mcp.ImageContent); ok {
			t.Fatal("hostile solid returned image")
		}
	}
	acceptedIDs := map[int64]string{}
	assertRunPages := func(wantCapability string) {
		t.Helper()
		for _, limit := range []int{1, 2} {
			seen := map[int64]bool{}
			var before int64
			for {
				r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_list_runs", Arguments: map[string]any{"library_id": lib.ID, "limit": limit, "before_id": before}})
				if err != nil || r.IsError {
					t.Fatalf("list: %+v %v", r, err)
				}
				raw, _ := json.Marshal(r.StructuredContent)
				var page []delegated.Run
				if err := json.Unmarshal(raw, &page); err != nil {
					t.Fatal(err)
				}
				if len(page) > limit {
					t.Fatal("unbounded page", page)
				}
				if len(page) == 0 {
					break
				}
				for _, run := range page {
					capability, own := acceptedIDs[run.ID]
					if !own || seen[run.ID] || run.LibraryID != lib.ID || (before > 0 && run.ID >= before) || (wantCapability != "" && capability != wantCapability) {
						t.Fatalf("wrong/duplicate/cursor run: %+v before=%d", run, before)
					}
					seen[run.ID] = true
					before = run.ID
				}
			}
			for id, capability := range acceptedIDs {
				want := wantCapability == "" || capability == wantCapability
				if seen[id] != want {
					t.Fatalf("lost admission run=%d capability=%s limit=%d", id, capability, limit)
				}
			}
		}
	}
	search := func(id int64, query string) map[string]any {
		s := call("library_search_sources", map[string]any{"series_id": id, "query": query, "limit": 2}, false)
		acceptedIDs[int64(s["run"].(map[string]any)["run_id"].(float64))] = "read"
		call("library_wait_run", map[string]any{"run_id": s["run"].(map[string]any)["run_id"], "timeout_ms": 30000}, false)
		return call("library_get_source_search", map[string]any{"search_id": s["search_id"]}, false)
	}
	uncertain := search(ambiguous, "Ambiguous")
	if len(uncertain["sources"].([]any)) != 3 {
		t.Fatal(uncertain)
	}
	call("library_propose_match", map[string]any{"search_id": uncertain["search_id"], "identity_confirmed": true, "edition_confirmed": false, "summary": "Edition unresolved", "fields": map[string]string{"summary": "bangumi:0"}}, true)
	inbox := web("GET", "/metadata/reviews", nil, 200)
	if inbox["total"] != float64(0) || state(ambiguous)["summary"] != nil {
		t.Fatal("ambiguous persisted", inbox)
	}
	broken := search(failed, "Failed")
	if broken["status"] != "failed" || state(failed)["summary"] != nil {
		t.Fatal(broken)
	}
	found := search(target, "Identity")
	call("library_assess_match", map[string]any{"search_id": found["search_id"]}, true)
	if calls.Load() != 0 {
		t.Fatal("model called without allowance")
	}
	model := call("library_assess_match", map[string]any{"search_id": found["search_id"], "allowance": 1}, false)
	call("library_wait_run", map[string]any{"run_id": model["run_id"], "timeout_ms": 30000}, false)
	acceptedIDs[int64(model["run_id"].(float64))] = "internal_llm"
	assertRunPages("")
	usage := call("library_get_model_operation", map[string]any{"operation_id": model["operation_id"]}, false)
	if usage["items"].([]any)[0].(map[string]any)["outcome"] != "uncertain" || len(usage["unprocessed"].([]any)) != 5 {
		t.Fatal(usage)
	}
	wantCalls := int32(1)
	assertBudget := func() {
		t.Helper()
		m := call("library_assess_match", map[string]any{"search_id": found["search_id"], "allowance": 1}, false)
		if m["operation_id"] != model["operation_id"] || m["used"] != float64(1) || m["remaining"] != float64(0) || calls.Load() != wantCalls {
			t.Fatal("budget reset/reissued", m, calls.Load())
		}
	}
	session.Close()
	connect()
	assertBudget()
	if web("GET", "/metadata/reviews", nil, 200)["total"] != float64(0) {
		t.Fatal("model uncertainty created proposal")
	}
	confirm := func(s map[string]any) any {
		return call("library_propose_match", map[string]any{"search_id": s["search_id"], "identity_confirmed": true, "edition_confirmed": true, "summary": "ComicInfo Original edition and page two credit identify the same release", "fields": map[string]string{"summary": "bangumi:0", "authors": "anilist:0", "publisher": "anilist:0", "tags": "bangumi:0"}}, false)["proposal_id"]
	}
	p := confirm(found)
	review := call("library_get_match_proposal", map[string]any{"series_id": target, "proposal_id": p}, false)
	origins := review["evidence"].(map[string]any)["fields"].(map[string]any)
	if origins["summary"].(map[string]any)["source"] != "bangumi" || origins["authors"].(map[string]any)["source"] != "anilist" {
		t.Fatal(review)
	}
	input := func(key string, p any) map[string]any {
		return map[string]any{"request_key": key, "series_id": target, "proposal_ids": []any{p}, "fields": []string{"summary", "authors", "cover"}, "expected": state(target), "cover": map[string]any{"book_id": book.ID, "page": 2, "quality_compared": true, "reason": "Blue page is complete artwork; red page is advertisement"}, "author_corrections": []any{map[string]any{"from": map[string]string{"name": "Alias", "role": "Artist"}, "to": map[string]string{"name": "Canonical", "role": "Writer"}, "reason": "ComicInfo and AniList credit agree"}}}
	}
	in := input("integrated-publish", p)
	if _, err = store.DB().Exec(fmt.Sprintf(`CREATE TRIGGER integrated_author_fault BEFORE INSERT ON series_authors WHEN NEW.series_id=%d BEGIN SELECT RAISE(ABORT,'controlled author failure'); END`, target)); err != nil {
		t.Fatal(err)
	}
	failedInput := map[string]any{}
	for k, v := range in {
		failedInput[k] = v
	}
	failedInput["request_key"] = "integrated-atomic-failure"
	fault := call("library_enrich_metadata", failedInput, false)
	if _, err = store.DB().Exec(`DROP TRIGGER integrated_author_fault`); err != nil {
		t.Fatal(err)
	}
	if fault["outcome"] != "failed" || state(target)["summary"] != nil || state(target)["cover"].(map[string]any)["path"] != oldPath || !reflect.DeepEqual(state(target)["authors"], state(shared)["authors"]) {
		t.Fatal("half publication", fault)
	}

	assertPublic := func(blue bool) {
		t.Helper()
		router := chi.NewRouter()
		process.HTTP.SetupRoutes(router)
		req := httptest.NewRequest("GET", fmt.Sprintf("/api/covers/%d", book.ID), nil)
		req.AddCookie(cookie)
		r := httptest.NewRecorder()
		router.ServeHTTP(r, req)
		decoded, _, e := image.Decode(bytes.NewReader(r.Body.Bytes()))
		if e != nil || r.Code != 200 {
			t.Fatalf("public image: %d %v", r.Code, e)
		}
		red, _, b, _ := decoded.At(20, 20).RGBA()
		if (b > red) != blue {
			t.Fatal("public cover/text mismatch")
		}
	}
	assertPublic(false)
	op := call("library_enrich_metadata", in, false)
	assertPublished := func() {
		t.Helper()
		s := state(target)
		if s["summary"] != "Verified journey" {
			t.Fatal(s)
		}
		a, _ := json.Marshal(s["authors"])
		if string(a) != `[{"name":"Canonical","role":"Writer"}]` {
			t.Fatal(s)
		}
		other, _ := json.Marshal(state(shared)["authors"])
		if string(other) != `[{"name":"Alias","role":"Artist"}]` {
			t.Fatal("shared author changed", string(other))
		}
		assertPublic(true)
	}
	if op["outcome"] != "changed" {
		t.Fatal(op)
	}
	assertPublished()
	closeProposal := func(p any, key string) {
		c := call("library_enrich_metadata", map[string]any{"request_key": key, "series_id": target, "proposal_ids": []any{p}, "fields": []string{}, "expected": state(target), "adjudication": map[string]string{"mode": "reject", "reason": "Discard remaining publisher/tag differences"}}, false)
		if a := c["adjudication"].(map[string]any); a["status"] != "supplemented_closed" || a["accepted"] != true {
			t.Fatal(c)
		}
	}
	decide := func(p any, key, field string) (map[string]any, map[string]any) {
		in := map[string]any{"request_key": key, "series_id": target, "proposal_ids": []any{p}, "fields": []string{field}, "expected": state(target), "adjudication": map[string]string{"mode": "partial", "reason": "Accept verified remaining field"}}
		out := call("library_enrich_metadata", in, false)
		if out["outcome"] != "changed" || out["adjudication"].(map[string]any)["accepted"] != true {
			t.Fatal(out)
		}
		return in, out
	}
	decide(p, "integrated-tags", "tags")
	closeProposal(p, "integrated-close")
	assertPublished()
	audit := web("GET", "/mcp/operations/"+op["id"].(string), nil, 200)
	raw, _ := json.Marshal(audit)
	if !strings.Contains(string(raw), "ComicInfo and AniList") {
		t.Fatal(audit)
	}
	report := map[int64]string{target: op["outcome"].(string), ambiguous: "ambiguous", failed: broken["status"].(string)}
	t.Logf("agent report from public IDs: %v", report)
	undo := call("library_undo_metadata_operation", map[string]any{"request_key": "integrated-undo", "operation_id": op["id"]}, false)
	if undo["outcome"] != "changed" || state(target)["summary"] != nil || !reflect.DeepEqual(state(target)["authors"], state(shared)["authors"]) {
		t.Fatal(undo)
	}
	assertPublic(false)
	if r := call("library_get_match_proposal", map[string]any{"series_id": target, "proposal_id": p}, false); r["status"] != "supplemented_closed" || r["accepted"] != true {
		t.Fatal("undo reopened terminal", r)
	}
	// New operation on the same series after safe undo; never reuse a terminal proposal.
	p2 := confirm(search(target, "Identity"))
	in2 := input("integrated-second-publish", p2)
	op2 := call("library_enrich_metadata", in2, false)
	if op2["outcome"] != "changed" {
		t.Fatal(op2)
	}
	decisionInput, decision := decide(p2, "integrated-publisher", "publisher")
	closeProposal(p2, "integrated-second-close")
	assertPublished()
	// Actual scan admission shares the engine; pause/cancel avoids changing this
	// fixture's deliberately curated metadata while still producing its real run.
	if _, err := process.Application.Engine.PauseAll(ctx); err != nil {
		t.Fatal(err)
	}
	scan := call("library_start_scan", map[string]any{"library_id": lib.ID}, false)
	acceptedIDs[int64(scan["run_id"].(float64))] = "scan_control"
	call("library_cancel_run", map[string]any{"run_id": scan["run_id"]}, false)
	if _, err := process.Application.Engine.ResumeAll(ctx); err != nil {
		t.Fatal(err)
	}
	cleanRun := call("library_start_metadata_cleanup", map[string]any{"library_id": lib.ID, "operations": []any{map[string]any{"operation_id": op["id"], "request_key": "recovery-cleanup"}}}, false)
	acceptedIDs[int64(cleanRun["run_id"].(float64))] = "cleanup"
	call("library_wait_run", map[string]any{"run_id": cleanRun["run_id"], "timeout_ms": 30000}, false)
	// A different credential of the same user/library creates a genuine search,
	// which must not appear in the owner's reconnect listing.
	ownerCredential := issued
	ownerSession := session
	issued = web("POST", "/mcp/credentials", map[string]any{"user_id": 1, "name": "sibling", "library_ids": []int64{lib.ID}, "capabilities": []string{"read"}}, 200)
	connect()
	sibling := call("library_search_sources", map[string]any{"series_id": target, "query": "Identity", "limit": 2}, false)
	call("library_wait_run", map[string]any{"run_id": sibling["run"].(map[string]any)["run_id"], "timeout_ms": 30000}, false)
	session.Close()
	issued, session = ownerCredential, ownerSession
	session.Close()
	connect()
	assertRunPages("")
	call("library_list_runs", map[string]any{"library_id": outside.ID}, true)
	if _, err = store.DB().Exec(`CREATE TRIGGER integrated_cleanup_fault BEFORE UPDATE ON metadata_cleanup_requests WHEN NEW.payload LIKE '%"deleted"%' BEGIN SELECT RAISE(ABORT,'controlled result failure'); END`); err != nil {
		t.Fatal(err)
	}
	cleanup := map[string]any{"request_key": "integrated-cleanup", "operation_id": op2["id"]}
	call("library_cleanup_metadata_operation", cleanup, true)
	if _, err = store.DB().Exec(`DROP TRIGGER integrated_cleanup_fault`); err != nil {
		t.Fatal(err)
	}
	pending := call("library_get_metadata_cleanup", map[string]any{"request_key": "integrated-cleanup"}, false)
	if pending["outcome"] != "pending" {
		t.Fatal(pending)
	}
	cropped := call("library_get_metadata_operation", map[string]any{"operation_id": op2["id"]}, false)
	if cropped["undo_unavailable"] != true {
		t.Fatal(cropped)
	}
	assertPublished()
	// The very same target now has a consumed in-flight model unit as well as
	// a pending cleanup. Closing the production graph is not a network EOF.
	hold.Store(true)
	interruptedSearch := search(target, "Identity")
	interrupted := call("library_assess_match", map[string]any{"search_id": interruptedSearch["search_id"], "allowance": 2}, false)
	acceptedIDs[int64(interrupted["run_id"].(float64))] = "internal_llm"
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("model barrier not reached")
	}
	wantCalls = 2
	session.Close()
	session = nil
	server.Close()
	server = nil
	process.Close()
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = database.NewStore(cfg.Snapshot().Database.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	process, err = bootstrap.Open(ctx, store, cfg, options)
	if err != nil {
		t.Fatal(err)
	}
	startServer()
	connect()
	resumed := call("library_cleanup_metadata_operation", cleanup, false)
	if resumed["id"] != pending["id"] || resumed["replayed"] != true || resumed["outcome"] != "complete" {
		t.Fatal(resumed)
	}
	assertBudget()
	for _, m := range []map[string]any{
		call("library_get_model_operation", map[string]any{"operation_id": interrupted["operation_id"]}, false),
		call("library_assess_match", map[string]any{"search_id": interruptedSearch["search_id"], "allowance": 2}, false),
	} {
		if m["operation_id"] != interrupted["operation_id"] || m["used"] != float64(1) || m["remaining"] != float64(1) || m["items"].([]any)[0].(map[string]any)["outcome"] != "unknown" || calls.Load() != 2 {
			t.Fatal("restart refunded or resent model", m, calls.Load())
		}
	}
	assertPublished()
	call("library_cleanup_metadata_operation", map[string]any{"request_key": "integrated-decision-cleanup", "operation_id": decision["id"]}, false)
	unavailable := call("library_undo_metadata_operation", map[string]any{"request_key": "integrated-cleaned-undo", "operation_id": op2["id"]}, false)
	if unavailable["outcome"] != "unavailable" || unavailable["applied"] != false {
		t.Fatal(unavailable)
	}
	replay := call("library_enrich_metadata", decisionInput, false)
	if replay["id"] != decision["id"] || replay["replayed"] != true || replay["undo_unavailable"] != true || !reflect.DeepEqual(replay["adjudication"], decision["adjudication"]) {
		t.Fatal("cropping lost permanent receipt", replay)
	}
	r := call("library_get_match_proposal", map[string]any{"series_id": target, "proposal_id": p2}, false)
	if r["status"] != "supplemented_closed" || r["accepted"] != true {
		t.Fatal(r)
	}
	// Same production graph and target: source runs cross current permission and
	// password transactions. The source barrier holds no metadata transaction.
	original := issued
	session.Close()
	for _, scenario := range []string{"queued-permission", "queued-password", "active-permission", "active-password", "eof-cancel"} {
		user, e := store.CreateUser(ctx, database.CreateUserParams{Username: scenario, Role: database.RoleAdmin})
		if e != nil {
			t.Fatal(e)
		}
		issued = web("POST", "/mcp/credentials", map[string]any{"user_id": user.ID, "name": scenario, "library_ids": []int64{lib.ID}, "capabilities": []string{"read"}}, 200)
		connect()
		queued := strings.HasPrefix(scenario, "queued")
		if queued {
			if _, e = process.Application.Engine.PauseAll(ctx); e != nil {
				t.Fatal(e)
			}
		}
		started := call("library_search_sources", map[string]any{"series_id": target, "query": scenario, "limit": 2}, false)
		runID := int64(started["run"].(map[string]any)["run_id"].(float64))
		barrier, active := barriers[scenario]
		if active {
			select {
			case <-barrier.entered:
			case <-ctx.Done():
				t.Fatal("source barrier not reached")
			}
		}
		if scenario == "eof-cancel" {
			// SDK Close sends EOF and reaps the real helper in stdio mode.
			session.Close()
			connect()
			r := call("library_get_run", map[string]any{"run_id": runID}, false)
			if r["status"] != "running" {
				t.Fatal("EOF cancelled accepted run", r)
			}
			call("library_cancel_run", map[string]any{"run_id": runID}, false)
		} else if strings.HasSuffix(scenario, "password") {
			web("POST", fmt.Sprintf("/users/%d/password", user.ID), map[string]string{"password": "new correct horse battery staple"}, 200)
		} else {
			web("PATCH", "/mcp/credentials/"+issued["id"].(string), map[string]any{"user_id": user.ID, "name": scenario, "library_ids": []int64{}, "capabilities": []string{"read"}}, 200)
		}
		if active {
			close(barrier.release)
		}
		if queued {
			if _, e = process.Application.Engine.ResumeAll(ctx); e != nil {
				t.Fatal(e)
			}
		}
		terminal, e := process.Application.Engine.Await(ctx, runID)
		if e != nil || terminal.Status != task.StatusCancelled || terminal.StartedAt.IsZero() != queued {
			t.Fatalf("%s: %+v %v", scenario, terminal, e)
		}
		if stateAfter := web("GET", fmt.Sprintf("/series/info/%d", target), nil, 200); stateAfter["summary"].(map[string]any)["String"] != "Verified journey" {
			t.Fatal("revocation rolled back committed series", stateAfter)
		}
		t.Logf("%s source run=%d cancelled, queued=%v", scenario, runID, queued)
		session.Close()
	}
	issued = original
	connect()
	finalCapabilities := []string{finalCapability}
	if finalCapability == "cleanup" {
		finalCapabilities = append(finalCapabilities, "metadata_write")
	}
	web("PATCH", "/mcp/credentials/"+issued["id"].(string), map[string]any{"user_id": 1, "name": "integrated", "library_ids": []int64{lib.ID}, "capabilities": finalCapabilities}, 200)
	assertRunPages(finalCapability)
	for id, required := range acceptedIDs {
		if required != finalCapability {
			call("library_get_run", map[string]any{"run_id": id}, true)
		}
	}
	call("library_enrich_metadata", decisionInput, true)
	altered := map[string]any{}
	for k, v := range decisionInput {
		altered[k] = v
	}
	delete(altered, "adjudication")
	call("library_enrich_metadata", altered, true)
	finalAudit := web("GET", "/mcp/operations/"+decision["id"].(string), nil, 200)
	if finalAudit["undo_unavailable"] != true || finalAudit["adjudication"] == nil {
		t.Fatal(finalAudit)
	}
	t.Logf("lineage series=%d search=%v model=%v proposal=%v operation=%v cleanup=%v; reopened normally, no power-loss claim", target, found["search_id"], model["operation_id"], p2, op2["id"], resumed["id"])
}
