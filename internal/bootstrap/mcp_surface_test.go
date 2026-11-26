// These tests guard the edges of the MCP surface: retired tool names are neither listed nor
// answered, a stored internal_llm flag grants nothing, the model budget records stay, and
// initialization tells an agent how to work.

package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
)

// retiredTools are the tool names the surface no longer offers.
var retiredTools = []string{
	"library_search_sources", "library_get_source_search", "library_propose_match", "library_get_match_proposal",
	"library_get_enrichment_state", "library_enrich_metadata", "library_enrich_metadata_batch",
	"library_assess_match", "library_get_model_operation",
}

// surface is one production process with an administrator session and a served MCP endpoint.
type surface struct {
	t       *testing.T
	store   database.Store
	cfg     *config.Manager
	web     func(method, path, body string, status int) *httptest.ResponseRecorder
	server  *httptest.Server
	library int64
	series  int64
}

func openSurface(t *testing.T) *surface {
	t.Helper()
	ctx := context.Background()
	store, cfg := fixture(t)
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Surface", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Surface series", Path: lib.Path + "/surface"})
	if err != nil {
		t.Fatal(err)
	}
	process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(process.Close)
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	snapshot := cfg.Snapshot()
	snapshot.MCP.PublicURL = server.URL
	snapshot.MCP.AllowLANHTTP = true
	cfg.Replace(&snapshot)
	return &surface{t: t, store: store, cfg: cfg, web: sourceHTTP(t, process), server: server, library: lib.ID, series: series.ID}
}

// issue signs a credential for the surface library over the administration API.
func (s *surface) issue(name string, capabilities ...string) (id, token string) {
	s.t.Helper()
	body, _ := json.Marshal(map[string]any{"user_id": 1, "name": name, "library_ids": []int64{s.library}, "capabilities": capabilities})
	var issued struct{ ID, Token string }
	if err := json.Unmarshal(s.web("POST", "/mcp/credentials", string(body), http.StatusOK).Body.Bytes(), &issued); err != nil {
		s.t.Fatal(err)
	}
	return issued.ID, issued.Token
}

func (s *surface) connect(token string) *mcp.ClientSession {
	s.t.Helper()
	httpClient := *s.server.Client()
	httpClient.Transport = &bearerTransport{base: s.server.Client().Transport, token: token}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "surface", Version: "1"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: s.server.URL + "/mcp", HTTPClient: &httpClient}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(func() { _ = session.Close() })
	return session
}

