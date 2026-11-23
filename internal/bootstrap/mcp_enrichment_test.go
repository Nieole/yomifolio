package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/api"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
)

type enrichmentBarrier struct{ entered, release chan struct{} }
type enrichmentStore struct {
	database.Store
	mu      sync.Mutex
	barrier *enrichmentBarrier
}

func (s *enrichmentStore) GetMetadataReview(ctx context.Context, id int64) (database.MetadataReview, error) {
	out, err := s.Store.GetMetadataReview(ctx, id)
	s.mu.Lock()
	barrier := s.barrier
	s.barrier = nil
	s.mu.Unlock()
	if barrier != nil {
		close(barrier.entered)
		select {
		case <-barrier.release:
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
	return out, err
}
func (s *enrichmentStore) gate() *enrichmentBarrier {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := &enrichmentBarrier{make(chan struct{}), make(chan struct{})}
	s.barrier = b
	return b
}

func TestProductionMCPEnrichmentAuditUndo(t *testing.T) {
	ctx := t.Context()
	store, cfg := fixture(t)
	wrapped := &enrichmentStore{Store: store}
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Comics", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	makeSeries := func(name string) int64 {
		row, e := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: name, Path: lib.Path + "/" + name})
		if e != nil {
			t.Fatal(e)
		}
		return row.ID
	}
	a, b, c := makeSeries("Alpha"), makeSeries("Beta"), makeSeries("Gamma")

	// Existing personal classification is preserved, but never imported from sources.
	if _, err = store.DB().Exec(`INSERT INTO tags(name) VALUES('Personal shelf'); INSERT INTO series_tags(series_id,tag_id) SELECT ?,id FROM tags WHERE name='Personal shelf'`, a); err != nil {
		t.Fatal(err)
	}
	options := bootstrap.Options{Sources: api.ExternalSources{Metadata: func(_ config.Config, name string) metadata.Provider {
		return &controlledSource{search: func(context.Context, string, int, int) ([]*metadata.SeriesMetadata, int, error) {
			score, scale := 9.0, 10.0
			if name == "anilist" {
				score = 80
				scale = 100
			}
			return []*metadata.SeriesMetadata{{Title: "Space", Summary: "Space journey", Rating: score / scale * 10, SiteRating: &metadata.SiteRating{Value: score, Scale: scale}, Tags: []string{"Science Fiction", "favorite", "2026", "冒险", "Bangumi"}, SourceID: 1, SourceURL: "https://example.org/" + name}}, 1, nil
		}}
	}}}
	process, err := bootstrap.Open(ctx, wrapped, cfg, options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { process.Close() }()
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	server := httptest.NewTLSServer(router)
	defer server.Close()
	snapshot := cfg.Snapshot()
	snapshot.MCP.PublicURL = server.URL
	cfg.Replace(&snapshot)
	var cookie *http.Cookie
	csrf := ""
	web := func(method, path string, body any, status int) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, e := http.NewRequestWithContext(ctx, method, server.URL+"/api"+path, strings.NewReader(string(raw)))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
			req.Header.Set("X-CSRF-Token", csrf)
		}
		response, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		data, e := io.ReadAll(response.Body)
		if e != nil || response.StatusCode != status {
			t.Fatalf("web %s %s: %d %s %v", method, path, response.StatusCode, data, e)
		}
		if path == "/auth/setup" {
			cookie = response.Cookies()[0]
		}
		return data
	}
	var login struct {
		CSRF string `json:"csrf_token"`
	}
	_ = json.Unmarshal(web("POST", "/auth/setup", map[string]string{"username": "owner", "password": "correct horse battery staple"}, 200), &login)
	csrf = login.CSRF
	var issued struct{ ID, Token string }
	_ = json.Unmarshal(web("POST", "/mcp/credentials", map[string]any{"user_id": 1, "name": "enrich", "library_ids": []int64{lib.ID}, "capabilities": []string{"read", "metadata_write"}}, 200), &issued)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "enrichment-acceptance", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: issued.Token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, input any) map[string]any {
		t.Helper()
		result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: input})
		if e != nil || result.IsError {
			t.Fatalf("%s: %+v %v", name, result, e)
		}
		raw, _ := json.Marshal(result.StructuredContent)
		out := map[string]any{}
		_ = json.Unmarshal(raw, &out)
		return out
	}
	propose := func(series int64, source string) any {
		search := call("library_search_sources", map[string]any{"series_id": series, "query": "Space"})
		call("library_wait_run", map[string]any{"run_id": search["run"].(map[string]any)["run_id"], "timeout_ms": 30000})
		out := call("library_propose_match", map[string]any{"search_id": search["search_id"], "identity_confirmed": true, "edition_confirmed": true, "summary": "Same series and edition confirmed from directory and release", "fields": map[string]string{"summary": source + ":0", "tags": source + ":0", "rating": source + ":0"}})
		return out["proposal_id"]
	}
	verifyCoversAfterReopen := verifyAtomicCovers(t, store, cfg, session, router, process.Application.Engine, issued.Token, wrapped.gate, func() error { return process.Application.Scans.CleanupThumbnails(ctx, nil) }, call, web, propose)
	pa, pb := propose(a, "bangumi"), propose(a, "anilist")
	input := map[string]any{"request_key": "first", "series_id": a, "proposal_ids": []any{pa, pb}, "fields": []string{"summary", "rating", "tags", "source_links"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": a})}
	applied := call("library_enrich_metadata", input)
	if applied["outcome"] != "changed" || applied["applied"] != true {
		t.Fatal(applied)
	}
	after := applied["after"].(map[string]any)
	if after["rating"] != 8.5 || after["summary"] != "Space journey" || len(after["tags"].([]any)) != 3 || len(after["links"].([]any)) != 2 {
		t.Fatal(after)
	}
	replay := call("library_enrich_metadata", input)
	if replay["replayed"] != true || replay["applied"] != false || replay["id"] != applied["id"] {
		t.Fatal(replay)
	}

	changedRetry := map[string]any{"request_key": "first", "series_id": a, "proposal_ids": []any{pa}, "fields": []string{"summary"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": a})}
	rejectedRetry, retryErr := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_enrich_metadata", Arguments: changedRetry})
	if retryErr != nil || !rejectedRetry.IsError {
		t.Fatalf("same-key changed request was replayed: %+v %v", rejectedRetry, retryErr)
	}
	history := web("GET", "/mcp/operations", nil, 200)
	if !strings.Contains(string(history), "8.5") || !strings.Contains(string(history), "Same series") {
		t.Fatal(string(history))
	}

	ratings := applied["fields"].([]any)[1].(map[string]any)
	if ratings["formula"] != "(9/10*10 + 80/100*10)/2 = 8.5" || len(ratings["sources"].([]any)) != 2 {
		t.Fatal("lost reproducible site evidence", ratings)
	}
	// Stale unrelated fields do not block a real new write, and survive its undo.
	unrelated := makeSeries("Unrelated")
	pu := propose(unrelated, "bangumi")
	expected := call("library_get_enrichment_state", map[string]any{"series_id": unrelated})
	if _, err = store.DB().Exec(`UPDATE series SET publisher='Concurrent unrelated publisher' WHERE id=?`, unrelated); err != nil {
		t.Fatal(err)
	}
	independent := call("library_enrich_metadata", map[string]any{"request_key": "unrelated-edit", "series_id": unrelated, "proposal_ids": []any{pu}, "fields": []string{"summary"}, "expected": expected})
	if independent["outcome"] != "changed" || independent["after"].(map[string]any)["publisher"] != "Concurrent unrelated publisher" {
		t.Fatal(independent)
	}
	reverted := call("library_undo_metadata_operation", map[string]any{"request_key": "unrelated-undo", "operation_id": independent["id"]})
	if reverted["outcome"] != "changed" || reverted["after"].(map[string]any)["summary"] != nil || reverted["after"].(map[string]any)["publisher"] != "Concurrent unrelated publisher" {
		t.Fatal(reverted)
	}

	// A real SQLite association-write failure cannot leave the earlier scalar write.
	broken := makeSeries("StorageFailure")
	pf := propose(broken, "bangumi")
	if _, err = store.DB().Exec(fmt.Sprintf(`CREATE TRIGGER enrichment_test_failure BEFORE INSERT ON series_tags WHEN NEW.series_id=%d BEGIN SELECT RAISE(ABORT,'controlled association failure'); END`, broken)); err != nil {
		t.Fatal(err)
	}
	failed := call("library_enrich_metadata", map[string]any{"request_key": "storage-failure", "series_id": broken, "proposal_ids": []any{pf}, "fields": []string{"summary", "tags"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": broken})})
	if failed["outcome"] != "failed" || failed["applied"] != false {
		t.Fatal(failed)
	}
	if current := call("library_get_enrichment_state", map[string]any{"series_id": broken}); current["summary"] != nil || len(current["tags"].([]any)) != 0 {
		t.Fatal("half committed failure", current)
	}
	savedFailure := call("library_get_metadata_operation", map[string]any{"operation_id": failed["id"]})
	if savedFailure["outcome"] != "failed" {
		t.Fatal(savedFailure)
	}
	if _, err = store.DB().Exec(`DROP TRIGGER enrichment_test_failure`); err != nil {
		t.Fatal(err)
	}
	// Undo must retain a durable failed attempt, not just roll back the data. A tag
	// deletion fails after the scalar update, exercising a genuinely partial write.
	undoFailure := makeSeries("UndoStorageFailure")
	pundo := propose(undoFailure, "bangumi")
	undoOriginal := call("library_enrich_metadata", map[string]any{"request_key": "undo-fill", "series_id": undoFailure, "proposal_ids": []any{pundo}, "fields": []string{"summary", "tags"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": undoFailure})})
	if _, err = store.DB().Exec(fmt.Sprintf(`CREATE TRIGGER undo_test_failure BEFORE DELETE ON series_tags WHEN OLD.series_id=%d BEGIN SELECT RAISE(ABORT,'private-trigger /private/path secret-value'); END`, undoFailure)); err != nil {
		t.Fatal(err)
	}
	undoRequest := map[string]any{"request_key": "undo-storage-failure", "operation_id": undoOriginal["id"]}
	failedUndo := call("library_undo_metadata_operation", undoRequest)
	if failedUndo["outcome"] != "failed" || failedUndo["applied"] != false || failedUndo["parent_id"] != undoOriginal["id"] || failedUndo["request_key"] != "undo-storage-failure" {
		t.Fatal(failedUndo)
	}
	actor := failedUndo["actor"].(map[string]any)
	if actor["user_id"] != float64(1) || actor["credential_id"] != issued.ID {
		t.Fatal("lost undo identity", actor)
	}
	encoded, _ := json.Marshal(failedUndo)
	for _, private := range []string{"private-trigger", "/private/path", "secret-value", "RAISE", "DELETE"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("storage diagnostics leaked", string(encoded))
		}
	}
	if fmt.Sprint(failedUndo["before"]) != fmt.Sprint(failedUndo["after"]) {
		t.Fatal("failed receipt claims changes", failedUndo)
	}
	for _, field := range failedUndo["fields"].([]any) {
		if field.(map[string]any)["changed"] != false {
			t.Fatal("failed field claims change", field)
		}
	}
	if current := call("library_get_enrichment_state", map[string]any{"series_id": undoFailure}); current["summary"] != "Space journey" || len(current["tags"].([]any)) != 2 {
		t.Fatal("partial undo survived", current)
	}
	originalBeforeRetry := call("library_get_metadata_operation", map[string]any{"operation_id": undoOriginal["id"]})
	if originalBeforeRetry["undone_by"] != nil || originalBeforeRetry["after"].(map[string]any)["summary"] != "Space journey" {
		t.Fatal("lost reversal material", originalBeforeRetry)
	}
	if saved := call("library_get_metadata_operation", map[string]any{"operation_id": failedUndo["id"]}); saved["outcome"] != "failed" {
		t.Fatal(saved)
	}
	if history := web("GET", "/mcp/operations", nil, 200); !strings.Contains(string(history), failedUndo["id"].(string)) {
		t.Fatal(string(history))
	}
	// Failure-receipt storage can itself fail. The public result must be an error,
	// not a fabricated durable failed receipt; the original data remains intact.
	if _, err = store.DB().Exec(`CREATE TRIGGER undo_journal_failure BEFORE INSERT ON metadata_operations WHEN NEW.request_key='undo-journal-unavailable' BEGIN SELECT RAISE(ABORT,'private-journal-error'); END`); err != nil {
		t.Fatal(err)
	}
	unavailable, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_undo_metadata_operation", Arguments: map[string]any{"request_key": "undo-journal-unavailable", "operation_id": undoOriginal["id"]}})
	if e != nil || !unavailable.IsError {
		t.Fatalf("journal failure claimed durability: %+v %v", unavailable, e)
	}
	publicError, _ := json.Marshal(unavailable)
	if strings.Contains(string(publicError), "private-journal-error") {
		t.Fatal("journal error leaked", string(publicError))
	}
	if _, err = store.DB().Exec(`DROP TRIGGER undo_journal_failure; UPDATE users SET role='user' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	unauthorized, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_undo_metadata_operation", Arguments: map[string]any{"request_key": "undo-denied-no-audit", "operation_id": undoOriginal["id"]}})
	if e != nil || !unauthorized.IsError {
		t.Fatalf("unauthorized undo became an audit probe: %+v %v", unauthorized, e)
	}
	if _, err = store.DB().Exec(`UPDATE users SET role='admin' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if history := web("GET", "/mcp/operations", nil, 200); strings.Contains(string(history), "undo-journal-unavailable") || strings.Contains(string(history), "undo-denied-no-audit") {
		t.Fatal("fabricated receipt", string(history))
	}
	if current := call("library_get_enrichment_state", map[string]any{"series_id": undoFailure}); current["summary"] != "Space journey" || len(current["tags"].([]any)) != 2 {
		t.Fatal("failure handling changed data", current)
	}
	// Removing the fault makes re-execution observable: replay must still fail,
	// preserve the original ID and leave the metadata untouched.
	if _, err = store.DB().Exec(`DROP TRIGGER undo_test_failure`); err != nil {
		t.Fatal(err)
	}
	if replay := call("library_undo_metadata_operation", undoRequest); replay["id"] != failedUndo["id"] || replay["outcome"] != "failed" || replay["replayed"] != true || replay["applied"] != false {
		t.Fatal(replay)
	}
	if current := call("library_get_enrichment_state", map[string]any{"series_id": undoFailure}); current["summary"] != "Space journey" {
		t.Fatal("replay wrote metadata", current)
	}
	mismatch, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_undo_metadata_operation", Arguments: map[string]any{"request_key": "undo-storage-failure", "operation_id": applied["id"]}})
	if e != nil || !mismatch.IsError {
		t.Fatalf("changed undo intent accepted: %+v %v", mismatch, e)
	}
	// A concurrent replay of the failed identity cannot hide a different-key success.
	type undoResult struct {
		result *mcp.CallToolResult
		err    error
	}
	results := make(chan undoResult, 2)
	for _, key := range []string{"undo-storage-failure", "undo-after-failure"} {
		go func() {
			result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_undo_metadata_operation", Arguments: map[string]any{"request_key": key, "operation_id": undoOriginal["id"]}})
			results <- undoResult{result, e}
		}()
	}
	var successfulUndoID string
	for range 2 {
		result := <-results
		if result.err != nil || result.result.IsError {
			t.Fatalf("concurrent undo: %+v %v", result.result, result.err)
		}
		raw, _ := json.Marshal(result.result.StructuredContent)
		var op map[string]any
		if err = json.Unmarshal(raw, &op); err != nil {
			t.Fatal(err)
		}
		if op["request_key"] == "undo-storage-failure" {
			if op["id"] != failedUndo["id"] || op["outcome"] != "failed" || op["replayed"] != true {
				t.Fatal(op)
			}
		} else {
			if op["outcome"] != "changed" || op["after"].(map[string]any)["summary"] != nil {
				t.Fatal(op)
			}
			successfulUndoID = op["id"].(string)
		}
	}
	if original := call("library_get_metadata_operation", map[string]any{"operation_id": undoOriginal["id"]}); successfulUndoID == "" || original["undone_by"] != successfulUndoID {
		t.Fatal("failed receipt hid successful undo", original)
	}
	// Whole tag-group changes and current locks block all selected undo fields.
	undoTool := func(key string) map[string]any {
		return call("library_undo_metadata_operation", map[string]any{"operation_id": applied["id"], "request_key": key})
	}
	if _, err = store.DB().Exec(`INSERT INTO tags(name) VALUES('Human tag'); INSERT INTO series_tags(series_id,tag_id) SELECT ?,id FROM tags WHERE name='Human tag'`, a); err != nil {
		t.Fatal(err)
	}
	if out := undoTool("tag-conflict"); out["outcome"] != "conflict" {
		t.Fatal(out)
	}
	if state := call("library_get_enrichment_state", map[string]any{"series_id": a}); state["summary"] != "Space journey" || len(state["tags"].([]any)) != 4 {
		t.Fatal("partial undo", state)
	}
	if _, err = store.DB().Exec(`DELETE FROM series_tags WHERE series_id=? AND tag_id IN (SELECT id FROM tags WHERE name='Human tag'); UPDATE series SET locked_fields='summary' WHERE id=?`, a, a); err != nil {
		t.Fatal(err)
	}
	if out := undoTool("lock-conflict"); out["outcome"] != "conflict" {
		t.Fatal(out)
	}
	if _, err = store.DB().Exec(`UPDATE series SET locked_fields='', publisher='Human publisher' WHERE id=?`, a); err != nil {
		t.Fatal(err)
	}
	// An unrelated publisher edit must not block replay or later undo.
	if state := call("library_get_enrichment_state", map[string]any{"series_id": a}); state["publisher"] != "Human publisher" {
		t.Fatal(state)
	}
	preserved := call("library_enrich_metadata", map[string]any{"request_key": "preserve-existing", "series_id": a, "proposal_ids": []any{pa}, "fields": []string{"summary", "rating", "tags", "source_links"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": a})})
	if preserved["outcome"] != "no-change" || preserved["applied"] != false {
		t.Fatal(preserved)
	}
	// Current user role, not just credential capabilities, bounds every write.
	if _, err = store.DB().Exec(`UPDATE users SET role='user' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	web("GET", "/mcp/operations", nil, 403)
	web("POST", fmt.Sprintf("/mcp/operations/%s/undo", applied["id"]), map[string]string{"request_key": "ordinary-user"}, 403)
	denied, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_undo_metadata_operation", Arguments: map[string]any{"operation_id": applied["id"], "request_key": "ordinary-mcp"}})
	if e != nil || !denied.IsError {
		t.Fatalf("ordinary MCP undo bypass: %+v %v", denied, e)
	}
	if _, err = store.DB().Exec(`UPDATE users SET role='admin' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	// A barrier at the persisted-evidence boundary deterministically puts a concurrent
	// editor, lock or revocation before the actual write transaction, never via sleeps.
	pc := propose(c, "bangumi")
	for _, scenario := range []string{"value", "lock", "revoke"} {
		in := map[string]any{"request_key": scenario, "series_id": c, "proposal_ids": []any{pc}, "fields": []string{"summary", "tags"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": c})}
		gate := wrapped.gate()
		done := make(chan *mcp.CallToolResult, 1)
		fail := make(chan error, 1)
		go func() {
			out, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_enrich_metadata", Arguments: in})
			if e != nil {
				fail <- e
				return
			}
			done <- out
		}()
		<-gate.entered
		switch scenario {
		case "value":
			_, err = store.DB().Exec(`UPDATE series SET summary='Concurrent editor' WHERE id=?`, c)
		case "lock":
			_, err = store.DB().Exec(`UPDATE series SET locked_fields='tags' WHERE id=?`, c)
		case "revoke":
			web("DELETE", "/mcp/credentials/"+issued.ID, nil, 200)
		}
		if err != nil {
			t.Fatal(err)
		}
		close(gate.release)
		select {
		case e := <-fail:
			t.Fatal(e)
		case result := <-done:
			if scenario == "revoke" {
				if !result.IsError {
					t.Fatal("revocation did not fence write", result)
				}
			} else {
				raw, _ := json.Marshal(result.StructuredContent)
				if !strings.Contains(string(raw), `"outcome":"conflict"`) {
					t.Fatal(string(raw))
				}
				var receipt struct {
					Fields []json.RawMessage `json:"fields"`
				}
				if e := json.Unmarshal(raw, &receipt); e != nil || len(receipt.Fields) != 2 {
					t.Fatalf("conflict must retain all selected field evidence: %s", raw)
				}
			}
		}
	}
	// Administrator undo remains available after the originating credential is revoked.
	undo := web("POST", fmt.Sprintf("/mcp/operations/%s/undo", applied["id"]), map[string]string{"request_key": "web-undo"}, 200)
	if !strings.Contains(string(undo), `"outcome":"changed"`) {
		t.Fatal(string(undo))
	}
	original := web("GET", fmt.Sprintf("/mcp/operations/%s", applied["id"]), nil, 200)
	if !strings.Contains(string(original), "undone_by") {
		t.Fatal(string(original))
	}

	var restored struct {
		After struct {
			Summary   *string  `json:"summary"`
			Publisher *string  `json:"publisher"`
			Rating    *float64 `json:"rating"`
			Tags      []string `json:"tags"`
			Links     []any    `json:"links"`
		} `json:"after"`
	}
	if e := json.Unmarshal(undo, &restored); e != nil || restored.After.Summary != nil || restored.After.Rating != nil || len(restored.After.Tags) != 1 || restored.After.Tags[0] != "Personal shelf" || len(restored.After.Links) != 0 || restored.After.Publisher == nil || *restored.After.Publisher != "Human publisher" {
		t.Fatalf("undo lost unrelated edits: %s", undo)
	}
	// A fresh credential can process independent targets; one conflict cannot roll back another.
	_ = json.Unmarshal(web("POST", "/mcp/credentials", map[string]any{"user_id": 1, "name": "enrich-again", "library_ids": []int64{lib.ID}, "capabilities": []string{"read", "metadata_write"}}, 200), &issued)
	_ = session.Close()
	session, err = mcp.NewClient(&mcp.Implementation{Name: "enrichment-retry", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: issued.Token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	pbb := propose(b, "bangumi")
	batch := call("library_enrich_metadata_batch", map[string]any{"operations": []any{
		map[string]any{"request_key": "batch-b", "series_id": b, "proposal_ids": []any{pbb}, "fields": []string{"summary"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": b})},
		map[string]any{"request_key": "batch-c", "series_id": c, "proposal_ids": []any{pc}, "fields": []string{"tags"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": c})},
	}})
	ops := batch["operations"].([]any)
	if ops[0].(map[string]any)["outcome"] != "changed" || ops[1].(map[string]any)["outcome"] != "conflict" {
		t.Fatal(batch)
	}
	// Reopen the production graph and real SQLite connection, retaining browser session.
	_ = session.Close()
	server.Close()
	process.Close()
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.NewStore(cfg.Snapshot().Database.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	process, err = bootstrap.Open(ctx, reopened, cfg, options)
	if err != nil {
		t.Fatal(err)
	}
	router = chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	server = httptest.NewTLSServer(router)
	defer server.Close()
	verifyRestoredCover := verifyCoversAfterReopen(web)
	var persistedFailure map[string]any
	if err = json.Unmarshal(web("GET", fmt.Sprintf("/mcp/operations/%s", failedUndo["id"]), nil, 200), &persistedFailure); err != nil || persistedFailure["outcome"] != "failed" || persistedFailure["actor"].(map[string]any)["credential_id"] != actor["credential_id"] {
		t.Fatal(persistedFailure, err)
	}
	persisted := web("GET", "/mcp/operations", nil, 200)
	if !strings.Contains(string(persisted), "undone_by") || !strings.Contains(string(persisted), "batch-b") {
		t.Fatal(string(persisted))
	}
	undo = web("POST", fmt.Sprintf("/mcp/operations/%s/undo", ops[0].(map[string]any)["id"]), map[string]string{"request_key": "after-reopen"}, 200)
	if !strings.Contains(string(undo), `"outcome":"changed"`) {
		t.Fatal(string(undo))
	}
	server.Close()
	process.Close()
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err = database.NewStore(cfg.Snapshot().Database.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	process, err = bootstrap.Open(ctx, reopened, cfg, options)
	if err != nil {
		t.Fatal(err)
	}
	router = chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	server = httptest.NewTLSServer(router)
	defer server.Close()
	verifyRestoredCover(web)

}
