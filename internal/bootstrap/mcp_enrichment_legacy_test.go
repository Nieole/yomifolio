package bootstrap_test

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/database"
)

// The archived database was produced by 81a8807f's production bootstrap, before
// field provenance recorded the operation that wrote it. Undo of such a receipt
// must still reclaim exactly its own provenance after the startup upgrade.
func TestProductionLegacyAdjudicationUndoReclaimsOwnProvenance(t *testing.T) {
	ctx := t.Context()
	var legacy struct {
		Username   string           `json:"username"`
		Password   string           `json:"password"`
		SeriesID   int64            `json:"series_id"`
		Receipt    map[string]any   `json:"summary_receipt"`
		TagsOpID   string           `json:"tags_operation_id"`
		Provenance []map[string]any `json:"provenance"`
	}
	raw, err := os.ReadFile("testdata/enrichment-adjudication-legacy.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	store, cfg := fixture(t)
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	compressed, err := os.Open("testdata/enrichment-adjudication-legacy.db.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	archive, err := gzip.NewReader(compressed)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cfg.Snapshot().Database.Path, data, 0o600); err != nil {
		t.Fatal(err)
	}
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
	var cookie *http.Cookie
	csrf := ""
	web := func(method, path string, body any) []byte {
		t.Helper()
		encoded, _ := json.Marshal(body)
		req := httptest.NewRequest(method, "/api"+path, strings.NewReader(string(encoded)))
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
			req.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("web %s %s: %d %s", method, path, w.Code, w.Body)
		}
		if path == "/auth/login" {
			cookie = w.Result().Cookies()[0]
		}
		return w.Body.Bytes()
	}
	var login struct {
		CSRF string `json:"csrf_token"`
	}
	if err = json.Unmarshal(web("POST", "/auth/login", map[string]string{"username": legacy.Username, "password": legacy.Password}), &login); err != nil {
		t.Fatal(err)
	}
	csrf = login.CSRF
	provenance := func() map[string]map[string]any {
		t.Helper()
		var review struct {
			Provenance []map[string]any `json:"provenance"`
		}
		if e := json.Unmarshal(web("GET", fmt.Sprintf("/series/%d/metadata-review", legacy.SeriesID), nil), &review); e != nil {
			t.Fatal(e)
		}
		out := map[string]map[string]any{}
		for _, entry := range review.Provenance {
			out[entry["field_name"].(string)] = entry
		}
		return out
	}
	if got := provenance(); len(got) != 2 || !reflect.DeepEqual(got["summary"], legacy.Provenance[0]) || !reflect.DeepEqual(got["tags"], legacy.Provenance[1]) {
		t.Fatalf("upgrade changed legacy provenance: %v", got)
	}
	receiptID := legacy.Receipt["id"].(string)
	stored := map[string]any{}
	if err = json.Unmarshal(web("GET", "/mcp/operations/"+receiptID, nil), &stored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored, legacy.Receipt) {
		t.Fatalf("legacy receipt changed:\ngot  %v\nwant %v", stored, legacy.Receipt)
	}
	undo := map[string]any{}
	if err = json.Unmarshal(web("POST", "/mcp/operations/"+receiptID+"/undo", map[string]string{"request_key": "legacy-summary-undo"}), &undo); err != nil {
		t.Fatal(err)
	}
	if undo["outcome"] != "changed" || undo["after"].(map[string]any)["summary"] != nil {
		t.Fatal(undo)
	}
	// Only the undone operation's field origin goes; the later tags operation keeps its own.
	if got := provenance(); len(got) != 1 || !reflect.DeepEqual(got["tags"], legacy.Provenance[1]) {
		t.Fatalf("undo reclaimed the wrong provenance: %v", got)
	}
	undo = map[string]any{}
	if err = json.Unmarshal(web("POST", "/mcp/operations/"+legacy.TagsOpID+"/undo", map[string]string{"request_key": "legacy-tags-undo"}), &undo); err != nil {
		t.Fatal(err)
	}
	if undo["outcome"] != "changed" {
		t.Fatal(undo)
	}
	if got := provenance(); len(got) != 0 {
		t.Fatalf("undo left a false latest source: %v", got)
	}
}
