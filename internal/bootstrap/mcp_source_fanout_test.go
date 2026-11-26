// 每个目录型来源的回答各自校验、各自报状态：一个来源的坏数据或超额只让它自己不可用，
// 留下的候选是脱离来源对象的快照。破了，坏来源会污染整次搜索或让候选在保存后被改写。

package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestMCPSourceSearchReportsEachCatalogueStatus(t *testing.T) {
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
	snapshot := cfg.Snapshot()
	snapshot.Scrapers.MALClientID = "test"
	snapshot.Scrapers.ComicVineAPIKey = "test"
	cfg.Replace(&snapshot)
	var mu sync.Mutex
	returned := map[string]*metadata.SeriesMetadata{}
	many := func(n int, item func(i int) *metadata.SeriesMetadata) []*metadata.SeriesMetadata {
		out := make([]*metadata.SeriesMetadata, n)
		for i := range out {
			out[i] = item(i)
		}
		return out
	}
	process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{Sources: api.ExternalSources{Metadata: func(_ config.Config, name string) metadata.Provider {
		return &controlledSource{search: func(_ context.Context, query string, limit, _ int) ([]*metadata.SeriesMetadata, int, error) {
			switch query {
			case "Mixed":
				switch name {
				case "anilist":
					return []*metadata.SeriesMetadata{{Title: "tagged", Tags: make([]string, 101)}}, 1, nil
				case "mangadex":
					return nil, 0, fmt.Errorf("unavailable")
				case "myanimelist":
					// Every candidate is valid on its own; together they exceed the result budget.
					return many(limit, func(int) *metadata.SeriesMetadata {
						return &metadata.SeriesMetadata{Summary: strings.Repeat("m", 30<<10)}
					}), 50, nil
				}
				item := &metadata.SeriesMetadata{Title: "Kept " + name, Tags: []string{"kept"}, SourceID: 7, Provider: "spoofed"}
				mu.Lock()
				returned[name] = item
				mu.Unlock()
				return []*metadata.SeriesMetadata{item}, 1, nil
			case "Cumulative":
				return many(4, func(int) *metadata.SeriesMetadata {
					return &metadata.SeriesMetadata{Summary: strings.Repeat("c", 25<<10)}
				}), 4, nil
			case "Oversized candidate":
				return []*metadata.SeriesMetadata{{Summary: strings.Repeat("x", 33<<10)}}, 1, nil
			case "Too many tags":
				return []*metadata.SeriesMetadata{{Tags: make([]string, 101)}}, 1, nil
			case "Too many tag votes":
				return []*metadata.SeriesMetadata{{TagVotes: make([]metadata.TagVote, 101)}}, 1, nil
			case "Negative tag votes":
				return []*metadata.SeriesMetadata{{TagVotes: []metadata.TagVote{{Name: "tag", Votes: -1}}}}, 1, nil
			case "Too many authors":
				return []*metadata.SeriesMetadata{{Authors: make([]metadata.SeriesAuthor, 101)}}, 1, nil
			case "Negative source ID":
				return []*metadata.SeriesMetadata{{SourceID: -1}}, 1, nil
			case "Negative volumes":
				return []*metadata.SeriesMetadata{{VolumeCount: -1}}, 1, nil
			case "Rating above scale":
				return []*metadata.SeriesMetadata{{Rating: 10.5}}, 1, nil
			case "Negative rating":
				return []*metadata.SeriesMetadata{{Rating: -1}}, 1, nil
			case "Non-finite rating":
				return []*metadata.SeriesMetadata{{Rating: math.NaN()}}, 1, nil
			case "Decision record":
				return []*metadata.SeriesMetadata{{MatchEvidence: &metadata.MatchEvidence{Summary: "source decided"}}}, 1, nil
			case "Missing result":
				return []*metadata.SeriesMetadata{nil}, 1, nil
			case "More than requested":
				return many(limit+1, func(int) *metadata.SeriesMetadata { return &metadata.SeriesMetadata{} }), limit + 1, nil
			case "Negative total":
				return nil, -1, nil
			}
			return nil, 0, fmt.Errorf("unexpected query %q", query)
		}}
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	manage := sourceHTTP(t, process)
	issuedResponse := manage("POST", "/mcp/credentials", fmt.Sprintf(`{"user_id":1,"name":"statuses","library_ids":[%d],"capabilities":["read"]}`, lib.ID), 200)
	var issued struct{ Token string }
	if err = json.Unmarshal(issuedResponse.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	server := httptest.NewTLSServer(router)
	defer server.Close()
	snapshot = cfg.Snapshot()
	snapshot.MCP.PublicURL = server.URL
	cfg.Replace(&snapshot)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "statuses", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: issued.Token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(t *testing.T, name string, args any) map[string]any {
		t.Helper()
		out, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil || out.IsError {
			t.Fatalf("%s: %+v %v", name, out, e)
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
	read := func(t *testing.T, id any) (string, []map[string]any) {
		t.Helper()
		found := call(t, "library_get_source_search", map[string]any{"search_id": id})
		var pages []map[string]any
		for _, page := range found["sources"].([]any) {
			pages = append(pages, page.(map[string]any))
		}
		return found["status"].(string), pages
	}
	start := func(t *testing.T, query string, limit int) any {
		t.Helper()
		started := call(t, "library_search_sources", map[string]any{"series_id": series.ID, "query": query, "limit": limit})
		call(t, "library_wait_run", map[string]any{"run_id": started["run"].(map[string]any)["run_id"], "timeout_ms": 30000})
		return started["search_id"]
	}
	search := func(t *testing.T, query string, limit int) (string, []map[string]any) {
		t.Helper()
		return read(t, start(t, query, limit))
	}
	statuses := func(pages []map[string]any) []string {
		var out []string
		for _, page := range pages {
			out = append(out, fmt.Sprintf("%s=%s", page["source"], page["status"]))
		}
		return out
	}

	t.Run("each catalogue keeps its own status", func(t *testing.T) {
		id := start(t, "Mixed", 10)
		status, pages := read(t, id)
		want := []string{"bangumi=available", "anilist=invalid_response", "mangadex=failed", "myanimelist=too_large", "comicvine=available"}
		if status != "partial" || fmt.Sprint(statuses(pages)) != fmt.Sprint(want) {
			t.Fatalf("status=%s pages=%v, want partial %v", status, statuses(pages), want)
		}
		for _, page := range pages {
			candidates := page["candidates"].([]any)
			if page["status"] != "available" {
				if len(candidates) != 0 {
					t.Errorf("%s kept candidates: %v", page["source"], page)
				}
				continue
			}
			if len(candidates) != 1 {
				t.Fatal(page)
			}
			kept := candidates[0].(map[string]any)
			if kept["candidate_id"] != fmt.Sprintf("%s:0", page["source"]) || kept["metadata"].(map[string]any)["Provider"] != page["source"] {
				t.Errorf("candidate not attributed to its catalogue: %v", kept)
			}
		}
		// Kept candidates are snapshots: the source changing its own objects later changes nothing.
		mu.Lock()
		for _, item := range returned {
			item.Title = "rewritten"
			item.Tags[0] = "rewritten"
		}
		mu.Unlock()
		_, again := read(t, id)
		for _, page := range again {
			for _, value := range page["candidates"].([]any) {
				kept := value.(map[string]any)["metadata"].(map[string]any)
				if kept["Title"] != fmt.Sprintf("Kept %s", page["source"]) || fmt.Sprint(kept["Tags"]) != "[kept]" {
					t.Errorf("%s candidate followed the source object: %v", page["source"], kept)
				}
			}
		}
	})

	t.Run("manual scraping offers the same catalogues plus the model", func(t *testing.T) {
		var listed []map[string]string
		if err := json.Unmarshal(manage("GET", "/metadata/providers", "", 200).Body.Bytes(), &listed); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, provider := range listed {
			if provider["name"] == "" || provider["description"] == "" {
				t.Errorf("unlabelled provider: %v", provider)
			}
			ids = append(ids, provider["id"])
		}
		if want := "[bangumi anilist mangadex myanimelist comicvine llm]"; fmt.Sprint(ids) != want {
			t.Fatalf("providers=%v, want %s", ids, want)
		}
	})

	t.Run("the result budget spans catalogues", func(t *testing.T) {
		status, pages := search(t, "Cumulative", 4)
		want := []string{"bangumi=available", "anilist=available", "mangadex=too_large", "myanimelist=too_large", "comicvine=too_large"}
		if status != "partial" || fmt.Sprint(statuses(pages)) != fmt.Sprint(want) {
			t.Fatalf("status=%s pages=%v, want partial %v", status, statuses(pages), want)
		}
	})

	for _, tc := range []struct{ query, page string }{
		{"Oversized candidate", "invalid_response"},
		{"Too many tags", "invalid_response"},
		{"Too many tag votes", "invalid_response"},
		{"Negative tag votes", "invalid_response"},
		{"Too many authors", "invalid_response"},
		{"Negative source ID", "invalid_response"},
		{"Negative volumes", "invalid_response"},
		{"Rating above scale", "invalid_response"},
		{"Negative rating", "invalid_response"},
		{"Non-finite rating", "invalid_response"},
		{"Decision record", "invalid_response"},
		{"Missing result", "invalid_response"},
		{"More than requested", "failed"},
		{"Negative total", "failed"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			status, pages := search(t, tc.query, 2)
			if status != "failed" || len(pages) != 5 {
				t.Fatalf("status=%s pages=%v", status, statuses(pages))
			}
			for _, page := range pages {
				if page["status"] != tc.page || len(page["candidates"].([]any)) != 0 {
					t.Errorf("%s: %v, want %s without candidates", page["source"], page, tc.page)
				}
			}
		})
	}
}
