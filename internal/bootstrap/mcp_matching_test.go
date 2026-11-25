package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"yomifolio/internal/api"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
)

func TestMCPMatchingConfirmedProposal(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Comics", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Local", Path: lib.Path + "/local"})
	if err != nil {
		t.Fatal(err)
	}
	entered, cancelled, release := make(chan struct{}, 2), make(chan struct{}, 2), make(chan struct{})
	process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{Sources: api.ExternalSources{Metadata: func(_ config.Config, name string) metadata.Provider {
		return &controlledSource{search: func(ctx context.Context, query string, limit, offset int) ([]*metadata.SeriesMetadata, int, error) {
			if query == "Cancel" {
				entered <- struct{}{}
				<-ctx.Done()
				cancelled <- struct{}{}
				return nil, 0, ctx.Err()
			}
			if query == "Revoke" && name == "bangumi" {
				entered <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					return nil, 0, ctx.Err()
				}
			}
			if strings.Contains(name, "llm") {
				t.Error("LLM factory called")
			}
			if query == "Failed" {
				return nil, 0, fmt.Errorf("failed")
			}
			if query == "InvalidField" {
				return []*metadata.SeriesMetadata{{Title: "invalid rating", Rating: 12}}, 1, nil
			}
			if query == "Invalid" {
				return []*metadata.SeriesMetadata{{}, {}, {}}, 3, nil
			}
			if name == "mangadex" {
				return nil, 0, fmt.Errorf("unavailable")
			}
			return []*metadata.SeriesMetadata{{Title: "Identity", Summary: name + " summary", Publisher: name + " publisher", SourceID: 11, SourceURL: "https://example.org/11"}, {Title: "Identity alternate edition", SourceID: 12}}, 2, nil
		}}
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	request := sourceHTTP(t, process)
	credential := request("POST", "/mcp/credentials", fmt.Sprintf(`{"user_id":1,"name":"matching","library_ids":[%d],"capabilities":["read","metadata_write"]}`, lib.ID), 200)
	var issued struct{ ID, Token string }
	if err = json.Unmarshal(credential.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	server := httptest.NewTLSServer(router)
	defer server.Close()
	snapshot := cfg.Snapshot()
	snapshot.MCP.PublicURL = server.URL
	cfg.Replace(&snapshot)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "matching", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: issued.Token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, args any, wantError bool) map[string]any {
		t.Helper()
		out, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil {
			// A capability the credential no longer holds also withdraws the tool itself.
			if wantError {
				return nil
			}
			t.Fatalf("%s: %+v %v", name, out, e)
		}
		if out.IsError != wantError {
			t.Fatalf("%s: %+v %v", name, out, e)
		}
		raw, _ := json.Marshal(out.StructuredContent)
		var result map[string]any
		_ = json.Unmarshal(raw, &result)
		return result
	}
	// Regular-account capability strings never confer current account write authority.
	reader, err := store.CreateUser(ctx, database.CreateUserParams{Username: "matcher-reader", Role: database.RoleRegular})
	if err != nil {
		t.Fatal(err)
	}
	readerCredential := request("POST", "/mcp/credentials", fmt.Sprintf(`{"user_id":%d,"name":"reader-matching","library_ids":[%d],"capabilities":["read","metadata_write"]}`, reader.ID, lib.ID), 200)
	var readerIssued struct{ Token string }
	if err = json.Unmarshal(readerCredential.Body.Bytes(), &readerIssued); err != nil {
		t.Fatal(err)
	}
	readerSession, err := mcp.NewClient(&mcp.Implementation{Name: "reader-matching", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: readerIssued.Token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readerSession.Close()
	ownerSession := session
	session = readerSession
	readerSearch := call("library_search_sources", map[string]any{"series_id": series.ID, "query": "Identity", "limit": 2}, false)
	call("library_wait_run", map[string]any{"run_id": readerSearch["run"].(map[string]any)["run_id"], "timeout_ms": 30000}, false)
	call("library_propose_match", map[string]any{"search_id": readerSearch["search_id"], "identity_confirmed": true, "edition_confirmed": true, "summary": "Identity agrees.", "fields": map[string]any{"summary": "bangumi:0"}}, true)
	session = ownerSession
	started := call("library_search_sources", map[string]any{"series_id": series.ID, "query": "Identity", "limit": 2}, false)
	id := started["search_id"].(string)
	call("library_wait_run", map[string]any{"run_id": started["run"].(map[string]any)["run_id"], "timeout_ms": 30000}, false)
	var found map[string]any
	deadline := time.Now().Add(3 * time.Second)
	for {
		found = call("library_get_source_search", map[string]any{"search_id": id}, false)
		if found["status"] != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(found)
		}
	}
	if found["status"] != "partial" {
		t.Fatal(found)
	}
	sources := found["sources"].([]any)
	if len(sources) != 3 {
		t.Fatal(found)
	}
	reviews := request("GET", "/metadata/reviews", "", 200)
	var inbox struct {
		Total int `json:"total"`
	}
	if err = json.Unmarshal(reviews.Body.Bytes(), &inbox); err != nil || inbox.Total != 0 {
		t.Fatal("unconfirmed candidates persisted", reviews.Body.String(), err)
	}
	if len(sources[0].(map[string]any)["candidates"].([]any)) != 2 {
		t.Fatal(found)
	}
	call("library_search_sources", map[string]any{"series_id": series.ID, "query": "Identity", "limit": 11}, true)
	call("library_search_sources", map[string]any{"series_id": series.ID, "query": "Identity", "offset": -1}, true)
	call("library_search_sources", map[string]any{"series_id": 999999, "query": "Identity"}, true)
	confirm := map[string]any{"search_id": id, "identity_confirmed": true, "edition_confirmed": true, "summary": "Title and release edition agree with local directory.", "fields": map[string]any{"summary": "bangumi:0", "publisher": "anilist:0"}}
	queued := call("library_propose_match", confirm, false)
	if queued["status"] != "queued" {
		t.Fatal(queued)
	}
	reused := call("library_propose_match", confirm, false)
	if reused["status"] != "reused_existing" {
		t.Fatal(reused)
	}
	read := call("library_get_match_proposal", map[string]any{"series_id": series.ID, "proposal_id": queued["proposal_id"]}, false)
	if read["evidence"] == nil || queued["metadata_written"] != false {
		t.Fatal(read, queued)
	}
	fields := read["evidence"].(map[string]any)["fields"].(map[string]any)
	if fields["summary"].(map[string]any)["source"] != "bangumi" || fields["publisher"].(map[string]any)["source"] != "anilist" {
		t.Fatal(read)
	}
	call("library_get_match_proposal", map[string]any{"series_id": 999999, "proposal_id": queued["proposal_id"]}, true)
	confirm["fields"] = map[string]any{"cover": "bangumi:0"}
	call("library_propose_match", confirm, true)
	confirm["fields"] = map[string]any{"summary": "http://127.0.0.1/private"}
	call("library_propose_match", confirm, true)
	confirm["fields"] = map[string]any{"summary": "bangumi:0", "publisher": "anilist:0"}
	request("PUT", fmt.Sprintf("/series/info/%d", series.ID), `{"summary":"Human text","locked_fields":"summary,publisher"}`, 200)
	locked := call("library_propose_match", confirm, false)
	if locked["status"] != "all_fields_locked" || locked["proposal_id"] != nil {
		t.Fatal(locked)
	}
	current := call("library_get_match_proposal", map[string]any{"series_id": series.ID, "proposal_id": queued["proposal_id"]}, false)
	for _, f := range current["fields"].([]any) {
		if !f.(map[string]any)["Locked"].(bool) {
			t.Fatal(current)
		}
	}
	request("PUT", fmt.Sprintf("/series/info/%d", series.ID), `{"summary":"bangumi summary","publisher":"anilist publisher"}`, 200)
	same := call("library_propose_match", confirm, false)
	if same["status"] != "no_changes" {
		t.Fatal(same)
	}
	view := request("GET", fmt.Sprintf("/series/%d/context", series.ID), "", 200)
	if !strings.Contains(view.Body.String(), "bangumi summary") {
		t.Fatal(view.Body.String())
	}
	cancelSearch := call("library_search_sources", map[string]any{"series_id": series.ID, "query": "Cancel", "limit": 2}, false)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("source not entered")
	}
	run := cancelSearch["run"].(map[string]any)["run_id"]
	call("library_cancel_run", map[string]any{"run_id": run}, false)
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("source context not cancelled")
	}
	call("library_wait_run", map[string]any{"run_id": run, "timeout_ms": 30000}, false)
	cancelledSearch := call("library_get_source_search", map[string]any{"search_id": cancelSearch["search_id"]}, false)
	if cancelledSearch["status"] != "cancelled" {
		t.Fatal(cancelledSearch)
	}
	// Current configuration activates credentialed catalogues without restart.
	snapshot = cfg.Snapshot()
	snapshot.Scrapers.MALClientID = "test"
	snapshot.Scrapers.ComicVineAPIKey = "test"
	cfg.Replace(&snapshot)
	configured := call("library_search_sources", map[string]any{"series_id": series.ID, "query": "Configured", "limit": 2}, false)
	call("library_wait_run", map[string]any{"run_id": configured["run"].(map[string]any)["run_id"], "timeout_ms": 30000}, false)
	all := call("library_get_source_search", map[string]any{"search_id": configured["search_id"]}, false)
	if len(all["sources"].([]any)) != 5 {
		t.Fatal(all)
	}
	for _, query := range []string{"Failed", "Invalid", "InvalidField"} {
		failed := call("library_search_sources", map[string]any{"series_id": series.ID, "query": query, "limit": 2}, false)
		call("library_wait_run", map[string]any{"run_id": failed["run"].(map[string]any)["run_id"], "timeout_ms": 30000}, false)
		details := call("library_get_source_search", map[string]any{"search_id": failed["search_id"]}, false)
		if details["status"] != "failed" {
			t.Fatal(details)
		}
	}
	request("PATCH", "/mcp/credentials/"+issued.ID, fmt.Sprintf(`{"user_id":1,"name":"matching","library_ids":[%d],"capabilities":["read"]}`, lib.ID), 200)
	call("library_propose_match", confirm, true)
	blocked := call("library_search_sources", map[string]any{"series_id": series.ID, "query": "Revoke", "limit": 2}, false)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("revocation barrier not entered")
	}
	request("PATCH", "/mcp/credentials/"+issued.ID, `{"user_id":1,"name":"matching","library_ids":[],"capabilities":["read"]}`, 200)
	close(release)
	call("library_wait_run", map[string]any{"run_id": blocked["run"].(map[string]any)["run_id"], "timeout_ms": 30000}, true)
	call("library_get_source_search", map[string]any{"search_id": blocked["search_id"]}, true)
	deadline = time.Now().Add(3 * time.Second)
	for {
		tasks := request("GET", "/system/tasks?type=source_search", "", 200)
		if strings.Contains(tasks.Body.String(), `"status":"cancelled"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(tasks.Body.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	call("library_propose_match", confirm, true)
	confirm["identity_confirmed"] = false
	call("library_propose_match", confirm, true)
	// Proposal evidence belongs to persistent proposal storage, not the search cache.
	fresh := request("POST", "/mcp/credentials", fmt.Sprintf(`{"user_id":1,"name":"reopen","library_ids":[%d],"capabilities":["read","metadata_write"]}`, lib.ID), 200)
	var reconnect struct{ Token string }
	if err = json.Unmarshal(fresh.Body.Bytes(), &reconnect); err != nil {
		t.Fatal(err)
	}
	readOnly := request("POST", "/mcp/credentials", fmt.Sprintf(`{"user_id":1,"name":"reopen-read-only","library_ids":[%d],"capabilities":["read"]}`, lib.ID), 200)
	var readOnlyToken struct{ Token string }
	if err = json.Unmarshal(readOnly.Body.Bytes(), &readOnlyToken); err != nil {
		t.Fatal(err)
	}
	session.Close()
	server.Close()
	process.Close()
	reopened, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	nextRouter := chi.NewRouter()
	reopened.HTTP.SetupRoutes(nextRouter)
	nextServer := httptest.NewTLSServer(nextRouter)
	defer nextServer.Close()
	snapshot = cfg.Snapshot()
	snapshot.MCP.PublicURL = nextServer.URL
	cfg.Replace(&snapshot)
	next, err := mcp.NewClient(&mcp.Implementation{Name: "reopen", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: nextServer.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: nextServer.Client().Transport, token: reconnect.Token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	readOnlyReopen, err := mcp.NewClient(&mcp.Implementation{Name: "reopen-read-only", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: nextServer.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: nextServer.Client().Transport, token: readOnlyToken.Token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnlyReopen.Close()
	refused, err := readOnlyReopen.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_match_proposal", Arguments: map[string]any{"series_id": series.ID, "proposal_id": queued["proposal_id"]}})
	if err != nil || !refused.IsError {
		t.Fatal("reopened proposal bypassed metadata_write", refused, err)
	}

	persisted, err := next.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_match_proposal", Arguments: map[string]any{"series_id": series.ID, "proposal_id": queued["proposal_id"]}})
	if err != nil || persisted.IsError {
		t.Fatal(persisted, err)
	}
	raw, _ := json.Marshal(persisted.StructuredContent)
	if !strings.Contains(string(raw), "Title and release edition agree") || !strings.Contains(string(raw), "anilist publisher") {
		t.Fatal(string(raw))
	}
}
