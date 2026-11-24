package bootstrap_test

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"yomifolio/internal/config"
	"yomifolio/internal/parser"
	"yomifolio/internal/storageio"
	"yomifolio/internal/task"
)

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
						<-exited
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
