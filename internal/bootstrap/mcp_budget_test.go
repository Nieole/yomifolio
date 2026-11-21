package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
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
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/task"
	"yomifolio/internal/taskstore"
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
	runMCPModelBudgetPersists(t, false)
}

func TestMCPModelBudgetStdioReconnectWithoutReplay(t *testing.T) {
	runMCPModelBudgetPersists(t, true)
}

func runMCPModelBudgetPersists(t *testing.T, stdio bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var binary string
	if stdio {
		binary = filepath.Join(t.TempDir(), "mcp-proxy.exe")
		build := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/mcp-proxy")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build: %v %s", err, out)
		}
	}
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
		var server *httptest.Server
		if stdio {
			server = httptest.NewServer(router)
		} else {
			server = httptest.NewTLSServer(router)
		}
		snapshot := cfg.Snapshot()
		snapshot.MCP.PublicURL = server.URL
		snapshot.MCP.AllowLANHTTP = stdio
		cfg.Replace(&snapshot)
		var transport mcp.Transport = &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: issued.Token}}}
		if stdio {
			cmd := exec.CommandContext(ctx, binary, "--url", server.URL, "--allow-lan-http")
			cmd.Dir = t.TempDir() // No application config/database in the helper's working directory.
			cmd.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN="+issued.Token)
			transport = &mcp.CommandTransport{Command: cmd}
		}
		session, e := mcp.NewClient(&mcp.Implementation{Name: "budget", Version: "1"}, nil).Connect(ctx, transport, nil)
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
	if details["allowance"] != float64(1) || details["used"] != float64(1) || details["remaining"] != float64(0) || details["status"] != "completed" || details["items"].([]any)[0].(map[string]any)["outcome"] != "uncertain" || len(details["unprocessed"].([]any)) != 2 || calls.Load() != 1 {
		t.Fatal(details, calls.Load())
	}
	repeated := call("library_assess_match", map[string]any{"search_id": search["search_id"], "allowance": 1}, false)
	if repeated["operation_id"] != started["operation_id"] {
		t.Fatal(repeated)
	}
	call("library_assess_match", map[string]any{"search_id": search["search_id"], "allowance": 2}, true)
	call("library_find_series", map[string]any{"library_ids": []int64{lib.ID}}, false)
	session.Close()
	sessionClient := mcp.NewClient(&mcp.Implementation{Name: "budget-reconnect", Version: "1"}, nil)
	var reconnectTransport mcp.Transport = &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: issued.Token}}}
	if stdio {
		cmd := exec.CommandContext(ctx, binary, "--url", server.URL, "--allow-lan-http")
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN="+issued.Token)
		reconnectTransport = &mcp.CommandTransport{Command: cmd}
	}
	session, err = sessionClient.Connect(ctx, reconnectTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	reconnected := call("library_assess_match", map[string]any{"search_id": search["search_id"], "allowance": 1}, false)
	if reconnected["operation_id"] != started["operation_id"] || reconnected["run_id"] != started["run_id"] || reconnected["used"] != float64(1) || reconnected["remaining"] != float64(0) || calls.Load() != 1 {
		t.Fatal("helper reconnect reset ownership/usage", reconnected, calls.Load())
	}
	// Age completed history past the normal retention threshold, then use the
	// production pruning path (not a direct DELETE or synthetic lookup error).
	if _, err = store.DB().ExecContext(ctx, `UPDATE runs SET finished_at=? WHERE id=?`, time.Now().Add(-100*24*time.Hour).UnixMilli(), started["run_id"]); err != nil {
		t.Fatal(err)
	}
	pruned, err := taskstore.New(store.DB()).PruneRuns(ctx, task.RetentionPolicy{TerminalAge: 90 * 24 * time.Hour})
	if err != nil || pruned.Runs != 1 {
		t.Fatalf("normal history pruning: %+v %v", pruned, err)
	}
	call("library_get_run", map[string]any{"run_id": started["run_id"]}, true)
	assertPruned := func() {
		t.Helper()
		for _, got := range []map[string]any{
			call("library_get_model_operation", map[string]any{"operation_id": started["operation_id"]}, false),
			call("library_assess_match", map[string]any{"search_id": search["search_id"], "allowance": 1}, false),
		} {
			want := make(map[string]any, len(details))
			for k, v := range details {
				want[k] = v
			}
			want["status"] = "history_unavailable"
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("pruning lost durable operation: got=%+v want=%+v", got, want)
			}
		}
		call("library_assess_match", map[string]any{"search_id": search["search_id"], "allowance": 2}, true)
	}
	assertPruned()
	if calls.Load() != 1 {
		t.Fatal("pruned operation replayed", calls.Load())
	}
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
	assertPruned()
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

	// Missing run history must not turn durable-operation reads into a bypass.
	adminRouter := chi.NewRouter()
	process.HTTP.SetupRoutes(adminRouter)
	var adminCookie *http.Cookie
	var adminSession struct {
		CSRF string `json:"csrf_token"`
	}
	manage := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api"+path, strings.NewReader(body))
		if adminCookie != nil {
			req.AddCookie(adminCookie)
			req.Header.Set("X-CSRF-Token", adminSession.CSRF)
		}
		rec := httptest.NewRecorder()
		adminRouter.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
		}
		return rec
	}
	login := manage("POST", "/auth/login", `{"username":"admin","password":"correct horse battery staple"}`, 200)
	adminCookie = login.Result().Cookies()[0]
	if err = json.Unmarshal(login.Body.Bytes(), &adminSession); err != nil {
		t.Fatal(err)
	}
	otherUser, err := store.CreateUser(ctx, database.CreateUserParams{Username: "other-budget-admin", Role: database.RoleAdmin})
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []int64{1, otherUser.ID} {
		var other struct{ ID, Token string }
		response := manage("POST", "/mcp/credentials", fmt.Sprintf(`{"user_id":%d,"name":"other-budget","library_ids":[%d],"capabilities":["read","internal_llm"]}`, user, lib.ID), 200)
		if err = json.Unmarshal(response.Body.Bytes(), &other); err != nil {
			t.Fatal(err)
		}
		otherSession, e := mcp.NewClient(&mcp.Implementation{Name: "other-budget", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: other.Token}}}, nil)
		if e != nil {
			t.Fatal(e)
		}
		ownerSession := session
		session = otherSession
		call("library_get_model_operation", map[string]any{"operation_id": started["operation_id"]}, true)
		call("library_assess_match", map[string]any{"search_id": search["search_id"], "allowance": 1}, true)
		session = ownerSession
		otherSession.Close()
	}
	denyOriginal := func() {
		t.Helper()
		call("library_get_model_operation", map[string]any{"operation_id": started["operation_id"]}, true)
		call("library_assess_match", map[string]any{"search_id": search["search_id"], "allowance": 1}, true)
	}
	// A changed current user role is independent of the credential and history.
	if err = store.UpdateUserProfile(ctx, 1, "", database.RoleRegular); err != nil {
		t.Fatal(err)
	}
	denyOriginal()
	if err = store.UpdateUserProfile(ctx, 1, "", database.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	assertPruned()
	movedLibrary, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Moved budget series", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE series SET library_id=? WHERE id=?`, movedLibrary.ID, series.ID); err != nil {
		t.Fatal(err)
	}
	denyOriginal()
	if _, err = store.DB().ExecContext(ctx, `UPDATE series SET library_id=? WHERE id=?`, lib.ID, series.ID); err != nil {
		t.Fatal(err)
	}
	assertPruned()
	manage("PATCH", "/mcp/credentials/"+issued.ID, fmt.Sprintf(`{"user_id":1,"name":"budget","library_ids":[%d],"capabilities":["read"]}`, lib.ID), 200)
	denyOriginal()
	call("library_get_series", map[string]any{"series_id": series.ID}, false)
	manage("PATCH", "/mcp/credentials/"+issued.ID, `{"user_id":1,"name":"budget","library_ids":[],"capabilities":["read"]}`, 200)
	denyOriginal()
	if calls.Load() != 2 {
		t.Fatal("history/authority checks reissued a model call", calls.Load())
	}

}
