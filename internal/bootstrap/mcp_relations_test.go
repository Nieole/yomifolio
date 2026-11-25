package bootstrap_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/database"
)

// relationHarness drives the production graph as an owner over web HTTP and as an agent over MCP.
type relationHarness struct {
	t        *testing.T
	store    database.Store
	session  *mcp.ClientSession
	library  int64
	other    int64
	web      func(method, path string, body any, status int) []byte
	connect  func(user int64, capabilities []string, libraries ...int64) *mcp.ClientSession
	makeIn   func(library int64, name string) int64
	textOnly func(*mcp.CallToolResult) map[string]any
}

func newRelationHarness(t *testing.T) *relationHarness {
	t.Helper()
	ctx := t.Context()
	store, cfg := fixture(t)
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Granted", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Elsewhere", Path: t.TempDir(), ScanMode: "none"})
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
	server := httptest.NewTLSServer(router)
	t.Cleanup(server.Close)
	snapshot := cfg.Snapshot()
	snapshot.MCP.PublicURL = server.URL
	cfg.Replace(&snapshot)
	h := &relationHarness{t: t, store: store, library: lib.ID, other: other.ID}
	var cookie *http.Cookie
	csrf := ""
	h.web = func(method, path string, body any, status int) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, e := http.NewRequestWithContext(ctx, method, server.URL+"/api"+path, strings.NewReader(string(raw)))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
			req.Header.Set("X-CSRF-Token", csrf)
		}
		response, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		data, e := io.ReadAll(response.Body)
		if e != nil || response.StatusCode != status {
			t.Fatalf("web %s %s: %d %s %v", method, path, response.StatusCode, data, e)
		}
		if path == "/auth/setup" {
			cookie = response.Cookies()[0]
		}
		return data
	}
	var login struct {
		CSRF string `json:"csrf_token"`
	}
	_ = json.Unmarshal(h.web("POST", "/auth/setup", map[string]string{"username": "owner", "password": "correct horse battery staple"}, 200), &login)
	csrf = login.CSRF
	h.connect = func(user int64, capabilities []string, libraries ...int64) *mcp.ClientSession {
		t.Helper()
		var issued struct{ Token string }
		_ = json.Unmarshal(h.web("POST", "/mcp/credentials", map[string]any{"user_id": user, "name": "relations", "library_ids": libraries, "capabilities": capabilities}, 200), &issued)
		session, e := mcp.NewClient(&mcp.Implementation{Name: "relation-acceptance", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: server.Client().Transport, token: issued.Token}}}, nil)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = session.Close() })
		return session
	}
	h.session = h.connect(1, []string{"read", "metadata_write"}, lib.ID)
	h.makeIn = func(library int64, name string) int64 {
		t.Helper()
		row, e := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: library, Name: name, Path: fmt.Sprintf("/library-%d/%s", library, name)})
		if e != nil {
			t.Fatal(e)
		}
		return row.ID
	}
	// Clients that read only text content must recover the whole result from the JSON copy.
	h.textOnly = func(result *mcp.CallToolResult) map[string]any {
		t.Helper()
		for _, block := range result.Content {
			if text, ok := block.(*mcp.TextContent); ok {
				out := map[string]any{}
				if e := json.Unmarshal([]byte(text.Text), &out); e != nil {
					t.Fatalf("text copy is not JSON: %q", text.Text)
				}
				return out
			}
		}
		t.Fatalf("result has no text copy: %+v", result)
		return nil
	}
	return h
}

// call invokes a tool that must succeed and returns its text copy, which must match the structured result.
func (h *relationHarness) call(session *mcp.ClientSession, name string, input any) map[string]any {
	h.t.Helper()
	result, err := session.CallTool(h.t.Context(), &mcp.CallToolParams{Name: name, Arguments: input})
	if err != nil || result.IsError {
		h.t.Fatalf("%s: %+v %v", name, result, err)
	}
	structured, _ := json.Marshal(result.StructuredContent)
	text, _ := json.Marshal(h.textOnly(result))
	var a, b any
	_ = json.Unmarshal(structured, &a)
	_ = json.Unmarshal(text, &b)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		h.t.Fatalf("%s text copy differs from structured result:\n%s\n%s", name, text, structured)
	}
	return h.textOnly(result)
}

// refused invokes a tool that must be refused and returns the refusal text.
func (h *relationHarness) refused(session *mcp.ClientSession, name string, input any) string {
	h.t.Helper()
	result, err := session.CallTool(h.t.Context(), &mcp.CallToolParams{Name: name, Arguments: input})
	if err != nil {
		return err.Error()
	}
	if !result.IsError {
		h.t.Fatalf("%s was not refused: %+v", name, result.StructuredContent)
	}
	encoded, _ := json.Marshal(result.Content)
	return string(encoded)
}

