package bootstrap_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/parser"
	"yomifolio/internal/storageio"
	"yomifolio/internal/task"
)

// Exercise actual socket closure and proxy stdin EOF, not cancellation of an
// application context. Keep the volume full until all admission has drained.
func verifyCoverTransportCancellation(t *testing.T, cfg *config.Manager, store database.Store, propose func(int64, string) any, router http.Handler, engine *task.Engine, library int64, archive, token string, committed, receipt map[string]any, call func(string, any) map[string]any) {
	t.Helper()
	state := call("library_get_enrichment_state", map[string]any{"series_id": committed["series_id"]})
	// Give each batch its own real text change before the blocked cover item.
	firstEntries := map[string]map[string]any{}
	for _, transport := range []string{"http", "stdio"} {
		for attempt := range 3 {
			key := fmt.Sprintf("before-disconnect-%s-%d", transport, attempt)
			series, err := store.CreateSeries(t.Context(), database.CreateSeriesParams{LibraryID: library, Name: key, Path: filepath.Join(filepath.Dir(archive), key)})
			if err != nil {
				t.Fatal(err)
			}
			firstEntries[key] = map[string]any{"request_key": key, "series_id": series.ID, "proposal_ids": []any{propose(series.ID, "bangumi")}, "fields": []string{"summary"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": series.ID})}
		}
	}
	server := httptest.NewServer(router)
	defer server.Close()
	previous := cfg.Snapshot()
	defer cfg.Replace(&previous)
	snapshot := previous
	snapshot.Library.IOPolicy.ArchiveOpenConcurrency = 1
	snapshot.MCP.PublicURL = server.URL
	snapshot.MCP.AllowLANHTTP = true
	cfg.Replace(&snapshot)
	binary := filepath.Join(t.TempDir(), "cover-cancel-proxy.exe")
	if out, err := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../../cmd/mcp-proxy").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	policy := config.ResolveStoragePolicy(snapshot, archive)
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
		for _, batch := range []bool{false, true} {
			t.Run(fmt.Sprintf("cover-disconnect-%s-batch-%t", transport, batch), func(t *testing.T) {
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
					input["expected"] = state
					input["request_key"] = fmt.Sprintf("disconnect-%s-%t-%d", transport, batch, attempt)
					name := "library_enrich_metadata"
					var arguments any = input
					if batch {
						name = "library_enrich_metadata_batch"
						first := firstEntries[fmt.Sprintf("before-disconnect-%s-%d", transport, attempt)]
						arguments = map[string]any{"operations": []any{first, input}}
					}
					// The first entry commits text and its receipt before the second
					// blocks on IO. Disconnect must preserve that durable audit.
					payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}})
					var disconnect func()
					if transport == "http" {
						conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
						if err != nil {
							t.Fatal(err)
						}
						defer conn.Close()
						_, err = fmt.Fprintf(conn, "POST /mcp HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nAccept: application/json, text/event-stream\r\nMCP-Protocol-Version: 2025-11-25\r\nContent-Length: %d\r\n\r\n%s", strings.TrimPrefix(server.URL, "http://"), token, len(payload), payload)
						if err != nil {
							t.Fatal(err)
						}
						disconnect = func() { _ = conn.Close() }
					} else {
						command := exec.CommandContext(t.Context(), binary, "--url", server.URL, "--allow-lan-http")
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
					if parser.ActiveArchiveBorrows(archive) != 0 {
						t.Fatal("retained archive borrow")
					}
				}
			})
		}
	}
	// Use the original credential for its receipt namespace. The disconnected
	// requests cannot change the publicly visible combined state or the old audit.
	cfg.Replace(&previous)
	if got := call("library_get_enrichment_state", map[string]any{"series_id": committed["series_id"]}); !reflect.DeepEqual(got, state) {
		t.Fatalf("cancel half-committed: %v", got)
	}
	for _, first := range firstEntries {
		op := call("library_enrich_metadata", first)
		if op["replayed"] != true || op["applied"] != false || op["outcome"] != "changed" || op["id"] == "" {
			t.Fatalf("lost committed batch prefix: %v", op)
		}
		changedState := call("library_get_enrichment_state", map[string]any{"series_id": first["series_id"]})
		if changedState["summary"] != "Space journey" {
			t.Fatalf("lost batch text: %v", changedState)
		}
		audit := call("library_get_metadata_operation", map[string]any{"operation_id": op["id"]})
		if audit["id"] != op["id"] || audit["outcome"] != "changed" {
			t.Fatalf("lost batch audit: %v", audit)
		}
	}
	replay := call("library_enrich_metadata", committed)
	if replay["id"] != receipt["id"] || replay["replayed"] != true || replay["applied"] != false {
		t.Fatalf("lost durable receipt: %v", replay)
	}
}
