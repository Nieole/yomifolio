package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"yomifolio/internal/database"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/bootstrap"
)

func TestMCPDelegatedSeriesLifecycle(t *testing.T) {
	for _, tls := range []bool{false, true} {
		t.Run(fmt.Sprintf("tls=%t", tls), func(t *testing.T) { testMCPDelegatedSeriesLifecycle(t, tls) })
	}
}

func testMCPDelegatedSeriesLifecycle(t *testing.T, tls bool) {
	store, cfg := fixture(t)
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if process != nil {
			process.Close()
		}
	}()
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	var cookie *http.Cookie
	var csrf string
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if cookie != nil {
			r.AddCookie(cookie)
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	setup := request("POST", "/api/auth/setup", `{"username":"admin","password":"password123"}`)
	if setup.Code != 200 {
		t.Fatalf("setup %d %s", setup.Code, setup.Body)
	}
	cookie = setup.Result().Cookies()[0]
	var auth struct {
		CSRF string `json:"csrf_token"`
	}
	_ = json.Unmarshal(setup.Body.Bytes(), &auth)
	csrf = auth.CSRF
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Allowed", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Other", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{LibraryID: lib.ID, Name: "Allowed series", Path: lib.Path + "/allowed"})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{LibraryID: other.ID, Name: "Hidden series", Path: other.Path + "/hidden"})
	if err != nil {
		t.Fatal(err)
	}
	// Issuance is a separate administration action, not a browser session.
	issued := request("POST", "/api/mcp/credentials", fmt.Sprintf(`{"user_id":1,"name":"catalogue","library_ids":[%d],"capabilities":["read","metadata_write","scan_control","internal_llm","cleanup"]}`, lib.ID))
	if issued.Code != 200 {
		t.Fatalf("issue %d %s", issued.Code, issued.Body)
	}
	var credential struct {
		Token string `json:"token"`
		ID    string `json:"id"`
	}
	_ = json.Unmarshal(issued.Body.Bytes(), &credential)
	if credential.Token == "" {
		t.Fatal("missing one-time credential")
	}
	listed := request("GET", "/api/mcp/credentials", "")
	if listed.Code != 200 || strings.Contains(listed.Body.String(), credential.Token) {
		t.Fatalf("list leaks credential: %s", listed.Body)
	}
	// Reopen the production graph: credentials and browser sessions are durable,
	// while raw credential secrets are never part of the list projection.
	process.Close()
	process, err = bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	router = chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	server := httptest.NewUnstartedServer(router)
	if tls {
		server.StartTLS()
	} else {
		server.Start()
	}
	defer server.Close()
	snapshot := cfg.Snapshot()
	snapshot.MCP.PublicURL = server.URL
	snapshot.MCP.AllowLANHTTP = !tls
	cfg.Replace(&snapshot)
	client := mcp.NewClient(&mcp.Implementation{Name: "acceptance", Version: "1"}, nil)
	httpClient := server.Client()
	httpClient.Transport = &bearerTransport{base: httpClient.Transport, token: credential.Token}
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: httpClient}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if got := session.InitializeResult().ProtocolVersion; got != "2025-11-25" {
		t.Fatalf("negotiated protocol %s", got)
	}
	tools, err := session.ListTools(context.Background(), nil)
	assertMCPToolDiscovery(t, tools, err)
	assertDiscoveryPage(t, session, lib.ID, series.ID)
	assertLocalClues(t, session, store, lib.ID, series.ID, other.ID, hidden.ID, lib.Path)
	allowed, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "library_get_series", Arguments: map[string]any{"series_id": series.ID}})
	if err != nil || allowed.IsError {
		t.Fatalf("authorized series %+v %v", allowed, err)
	}
	encoded, _ := json.Marshal(allowed)
	if !strings.Contains(string(encoded), "Allowed series") || strings.Contains(string(encoded), lib.Path) {
		t.Fatalf("unsafe projection %s", encoded)
	}
	outside, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "library_get_series", Arguments: map[string]any{"series_id": hidden.ID}})
	if err != nil || !outside.IsError {
		t.Fatalf("cross-library lookup %+v %v", outside, err)
	}
	denied, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "library_get_series", Arguments: map[string]any{"series_id": 999}})
	if err != nil || !denied.IsError {
		t.Fatalf("guessed id %+v %v", denied, err)
	}
	expanded := request("PATCH", "/api/mcp/credentials/"+credential.ID, fmt.Sprintf(`{"user_id":1,"name":"catalogue","library_ids":[%d,%d],"capabilities":["read"]}`, lib.ID, other.ID))
	if expanded.Code != 400 {
		t.Fatalf("expansion accepted: %d %s", expanded.Code, expanded.Body)
	}
	narrowed := request("PATCH", "/api/mcp/credentials/"+credential.ID, fmt.Sprintf(`{"user_id":1,"name":"catalogue","library_ids":[%d],"capabilities":["cleanup"]}`, lib.ID))
	if narrowed.Code != 200 {
		t.Fatalf("narrow %d %s", narrowed.Code, narrowed.Body)
	}
	denied, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "library_get_series", Arguments: map[string]any{"series_id": series.ID}})
	if err != nil || !denied.IsError {
		t.Fatalf("removed read capability still usable %+v %v", denied, err)
	}
	// Cookie catalogue reading remains unaffected by MCP restrictions.
	if web := request("GET", fmt.Sprintf("/api/series/%d", hidden.ID), ""); web.Code != 200 {
		t.Fatalf("web catalogue changed %d %s", web.Code, web.Body)
	}
	revoked := request("DELETE", "/api/mcp/credentials/"+credential.ID, "")
	if revoked.Code != 200 {
		t.Fatalf("revoke %d %s", revoked.Code, revoked.Body)
	}
	if _, err := session.ListTools(context.Background(), nil); err == nil {
		t.Fatal("revoked credential still accepted")
	}
}

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (b *bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	base := b.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

func TestMCPRejectsUnsafeTransport(t *testing.T) {
	store, cfg := fixture(t)
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	for _, tc := range []struct {
		name, url, peer, host, proto string
		lan                          bool
		origins                      []string
		proxies                      []string
		want                         int
	}{
		{name: "plaintext disabled", url: "http://127.0.0.1", peer: "127.0.0.1:123", host: "127.0.0.1", want: 403},
		{name: "explicit loopback", url: "http://127.0.0.1", peer: "127.0.0.1:123", host: "127.0.0.1", lan: true, want: 401},
		{name: "public peer", url: "http://127.0.0.1", peer: "8.8.8.8:123", host: "127.0.0.1", lan: true, want: 403},
		{name: "public host", url: "http://example.com", peer: "127.0.0.1:123", host: "example.com", lan: true, want: 403},
		{name: "host rebinding", url: "http://127.0.0.1", peer: "127.0.0.1:123", host: "evil.example", lan: true, want: 403},
		{name: "origin", url: "http://127.0.0.1", peer: "127.0.0.1:123", host: "127.0.0.1", lan: true, origins: []string{"https://evil.example"}, want: 403},
		{name: "duplicate origin", url: "http://127.0.0.1", peer: "127.0.0.1:123", host: "127.0.0.1", lan: true, origins: []string{"http://127.0.0.1", "https://evil.example"}, want: 403},
		{name: "forged tls", url: "https://example.com", peer: "127.0.0.1:123", host: "example.com", proto: "https", want: 403},
		{name: "trusted tls", url: "https://example.com", peer: "127.0.0.1:123", host: "example.com", proto: "https", proxies: []string{"127.0.0.1/32"}, want: 401},
		{name: "lan proxy missing client", url: "http://127.0.0.1", peer: "127.0.0.1:123", host: "127.0.0.1", lan: true, proxies: []string{"127.0.0.1/32"}, want: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := cfg.Snapshot()
			snapshot.MCP.PublicURL = tc.url
			snapshot.MCP.AllowLANHTTP = tc.lan
			snapshot.Server.TrustedProxies = tc.proxies
			cfg.Replace(&snapshot)
			r := httptest.NewRequest("POST", "http://"+tc.host+"/mcp", strings.NewReader(`{}`))
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-Proto", tc.proto)
			for _, origin := range tc.origins {
				r.Header.Add("Origin", origin)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d want %d: %s", w.Code, tc.want, w.Body)
			}
		})
	}
}

// assertMCPToolDiscovery verifies the complete public registration set, not just a count.
func assertMCPToolDiscovery(t *testing.T, result *mcp.ListToolsResult, err error) {
	t.Helper()
	if err != nil || result == nil {
		t.Fatalf("tools %+v %v", result, err)
	}
	expected := map[string]bool{
		"library_get_series": true, "library_find_series": true,
		"library_list_books": true, "library_get_book_clues": true,
		"library_search_sources": false, "library_get_source_search": true,
		"library_propose_match": false, "library_get_match_proposal": true,
		"library_start_scan": false, "library_get_run": true,
		"library_list_runs": true, "library_wait_run": true, "library_cancel_run": false,
	}
	seen := map[string]bool{}
	for _, tool := range result.Tools {
		readOnly, ok := expected[tool.Name]
		if !ok || seen[tool.Name] {
			t.Fatalf("unexpected or duplicate tool %s", tool.Name)
		}
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != readOnly {
			t.Fatalf("incorrect read-only annotation for %s: %+v", tool.Name, tool.Annotations)
		}
		seen[tool.Name] = true
	}
	for name := range expected {
		if !seen[name] {
			t.Errorf("missing tool %s", name)
		}
	}
}
