package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

type budgetAI struct {
	metadata.AIProvider
	calls   *atomic.Int32
	hold    *atomic.Bool
	entered chan struct{}
}

func (a budgetAI) TestLLM(ctx context.Context, _ string) (string, error) {
	a.calls.Add(1)
	if a.hold.Load() {
		a.entered <- struct{}{}
		<-ctx.Done()
		return "", ctx.Err()
	}
	return "uncertain", nil
}

func TestMCPModelBudgetPersistsWithoutReplay(t *testing.T) {
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
	var hold atomic.Bool
	entered := make(chan struct{}, 1)
	sources := api.ExternalSources{AI: func(string, string, string, string, string, string, int) metadata.AIProvider {
		return budgetAI{calls: &calls, hold: &hold, entered: entered}
	}, Metadata: func(config.Config, string) metadata.Provider {
		return &controlledSource{search: func(context.Context, string, int, int) ([]*metadata.SeriesMetadata, int, error) {
			return []*metadata.SeriesMetadata{{Title: "Candidate"}}, 1, nil
		}}
	}}
	process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	request := sourceHTTP(t, process)
	var issued struct{ ID, Token string }
	raw := request("POST", "/mcp/credentials", fmt.Sprintf(`{"user_id":1,"name":"budget","library_ids":[%d],"capabilities":["read","internal_llm"]}`, lib.ID), 200)
	if err = json.Unmarshal(raw.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	connect := func(p *bootstrap.Process) (*httptest.Server, *mcp.ClientSession) {
		router := chi.NewRouter()
		p.HTTP.SetupRoutes(router)
		server := httptest.NewTLSServer(router)
		snapshot := cfg.Snapshot()
		snapshot.MCP.PublicURL = server.URL
		cfg.Replace(&snapshot)
		session, e := mcp.NewClient(&mcp.Implementation{Name: "budget", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: issued.Token}}}, nil)
		if e != nil {
			t.Fatal(e)
		}
		return server, session
	}
	server, session := connect(process)
	call := func(name string, args any, wantError bool) map[string]any {
		t.Helper()
		out, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil || out.IsError != wantError {
			t.Fatalf("%s: %+v %v", name, out, e)
		}
		raw, _ := json.Marshal(out.StructuredContent)
		var result map[string]any
		_ = json.Unmarshal(raw, &result)
		return result
	}
	search := call("library_search_sources", map[string]any{"series_id": series.ID, "query": "Local"}, false)
	call("library_wait_run", map[string]any{"run_id": search["run"].(map[string]any)["run_id"], "timeout_ms": 30000}, false)
	call("library_assess_match", map[string]any{"search_id": search["search_id"]}, true)
	if calls.Load() != 0 {
		t.Fatal("no allowance called model")
	}
	started := call("library_assess_match", map[string]any{"search_id": search["search_id"], "allowance": 1}, false)
	call("library_wait_run", map[string]any{"run_id": started["run_id"], "timeout_ms": 30000}, false)
	details := call("library_get_model_operation", map[string]any{"operation_id": started["operation_id"]}, false)
	if details["used"] != float64(1) || details["remaining"] != float64(0) || len(details["unprocessed"].([]any)) != 2 || calls.Load() != 1 {
		t.Fatal(details, calls.Load())
	}
	repeated := call("library_assess_match", map[string]any{"search_id": search["search_id"], "allowance": 1}, false)
	if repeated["operation_id"] != started["operation_id"] {
		t.Fatal(repeated)
	}
	call("library_assess_match", map[string]any{"search_id": search["search_id"], "allowance": 2}, true)
	call("library_find_series", map[string]any{"library_ids": []int64{lib.ID}}, false)
	hold.Store(true)
	pendingSearch := call("library_search_sources", map[string]any{"series_id": series.ID, "query": "Local"}, false)
	call("library_wait_run", map[string]any{"run_id": pendingSearch["run"].(map[string]any)["run_id"], "timeout_ms": 30000}, false)
	pending := call("library_assess_match", map[string]any{"search_id": pendingSearch["search_id"], "allowance": 3}, false)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("restart barrier not entered")
	}
	session.Close()
	server.Close()
	process.Close()
	process, err = bootstrap.Open(ctx, store, cfg, bootstrap.Options{Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	server, session = connect(process)
	defer server.Close()
	defer session.Close()
	details = call("library_get_model_operation", map[string]any{"operation_id": started["operation_id"]}, false)
	if details["used"] != float64(1) || calls.Load() != 2 {
		t.Fatal(details, calls.Load())
	}
	repeated = call("library_assess_match", map[string]any{"search_id": search["search_id"], "allowance": 1}, false)
	if repeated["operation_id"] != started["operation_id"] || calls.Load() != 2 {
		t.Fatal(repeated, calls.Load())
	}
	pendingDetails := call("library_get_model_operation", map[string]any{"operation_id": pending["operation_id"]}, false)
	if pendingDetails["used"] != float64(1) || pendingDetails["remaining"] != float64(2) || len(pendingDetails["unprocessed"].([]any)) != 2 || pendingDetails["items"].([]any)[0].(map[string]any)["outcome"] != "unknown" {
		t.Fatal(pendingDetails)
	}
	resumed := call("library_assess_match", map[string]any{"search_id": pendingSearch["search_id"], "allowance": 3}, false)
	if resumed["operation_id"] != pending["operation_id"] || calls.Load() != 2 {
		t.Fatal("restart replayed paid work", resumed, calls.Load())
	}

}
