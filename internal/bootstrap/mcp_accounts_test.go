package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/database"
)

func TestMCPAccountPasswordAndManagementIsolation(t *testing.T) {
	store, cfg := fixture(t)
	ctx := context.Background()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Comics", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Comics", Path: lib.Path + "/series"})
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
	type browser struct {
		cookie *http.Cookie
		csrf   string
	}
	request := func(method, path, body string, b browser) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if b.cookie != nil {
			r.AddCookie(b.cookie)
			r.Header.Set("X-CSRF-Token", b.csrf)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	session := func(w *httptest.ResponseRecorder) browser {
		t.Helper()
		if w.Code != 200 {
			t.Fatalf("browser %d %s", w.Code, w.Body)
		}
		var body struct {
			CSRF string `json:"csrf_token"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return browser{w.Result().Cookies()[0], body.CSRF}
	}
	admin := session(request("POST", "/api/auth/setup", `{"username":"owner","password":"password123"}`, browser{}))
	create := request("POST", "/api/users", `{"username":"reader","password":"initialpass"}`, admin)
	if create.Code != 200 {
		t.Fatal(create.Body)
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	reader := session(request("POST", "/api/auth/login", `{"username":"reader","password":"initialpass"}`, browser{}))
	reader = session(request("POST", "/api/auth/change-password", `{"current_password":"initialpass","new_password":"readerpass"}`, reader))
	issue := func() struct{ ID, Token string } {
		t.Helper()
		w := request("POST", "/api/mcp/credentials", fmt.Sprintf(`{"user_id":%d,"name":"Reader agent","library_ids":[%d],"capabilities":["read","metadata_write","scan_control","internal_llm","cleanup"]}`, user.ID, lib.ID), admin)
		if w.Code != 200 {
			t.Fatalf("issue %d %s", w.Code, w.Body)
		}
		var out struct{ ID, Token string }
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	unknown := request("POST", "/api/mcp/credentials", `{"user_id":99999,"name":"Unknown","library_ids":[],"capabilities":["read"]}`, admin)
	if unknown.Code != 400 {
		t.Fatalf("nonexistent user accepted %d", unknown.Code)
	}
	credential := issue()
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		path := "/api/mcp/credentials"
		body := "{}"
		if method == "PATCH" || method == "DELETE" {
			path += "/" + credential.ID
		}
		w := request(method, path, body, reader)
		if w.Code != 403 {
			t.Fatalf("regular management %s: %d", method, w.Code)
		}
	}
	// The agent's Bearer token cannot authenticate to the browser management tree.
	r := httptest.NewRequest("DELETE", "/api/mcp/credentials/"+credential.ID, nil)
	r.Header.Set("Authorization", "Bearer "+credential.Token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("agent self-management %d", w.Code)
	}
	server := httptest.NewTLSServer(router)
	defer server.Close()
	snapshot := cfg.Snapshot()
	snapshot.MCP.PublicURL = server.URL
	cfg.Replace(&snapshot)
	connect := func(token string) (*mcp.ClientSession, error) {
		httpClient := &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: token}}
		return mcp.NewClient(&mcp.Implementation{Name: "account-acceptance", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: httpClient}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	}
	if s, err := connect("invalid"); err == nil {
		s.Close()
		t.Fatal("invalid token accepted")
	}
	s, err := connect(credential.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result, err := s.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_series", Arguments: map[string]any{"series_id": series.ID}})
	if err != nil || result.IsError {
		t.Fatalf("regular read %+v %v", result, err)
	}
	// Scan tools are discoverable but cannot elevate a regular account.
	deniedScan, err := s.CallTool(ctx, &mcp.CallToolParams{Name: "library_start_scan", Arguments: map[string]any{"library_id": lib.ID}})
	if err != nil || !deniedScan.IsError {
		t.Fatalf("regular scan %+v %v", deniedScan, err)
	}
	tools, err := s.ListTools(ctx, nil)
	assertMCPToolDiscovery(t, tools, err)
	// An unsuccessful password change must leave both browser and delegated access intact.
	failed := request("POST", "/api/auth/change-password", `{"current_password":"wrong-password","new_password":"replacement"}`, reader)
	if failed.Code == 200 {
		t.Fatal("wrong password accepted")
	}
	if _, err := s.ListTools(ctx, nil); err != nil {
		t.Fatalf("failed password change revoked credential: %v", err)
	}
	// Force delegated revocation to fail inside the password transaction. The
	// externally visible old password, browser session and MCP access must survive.
	if _, err := store.DB().ExecContext(ctx, `CREATE TRIGGER fail_mcp_revocation BEFORE UPDATE ON mcp_credentials BEGIN SELECT RAISE(ABORT, 'controlled revocation failure'); END`); err != nil {
		t.Fatal(err)
	}
	rollback := request("POST", "/api/auth/change-password", `{"current_password":"readerpass","new_password":"must-not-commit"}`, reader)
	if rollback.Code == 200 {
		t.Fatal("revocation failure committed password")
	}
	if _, err := s.ListTools(ctx, nil); err != nil {
		t.Fatalf("rollback lost delegated access: %v", err)
	}
	if unchanged := request("GET", "/api/auth/me", "", reader); unchanged.Code != 200 {
		t.Fatalf("rollback lost browser session: %d", unchanged.Code)
	}
	if _, err := store.DB().ExecContext(ctx, `DROP TRIGGER fail_mcp_revocation`); err != nil {
		t.Fatal(err)
	}
	changed := request("POST", "/api/auth/change-password", `{"current_password":"readerpass","new_password":"replacement"}`, reader)
	if changed.Code != 200 {
		t.Fatalf("change %d %s", changed.Code, changed.Body)
	}
	if _, err := s.ListTools(ctx, nil); err == nil {
		t.Fatal("password change retained MCP credential")
	}
	credential = issue()
	s2, err := connect(credential.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	reset := request("POST", fmt.Sprintf("/api/users/%d/password", user.ID), `{"password":"reset-password"}`, admin)
	if reset.Code != 200 {
		t.Fatalf("reset %d %s", reset.Code, reset.Body)
	}
	if _, err := s2.ListTools(ctx, nil); err == nil {
		t.Fatal("password reset retained MCP credential")
	}
	// A credential issued to an account requiring a password change remains unusable.
	pending := issue()
	if session, err := connect(pending.Token); err == nil {
		session.Close()
		t.Fatal("pending password account authenticated")
	}
}
