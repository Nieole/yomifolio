package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"yomifolio/internal/api"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
)

func TestMCPMatchingPagination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, cfg := fixture(t)
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Comics", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Local", Path: lib.Path + "/local"})
	if err != nil {
		t.Fatal(err)
	}
	type request struct {
		query         string
		limit, offset int
	}
	var mu sync.Mutex
	var requests []request
	process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{Sources: api.ExternalSources{Metadata: func(_ config.Config, name string) metadata.Provider {
		return &controlledSource{search: func(_ context.Context, query string, limit, offset int) ([]*metadata.SeriesMetadata, int, error) {
			mu.Lock()
			requests = append(requests, request{query, limit, offset})
			mu.Unlock()
			if limit < 1 || limit > 10 || offset < 0 || offset > 1000 || offset%limit != 0 {
				return nil, 0, fmt.Errorf("request exceeded source budget")
			}
			total := 2000
			switch query {
			case "normal":
				total = 5
			case "boundary-tail":
				total = 1010
			case "unaligned-tail":
				total = 1001
			}
			count := min(limit, max(0, total-offset))
			if query == "short-boundary" {
				count = 1
			}
			results := make([]*metadata.SeriesMetadata, count)
			for i := range results {
				results[i] = &metadata.SeriesMetadata{Title: fmt.Sprintf("%s:%d", name, offset+i), SourceID: offset + i}
			}
			return results, total, nil
		}}
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	manage := sourceHTTP(t, process)
	issuedResponse := manage("POST", "/mcp/credentials", fmt.Sprintf(`{"user_id":1,"name":"pagination","library_ids":[%d],"capabilities":["read"]}`, lib.ID), 200)
	var issued struct{ Token string }
	if err = json.Unmarshal(issuedResponse.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	server := httptest.NewTLSServer(router)
	defer server.Close()
	snapshot := cfg.Snapshot()
	snapshot.MCP.PublicURL = server.URL
	cfg.Replace(&snapshot)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "pagination", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: issued.Token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(t *testing.T, name string, args any, wantError bool) map[string]any {
		t.Helper()
		out, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil {
			// A capability the credential no longer holds also withdraws the tool itself.
			if wantError {
				return nil
			}
			t.Fatal(e)
		}
		if out.IsError != wantError {
			t.Fatalf("%s: %+v", name, out)
		}
		raw, e := json.Marshal(out.StructuredContent)
		if e != nil {
			t.Fatal(e)
		}
		var result map[string]any
		if e = json.Unmarshal(raw, &result); e != nil {
			t.Fatal(e)
		}
		return result
	}
	search := func(t *testing.T, query string, limit, offset int) map[string]any {
		t.Helper()
		started := call(t, "library_search_sources", map[string]any{"series_id": series.ID, "query": query, "limit": limit, "offset": offset}, false)
		call(t, "library_wait_run", map[string]any{"run_id": started["run"].(map[string]any)["run_id"], "timeout_ms": 30000}, false)
		found := call(t, "library_get_source_search", map[string]any{"search_id": started["search_id"]}, false)
		if found["status"] != "completed" {
			t.Fatal(found)
		}
		return found
	}
	for _, tc := range []struct {
		name, query                       string
		limit, offset, next, count, total int
		truncated                         bool
	}{
		{"boundary-10", "boundary", 10, 1000, -1, 10, 2000, true},
		{"short-boundary", "short-boundary", 10, 1000, -1, 1, 2000, true},
		{"last-3", "boundary", 3, 999, -1, 3, 2000, true},
		{"last-7", "boundary", 7, 994, -1, 7, 2000, true},
		{"last-9", "boundary", 9, 999, -1, 9, 2000, true},
		{"follow-10", "boundary", 10, 990, 1000, 10, 2000, false},
		{"follow-3", "boundary", 3, 996, 999, 3, 2000, false},
		{"follow-7", "boundary", 7, 987, 994, 7, 2000, false},
		{"follow-9", "boundary", 9, 990, 999, 9, 2000, false},
		{"normal-two-pages", "normal", 3, 0, 3, 3, 5, false},
		{"total-tail", "normal", 3, 3, -1, 2, 5, false},
		{"boundary-total-tail", "boundary-tail", 10, 1000, -1, 10, 1010, false},
		{"unaligned-total-tail", "unaligned-tail", 3, 999, -1, 2, 1001, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := func(found map[string]any, offset, count, wantNext int, truncated bool) {
				t.Helper()
				pages := found["sources"].([]any)
				if len(pages) != 3 {
					t.Fatal(found)
				}
				for _, value := range pages {
					page := value.(map[string]any)
					if page["status"] != "available" || page["total"] != float64(tc.total) {
						t.Fatal(page)
					}
					if (page["truncated"] == true) != truncated {
						t.Errorf("truncated=%v, want %v: %v", page["truncated"], truncated, page)
					}
					candidates := page["candidates"].([]any)
					if len(candidates) != count || len(candidates) > tc.limit {
						t.Fatal(page)
					}
					for i, value := range candidates {
						candidate := value.(map[string]any)["metadata"].(map[string]any)
						if candidate["Title"] != fmt.Sprintf("%s:%d", page["source"], offset+i) {
							t.Fatal(candidate)
						}
					}
					next, exists := page["next_offset"]
					if wantNext < 0 {
						if exists {
							t.Errorf("unusable/unnecessary next_offset=%v: %v", next, page)
						}
					} else if !exists || next != float64(wantNext) || wantNext > 1000 || wantNext%tc.limit != 0 {
						t.Errorf("next_offset=%v, want %d", next, wantNext)
					}
				}
			}
			found := search(t, tc.query, tc.limit, tc.offset)
			check(found, tc.offset, tc.count, tc.next, tc.truncated)
			if tc.next >= 0 {
				// Submit the actual advertised cursor, not a separately constructed request.
				next := int(found["sources"].([]any)[0].(map[string]any)["next_offset"].(float64))
				following := search(t, tc.query, tc.limit, next)
				if tc.query == "normal" {
					check(following, 3, 2, -1, false)
				} else {
					check(following, tc.next, tc.limit, -1, true)
				}
			}
		})
	}
	mu.Lock()
	before := len(requests)
	mu.Unlock()
	for _, args := range []map[string]any{
		{"limit": 10, "offset": 1010}, {"limit": 7, "offset": 1000}, {"limit": 11, "offset": 0},
	} {
		args["series_id"], args["query"] = series.ID, "invalid"
		call(t, "library_search_sources", args, true)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != before {
		t.Fatal("invalid requests reached external sources")
	}
	// Each accepted page contacts each configured source exactly once, without prefetch.
	if len(requests) != 54 {
		t.Fatalf("source requests=%d, want 54", len(requests))
	}
}
