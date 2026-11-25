package bootstrap_test

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/database"
)

// A listing advertises only the tools the credential's current capabilities cover, and every
// structured result also carries its payload as text for clients that read content only.
func TestMCPListingFollowsGrantedCapabilities(t *testing.T) {
	ctx := t.Context()
	// Run reads follow the credential's own work, so every capability keeps them.
	runReads := []string{"library_get_run", "library_wait_run", "library_cancel_run", "library_list_runs"}
	cases := []struct {
		capability string
		want       []string
	}{
		{capability: delegated.Read, want: append(slices.Clone(runReads),
			"library_get_series", "library_find_series", "library_list_books", "library_get_book_clues",
			"library_get_enrichment_state", "library_get_metadata_operation", "library_list_metadata_operations",
			"library_get_series_cover", "library_get_page_thumbnails",
			"library_search_sources", "library_get_source_search", "library_get_match_proposal")},
		{capability: delegated.MetadataWrite, want: append(slices.Clone(runReads),
			"library_enrich_metadata", "library_enrich_metadata_batch", "library_undo_metadata_operation",
			"library_propose_match")},
		{capability: delegated.ScanControl, want: append(slices.Clone(runReads), "library_start_scan")},
		{capability: delegated.InternalLLM, want: append(slices.Clone(runReads),
			"library_assess_match", "library_get_model_operation")},
		{capability: delegated.Cleanup, want: append(slices.Clone(runReads),
			"library_start_metadata_cleanup", "library_get_metadata_cleanup",
			"library_cleanup_metadata_operation")},
	}
	store, cfg := fixture(t)
	if _, err := store.CreateUser(ctx, database.CreateUserParams{Username: "admin", PasswordHash: "unused", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Granted", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{Name: "Granted series", LibraryID: lib.ID, Path: lib.Path + "/granted"})
	if err != nil {
		t.Fatal(err)
	}
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
	svc := delegated.New(delegated.NewSQLStore(store.DB()))
	connect := func(capabilities []string, name string) *mcp.ClientSession {
		t.Helper()
		issued, e := svc.Issue(ctx, 1, delegated.Restrictions{UserID: 1, Name: name, LibraryIDs: []int64{lib.ID}, Capabilities: capabilities})
		if e != nil {
			t.Fatal(e)
		}
		client := mcp.NewClient(&mcp.Implementation{Name: "listing", Version: "1"}, nil)
		httpClient := *server.Client()
		httpClient.Transport = &bearerTransport{base: server.Client().Transport, token: issued.Token}
		session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &httpClient}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
		if e != nil {
			t.Fatal(e)
		}
		return session
	}
	for _, tc := range cases {
		t.Run(tc.capability, func(t *testing.T) {
			session := connect([]string{tc.capability}, tc.capability)
			defer session.Close()
			listed, e := session.ListTools(ctx, nil)
			if e != nil {
				t.Fatal(e)
			}
			names := make([]string, 0, len(listed.Tools))
			for _, tool := range listed.Tools {
				names = append(names, tool.Name)
			}
			slices.Sort(names)
			slices.Sort(tc.want)
			if !slices.Equal(names, tc.want) {
				t.Fatalf("listing %v want %v", names, tc.want)
			}
		})
	}
	session := connect([]string{delegated.Read, delegated.MetadataWrite, delegated.ScanControl, delegated.InternalLLM, delegated.Cleanup}, "all")
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	assertMCPToolDiscovery(t, listed, err)
	call, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_series", Arguments: map[string]any{"series_id": series.ID}})
	if err != nil || call.IsError {
		t.Fatalf("series %+v %v", call, err)
	}
	text := ""
	for _, block := range call.Content {
		if content, ok := block.(*mcp.TextContent); ok {
			text = content.Text
		}
	}
	var payload map[string]any
	if text == "" || json.Unmarshal([]byte(text), &payload) != nil {
		t.Fatalf("text content is not the result payload: %q", text)
	}
	if payload["name"] != "Granted series" || call.StructuredContent == nil {
		t.Fatalf("structured %+v text %s", call.StructuredContent, text)
	}
}
