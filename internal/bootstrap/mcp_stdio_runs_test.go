package bootstrap_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/database"
	"yomifolio/internal/task"
)

// Accepted runs belong to the server, not the helper's stdin lifetime.
func TestMCPStdioAcceptedScanSurvivesEOFAndExplicitCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "mcp-proxy.exe")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/mcp-proxy").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	store, cfg := fixture(t)
	if _, err := store.CreateUser(ctx, database.CreateUserParams{Username: "admin", PasswordHash: "unused", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Offline scan", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Outside", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	writeStdioRunArchive(t, filepath.Join(lib.Path, "01.cbz"))

	// Hold the real scanner at dispatch until the accepting helper has exited.
	permits := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(permits) }) }
	var workers sync.WaitGroup
	process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{Tasks: task.Config{RunBackground: func(fn func()) {
		workers.Add(1)
		go func() { defer workers.Done(); <-permits; fn() }()
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { release(); process.Close(); workers.Wait() }()
	svc := delegated.New(delegated.NewSQLStore(store.DB()))
	issue := func(name string, library int64) delegated.IssuedCredential {
		t.Helper()
		cred, err := svc.Issue(ctx, 1, delegated.Restrictions{UserID: 1, Name: name, LibraryIDs: []int64{library}, Capabilities: []string{delegated.ScanControl}})
		if err != nil {
			t.Fatal(err)
		}
		return cred
	}
	cred := issue("stdio scan", lib.ID)
	outside := issue("outside", other.ID)
	sibling := issue("same library different credential", lib.ID)
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	server := httptest.NewServer(router)
	defer server.Close()
	snapshot := cfg.Snapshot()
	snapshot.MCP.PublicURL, snapshot.MCP.AllowLANHTTP = server.URL, true
	cfg.Replace(&snapshot)

	connect := func(token string) (*mcp.ClientSession, func()) {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, "--url", server.URL, "--allow-lan-http")
		cmd.Dir = t.TempDir() // No server config/database is available to the helper.
		cmd.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN="+token)
		stderr := new(bytes.Buffer)
		cmd.Stderr = stderr
		client := mcp.NewClient(&mcp.Implementation{Name: "stdio-run-acceptance", Version: "1"}, nil)
		session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
		if err != nil {
			t.Fatalf("connect: %v %s", err, stderr)
		}
		var once sync.Once
		closeHelper := func() {
			t.Helper()
			once.Do(func() {
				if err := session.Close(); err != nil {
					t.Errorf("close helper: %v", err)
				}
				if cmd.ProcessState == nil || !cmd.ProcessState.Success() {
					t.Errorf("helper not reaped after EOF: %v %s", cmd.ProcessState, stderr)
				}
				if bytes.Contains(stderr.Bytes(), []byte(token)) {
					t.Error("credential in diagnostics")
				}
			})
		}
		t.Cleanup(closeHelper)
		return session, closeHelper
	}
	call := func(session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return result
	}
	runResult := func(result *mcp.CallToolResult) delegated.Run {
		t.Helper()
		if result.IsError {
			t.Fatalf("tool rejected: %+v", result)
		}
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var run delegated.Run
		if err := json.Unmarshal(raw, &run); err != nil || run.ID == 0 {
			t.Fatalf("run: %s %v", raw, err)
		}
		return run
	}
	assertRun := func(session *mcp.ClientSession, id int64, status task.RunStatus) {
		t.Helper()
		got := runResult(call(session, "library_get_run", map[string]any{"run_id": id}))
		if got.ID != id || got.LibraryID != lib.ID || got.Status != status {
			t.Fatalf("run identity/status: %+v, want %d %s", got, id, status)
		}
	}
	session, closeFirst := connect(cred.Token)
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range tools.Tools {
		if tool.Name == "library_start_scan" {
			found = true
		}
	}
	if !found {
		t.Fatal("scan tool not discovered")
	}
	accepted := runResult(call(session, "library_start_scan", map[string]any{"library_id": lib.ID}))
	assertRun(session, accepted.ID, task.StatusQueued)
	closeFirst() // SDK closes stdin (EOF) and waits for the actual helper process.

	release()
	finished, err := process.Application.Engine.Await(ctx, accepted.ID)
	if err != nil || finished.Status != task.StatusCompleted {
		t.Fatalf("offline execution: %+v %v", finished, err)
	}
	books, err := store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil || len(books) != 1 {
		t.Fatalf("real archive not ingested offline: %+v %v", books, err)
	}
	reconnected, closeReconnect := connect(cred.Token)
	assertRun(reconnected, accepted.ID, task.StatusCompleted)

	// The public engine pause controls the second run after dispatch is released.
	// Neither another library nor another credential may cancel it.
	if _, err := process.Application.Engine.PauseAll(ctx); err != nil {
		t.Fatal(err)
	}
	stopped := runResult(call(reconnected, "library_start_scan", map[string]any{"library_id": lib.ID}))
	if stopped.ID == accepted.ID {
		t.Fatal("second scan reused completed run")
	}
	assertRun(reconnected, stopped.ID, task.StatusQueued)
	for _, token := range []string{outside.Token, sibling.Token} {
		hidden, closeHidden := connect(token)
		for _, name := range []string{"library_get_run", "library_wait_run", "library_cancel_run"} {
			if got := call(hidden, name, map[string]any{"run_id": stopped.ID}); !got.IsError {
				t.Fatalf("authority bypass %s: %+v", name, got)
			}
		}
		closeHidden()
	}
	assertRun(reconnected, stopped.ID, task.StatusQueued)
	if got := call(reconnected, "library_cancel_run", map[string]any{"run_id": stopped.ID}); got.IsError {
		t.Fatalf("explicit cancel: %+v", got)
	}
	if _, err := process.Application.Engine.ResumeAll(ctx); err != nil {
		t.Fatal(err)
	}
	finished, err = process.Application.Engine.Await(ctx, stopped.ID)
	if err != nil || finished.Status != task.StatusCancelled {
		t.Fatalf("explicit cancellation: %+v %v", finished, err)
	}
	assertRun(reconnected, stopped.ID, task.StatusCancelled)
	closeReconnect()
	final, closeFinal := connect(cred.Token)
	assertRun(final, accepted.ID, task.StatusCompleted)
	assertRun(final, stopped.ID, task.StatusCancelled)
	closeFinal()
	t.Logf("stdio EOF run %d completed offline; explicit cancel run %d remained cancelled across fresh helper", accepted.ID, stopped.ID)
}

func writeStdioRunArchive(t *testing.T, path string) {
	t.Helper()
	var archive, picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(&archive)
	page, err := z.Create("01.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.Write(picture.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}
