package bootstrap_test

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/database"
)

// The archived database and requests were produced by c4540ce4's production
// bootstrap, not by computing an expected digest with the implementation under test.
func TestProductionMCPEnrichmentBase08Replay(t *testing.T) {
	for _, stdio := range []bool{false, true} {
		t.Run(fmt.Sprintf("stdio=%t", stdio), func(t *testing.T) {
			ctx := t.Context()
			var fixtureData struct {
				Token      string         `json:"token"`
				Input      map[string]any `json:"input"`
				Applied    map[string]any `json:"applied"`
				UndoInput  map[string]any `json:"undo_input"`
				Undone     map[string]any `json:"undone"`
				Unreversed map[string]any `json:"unreversed"`
			}
			raw, err := os.ReadFile("testdata/enrichment-base08.json")
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(raw, &fixtureData); err != nil {
				t.Fatal(err)
			}
			store, cfg := fixture(t)
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			compressed, err := os.Open("testdata/enrichment-base08.db.gz")
			if err != nil {
				t.Fatal(err)
			}
			defer compressed.Close()
			archive, err := gzip.NewReader(compressed)
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()
			data, err := io.ReadAll(archive)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(cfg.Snapshot().Database.Path, data, 0600); err != nil {
				t.Fatal(err)
			}
			// Exercise the actual startup upgrade path on the archived 08 DB.
			if err = database.Migrate(cfg.Snapshot().Database.Path); err != nil {
				t.Fatal(err)
			}
			store, err = database.NewStore(cfg.Snapshot().Database.Path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer process.Close()
			router := chi.NewRouter()
			process.HTTP.SetupRoutes(router)
			server := httptest.NewServer(router)
			defer server.Close()
			snapshot := cfg.Snapshot()
			snapshot.MCP.PublicURL = server.URL
			snapshot.MCP.AllowLANHTTP = true
			cfg.Replace(&snapshot)
			var transport mcp.Transport = &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: fixtureData.Token}}}
			if stdio {
				binary := filepath.Join(t.TempDir(), "mcp-proxy.exe")
				if output, e := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/mcp-proxy").CombinedOutput(); e != nil {
					t.Fatalf("build proxy: %s %v", output, e)
				}
				cmd := exec.CommandContext(ctx, binary, "--url", server.URL, "--allow-lan-http")
				cmd.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN="+fixtureData.Token)
				transport = &mcp.CommandTransport{Command: cmd}
			}
			session, err := mcp.NewClient(&mcp.Implementation{Name: "base08-replay", Version: "1"}, nil).Connect(ctx, transport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			call := func(name string, in any) map[string]any {
				t.Helper()
				result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: in})
				if e != nil || result.IsError {
					t.Fatalf("%s: %+v %v", name, result, e)
				}
				raw, e := json.Marshal(result.StructuredContent)
				if e != nil {
					t.Fatal(e)
				}
				out := map[string]any{}
				if e = json.Unmarshal(raw, &out); e != nil {
					t.Fatal(e)
				}
				return out
			}
			reject := func(name string, in any) {
				t.Helper()
				result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: in})
				if e != nil || !result.IsError {
					t.Fatalf("changed payload replayed: %+v %v", result, e)
				}
			}
			checkReplay := func(got, want map[string]any) {
				t.Helper()
				// Response State gained authors, but the old audit never asserted that field.
				for _, field := range []string{"before", "after"} {
					state := got[field].(map[string]any)
					if state["authors"] != nil {
						t.Fatalf("invented old authors: %v", state)
					}
					delete(state, "authors")
				}
				wantCopy := map[string]any{}
				for k, v := range want {
					wantCopy[k] = v
				}
				wantCopy["replayed"] = true
				wantCopy["applied"] = false
				if !reflect.DeepEqual(got, wantCopy) {
					t.Fatalf("receipt changed:\ngot %v\nwant %v", got, wantCopy)
				}
			}
			// Even an already-undone enrich must return its immutable original receipt.
			want := fixtureData.Applied
			want["undone_by"] = fixtureData.Undone["id"]
			expected := fixtureData.Input["expected"].(map[string]any)
			// Explicit null also passes the pre-fix schema: failure here exposes
			// the changed persisted digest, not just a newly required property.
			expected["authors"] = nil
			checkReplay(call("library_enrich_metadata", fixtureData.Input), want)
			delete(expected, "authors")
			checkReplay(call("library_enrich_metadata", fixtureData.Input), want)
			// Missing, null and empty authors all denote the absent extension.
			for _, authors := range []any{nil, []any{}} {
				expected["authors"] = authors
				checkReplay(call("library_enrich_metadata", fixtureData.Input), want)
			}
			// Compatibility must never drop a nonempty author expectation.
			expected["authors"] = []any{map[string]any{"name": "Different", "role": "Writer"}}
			reject("library_enrich_metadata", fixtureData.Input)
			delete(expected, "authors")
			batch := call("library_enrich_metadata_batch", map[string]any{"operations": []any{fixtureData.Input}})
			checkReplay(batch["operations"].([]any)[0].(map[string]any), want)

			checkReplay(call("library_undo_metadata_operation", fixtureData.UndoInput), fixtureData.Undone)
			fixtureData.Input["proposal_ids"] = []int{999}
			reject("library_enrich_metadata", fixtureData.Input)
			fixtureData.UndoInput["operation_id"] = fixtureData.Unreversed["id"]
			reject("library_undo_metadata_operation", fixtureData.UndoInput)
			// Decode old before/after State and actually undo it; missing authors must not
			// clear an unrelated author group introduced after the old operation.
			if _, err = store.DB().Exec(`INSERT INTO authors(name,role) VALUES('Later author','Writer')`); err != nil {
				t.Fatal(err)
			}
			if _, err = store.DB().Exec(`INSERT INTO series_authors SELECT ?,id FROM authors WHERE name='Later author'`, fixtureData.Unreversed["series_id"]); err != nil {
				t.Fatal(err)
			}
			undone := call("library_undo_metadata_operation", map[string]any{"request_key": "09-undo-old-audit", "operation_id": fixtureData.Unreversed["id"]})
			if undone["outcome"] != "changed" || undone["after"].(map[string]any)["summary"] != nil {
				t.Fatal(undone)
			}
			authors := undone["after"].(map[string]any)["authors"]
			if !reflect.DeepEqual(authors, []any{map[string]any{"name": "Later author", "role": "Writer"}}) {
				t.Fatalf("old undo changed authors: %v", authors)
			}
		})
	}
}
