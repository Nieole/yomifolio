package parser_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/parser"
	"yomifolio/internal/storageio"
)

// The existing test-only ordinary-session mutex barrier is combined with actual
// production bootstrap, HTTP sockets and proxy stdin EOF here. No production
// hook, handler invocation shortcut or SDK Close-as-disconnect is involved.
func TestPublicClueDisconnectWhileRarSessionOwned(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "mcp-proxy.exe")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../../cmd/mcp-proxy")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("proxy build: %s %v", out, err)
	}
	for _, stdio := range []bool{false, true} {
		t.Run(fmt.Sprintf("stdio=%t", stdio), func(t *testing.T) { publicClueOwnedSession(t, binary, stdio) })
	}
}

func publicClueOwnedSession(t *testing.T, binary string, stdio bool) {
	ctx := t.Context()
	dbpath := filepath.Join(t.TempDir(), "process.db")
	if err := database.Migrate(dbpath); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := config.Config{}
	cfg.Database.Path = dbpath
	cfg.Cache.Dir = t.TempDir()
	config.NormalizeConfig(&cfg)
	cfg.Library.IOPolicy.ArchiveOpenConcurrency = 1
	cfg.MCP.AllowLANHTTP = true
	manager := config.NewManager(&cfg)
	process, err := bootstrap.Open(ctx, store, manager, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	setup := httptest.NewRecorder()
	router.ServeHTTP(setup, httptest.NewRequest("POST", "/api/auth/setup", strings.NewReader(`{"username":"admin","password":"password123"}`)))
	if setup.Code != 200 {
		t.Fatalf("setup: %d %s", setup.Code, setup.Body)
	}
	var auth struct {
		CSRF string `json:"csrf_token"`
	}
	if err = json.Unmarshal(setup.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Owned RAR", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Owned", Path: lib.Path})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(lib.Path, "owned.cbr")
	data, err := os.ReadFile("testdata/vol.cbr")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	book, err := store.CreateBook(ctx, database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID, Name: "owned.cbr", Path: path, Size: int64(len(data)), PageCount: 4})
	if err != nil {
		t.Fatal(err)
	}
	issue := httptest.NewRequest("POST", "/api/mcp/credentials", strings.NewReader(fmt.Sprintf(`{"user_id":1,"name":"lock-cancellation","library_ids":[%d],"capabilities":["read"]}`, lib.ID)))
	issue.AddCookie(setup.Result().Cookies()[0])
	issue.Header.Set("X-CSRF-Token", auth.CSRF)
	issued := httptest.NewRecorder()
	router.ServeHTTP(issued, issue)
	if issued.Code != 200 {
		t.Fatalf("issue: %d %s", issued.Code, issued.Body)
	}
	var credential struct {
		Token string `json:"token"`
	}
	if err = json.Unmarshal(issued.Body.Bytes(), &credential); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router)
	defer server.Close()
	cfg = manager.Snapshot()
	cfg.MCP.PublicURL = server.URL
	manager.Replace(&cfg)
	parser.InitPool(8)
	defer parser.CloseArchivePool()
	arc, release, err := parser.GetArchiveFromPool(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	wait := func(label string, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !condition() {
			if time.Now().After(deadline) {
				t.Fatalf("timeout: %s", label)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	volume := config.ResolveStoragePolicy(cfg, path).VolumeKey
	activeIO := func() int {
		n := 0
		for _, v := range storageio.Default.Snapshot() {
			if v.VolumeKey == volume {
				n += v.ReaderActive + v.ReaderWaiting
			}
		}
		return n
	}
	for attempt := range 2 {
		func() {
			unlock := parser.HoldRarSessionForTest(arc)
			defer func() { unlock() }()
			disconnect, exited := startOwnedClueRequest(t, binary, server.URL, credential.Token, book.ID, stdio)
			defer disconnect()
			wait("request borrowed while ordinary owner remains active", func() bool { return parser.ActiveArchiveBorrows(path) == 2 && activeIO() == 1 })
			var closed chan error
			if attempt == 1 {
				closed = make(chan error, 1)
				go func() { closed <- arc.Close() }()
			}
			disconnect()
			wait("cancelled request released borrow and IO with owner still active", func() bool { return parser.ActiveArchiveBorrows(path) == 1 && activeIO() == 0 })
			wait("cancelled request released library read with owner still active", func() bool {
				conflicts, err := process.Application.Engine.LibraryConflicts(ctx, lib.ID, 0)
				if err != nil {
					t.Fatal(err)
				}
				for _, c := range conflicts {
					if c.Reason == "wait_for_reads" {
						return false
					}
				}
				return true
			})
			if exited != nil {
				select {
				case err := <-exited:
					if err != nil {
						t.Fatalf("normal proxy EOF: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("proxy did not exit on EOF")
				}
			}
			if closed != nil {
				select {
				case err := <-closed:
					t.Fatalf("Close bypassed active owner: %v", err)
				default:
				}
			}
			unlock()
			unlock = func() {}
			if closed != nil {
				select {
				case err := <-closed:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("Close did not finish after owner released")
				}
				if _, err := parser.ReadMetadataLimited(context.Background(), arc, "ComicInfo.xml", 64<<10); !errors.Is(err, parser.ErrArchiveClosed) {
					t.Fatalf("closed archive reopened: %v", err)
				}
			} else {
				data, err := arc.ReadPage("1.jpg")
				if err != nil || string(data) != "j1" {
					t.Fatalf("ordinary reader after cancellation: %q %v", data, err)
				}
			}
			t.Logf("attempt %d: cancellation released IO/borrow/library before ordinary owner; close race=%t", attempt, closed != nil)
		}()
	}
}

func startOwnedClueRequest(t *testing.T, binary, url, token string, book int64, stdio bool) (func(), <-chan error) {
	t.Helper()
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"library_get_book_clues","arguments":{"book_id":%d}}}`, book)
	if !stdio {
		host := strings.TrimPrefix(url, "http://")
		conn, err := net.Dial("tcp", host)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = fmt.Fprintf(conn, "POST /mcp HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nAccept: application/json, text/event-stream\r\nMCP-Protocol-Version: 2025-11-25\r\nContent-Length: %d\r\n\r\n%s", host, token, len(payload), payload); err != nil {
			t.Fatal(err)
		}
		return func() { _ = conn.Close() }, nil
	}
	command := exec.CommandContext(t.Context(), binary, "--url", url, "--allow-lan-http")
	command.Dir = t.TempDir()
	command.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN="+token)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	finished := make(chan struct{})
	go func() { exited <- command.Wait(); close(finished) }()
	t.Cleanup(func() {
		_ = stdin.Close()
		select {
		case <-finished:
		default:
			_ = command.Process.Kill() // failure cleanup, never counted as EOF success
			<-finished
		}
	})
	if _, err = fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"held-lock","version":"1"}}}`); err != nil {
		t.Fatal(err)
	}
	if _, err = bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprintln(stdin, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprintln(stdin, payload); err != nil {
		t.Fatal(err)
	}
	return func() { _ = stdin.Close() }, exited
}
