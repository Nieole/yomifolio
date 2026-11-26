// Guards that every registered tool hands text-only clients its whole result: a successful call
// carries structuredContent again as JSON text. A tool this test never calls successfully fails it,
// so a new tool cannot join the listing without proving its text copy.

package bootstrap_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/api"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
)

func TestMCPEveryToolResultCarriesItsJSONText(t *testing.T) {
	ctx := t.Context()
	store, cfg := fixture(t)
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Text", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{Name: "Copy", LibraryID: lib.ID, Path: filepath.Join(lib.Path, "Copy")})
	if err != nil {
		t.Fatal(err)
	}
	page := image.NewRGBA(image.Rect(0, 0, 64, 96))
	for i := range page.Pix {
		page.Pix[i] = 200
	}
	var picture bytes.Buffer
	if err = png.Encode(&picture, page); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	for name, data := range map[string][]byte{"01.png": picture.Bytes(), "ComicInfo.xml": []byte(`<ComicInfo><Series>Copy</Series></ComicInfo>`)} {
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
	path := filepath.Join(lib.Path, "Copy.cbz")
	if err = os.WriteFile(path, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	modified := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	book, err := store.CreateBook(ctx, database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID, Name: "Copy.cbz", Path: path, Size: int64(archive.Len()), FileModifiedAt: modified, PageCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	cover := automaticCoverPath(path, modified, int64(archive.Len()))
	coverFile := filepath.Join(config.ThumbnailDir(cfg.Snapshot()), cover)
	if err = os.MkdirAll(filepath.Dir(coverFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(coverFile, picture.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().Exec(`UPDATE books SET cover_path=? WHERE id=?`, cover, book.ID); err != nil {
		t.Fatal(err)
	}
	var modelCalls atomic.Int32
	var hold atomic.Bool
	sources := api.ExternalSources{
		AI: func(string, string, string, string, string, string, int) metadata.AIProvider {
			return budgetAI{calls: &modelCalls, hold: &hold, entered: make(chan struct{}, 1)}
		},
		Metadata: func(config.Config, string) metadata.Provider {
			return &controlledSource{search: func(context.Context, string, int, int) ([]*metadata.SeriesMetadata, int, error) {
				return []*metadata.SeriesMetadata{{Title: "Copy", Summary: "Copied summary", Publisher: "Copy press", SourceID: 1}}, 1, nil
			}}
		},
	}
	process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	web := sourceHTTP(t, process)
	var issued struct{ Token string }
	raw := web("POST", "/mcp/credentials", fmt.Sprintf(`{"user_id":1,"name":"text","library_ids":[%d],"capabilities":["read","metadata_write","scan_control","internal_llm","cleanup"]}`, lib.ID), 200)
	if err = json.Unmarshal(raw.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	server := httptest.NewServer(router)
	defer server.Close()
	snapshot := cfg.Snapshot()
	snapshot.MCP.PublicURL = server.URL
	snapshot.MCP.AllowLANHTTP = true
	cfg.Replace(&snapshot)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "text", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: issued.Token}}}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	copied := map[string]bool{}
	call := func(name string, args any) map[string]any {
		t.Helper()
		result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil || result.IsError {
			t.Fatalf("%s: %+v %v", name, result, e)
		}
		structured, e := json.Marshal(result.StructuredContent)
		if e != nil || result.StructuredContent == nil {
			t.Fatalf("%s: no structured result %v", name, e)
		}
		var want any
		if e = json.Unmarshal(structured, &want); e != nil {
			t.Fatal(e)
		}
		for _, block := range result.Content {
			text, ok := block.(*mcp.TextContent)
			if !ok {
				continue
			}
			var got any
			if json.Unmarshal([]byte(text.Text), &got) == nil && reflect.DeepEqual(got, want) {
				copied[name] = true
			}
		}
		if !copied[name] {
			t.Errorf("%s: no text block repeats the structured result %s", name, structured)
		}
		object, _ := want.(map[string]any)
		return object
	}
	wait := func(run any) {
		t.Helper()
		call("library_wait_run", map[string]any{"run_id": run, "timeout_ms": 30000})
	}

	call("library_get_series", map[string]any{"series_id": series.ID})
	call("library_find_series", map[string]any{"missing_fields": []string{"summary"}})
	call("library_list_books", map[string]any{"series_id": series.ID})
	call("library_get_book_clues", map[string]any{"book_id": book.ID})
	call("library_get_series_cover", map[string]any{"series_id": series.ID})
	call("library_get_page_thumbnails", map[string]any{"series_id": series.ID, "book_id": book.ID, "pages": []int{1}})

	started := call("library_search_sources", map[string]any{"series_id": series.ID, "query": "Copy", "limit": 1})
	wait(started["run"].(map[string]any)["run_id"])
	call("library_get_source_search", map[string]any{"search_id": started["search_id"]})
	proposal := call("library_propose_match", map[string]any{"search_id": started["search_id"], "identity_confirmed": true, "edition_confirmed": true, "summary": "ComicInfo series and title agree", "fields": map[string]string{"summary": "bangumi:0", "publisher": "bangumi:0"}})["proposal_id"]
	call("library_get_match_proposal", map[string]any{"series_id": series.ID, "proposal_id": proposal})
	state := func() map[string]any {
		return call("library_get_enrichment_state", map[string]any{"series_id": series.ID})
	}
	single := call("library_enrich_metadata", map[string]any{"request_key": "text-single", "series_id": series.ID, "proposal_ids": []any{proposal}, "fields": []string{"summary"}, "expected": state()})
	batch := call("library_enrich_metadata_batch", map[string]any{"operations": []any{map[string]any{"request_key": "text-batch", "series_id": series.ID, "proposal_ids": []any{proposal}, "fields": []string{"publisher"}, "expected": state()}}})
	call("library_get_metadata_operation", map[string]any{"operation_id": single["id"]})
	call("library_list_metadata_operations", map[string]any{"series_id": series.ID})
	call("library_undo_metadata_operation", map[string]any{"request_key": "text-undo", "operation_id": single["id"]})

	model := call("library_assess_match", map[string]any{"search_id": started["search_id"], "allowance": 1})
	wait(model["run_id"])
	call("library_get_model_operation", map[string]any{"operation_id": model["operation_id"]})

	// A paused engine keeps the scan from touching the curated fixture while it still yields a run.
	if _, err = process.Application.Engine.PauseAll(ctx); err != nil {
		t.Fatal(err)
	}
	scan := call("library_start_scan", map[string]any{"library_id": lib.ID})
	call("library_get_run", map[string]any{"run_id": scan["run_id"]})
	call("library_cancel_run", map[string]any{"run_id": scan["run_id"]})
	if _, err = process.Application.Engine.ResumeAll(ctx); err != nil {
		t.Fatal(err)
	}
	call("library_list_runs", map[string]any{"library_id": lib.ID})

	call("library_cleanup_metadata_operation", map[string]any{"request_key": "text-cleanup-single", "operation_id": single["id"]})
	batched := batch["operations"].([]any)[0].(map[string]any)["id"]
	cleanup := call("library_start_metadata_cleanup", map[string]any{"library_id": lib.ID, "operations": []any{map[string]any{"operation_id": batched, "request_key": "text-cleanup-batch"}}})
	wait(cleanup["run_id"])
	call("library_get_metadata_cleanup", map[string]any{"request_key": "text-cleanup-batch"})

	call("library_identification_progress", map[string]any{"library_ids": []int64{lib.ID}})
	call("library_find_duplicate_series", map[string]any{"library_ids": []int64{lib.ID}})
	call("library_count_tags", map[string]any{"sort": "name"})
	call("library_report_series", map[string]any{"series_id": series.ID, "category": "suspected_duplicate", "reason": "same directory name as another series"})
	sequel, err := store.CreateSeries(ctx, database.CreateSeriesParams{Name: "Copy sequel", LibraryID: lib.ID, Path: filepath.Join(lib.Path, "Copy sequel")})
	if err != nil {
		t.Fatal(err)
	}
	call("library_add_series_relation", map[string]any{"request_key": "text-relation", "series_id": series.ID, "target_series_id": sequel.ID, "relation_type": "sequel", "reason": "Sequel volume names it"})

	unpaced(t, cfg)
	var launch struct {
		RunID int64 `json:"run_id"`
	}
	if err = json.Unmarshal(web("POST", "/identification/prefetch", fmt.Sprintf(`{"library_id":%d}`, lib.ID), 200).Body.Bytes(), &launch); err != nil {
		t.Fatal(err)
	}
	if _, err = process.Application.Engine.Await(ctx, launch.RunID); err != nil {
		t.Fatal(err)
	}
	claimed := call("library_claim_series", map[string]any{"library_ids": []int64{lib.ID}})
	first := claimed["series"].([]any)[0].(map[string]any)["series_id"]
	call("library_submit_verdicts", map[string]any{"request_key": "text-verdicts", "lease_id": claimed["lease_id"], "verdicts": []any{
		map[string]any{"series_id": first, "verdict": "escalate", "category": "tag_problem", "reason": "tags name the shop"},
	}})

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if !copied[tool.Name] {
			t.Errorf("%s: no successful call here proves its text copy", tool.Name)
		}
	}
}
