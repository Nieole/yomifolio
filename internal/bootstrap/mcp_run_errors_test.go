package bootstrap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"modernc.org/sqlite"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/database"
	"yomifolio/internal/task"
)

type runErrorLogBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *runErrorLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}
func (b *runErrorLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

// Observe the actual wire, including JSON-RPC errors and SDK-generated text.
type runErrorTransport struct {
	base      http.RoundTripper
	responses bytes.Buffer
}

func (r *runErrorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := r.base.RoundTrip(req)
	if err != nil {
		return res, err
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		return nil, err
	}
	r.responses.Write(body)
	res.Body = io.NopCloser(bytes.NewReader(body))
	return res, nil
}

func TestMCPRunPersistenceErrorsStayPrivate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, cfg := fixture(t)
	if _, err := store.CreateUser(ctx, database.CreateUserParams{Username: "admin", PasswordHash: "unused", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Allowed", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	// Fault only the request under test, not the independent startup janitor.
	// Synchronize on its public run state rather than sleeping or replacing work.
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		runs, e := process.Application.Engine.ListSnapshots(ctx, task.RunFilter{Types: []task.Type{"cleanup_run_history"}})
		if e != nil {
			t.Fatal(e)
		}
		if len(runs) > 0 && runs[0].Run.Status.IsTerminal() {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-tick.C:
		}
	}
	svc := delegated.NewWithRuns(delegated.NewSQLStore(store.DB()), delegated.RunAccess{Engine: process.Application.Engine, Scans: process.Application.Scans, Libraries: store})
	cred, err := svc.Issue(ctx, 1, delegated.Restrictions{UserID: 1, Name: "failure-boundary", LibraryIDs: []int64{lib.ID}, Capabilities: []string{delegated.ScanControl}})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := svc.Authenticate(ctx, cred.Token)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	server := httptest.NewServer(router)
	defer server.Close()
	snap := cfg.Snapshot()
	snap.MCP.PublicURL = server.URL
	snap.MCP.AllowLANHTTP = true
	cfg.Replace(&snap)
	wire := &runErrorTransport{base: &bearerTransport{base: server.Client().Transport, token: cred.Token}}
	client := mcp.NewClient(&mcp.Implementation{Name: "run-error-acceptance", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: wire}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var logs runErrorLogBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	exec := func(query string) {
		t.Helper()
		if _, e := store.DB().ExecContext(ctx, query); e != nil {
			t.Fatal(e)
		}
	}
	call := func(name string, args map[string]any, expected string) *mcp.CallToolResult {
		t.Helper()
		wire.responses.Reset()
		out, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		raw, _ := json.Marshal(out)
		// These markers originate in real SQLite diagnostics, not a fake service.
		for _, marker := range []string{"private-run-table", "private/catalogue.sqlite", "SELECT secret_payload", cred.Token} {
			if strings.Contains(string(raw)+wire.responses.String()+logs.String(), marker) || (e != nil && strings.Contains(e.Error(), marker)) {
				t.Errorf("%s exposed a private diagnostic marker (wire=%t logs=%t)", name, strings.Contains(wire.responses.String(), marker), strings.Contains(logs.String(), marker))
			}
		}
		if e != nil {
			t.Fatalf("%s protocol error: %v", name, e)
		}
		if expected != "" && (!out.IsError || !strings.Contains(string(raw), expected)) {
			t.Errorf("%s: expected public failure %q, got %s", name, expected, raw)
		}
		if expected != "" && out.StructuredContent != nil {
			t.Errorf("%s returned success data with a failure", name)
		}
		if expected == "" && out.IsError {
			t.Errorf("%s unexpected failure: %s", name, raw)
		}
		return out
	}
	// A real admission transaction fails before a run can be accepted.
	exec(`CREATE TRIGGER reject_run BEFORE INSERT ON runs BEGIN SELECT RAISE(ABORT, 'private/catalogue.sqlite SELECT secret_payload'); END`)
	_, diagnostic := svc.StartScan(ctx, subject, lib.ID)
	var storageError *sqlite.Error
	if !errors.As(diagnostic, &storageError) || !strings.Contains(diagnostic.Error(), "private/catalogue.sqlite") {
		t.Fatal("application lost typed storage diagnostic")
	}
	call("library_start_scan", map[string]any{"library_id": lib.ID}, "run operation unavailable; retry later")
	exec(`DROP TRIGGER reject_run`)
	listed := call("library_list_runs", map[string]any{"library_id": lib.ID}, "")
	raw, _ := json.Marshal(listed.StructuredContent)
	if string(raw) != "[]" {
		t.Fatalf("failed admission created ghost run: %s", raw)
	}
	call("library_wait_run", map[string]any{"run_id": 1, "timeout_ms": 30001}, "invalid run parameters")
	call("library_get_run", map[string]any{"run_id": -1}, "delegated access denied")
	// A broken SQLite view preserves authentication/library queries but fails all run reads.
	exec(`ALTER TABLE runs RENAME TO saved_runs`)
	exec(`CREATE VIEW runs AS SELECT * FROM "private-run-table"`)
	defer func() { exec(`DROP VIEW runs`); exec(`ALTER TABLE saved_runs RENAME TO runs`) }()
	for _, name := range []string{"library_list_runs", "library_get_run", "library_wait_run", "library_cancel_run"} {
		args := map[string]any{"run_id": 1}
		if name == "library_wait_run" {
			args["timeout_ms"] = 0
		}
		if name == "library_list_runs" {
			args = map[string]any{"library_id": lib.ID}
		}
		call(name, args, "run operation unavailable; retry later")
	}
}
