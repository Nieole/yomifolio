package bootstrap_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
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

func TestMCPScanDisconnectQueueRevocationAndScope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, cfg := fixture(t)
	_, err := store.CreateUser(ctx, database.CreateUserParams{Username: "admin", PasswordHash: "unused", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Allowed", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var archive, picture bytes.Buffer
	if err = png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(&archive)
	page, err := z.Create("01.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = page.Write(picture.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(lib.Path, "01.cbz"), archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Other", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	var once sync.Once
	var workers sync.WaitGroup
	release := func() { once.Do(func() { close(released) }) }
	process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{Tasks: task.Config{RunBackground: func(fn func()) {
		workers.Add(1)
		go func() { defer workers.Done(); <-released; fn() }()
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { release(); process.Close(); workers.Wait() }()
	svc := delegated.NewWithRuns(delegated.NewSQLStore(store.DB()), delegated.RunAccess{Engine: process.Application.Engine, Scans: process.Application.Scans, Libraries: store})
	issue := func(name string, library int64) delegated.IssuedCredential {
		t.Helper()
		c, e := svc.Issue(ctx, 1, delegated.Restrictions{UserID: 1, Name: name, LibraryIDs: []int64{library}, Capabilities: []string{delegated.ScanControl}})
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	cred := issue("scanner", lib.ID)
	outside := issue("outside", other.ID)
	subject, _ := svc.Authenticate(ctx, cred.Token)
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	server := httptest.NewServer(router)
	defer server.Close()
	snap := cfg.Snapshot()
	snap.MCP.PublicURL = server.URL
	snap.MCP.AllowLANHTTP = true
	cfg.Replace(&snap)
	connect := func(token string) *mcp.ClientSession {
		t.Helper()
		client := mcp.NewClient(&mcp.Implementation{Name: "run-test", Version: "1"}, nil)
		httpClient := *server.Client()
		httpClient.Transport = &bearerTransport{base: server.Client().Transport, token: token}
		session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &httpClient}, nil)
		if e != nil {
			t.Fatal(e)
		}
		return session
	}
	call := func(session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		out, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	session := connect(cred.Token)
	out := call(session, "library_start_scan", map[string]any{"library_id": lib.ID})
	if out.IsError {
		t.Fatalf("start %+v", out)
	}
	raw, _ := json.Marshal(out.StructuredContent)
	var run delegated.Run
	if err = json.Unmarshal(raw, &run); err != nil || run.ID == 0 {
		t.Fatalf("run %s %v", raw, err)
	}
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	// The accepted body is held at the production scheduler seam, not the request.
	session = connect(cred.Token)
	defer session.Close()
	if got := call(session, "library_get_run", map[string]any{"run_id": run.ID}); got.IsError {
		t.Fatalf("reconnect %+v", got)
	}
	hidden := connect(outside.Token)
	defer hidden.Close()
	for _, name := range []string{"library_get_run", "library_wait_run", "library_cancel_run"} {
		if got := call(hidden, name, map[string]any{"run_id": run.ID}); !got.IsError {
			t.Fatalf("scope bypass %s %+v", name, got)
		}
	}
	if got := call(hidden, "library_list_runs", map[string]any{"library_id": lib.ID}); !got.IsError {
		t.Fatalf("list bypass %+v", got)
	}
	if got := call(session, "library_wait_run", map[string]any{"run_id": run.ID, "timeout_ms": 0}); got.IsError {
		t.Fatalf("bounded wait %+v", got)
	}
	release()
	finished, err := process.Application.Engine.Await(ctx, run.ID)
	if err != nil || finished.Status != task.StatusCompleted {
		t.Fatalf("disconnect canceled %+v %v", finished, err)
	}
	books, err := store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil || len(books) != 1 {
		t.Fatalf("real archive not ingested %+v %v", books, err)
	}
	followups, err := process.Application.Engine.ListSnapshots(ctx, task.RunFilter{Types: []task.Type{"generate_covers", "backfill_book_hash"}})
	if err != nil || len(followups) != 0 {
		t.Fatalf("delegated scan escaped into unowned follow-up %+v %v", followups, err)
	}
	// Pause the shared engine to control a real queued scan, then remove its capability.
	if _, err = process.Application.Engine.PauseAll(ctx); err != nil {
		t.Fatal(err)
	}
	queued, err := svc.StartScan(ctx, subject, lib.ID)
	if err != nil || queued.Status != task.StatusQueued {
		t.Fatalf("queue %+v %v", queued, err)
	}
	merged, err := svc.StartScan(ctx, subject, lib.ID)
	if err != nil || !merged.Coalesced || merged.ID != queued.ID {
		t.Fatalf("coalescing %+v %v", merged, err)
	}
	if got := call(session, "library_cancel_run", map[string]any{"run_id": queued.ID}); got.IsError {
		t.Fatalf("authorized cancellation %+v", got)
	}
	if stopped, e := process.Application.Engine.Await(ctx, queued.ID); e != nil || stopped.Status != task.StatusCancelled {
		t.Fatalf("cancel %+v %v", stopped, e)
	}
	queued, err = svc.StartScan(ctx, subject, lib.ID)
	if err != nil || queued.Status != task.StatusQueued {
		t.Fatalf("queue after explicit cancel %+v %v", queued, err)
	}
	listed, e := svc.ListRuns(ctx, subject, lib.ID, 0, 1)
	if e != nil || len(listed) != 1 || listed[0].ID != queued.ID {
		t.Fatalf("bounded list %+v %v", listed, e)
	}
	older, e := svc.ListRuns(ctx, subject, lib.ID, queued.ID, 1)
	if e != nil || len(older) != 1 || older[0].ID >= queued.ID {
		t.Fatalf("cursor %+v %v", older, e)
	}
	_, err = svc.Narrow(ctx, 1, cred.ID, delegated.Restrictions{UserID: 1, Name: "scanner", LibraryIDs: []int64{lib.ID}, Capabilities: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = process.Application.Engine.ResumeAll(ctx); err != nil {
		t.Fatal(err)
	}
	canceled, err := process.Application.Engine.Await(ctx, queued.ID)
	if err != nil || canceled.Status != task.StatusCancelled || !canceled.StartedAt.IsZero() {
		t.Fatalf("revoked queue started %+v %v", canceled, err)
	}
	if got := call(session, "library_get_run", map[string]any{"run_id": queued.ID}); !got.IsError {
		t.Fatalf("narrowed control %+v", got)
	}
	// Attribution remains available to the engine after the protocol session is gone.
	snapshot, err := process.Application.Engine.SnapshotOf(ctx, run.ID)
	if err != nil || snapshot.Side.Args["delegated_credential_id"] != cred.ID || snapshot.Side.Args["delegated_user_id"] != "1" {
		t.Fatalf("attribution %+v %v", snapshot, err)
	}
	t.Logf("disconnected run %d completed; queued run %d canceled before start (%s)", run.ID, queued.ID, fmt.Sprint(canceled.Status))
}
