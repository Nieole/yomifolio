package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"yomifolio/internal/api"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
)

func TestProductionMCPAuthorCorrections(t *testing.T)      { runProductionAuthorCorrections(t, false) }
func TestProductionMCPAuthorCorrectionsStdio(t *testing.T) { runProductionAuthorCorrections(t, true) }
func runProductionAuthorCorrections(t *testing.T, stdio bool) {
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
	a, b := makeSeries("Alpha"), makeSeries("Beta")

	// Existing personal classification is preserved, but never imported from sources.
	if _, err = store.DB().Exec(`INSERT INTO tags(name) VALUES('Personal shelf'); INSERT INTO series_tags(series_id,tag_id) SELECT ?,id FROM tags WHERE name='Personal shelf'`, a); err != nil {
		t.Fatal(err)
	}
	options := bootstrap.Options{Sources: api.ExternalSources{Metadata: func(_ config.Config, name string) metadata.Provider {
		return &controlledSource{search: func(context.Context, string, int, int) ([]*metadata.SeriesMetadata, int, error) {
			sourceAuthors := []metadata.SeriesAuthor{{Name: "Canonical", Role: "Writer"}, {Name: "Artist", Role: "Artist"}}
			score, scale := 9.0, 10.0
			if name == "anilist" {
				sourceAuthors = []metadata.SeriesAuthor{{Name: "Alias", Role: "Artist"}}
				score = 80
				scale = 100
			}
			return []*metadata.SeriesMetadata{{Authors: sourceAuthors, Title: "Space", Summary: "Space journey", Rating: score / scale * 10, SiteRating: &metadata.SiteRating{Value: score, Scale: scale}, Tags: []string{"Science Fiction", "favorite", "2026", "冒险", "Bangumi"}, SourceID: 1, SourceURL: "https://example.org/" + name}}, 1, nil
		}}
	}}}
	process, err := bootstrap.Open(ctx, wrapped, cfg, options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { process.Close() }()
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	serve := httptest.NewTLSServer
	if stdio {
		serve = httptest.NewServer
	}
	server := serve(router)
	defer server.Close()
	snapshot := cfg.Snapshot()
	snapshot.MCP.PublicURL = server.URL
	snapshot.MCP.AllowLANHTTP = stdio
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
	var transport mcp.Transport = &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: issued.Token}}}
	if stdio {
		binary := filepath.Join(t.TempDir(), "mcp-proxy.exe")
		if output, e := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/mcp-proxy").CombinedOutput(); e != nil {
			t.Fatalf("build proxy: %s %v", output, e)
		}
		cmd := exec.CommandContext(ctx, binary, "--url", server.URL, "--allow-lan-http")
		cmd.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN="+issued.Token)
		transport = &mcp.CommandTransport{Command: cmd}
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "authors-acceptance", Version: "1"}, nil).Connect(ctx, transport, nil)
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
		out := call("library_propose_match", map[string]any{"search_id": search["search_id"], "identity_confirmed": true, "edition_confirmed": true, "summary": "Same series and edition confirmed from directory and release", "fields": map[string]string{"summary": source + ":0", "tags": source + ":0", "rating": source + ":0", "authors": source + ":0"}})
		return out["proposal_id"]
	}
	// Both series share the exact identity, whose role is part of its uniqueness.
	if _, err = store.DB().Exec(`INSERT INTO authors(name,role) VALUES('Alias','Artist')`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{a, b} {
		if _, err = store.DB().Exec(`INSERT INTO series_authors SELECT ?,id FROM authors WHERE name='Alias'`, id); err != nil {
			t.Fatal(err)
		}
	}

	p := propose(a, "bangumi")
	aliasProposal := propose(a, "anilist")
	expected := call("library_get_enrichment_state", map[string]any{"series_id": a})
	input := map[string]any{"request_key": "authors-first", "series_id": a, "proposal_ids": []any{p, aliasProposal}, "fields": []string{"summary", "authors"}, "expected": expected, "author_corrections": []any{map[string]any{"from": map[string]string{"name": "Alias", "role": "Artist"}, "to": map[string]string{"name": "Canonical", "role": "Writer"}, "reason": "Published credit uses canonical pen name and writing role"}}}
	invalid := map[string]any{}
	for k, v := range input {
		invalid[k] = v
	}
	invalid["request_key"] = "authors-unconfirmed"
	invalid["author_corrections"] = []any{map[string]any{"from": map[string]string{"name": "Alias", "role": "Artist"}, "to": map[string]string{"name": "Invented identity", "role": "Writer"}, "reason": "Not in confirmed evidence"}}
	rejected, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_enrich_metadata", Arguments: invalid})
	if e != nil || !rejected.IsError {
		t.Fatal("arbitrary author fact accepted", rejected, e)
	}

	applied := call("library_enrich_metadata", input)
	if applied["outcome"] != "changed" {
		t.Fatal(applied)
	}
	after := applied["after"].(map[string]any)
	authors := after["authors"].([]any)
	authorJSON, _ := json.Marshal(authors)
	if string(authorJSON) != `[{"name":"Artist","role":"Artist"},{"name":"Canonical","role":"Writer"}]` || after["summary"] != "Space journey" {
		t.Fatal(after)
	}

	replay := call("library_enrich_metadata", input)
	if replay["id"] != applied["id"] || replay["replayed"] != true || replay["applied"] != false {
		t.Fatal(replay)
	}
	// Same key cannot hide a different identity, role, or correction reason.
	savedCorrections := input["author_corrections"]
	for _, correction := range []map[string]any{
		{"from": map[string]string{"name": "Alias", "role": "Artist"}, "to": map[string]string{"name": "Artist", "role": "Artist"}, "reason": "Published credit uses canonical pen name and writing role"},
		{"from": map[string]string{"name": "Alias", "role": "Artist"}, "to": map[string]string{"name": "Canonical", "role": "Artist"}, "reason": "Published credit uses canonical pen name and writing role"},
		{"from": map[string]string{"name": "Alias", "role": "Artist"}, "to": map[string]string{"name": "Canonical", "role": "Writer"}, "reason": "Different reason"},
	} {
		input["author_corrections"] = []any{correction}
		result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_enrich_metadata", Arguments: input})
		if e != nil || !result.IsError {
			t.Fatalf("different correction replayed: %+v %v", result, e)
		}
	}
	input["author_corrections"] = savedCorrections
	originalAuthors := expected["authors"]
	expected["authors"] = []any{map[string]string{"name": "Alias", "role": "Writer"}}
	result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_enrich_metadata", Arguments: input})
	if e != nil || !result.IsError {
		t.Fatalf("different expected author role replayed: %+v %v", result, e)
	}
	expected["authors"] = originalAuthors
	other := call("library_get_enrichment_state", map[string]any{"series_id": b})["authors"].([]any)
	if len(other) != 1 || other[0].(map[string]any)["name"] != "Alias" || other[0].(map[string]any)["role"] != "Artist" {
		t.Fatal(other)
	}
	history := web("GET", "/mcp/operations/"+applied["id"].(string), nil, 200)
	if !strings.Contains(string(history), "Published credit") || !strings.Contains(string(history), "Canonical") || !strings.Contains(string(history), "Alias") {
		t.Fatal(string(history))
	}
	undone := call("library_undo_metadata_operation", map[string]any{"request_key": "authors-undo", "operation_id": applied["id"]})
	if undone["outcome"] != "changed" || len(undone["after"].(map[string]any)["authors"].([]any)) != 1 {
		t.Fatal(undone)
	}
	// Stable group comparison: permutation and duplicate transport members are not edits.
	input["request_key"] = "authors-set"
	expected["authors"] = append(expected["authors"].([]any), expected["authors"].([]any)[0])
	input["expected"] = expected
	stable := call("library_enrich_metadata", input)
	if stable["outcome"] != "changed" {
		t.Fatal(stable)
	}
	// Every member of the group is fenced, not just the corrected identity.
	if _, err = store.DB().Exec(`INSERT INTO authors(name,role) VALUES('Unrelated credit','Writer')`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().Exec(`INSERT INTO series_authors SELECT ?,id FROM authors WHERE name='Unrelated credit'`, a); err != nil {
		t.Fatal(err)
	}
	conflict := call("library_undo_metadata_operation", map[string]any{"request_key": "authors-group-conflict", "operation_id": stable["id"]})
	if conflict["outcome"] != "conflict" {
		t.Fatal(conflict)
	}
	input["request_key"] = "authors-stale-group"
	if result := call("library_enrich_metadata", input); result["outcome"] != "conflict" {
		t.Fatal(result)
	}
	if _, err = store.DB().Exec(`DELETE FROM series_authors WHERE series_id=? AND author_id IN(SELECT id FROM authors WHERE name='Unrelated credit')`, a); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().Exec(`UPDATE series SET locked_fields='authors' WHERE id=?`, a); err != nil {
		t.Fatal(err)
	}
	if result := call("library_undo_metadata_operation", map[string]any{"request_key": "authors-locked-undo", "operation_id": stable["id"]}); result["outcome"] != "conflict" {
		t.Fatal(result)
	}
	if _, err = store.DB().Exec(`UPDATE series SET locked_fields='',publisher='Unrelated human edit' WHERE id=?`, a); err != nil {
		t.Fatal(err)
	}
	if result := call("library_undo_metadata_operation", map[string]any{"request_key": "authors-independent-undo", "operation_id": stable["id"]}); result["outcome"] != "changed" || result["after"].(map[string]any)["publisher"] != "Unrelated human edit" {
		t.Fatal(result)
	}
	// Actual SQLite INSERT failure after the scalar write rolls back both groups.
	if _, err = store.DB().Exec(fmt.Sprintf(`CREATE TRIGGER authors_fail BEFORE INSERT ON series_authors WHEN NEW.series_id=%d BEGIN SELECT RAISE(ABORT,'private diagnosis'); END`, a)); err != nil {
		t.Fatal(err)
	}
	input["request_key"] = "authors-failure"
	failed := call("library_enrich_metadata", input)
	if failed["outcome"] != "failed" {
		t.Fatal(failed)
	}
	state := call("library_get_enrichment_state", map[string]any{"series_id": a})
	if state["summary"] != nil || len(state["authors"].([]any)) != 1 || state["authors"].([]any)[0].(map[string]any)["name"] != "Alias" {
		t.Fatal("partial write", state)
	}
	if _, err = store.DB().Exec(`DROP TRIGGER authors_fail`); err != nil {
		t.Fatal(err)
	}
	// Two aliases merge to one existing canonical credit; another role stays distinct.
	if _, err = store.DB().Exec(`INSERT INTO authors(name,role) VALUES('Other alias','Writer'),('Canonical','Artist')`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().Exec(`INSERT OR IGNORE INTO series_authors SELECT ?,id FROM authors WHERE name IN('Other alias','Canonical')`, a); err != nil {
		t.Fatal(err)
	}
	input["request_key"] = "authors-merge"
	input["proposal_ids"] = []any{p}
	input["expected"] = call("library_get_enrichment_state", map[string]any{"series_id": a})
	corrections := input["author_corrections"].([]any)
	input["author_corrections"] = append(corrections, map[string]any{"from": map[string]string{"name": "Other alias", "role": "Writer"}, "to": map[string]string{"name": "Canonical", "role": "Writer"}, "reason": "Source identifies the same pen name"})
	merged := call("library_enrich_metadata", input)
	if merged["outcome"] != "changed" || len(merged["after"].(map[string]any)["authors"].([]any)) != 3 {
		t.Fatal(merged)
	}
	mergedJSON, _ := json.Marshal(merged["after"].(map[string]any)["authors"])
	if string(mergedJSON) != `[{"name":"Artist","role":"Artist"},{"name":"Canonical","role":"Artist"},{"name":"Canonical","role":"Writer"}]` {
		t.Fatal("lost distinct roles", string(mergedJSON))
	}

	// Ordering has no membership meaning, including multiple roles for one name.
	permuted := call("library_get_enrichment_state", map[string]any{"series_id": a})
	group := permuted["authors"].([]any)
	group[0], group[2] = group[2], group[0]
	permuted["authors"] = append(group, group[0])
	noChange := call("library_enrich_metadata", map[string]any{"request_key": "authors-permutation", "series_id": a, "proposal_ids": []any{p}, "fields": []string{"authors"}, "expected": permuted})
	if noChange["outcome"] != "no-change" {
		t.Fatal(noChange)
	}
	// Failed undo retains the complete post-correction group and its persistent receipt.
	if _, err = store.DB().Exec(fmt.Sprintf(`CREATE TRIGGER authors_undo_fail BEFORE DELETE ON series_authors WHEN OLD.series_id=%d BEGIN SELECT RAISE(ABORT,'private diagnosis'); END`, a)); err != nil {
		t.Fatal(err)
	}
	failedUndo := call("library_undo_metadata_operation", map[string]any{"request_key": "authors-failed-undo", "operation_id": merged["id"]})
	if failedUndo["outcome"] != "failed" || len(failedUndo["after"].(map[string]any)["authors"].([]any)) != 3 || failedUndo["after"].(map[string]any)["summary"] != "Space journey" {
		t.Fatal(failedUndo)
	}
	if _, err = store.DB().Exec(`DROP TRIGGER authors_undo_fail`); err != nil {
		t.Fatal(err)
	}
	persistedFailure := call("library_get_metadata_operation", map[string]any{"operation_id": failedUndo["id"]})
	if persistedFailure["outcome"] != "failed" || persistedFailure["parent_id"] != merged["id"] {
		t.Fatal(persistedFailure)
	}

	// Retained old identities must not be UPDATEd even when reusing them for undo.
	if _, err = store.DB().Exec(`CREATE TRIGGER identity_immutable BEFORE UPDATE ON authors BEGIN SELECT RAISE(ABORT,'identity update forbidden'); END`); err != nil {
		t.Fatal(err)
	}
	// Preflight evidence reads are not authority or lock snapshots: fence in the write tx.
	outside, e := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Outside", Path: t.TempDir(), ScanMode: "none"})
	if e != nil {
		t.Fatal(e)
	}
	for _, scenario := range []string{"lock", "member", "library", "capability"} {
		target := makeSeries("race-" + scenario)
		proposal := propose(target, "bangumi")
		raceInput := map[string]any{"request_key": "authors-race-" + scenario, "series_id": target, "proposal_ids": []any{proposal}, "fields": []string{"authors", "summary"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": target})}
		gate := wrapped.gate()
		type response struct {
			result *mcp.CallToolResult
			err    error
		}
		done := make(chan response, 1)
		go func() {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_enrich_metadata", Arguments: raceInput})
			done <- response{result, err}
		}()
		<-gate.entered
		func() {
			defer close(gate.release)
			switch scenario {
			case "lock":
				_, err = store.DB().Exec(`UPDATE series SET locked_fields='authors' WHERE id=?`, target)
			case "member":
				_, err = store.DB().Exec(`INSERT INTO series_authors SELECT ?,id FROM authors WHERE name='Unrelated credit'`, target)
			case "library":
				_, err = store.DB().Exec(`UPDATE series SET library_id=? WHERE id=?`, outside.ID, target)
			case "capability":
				web("PATCH", "/mcp/credentials/"+issued.ID, map[string]any{"user_id": 1, "name": "enrich", "library_ids": []int64{lib.ID}, "capabilities": []string{"read"}}, 200)
			}
		}()
		if err != nil {
			t.Fatal(err)
		}
		result := <-done
		if result.err != nil {
			t.Fatal(result.err)
		}
		if scenario == "library" || scenario == "capability" {
			if !result.result.IsError {
				t.Fatal("authority was not rechecked", scenario, result.result)
			}
		} else {
			raw, _ := json.Marshal(result.result.StructuredContent)
			if result.result.IsError || !strings.Contains(string(raw), `"outcome":"conflict"`) {
				t.Fatal("group or lock was not rechecked", scenario, string(raw))
			}
		}
	}
	denied, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_undo_metadata_operation", Arguments: map[string]string{"request_key": "authors-denied-undo", "operation_id": merged["id"].(string)}})
	if err != nil || !denied.IsError {
		t.Fatal("undo ignored current metadata capability", denied, err)
	}

	// Reopen storage AND production bootstrap; HTTP undo restores the old association set.
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
	server = serve(router)
	defer server.Close()
	restored := web("POST", "/mcp/operations/"+merged["id"].(string)+"/undo", map[string]string{"request_key": "authors-reopen-undo"}, 200)
	var receipt map[string]any
	if err = json.Unmarshal(restored, &receipt); err != nil || receipt["outcome"] != "changed" || len(receipt["after"].(map[string]any)["authors"].([]any)) != 4 {
		t.Fatal(string(restored), err)
	}
	restoredJSON, _ := json.Marshal(receipt["after"].(map[string]any)["authors"])
	if string(restoredJSON) != `[{"name":"Alias","role":"Artist"},{"name":"Canonical","role":"Artist"},{"name":"Canonical","role":"Writer"},{"name":"Other alias","role":"Writer"}]` {
		t.Fatal("did not restore exact identities", string(restoredJSON))
	}

	if strings.Contains(string(web("GET", "/mcp/operations", nil, 200)), "private diagnosis") {
		t.Fatal("private storage diagnostic leaked")
	}
}