// listed names the tools a session's listing offers, sorted.
func listed(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

func TestRetiredToolsAreNeitherListedNorAnswered(t *testing.T) {
	s := openSurface(t)
	_, token := s.issue("everything", "read", "metadata_write", "scan_control", "cleanup")
	session := s.connect(token)
	names := listed(t, session)
	for _, name := range retiredTools {
		if slices.Contains(names, name) {
			t.Errorf("%s is still listed", name)
		}
	}
	// A retired name is refused exactly like a name that never existed, whatever it is called with.
	refusal := func(name string, args any) string {
		t.Helper()
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err == nil {
			t.Fatalf("%s answered: %+v", name, result)
		}
		return strings.ReplaceAll(err.Error(), name, "<tool>")
	}
	unknown := refusal("library_never_existed", map[string]any{"series_id": s.series})
	for _, name := range retiredTools {
		if got := refusal(name, map[string]any{"series_id": s.series, "search_id": "x", "operation_id": "x"}); got != unknown {
			t.Errorf("%s refused as %q, an unknown tool as %q", name, got, unknown)
		}
	}
}

func TestInternalModelCapabilityGrantsNothing(t *testing.T) {
	s := openSurface(t)
	// Issuing and narrowing no longer accept it.
	s.web("POST", "/mcp/credentials", fmt.Sprintf(`{"user_id":1,"name":"model","library_ids":[%d],"capabilities":["read","internal_llm"]}`, s.library), http.StatusBadRequest)
	readerID, readerToken := s.issue("reader", "read")
	s.web("PATCH", "/mcp/credentials/"+readerID, fmt.Sprintf(`{"user_id":1,"name":"reader","library_ids":[%d],"capabilities":["read","internal_llm"]}`, s.library), http.StatusBadRequest)
	readTools := listed(t, s.connect(readerToken))

	// A credential signed before the retirement still carries the flag, and it grants nothing.
	oldID, oldToken := s.issue("signed before", "read")
	modelID, modelToken := s.issue("model only", "read")
	for id, stored := range map[string]string{oldID: `["read","internal_llm"]`, modelID: `["internal_llm"]`} {
		if _, err := s.store.DB().Exec(`UPDATE mcp_credentials SET capabilities=? WHERE id=?`, stored, id); err != nil {
			t.Fatal(err)
		}
	}
	if got := listed(t, s.connect(oldToken)); !slices.Equal(got, readTools) {
		t.Errorf("read+internal_llm lists %v, read alone %v", got, readTools)
	}
	if got := listed(t, s.connect(modelToken)); len(got) != 0 {
		t.Errorf("internal_llm alone lists %v", got)
	}
	// The owner no longer sees it on the credentials, and narrowing them keeps working.
	var credentials []struct {
		ID           string   `json:"id"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(s.web("GET", "/mcp/credentials", "", http.StatusOK).Body.Bytes(), &credentials); err != nil {
		t.Fatal(err)
	}
	for _, c := range credentials {
		if slices.Contains(c.Capabilities, "internal_llm") {
			t.Errorf("credential %s still shows internal_llm: %v", c.ID, c.Capabilities)
		}
	}
	s.web("PATCH", "/mcp/credentials/"+oldID, fmt.Sprintf(`{"user_id":1,"name":"signed before","library_ids":[%d],"capabilities":["read"]}`, s.library), http.StatusOK)
	if got := listed(t, s.connect(oldToken)); !slices.Equal(got, readTools) {
		t.Errorf("narrowed old credential lists %v, want %v", got, readTools)
	}
}

func TestInternalModelBudgetRecordsAreKept(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Budget", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Assessed", Path: lib.Path + "/assessed"})
	if err != nil {
		t.Fatal(err)
	}
	// Records the retired model assessment left behind before the upgrade.
	if _, err = store.DB().Exec(`INSERT INTO mcp_model_operations(id,user_id,credential_id,library_id,series_id,purpose,search_id,allowance,used,run_id,evidence) VALUES('model-budget:kept',1,'cred',?,?,'match_assessment','search',3,2,7,'{}');
		INSERT INTO mcp_model_calls(operation_id,item,outcome) VALUES('model-budget:kept',0,'consistent'),('model-budget:kept',1,'unknown')`, lib.ID, series.ID); err != nil {
		t.Fatal(err)
	}
	process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	process.Close()
	var allowance, used, calls int
	if err = store.DB().QueryRow(`SELECT allowance, used, (SELECT count(*) FROM mcp_model_calls WHERE operation_id=o.id) FROM mcp_model_operations o WHERE id='model-budget:kept'`).Scan(&allowance, &used, &calls); err != nil {
		t.Fatal(err)
	}
	if allowance != 3 || used != 2 || calls != 2 {
		t.Fatalf("budget record changed: allowance=%d used=%d calls=%d", allowance, used, calls)
	}
}

func TestInitializationExplainsTheMainLoopAndEveryListedTool(t *testing.T) {
	s := openSurface(t)
	for _, tc := range []struct {
		name         string
		capabilities []string
		loop         bool
	}{
		{name: "writer", capabilities: []string{"read", "metadata_write", "scan_control", "cleanup"}, loop: true},
		{name: "reader", capabilities: []string{"read"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, token := s.issue(tc.name, tc.capabilities...)
			session := s.connect(token)
			instructions := session.InitializeResult().Instructions
			for _, name := range listed(t, session) {
				if !strings.Contains(instructions, name) {
					t.Errorf("instructions do not mention listed tool %s:\n%s", name, instructions)
				}
			}
			// The main loop is claim, then submit, escalating what is hard, with no human to ask.
			claim := strings.Index(instructions, "library_claim_series")
			submit := strings.Index(instructions, "library_submit_verdicts")
			if !tc.loop {
				if claim >= 0 || submit >= 0 {
					t.Errorf("a credential without metadata_write is told to claim:\n%s", instructions)
				}
				return
			}
			if claim < 0 || submit < claim || !strings.Contains(instructions[submit:], "escalate") {
				t.Errorf("main loop missing or out of order:\n%s", instructions)
			}
			if !strings.Contains(strings.ToLower(instructions), "do not ask") {
				t.Errorf("instructions do not say to write without asking:\n%s", instructions)
			}
		})
	}
}
