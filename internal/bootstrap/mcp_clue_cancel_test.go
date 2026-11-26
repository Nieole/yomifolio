package bootstrap_test

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/parser"
	"yomifolio/internal/storageio"
	"yomifolio/internal/task"
)

// Real RAR clues hold a request-owned admission and volume IO lease; a client that goes away lets
// go of both over HTTPS and over the stdio helper. A solid stream whose first declaration is
// undersized reads as unavailable clues and yields no page image.
func TestComicInfoCluesLetGoWhenTheClientLeaves(t *testing.T) {
	for _, stdio := range []bool{false, true} {
		t.Run(map[bool]string{false: "https", true: "stdio"}[stdio], func(t *testing.T) {
			ctx := context.Background()
			store, cfg := fixture(t)
			lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Clues", Path: t.TempDir(), ScanMode: "none"})
			if err != nil {
				t.Fatal(err)
			}
			series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Clues", Path: filepath.Join(lib.Path, "Clues")})
			if err != nil {
				t.Fatal(err)
			}
			book := func(name string, pages int64) (int64, string) {
				t.Helper()
				data, err := os.ReadFile("../parser/testdata/" + name)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(lib.Path, name)
				if err = os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
				created, err := store.CreateBook(ctx, database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID, Name: name, Path: path, Size: int64(len(data)), FileModifiedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), PageCount: pages})
				if err != nil {
					t.Fatal(err)
				}
				return created.ID, path
			}
			rar, rarPath := book("vol.cbr", 4)
			hostile, _ := book("solid-undersized.cbr", 1)
			process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer process.Close()
			router := chi.NewRouter()
			process.HTTP.SetupRoutes(router)
			var server *httptest.Server
			if stdio {
				server = httptest.NewServer(router)
			} else {
				server = httptest.NewTLSServer(router)
			}
			defer server.Close()
			snapshot := cfg.Snapshot()
			snapshot.Library.IOPolicy.ArchiveOpenConcurrency = 1
			snapshot.MCP.PublicURL = server.URL
			snapshot.MCP.AllowLANHTTP = stdio
			cfg.Replace(&snapshot)
			var issued struct{ Token string }
			if err = json.Unmarshal(sourceHTTP(t, process)("POST", "/mcp/credentials", fmt.Sprintf(`{"user_id":1,"name":"clues","library_ids":[%d],"capabilities":["read"]}`, lib.ID), http.StatusOK).Body.Bytes(), &issued); err != nil {
				t.Fatal(err)
			}
			binary := ""
			if stdio {
				binary = filepath.Join(t.TempDir(), "mcp-proxy.exe")
				if out, err := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/mcp-proxy").CombinedOutput(); err != nil {
					t.Fatalf("build: %v %s", err, out)
				}
			}
			assertClueTransportDisconnect(t, cfg.Snapshot(), server, binary, issued.Token, process.Application.Engine, rarPath, lib.ID, rar, stdio)

			session, err := mcp.NewClient(&mcp.Implementation{Name: "clues", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: issued.Token}}}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			clueStatus := func(id int64) any {
				t.Helper()
				result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_book_clues", Arguments: map[string]any{"book_id": id}})
				if err != nil || result.IsError {
					t.Fatalf("clues %d: %+v %v", id, result, err)
				}
				var out struct {
					ComicInfo map[string]any `json:"comic_info"`
				}
				raw, _ := json.Marshal(result.StructuredContent)
				_ = json.Unmarshal(raw, &out)
				return out.ComicInfo["status"]
			}
			if got := clueStatus(rar); got != "available" {
				t.Fatalf("clues after the disconnects: %v", got)
			}
			if got := clueStatus(hostile); got != "unavailable" {
				t.Fatalf("hostile solid clues: %v", got)
			}
			rejected, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_page_thumbnails", Arguments: map[string]any{"series_id": series.ID, "book_id": hostile, "pages": []int{1}}})
			if err != nil || !rejected.IsError {
				t.Fatalf("hostile solid image: %+v %v", rejected, err)
			}
			for _, content := range rejected.Content {
				if _, ok := content.(*mcp.ImageContent); ok {
					t.Fatal("hostile solid returned an image")
				}
			}
		})
	}
}

// Close the actual socket/stdin, rather than SDK Close (which may first wait
// for in-flight RPCs). Keep IO saturated until server-owned admission is released.
func assertClueTransportDisconnect(t *testing.T, cfg config.Config, server *httptest.Server, binary, token string, engine *task.Engine, path string, library, book int64, stdio bool) {
	t.Helper()
	policy := config.ResolveStoragePolicy(cfg, path)
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
	waiting := func() int {
		n := 0
		for _, s := range storageio.Default.Snapshot() {
			if s.VolumeKey == policy.VolumeKey {
				n += s.ReaderWaiting
			}
		}
		return n
	}
	for attempt := range 2 {
		lease, err := storageio.Default.Acquire(t.Context(), storageio.Request{VolumeKey: policy.VolumeKey, Limit: 1, Kind: storageio.WorkKindReader})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer lease.Release()
			payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "library_get_book_clues", "arguments": map[string]any{"book_id": book}}})
			var disconnect func()
			if !stdio {
				host := strings.TrimPrefix(strings.TrimPrefix(server.URL, "https://"), "http://")
				var conn net.Conn
				if strings.HasPrefix(server.URL, "https://") {
					conn, err = tls.Dial("tcp", host, server.Client().Transport.(*http.Transport).TLSClientConfig)
				} else {
					conn, err = net.Dial("tcp", host)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if _, err = fmt.Fprintf(conn, "POST /mcp HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nAccept: application/json, text/event-stream\r\nMCP-Protocol-Version: 2025-11-25\r\nContent-Length: %d\r\n\r\n%s", host, token, len(payload), payload); err != nil {
					t.Fatal(err)
				}
				disconnect = func() { _ = conn.Close() }
			} else {
				command := exec.CommandContext(t.Context(), binary, "--url", server.URL, "--allow-lan-http")
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
				go func() { exited <- command.Wait() }()
				defer func() {
					select {
					case err := <-exited:
						if err != nil {
							t.Errorf("helper EOF exit: %v", err)
						}
					case <-time.After(5 * time.Second):
						_ = command.Process.Kill()
						if err := <-exited; err != nil {
							t.Logf("helper exit after kill: %v", err)
						}
						t.Error("helper did not exit normally after EOF")
					}
				}()
				_, _ = fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"clue-eof","version":"1"}}}`)
				if _, err = bufio.NewReader(stdout).ReadString('\n'); err != nil {
					t.Fatal(err)
				}
				_, _ = fmt.Fprintln(stdin, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
				_, _ = fmt.Fprintln(stdin, string(payload))
				disconnect = func() { _ = stdin.Close() }
			}
			wait("clue reached saturated IO", func() bool { return waiting() > 0 })
			disconnect()
			wait("clue released IO waiter", func() bool { return waiting() == 0 })
			wait("clue released library read", func() bool {
				conflicts, err := engine.LibraryConflicts(t.Context(), library, 0)
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
			if parser.ActiveArchiveBorrows(path) != 0 {
				t.Fatal("retained clue archive borrow")
			}
		}()
		t.Logf("clue socket/EOF cancellation attempt %d released admission", attempt)
	}
}