type webRelation struct {
	ID             int64  `json:"id"`
	TargetSeriesID int64  `json:"target_series_id"`
	RelationType   string `json:"relation_type"`
}

func (h *relationHarness) relations(series int64) []webRelation {
	h.t.Helper()
	var out []webRelation
	if err := json.Unmarshal(h.web("GET", fmt.Sprintf("/series/%d/relations", series), nil, 200), &out); err != nil {
		h.t.Fatal(err)
	}
	return out
}

// franchiseSettled waits on the durable projection state, never on a fixed delay.
func (h *relationHarness) franchiseSettled(after int64) {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var state struct {
			Desired int64  `json:"desired_version"`
			Applied int64  `json:"applied_version"`
			Status  string `json:"status"`
		}
		_ = json.Unmarshal(h.web("GET", "/franchise-state", nil, 200), &state)
		if state.Desired > after && state.Applied == state.Desired && state.Status == "current" {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("franchise projection never caught up: %+v", state)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (h *relationHarness) franchiseVersion() int64 {
	h.t.Helper()
	var state struct {
		Desired int64 `json:"desired_version"`
	}
	_ = json.Unmarshal(h.web("GET", "/franchise-state", nil, 200), &state)
	return state.Desired
}

func (h *relationHarness) franchiseSizes() []float64 {
	h.t.Helper()
	var collections []struct {
		SourceType  string  `json:"source_type"`
		SeriesCount float64 `json:"series_count"`
	}
	_ = json.Unmarshal(h.web("GET", "/collection-views", nil, 200), &collections)
	sizes := []float64{}
	for _, c := range collections {
		if c.SourceType == "system_franchise" {
			sizes = append(sizes, c.SeriesCount)
		}
	}
	return sizes
}

func TestMCPAgentAddsFranchiseRelationWithReceipt(t *testing.T) {
	h := newRelationHarness(t)
	first, sequel := h.makeIn(h.library, "First"), h.makeIn(h.library, "Second")
	version := h.franchiseVersion()
	tools, err := h.session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !offersTool(tools, "library_add_series_relation") {
		t.Fatal("relation write not listed for metadata_write")
	}
	for _, tool := range tools.Tools {
		if tool.Name != "library_add_series_relation" {
			continue
		}
		schema, _ := json.Marshal(tool.InputSchema)
		// The kinds a weak model may choose come with the schema, matching the owner's relation editor.
		if !strings.Contains(string(schema), `"enum":["parent_story","sequel","prequel","spinoff","side_story","adaptation","remake","same_universe","alternative_version","alternate_story","crossover","one_shot","anthology","doujinshi"]`) {
			t.Fatalf("relation kinds missing from schema %s", schema)
		}
		if len(tool.Description) > 400 || !strings.Contains(tool.Description, "Example:") {
			t.Fatalf("description %d chars without example: %s", len(tool.Description), tool.Description)
		}
	}

	added := h.call(h.session, "library_add_series_relation", map[string]any{"request_key": "rel-1", "series_id": first, "target_series_id": sequel, "relation_type": "sequel", "reason": "Second volume afterword names it the sequel"})
	if added["outcome"] != "changed" || added["applied"] != true || added["kind"] != "relation" || added["reason"] != "Second volume afterword names it the sequel" {
		t.Fatalf("add receipt %+v", added)
	}
	relation, _ := added["relation"].(map[string]any)
	if relation == nil || relation["source_series_id"] != float64(first) || relation["target_series_id"] != float64(sequel) || relation["relation_type"] != "sequel" || relation["id"] == nil {
		t.Fatalf("receipt relation %+v", added["relation"])
	}
	if got := h.relations(first); len(got) != 1 || got[0].TargetSeriesID != sequel || got[0].RelationType != "sequel" {
		t.Fatalf("owner sees %+v", got)
	}
	if got := h.relations(sequel); len(got) != 1 || got[0].TargetSeriesID != first || got[0].RelationType != "prequel" {
		t.Fatalf("owner sees from the sequel %+v", got)
	}
	// The existing durable rebuild derives the franchise; it is not a second mechanism.
	h.franchiseSettled(version)
	if sizes := h.franchiseSizes(); len(sizes) != 1 || sizes[0] != 2 {
		t.Fatalf("franchise collections %v", sizes)
	}

	receipt := h.call(h.session, "library_get_metadata_operation", map[string]any{"operation_id": added["id"]})
	if receipt["id"] != added["id"] || receipt["kind"] != "relation" || receipt["reason"] != added["reason"] {
		t.Fatalf("queried receipt %+v", receipt)
	}
	listed := h.call(h.session, "library_list_metadata_operations", map[string]any{"series_id": first})
	operations, _ := listed["operations"].([]any)
	if len(operations) != 1 || operations[0].(map[string]any)["id"] != added["id"] {
		t.Fatalf("listed receipts %+v", listed)
	}
	replayed := h.call(h.session, "library_add_series_relation", map[string]any{"request_key": "rel-1", "series_id": first, "target_series_id": sequel, "relation_type": "sequel", "reason": "Second volume afterword names it the sequel"})
	if replayed["id"] != added["id"] || replayed["replayed"] != true || replayed["applied"] != false {
		t.Fatalf("replay %+v", replayed)
	}
	h.refused(h.session, "library_add_series_relation", map[string]any{"request_key": "rel-1", "series_id": first, "target_series_id": sequel, "relation_type": "spinoff", "reason": "changed intent under the same key"})
}

func TestMCPAgentUndoesOnlyItsOwnUnchangedRelation(t *testing.T) {
	h := newRelationHarness(t)
	add := func(key string, series, target int64, kind string) map[string]any {
		t.Helper()
		return h.call(h.session, "library_add_series_relation", map[string]any{"request_key": key, "series_id": series, "target_series_id": target, "relation_type": kind, "reason": "Publisher page lists the relation"})
	}
	undo := func(key string, operation any) map[string]any {
		t.Helper()
		return h.call(h.session, "library_undo_metadata_operation", map[string]any{"request_key": key, "operation_id": operation})
	}
	relationID := func(op map[string]any) int64 { return int64(op["relation"].(map[string]any)["id"].(float64)) }

	base, sequel := h.makeIn(h.library, "Base"), h.makeIn(h.library, "Sequel")
	added := add("add-sequel", base, sequel, "sequel")
	h.franchiseSettled(0)
	version := h.franchiseVersion()
	reverted := undo("undo-sequel", added["id"])
	if reverted["outcome"] != "changed" || reverted["applied"] != true || reverted["kind"] != "undo" || reverted["parent_id"] != added["id"] {
		t.Fatalf("undo %+v", reverted)
	}
	if got := h.relations(base); len(got) != 0 {
		t.Fatalf("undone relation still present %+v", got)
	}
	h.franchiseSettled(version)
	if sizes := h.franchiseSizes(); len(sizes) != 0 {
		t.Fatalf("franchise survived the undo %v", sizes)
	}
	if original := h.call(h.session, "library_get_metadata_operation", map[string]any{"operation_id": added["id"]}); original["undone_by"] != reverted["id"] {
		t.Fatalf("original receipt %+v", original)
	}
	if again := undo("undo-sequel-again", added["id"]); again["outcome"] != "no-change" || again["applied"] != false {
		t.Fatalf("second undo %+v", again)
	}

	// An owner's later edit makes the relation no longer the one this operation added.
	spinoff := h.makeIn(h.library, "Spinoff")
	retyped := add("add-retyped", base, spinoff, "sequel")
	h.web("PUT", fmt.Sprintf("/relations/%d", relationID(retyped)), map[string]string{"relation_type": "spinoff"}, 200)
	if conflict := undo("undo-retyped", retyped["id"]); conflict["outcome"] != "conflict" || conflict["applied"] != false {
		t.Fatalf("undo over an edit %+v", conflict)
	}
	if got := h.relations(spinoff); len(got) != 1 || got[0].RelationType != "parent_story" {
		t.Fatalf("owner edit lost %+v", got)
	}
	// Deleting and recreating the same pair is a different relation, which undo leaves alone.
	remade := h.makeIn(h.library, "Remade")
	recreated := add("add-recreated", base, remade, "remake")
	h.web("DELETE", fmt.Sprintf("/relations/%d", relationID(recreated)), nil, 200)
	h.web("POST", fmt.Sprintf("/series/%d/relations", base), map[string]any{"target_series_id": remade, "relation_type": "remake"}, 201)
	if conflict := undo("undo-recreated", recreated["id"]); conflict["outcome"] != "conflict" {
		t.Fatalf("undo over a recreated relation %+v", conflict)
	}
	if got := h.relations(remade); len(got) != 1 {
		t.Fatalf("owner relation removed %+v", got)
	}

	// A relation the owner made is never edited or removed through the agent's receipts.
	prequel := h.makeIn(h.library, "Prequel")
	h.web("POST", fmt.Sprintf("/series/%d/relations", prequel), map[string]any{"target_series_id": base, "relation_type": "sequel"}, 201)
	existing := add("add-existing", base, prequel, "side_story")
	relation := existing["relation"].(map[string]any)
	if existing["outcome"] != "no-change" || existing["applied"] != false || relation["source_series_id"] != float64(prequel) || relation["relation_type"] != "sequel" {
		t.Fatalf("existing relation receipt %+v", existing)
	}
	if kept := undo("undo-existing", existing["id"]); kept["outcome"] != "no-change" {
		t.Fatalf("undo of an existing relation %+v", kept)
	}
	if got := h.relations(prequel); len(got) != 1 || got[0].TargetSeriesID != base || got[0].RelationType != "sequel" {
		t.Fatalf("owner relation changed %+v", got)
	}
}

func TestMCPRelationWritesStayInsideTheCredentialScope(t *testing.T) {
	h := newRelationHarness(t)
	inside, also := h.makeIn(h.library, "Inside"), h.makeIn(h.library, "Also inside")
	outside := h.makeIn(h.other, "Outside")
	request := func(key string, series, target int64, kind, reason string) map[string]any {
		return map[string]any{"request_key": key, "series_id": series, "target_series_id": target, "relation_type": kind, "reason": reason}
	}
	toOutside := h.refused(h.session, "library_add_series_relation", request("out-target", inside, outside, "sequel", "Same author"))
	fromOutside := h.refused(h.session, "library_add_series_relation", request("out-source", outside, inside, "sequel", "Same author"))
	unknown := h.refused(h.session, "library_add_series_relation", request("unknown", inside, 999999, "sequel", "Same author"))
	if toOutside != unknown || fromOutside != unknown {
		t.Fatalf("refusals disclose existence:\n%s\n%s\n%s", toOutside, fromOutside, unknown)
	}
	for name, input := range map[string]map[string]any{
		"self":      request("self", inside, inside, "sequel", "Same series"),
		"kind":      request("kind", inside, also, "rival", "Not a franchise relation kind"),
		"no reason": request("no-reason", inside, also, "sequel", "  "),
		"no key":    request("", inside, also, "sequel", "Same author"),
	} {
		if refusal := h.refused(h.session, "library_add_series_relation", input); refusal == "" {
			t.Fatalf("%s accepted", name)
		}
	}
	if got := h.relations(inside); len(got) != 0 {
		t.Fatalf("refused writes left relations %+v", got)
	}
	if got := h.relations(outside); len(got) != 0 {
		t.Fatalf("outside series changed %+v", got)
	}

	// Read alone neither lists nor performs the write.
	reader := h.connect(1, []string{"read"}, h.library)
	listed, err := reader.ListTools(t.Context(), nil)
	if err != nil || offersTool(listed, "library_add_series_relation") {
		t.Fatalf("read-only listing %v", err)
	}
	h.refused(reader, "library_add_series_relation", request("read-only", inside, also, "sequel", "Same author"))

	// A regular account's credential reads but never writes, whatever it was issued with.
	account, err := h.store.CreateUser(t.Context(), database.CreateUserParams{Username: "reader", PasswordHash: "unused", Role: "user"})
	if err != nil {
		t.Fatal(err)
	}
	regular := h.connect(account.ID, []string{"read", "metadata_write"}, h.library)
	h.call(regular, "library_get_series", map[string]any{"series_id": inside})
	h.refused(regular, "library_add_series_relation", request("regular", inside, also, "sequel", "Same author"))
	if got := h.relations(inside); len(got) != 0 {
		t.Fatalf("unauthorized writers left relations %+v", got)
	}

	// Both ends must be in scope, so undo needs both libraries too.
	wide := h.connect(1, []string{"read", "metadata_write"}, h.library, h.other)
	crossing := h.call(wide, "library_add_series_relation", request("crossing", inside, outside, "crossover", "Crossover issue names both"))
	if crossing["outcome"] != "changed" {
		t.Fatalf("cross-library relation within scope %+v", crossing)
	}
	h.refused(h.session, "library_undo_metadata_operation", map[string]any{"request_key": "narrow-undo", "operation_id": crossing["id"]})
	if got := h.relations(outside); len(got) != 1 {
		t.Fatalf("narrow credential removed a relation %+v", got)
	}
	if undone := h.call(wide, "library_undo_metadata_operation", map[string]any{"request_key": "wide-undo", "operation_id": crossing["id"]}); undone["outcome"] != "changed" {
		t.Fatalf("wide undo %+v", undone)
	}
}
