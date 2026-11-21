package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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

type barrierBudgetAI struct {
	metadata.AIProvider
	call func(context.Context, string) (string, error)
}

func (a barrierBudgetAI) TestLLM(ctx context.Context, prompt string) (string, error) {
	return a.call(ctx, prompt)
}

func TestMCPModelBudgetAuthorityDisconnectAndFailedAdmissions(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Budget", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Local", Path: lib.Path + "/local"})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	entered, release := make(chan struct{}, 8), make(chan struct{}, 8)
	sources := api.ExternalSources{AI: func(string, string, string, string, string, string, int) metadata.AIProvider {
		return barrierBudgetAI{call: func(ctx context.Context, _ string) (string, error) {
			calls.Add(1)
			entered <- struct{}{}
			select {
			case <-release:
				return "", fmt.Errorf("controlled upstream failure with secret text")
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}}
	}, Metadata: func(config.Config, string) metadata.Provider {
		return &controlledSource{search: func(context.Context, string, int, int) ([]*metadata.SeriesMetadata, int, error) {
			return []*metadata.SeriesMetadata{{Title: "Candidate"}}, 1, nil
		}}
	}}
	process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	manage := sourceHTTP(t, process)
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	server := httptest.NewTLSServer(router)
	defer server.Close()
	snap := cfg.Snapshot()
	snap.MCP.PublicURL = server.URL
	cfg.Replace(&snap)
	type credential struct{ ID, Token string }
	issue := func(user int64, name, caps string) credential {
		t.Helper()
		var c credential
		out := manage("POST", "/mcp/credentials", fmt.Sprintf(`{"user_id":%d,"name":"%s","library_ids":[%d],"capabilities":%s}`, user, name, lib.ID, caps), 200)
		if e := json.Unmarshal(out.Body.Bytes(), &c); e != nil {
			t.Fatal(e)
		}
		return c
	}
	connect := func(c credential) *mcp.ClientSession {
		t.Helper()
		s, e := mcp.NewClient(&mcp.Implementation{Name: "budget", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: c.Token}}}, nil)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	call := func(s *mcp.ClientSession, name string, args any, wantError bool) map[string]any {
		t.Helper()
		out, e := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil || out.IsError != wantError {
			t.Fatalf("%s: %+v %v", name, out, e)
		}
		raw, _ := json.Marshal(out.StructuredContent)
		var result map[string]any
		_ = json.Unmarshal(raw, &result)
		return result
	}
	search := func(s *mcp.ClientSession) map[string]any {
		t.Helper()
		out := call(s, "library_search_sources", map[string]any{"series_id": series.ID, "query": "Local"}, false)
		call(s, "library_wait_run", map[string]any{"run_id": out["run"].(map[string]any)["run_id"], "timeout_ms": 30000}, false)
		return out
	}
	reader, err := store.CreateUser(ctx, database.CreateUserParams{Username: "budget-reader", Role: database.RoleRegular})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []credential{issue(1, "read-only", `["read"]`), issue(reader.ID, "ordinary", `["read","internal_llm"]`)} {
		s := connect(c)
		evidence := search(s)
		call(s, "library_assess_match", map[string]any{"search_id": evidence["search_id"], "allowance": 3}, true)
		call(s, "library_get_series", map[string]any{"series_id": series.ID}, false)
	}
	if calls.Load() != 0 {
		t.Fatal("unauthorized paid call")
	}
	owner := issue(1, "owner", `["read","internal_llm"]`)
	session := connect(owner)
	evidence := search(session)
	started := call(session, "library_assess_match", map[string]any{"search_id": evidence["search_id"], "allowance": 1}, false)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("model barrier not entered")
	}
	// Concurrent split/repeated requests cannot create a second run or budget.
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			out := call(session, "library_assess_match", map[string]any{"search_id": evidence["search_id"], "allowance": 1}, false)
			if out["operation_id"] != started["operation_id"] {
				t.Error(out)
			}
		})
	}
	wg.Wait()
	call(session, "library_assess_match", map[string]any{"search_id": evidence["search_id"], "allowance": 2}, true)
	other := connect(issue(1, "other", `["read","internal_llm"]`))
	call(other, "library_get_model_operation", map[string]any{"operation_id": started["operation_id"]}, true)
	session.Close()
	release <- struct{}{}
	session = connect(owner)
	call(session, "library_wait_run", map[string]any{"run_id": started["run_id"], "timeout_ms": 30000}, false)
	details := call(session, "library_get_model_operation", map[string]any{"operation_id": started["operation_id"]}, false)
	if calls.Load() != 1 || details["used"] != float64(1) || details["remaining"] != float64(0) || details["items"].([]any)[0].(map[string]any)["outcome"] != "failed" {
		t.Fatal(details, calls.Load())
	}
	call(session, "library_get_series", map[string]any{"series_id": series.ID}, false)
	// Explicit run cancellation consumes the admission, never retries it.
	evidence = search(session)
	cancelled := call(session, "library_assess_match", map[string]any{"search_id": evidence["search_id"], "allowance": 3}, false)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation barrier not entered")
	}
	call(session, "library_cancel_run", map[string]any{"run_id": cancelled["run_id"]}, false)
	call(session, "library_wait_run", map[string]any{"run_id": cancelled["run_id"], "timeout_ms": 30000}, false)
	cancelledDetails := call(session, "library_get_model_operation", map[string]any{"operation_id": cancelled["operation_id"]}, false)
	if cancelledDetails["used"] != float64(1) || cancelledDetails["remaining"] != float64(2) || cancelledDetails["status"] != "cancelled" || len(cancelledDetails["unprocessed"].([]any)) != 2 {
		t.Fatal(cancelledDetails)
	}
	again := call(session, "library_assess_match", map[string]any{"search_id": evidence["search_id"], "allowance": 3}, false)
	if again["operation_id"] != cancelled["operation_id"] || calls.Load() != 2 {
		t.Fatal(again, calls.Load())
	}
	// Removing only model capability at an in-flight barrier leaves read work legal.
	evidence = search(session)
	revoked := call(session, "library_assess_match", map[string]any{"search_id": evidence["search_id"], "allowance": 3}, false)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("revocation barrier not entered")
	}
	manage("PATCH", "/mcp/credentials/"+owner.ID, fmt.Sprintf(`{"user_id":1,"name":"owner","library_ids":[%d],"capabilities":["read"]}`, lib.ID), 200)
	release <- struct{}{}
	call(session, "library_get_model_operation", map[string]any{"operation_id": revoked["operation_id"]}, true)
	call(session, "library_get_series", map[string]any{"series_id": series.ID}, false)
	// Shutdown drains the revoked run, giving a deterministic no-later-call assertion.
	process.Close()
	if calls.Load() != 3 {
		t.Fatal("model continued after revoke", calls.Load())
	}
}
