package bootstrap_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/config"
	"yomifolio/internal/parser"
	"yomifolio/internal/storageio"
)

// A cover improvement whose client goes away while it waits for archive IO lets go of everything
// it held: the IO wait, the library read, the archive and the cover slot. Exercise actual socket
// closure and proxy stdin EOF, not cancellation of an application context, and keep the volume
// full until all admission has drained.
func TestCoverImprovementLetsGoWhenItsClientLeaves(t *testing.T) {
	w := openCoverWorld(t)
	committed := map[string]any{"request_key": "cover-committed", "series_id": w.series, "book_id": w.book, "page": 2, "reason": "page 2 is the full cover art"}
	receipt := w.improve("cover-committed", committed)
	if receipt.Outcome != "changed" || !w.blue() {
		t.Fatalf("committed cover %+v", receipt)
	}
	before := w.operations()
	issued, err := delegated.New(delegated.NewSQLStore(w.store.DB())).Issue(t.Context(), 1, delegated.Restrictions{UserID: 1, Name: "leaving", LibraryIDs: []int64{w.library.ID}, Capabilities: []string{delegated.Read, delegated.MetadataWrite}})
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(w.library.Path, "book.cbz")
	server := httptest.NewServer(w.web.router)
	defer server.Close()
	previous := w.cfg.Snapshot()
	defer w.cfg.Replace(&previous)
	snapshot := previous
	snapshot.Library.IOPolicy.ArchiveOpenConcurrency = 1
	snapshot.MCP.PublicURL = server.URL
	snapshot.MCP.AllowLANHTTP = true
	w.cfg.Replace(&snapshot)
	binary := filepath.Join(t.TempDir(), "cover-cancel-proxy.exe")
	if out, err := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../../cmd/mcp-proxy").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	policy := config.ResolveStoragePolicy(snapshot, archive)
	engine := w.web.process.Application.Engine
	wait := func(t *testing.T, label string, condition func() bool) {
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
	for _, transport := range []string{"http", "stdio"} {
		t.Run("cover-disconnect-"+transport, func(t *testing.T) {
			// Two abandoned preparations must not exhaust the two cover slots. A third
			// preparation must reach the IO gate too, proving both image and cover release.
			var leases []storageio.Lease
			for range policy.IOPolicy.ArchiveOpenConcurrency {
				lease, err := storageio.Default.Acquire(t.Context(), storageio.Request{VolumeKey: policy.VolumeKey, Limit: policy.IOPolicy.ArchiveOpenConcurrency, Kind: storageio.WorkKindReader})
				if err != nil {
					t.Fatal(err)
				}
				leases = append(leases, lease)
			}
			defer func() {
				for _, l := range leases {
					l.Release()
				}
			}()
			for attempt := range 3 {
				input := map[string]any{}
				for k, v := range committed {
					input[k] = v
				}
				input["request_key"] = fmt.Sprintf("disconnect-%s-%d", transport, attempt)
				payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "library_improve_cover", "arguments": input}})
				var disconnect func()
				if transport == "http" {
					conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
					_, err = fmt.Fprintf(conn, "POST /mcp HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nAccept: application/json, text/event-stream\r\nMCP-Protocol-Version: 2025-11-25\r\nContent-Length: %d\r\n\r\n%s", strings.TrimPrefix(server.URL, "http://"), issued.Token, len(payload), payload)
					if err != nil {
						t.Fatal(err)
					}
					disconnect = func() { _ = conn.Close() }
				} else {
					command := exec.CommandContext(t.Context(), binary, "--url", server.URL, "--allow-lan-http")
					command.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN="+issued.Token)
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
					defer func() { _ = command.Process.Kill() }()
					_, _ = fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"cover-eof","version":"1"}}}`)
					if _, err = bufio.NewReader(stdout).ReadString('\n'); err != nil {
						t.Fatal(err)
					}
					_, _ = fmt.Fprintln(stdin, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
					_, _ = fmt.Fprintln(stdin, string(payload))
					disconnect = func() { _ = stdin.Close() }
					defer func() { _ = command.Wait() }()
				}
				wait(t, "cover reached saturated volume", func() bool { return waiting() > 0 })
				disconnect()
				wait(t, "disconnected cover released IO wait", func() bool { return waiting() == 0 })
				wait(t, "disconnected cover released library read", func() bool {
					conflicts, err := engine.LibraryConflicts(t.Context(), w.library.ID, 0)
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
				if parser.ActiveArchiveBorrows(archive) != 0 {
					t.Fatal("retained archive borrow")
				}
			}
		})
	}
	// The abandoned requests changed nothing and recorded nothing; the committed receipt still replays.
	w.cfg.Replace(&previous)
	if !w.blue() {
		t.Fatal("an abandoned request changed the cover")
	}
	if after := w.operations(); after != before {
		t.Fatalf("abandoned requests left receipts: %s", after)
	}
	if replay := w.improve("cover-committed", committed); replay.ID != receipt.ID || !replay.Replayed || replay.Applied {
		t.Fatalf("lost durable receipt: %+v", replay)
	}
}
