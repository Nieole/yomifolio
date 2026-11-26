// These tests guard undo as every receipt relies on it: it reverses only what the operation
// changed and still holds, a failure is a durable receipt that changed nothing and leaks no storage
// detail, an unrecordable failure is an error rather than an invented receipt, and the current
// role and grant decide who may undo. The receipts come from an agent's identification.

package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/task"
)

// undoWorld is a library whose series an agent identified, each supplemented with the source's
// summary and entry link.
type undoWorld struct {
	t      *testing.T
	store  database.Store
	cfg    *config.Manager
	web    *identificationWeb
	agent  *mainLoopAgent
	series []int64
	ops    map[int64]string
}

func openUndoWorld(t *testing.T, directories ...string) *undoWorld {
	t.Helper()
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Undo")
	w := &undoWorld{t: t, store: store, cfg: cfg, ops: map[int64]string{}}
	cite := map[int64][]string{}
	for i, directory := range directories {
		id := library.series(directory, "", "01.cbz")
		w.series = append(w.series, id)
		cite[id] = []string{fmt.Sprintf("bangumi:%d", i+1)}
	}
	sources := &recordingSources{answer: numbered(func(n int) *metadata.SeriesMetadata {
		return &metadata.SeriesMetadata{Title: fmt.Sprintf("Title %d", n), Summary: "Space journey"}
	})}
	w.web = openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := w.web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	w.agent = w.web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	for id, verdict := range w.agent.identifyAll("identify", cite) {
		w.ops[id] = verdict.OperationID
	}
	return w
}

// undoReceipt mirrors the receipt fields these tests read.
type undoReceipt struct {
	ID         string `json:"id"`
	RequestKey string `json:"request_key"`
	ParentID   string `json:"parent_id"`
	Outcome    string `json:"outcome"`
	Applied    bool   `json:"applied"`
	Replayed   bool   `json:"replayed"`
	UndoneBy   string `json:"undone_by"`
	Actor      struct {
		UserID       int64  `json:"user_id"`
		CredentialID string `json:"credential_id"`
	} `json:"actor"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
	Fields []struct {
		Changed bool `json:"changed"`
	} `json:"fields"`
}

// undo asks the agent's undo tool to reverse one operation; a refusal comes back with IsError set.
func (w *undoWorld) undo(key, operation string) (undoReceipt, *mcp.CallToolResult) {
	w.t.Helper()
	var out undoReceipt
	result := w.agent.call("library_undo_metadata_operation", map[string]any{"request_key": key, "operation_id": operation}, &out)
	return out, result
}

func (w *undoWorld) undone(key, operation string) undoReceipt {
	w.t.Helper()
	out, result := w.undo(key, operation)
	if result.IsError {
		w.t.Fatalf("undo %s refused: %s", key, resultText(result))
	}
	return out
}

func (w *undoWorld) receipt(operation string) undoReceipt {
	w.t.Helper()
	var out undoReceipt
	if result := w.agent.call("library_get_metadata_operation", map[string]any{"operation_id": operation}, &out); result.IsError {
		w.t.Fatalf("receipt %s refused: %s", operation, resultText(result))
	}
	return out
}

func (w *undoWorld) exec(query string, args ...any) {
	w.t.Helper()
	if _, err := w.store.DB().Exec(query, args...); err != nil {
		w.t.Fatal(err)
	}
}

// links counts the series' source links as the owner's series page shows them.
func (w *undoWorld) links(series int64) int {
	w.t.Helper()
	var view struct {
		Links []json.RawMessage `json:"links"`
	}
	w.web.call("GET", fmt.Sprintf("/api/series/%d/context", series), http.StatusOK, &view)
	return len(view.Links)
}

// history is the owner's operation history over the administration API.
func (w *undoWorld) history() string {
	w.t.Helper()
	rec := w.web.raw("GET", "/api/mcp/operations", "")
	if rec.Code != http.StatusOK {
		w.t.Fatalf("history %d %s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func TestUndoReversesOnlyWhatItsOperationStillHolds(t *testing.T) {
	w := openUndoWorld(t, "Title 1", "Title 2")
	first, second := w.series[0], w.series[1]
	if got := w.web.series(first); got.Summary.String != "Space journey" || w.links(first) != 1 {
		t.Fatalf("identified series %+v links=%d", got, w.links(first))
	}
	// An edit of a field the operation did not change neither blocks undo nor is undone by it.
	w.exec(`UPDATE series SET publisher = 'Owner publisher' WHERE id = ?`, first)
	reverted := w.undone("undo-first", w.ops[first])
	if reverted.Outcome != "changed" || reverted.ParentID != w.ops[first] {
		t.Fatalf("undo %+v", reverted)
	}
	if got := w.web.series(first); got.Summary.Valid || got.Publisher.String != "Owner publisher" || w.links(first) != 0 {
		t.Fatalf("undone series %+v links=%d", got, w.links(first))
	}
	if again := w.undone("undo-first-again", w.ops[first]); again.Outcome != "no-change" {
		t.Fatalf("second undo %+v", again)
	}

	// A changed link set or a lock on a changed field blocks the whole undo.
	w.exec(`INSERT INTO series_links(series_id, name, url) VALUES(?, 'Owner', 'https://example.org/owner')`, second)
	if conflict := w.undone("undo-links-changed", w.ops[second]); conflict.Outcome != "conflict" {
		t.Fatalf("undo over a changed link set %+v", conflict)
	}
	w.exec(`DELETE FROM series_links WHERE series_id = ? AND name = 'Owner'`, second)
	w.exec(`UPDATE series SET locked_fields = 'summary' WHERE id = ?`, second)
	if conflict := w.undone("undo-locked", w.ops[second]); conflict.Outcome != "conflict" {
		t.Fatalf("undo over a locked field %+v", conflict)
	}
	if got := w.web.series(second); got.Summary.String != "Space journey" || w.links(second) != 1 {
		t.Fatalf("a blocked undo changed the series %+v", got)
	}
	w.exec(`UPDATE series SET locked_fields = '' WHERE id = ?`, second)

	// The owner can still undo once the agent's credential is revoked.
	if rec := w.web.raw("DELETE", "/api/mcp/credentials/"+w.agent.credential, ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke %d %s", rec.Code, rec.Body)
	}
	rec := w.web.raw("POST", "/api/mcp/operations/"+w.ops[second]+"/undo", `{"request_key":"owner-undo"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"outcome":"changed"`) {
		t.Fatalf("owner undo %d %s", rec.Code, rec.Body)
	}
	if got := w.web.series(second); got.Summary.Valid || w.links(second) != 0 {
		t.Fatalf("owner-undone series %+v", got)
	}
}

func TestUndoFailuresAreDurablePrivateReceipts(t *testing.T) {
	w := openUndoWorld(t, "Title 1", "Title 2")
	series, operation := w.series[0], w.ops[w.series[0]]
	// A link deletion fails after the summary was restored, so the write is genuinely partial.
	w.exec(fmt.Sprintf(`CREATE TRIGGER undo_test_failure BEFORE DELETE ON series_links WHEN OLD.series_id=%d BEGIN SELECT RAISE(ABORT,'private-trigger /private/path secret-value'); END`, series))
	failed := w.undone("undo-storage-failure", operation)
	if failed.Outcome != "failed" || failed.Applied || failed.ParentID != operation || failed.RequestKey != "undo-storage-failure" {
		t.Fatalf("failed undo %+v", failed)
	}
	if failed.Actor.UserID != 1 || failed.Actor.CredentialID != w.agent.credential {
		t.Fatalf("failed undo lost its actor %+v", failed.Actor)
	}
	encoded, _ := json.Marshal(failed)
	for _, private := range []string{"private-trigger", "/private/path", "secret-value", "RAISE", "DELETE"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("storage detail leaked: %s", encoded)
		}
	}
	if string(failed.Before) != string(failed.After) {
		t.Fatalf("failed receipt claims changes: %s", encoded)
	}
	for _, field := range failed.Fields {
		if field.Changed {
			t.Fatalf("failed field claims a change: %s", encoded)
		}
	}
	if got := w.web.series(series); got.Summary.String != "Space journey" || w.links(series) != 1 {
		t.Fatalf("partial undo survived %+v", got)
	}
	if original := w.receipt(operation); original.UndoneBy != "" {
		t.Fatalf("a failed undo marked the operation undone %+v", original)
	}
	if saved := w.receipt(failed.ID); saved.Outcome != "failed" || !strings.Contains(w.history(), failed.ID) {
		t.Fatalf("failed receipt not kept %+v", saved)
	}

	// Failing to record the failure is an error, not an invented receipt, and leaks nothing either.
	w.exec(`CREATE TRIGGER undo_journal_failure BEFORE INSERT ON metadata_operations WHEN NEW.request_key='undo-journal-unavailable' BEGIN SELECT RAISE(ABORT,'private-journal-error'); END`)
	_, unavailable := w.undo("undo-journal-unavailable", operation)
	if raw, _ := json.Marshal(unavailable); !unavailable.IsError || strings.Contains(string(raw), "private-journal-error") {
		t.Fatalf("journal failure claimed durability or leaked: %s", raw)
	}
	w.exec(`DROP TRIGGER undo_journal_failure`)
	// The current role decides: a demoted owner's credential may not undo, and leaves no receipt.
	w.exec(`UPDATE users SET role = 'user' WHERE id = 1`)
	if _, denied := w.undo("undo-denied-no-audit", operation); !denied.IsError {
		t.Fatal("a demoted account undid an operation")
	}
	w.exec(`UPDATE users SET role = 'admin' WHERE id = 1`)
	if history := w.history(); strings.Contains(history, "undo-journal-unavailable") || strings.Contains(history, "undo-denied-no-audit") {
		t.Fatalf("invented receipt: %s", history)
	}

	// With the fault gone, the failed request still replays as the failure and writes nothing.
	w.exec(`DROP TRIGGER undo_test_failure`)
	if replay := w.undone("undo-storage-failure", operation); replay.ID != failed.ID || replay.Outcome != "failed" || !replay.Replayed || replay.Applied {
		t.Fatalf("replay %+v", replay)
	}
	if got := w.web.series(series); got.Summary.String != "Space journey" {
		t.Fatalf("replay wrote metadata %+v", got)
	}
	if _, changed := w.undo("undo-storage-failure", w.ops[w.series[1]]); !changed.IsError {
		t.Fatal("the failed request key accepted another target")
	}
	// Replaying the failure concurrently cannot hide a successful undo under another key.
	type outcome struct {
		receipt undoReceipt
		result  *mcp.CallToolResult
	}
	results := make(chan outcome, 2)
	for _, key := range []string{"undo-storage-failure", "undo-after-failure"} {
		go func() {
			r, err := w.agent.session.CallTool(context.Background(), &mcp.CallToolParams{Name: "library_undo_metadata_operation", Arguments: map[string]any{"request_key": key, "operation_id": operation}})
			var out undoReceipt
			if err == nil && !r.IsError {
				raw, _ := json.Marshal(r.StructuredContent)
				_ = json.Unmarshal(raw, &out)
			}
			results <- outcome{out, r}
		}()
	}
	succeeded := ""
	for range 2 {
		got := <-results
		if got.result == nil || got.result.IsError {
			t.Fatalf("concurrent undo refused: %+v", got.result)
		}
		if got.receipt.RequestKey == "undo-storage-failure" {
			if got.receipt.ID != failed.ID || got.receipt.Outcome != "failed" || !got.receipt.Replayed {
				t.Fatalf("concurrent replay %+v", got.receipt)
			}
		} else {
			if got.receipt.Outcome != "changed" {
				t.Fatalf("concurrent undo %+v", got.receipt)
			}
			succeeded = got.receipt.ID
		}
	}
	if original := w.receipt(operation); succeeded == "" || original.UndoneBy != succeeded {
		t.Fatalf("the failure hid the successful undo %+v", original)
	}
	if got := w.web.series(series); got.Summary.Valid || w.links(series) != 0 {
		t.Fatalf("undone series %+v", got)
	}

	// The receipts outlive a restart of the production graph.
	w.web.process.Close()
	process, err := bootstrap.Open(context.Background(), w.store, w.cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(process.Close)
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	w.web.process, w.web.router = process, router
	var persisted undoReceipt
	w.web.call("GET", "/api/mcp/operations/"+failed.ID, http.StatusOK, &persisted)
	if persisted.Outcome != "failed" || persisted.Actor.CredentialID != w.agent.credential {
		t.Fatalf("failed receipt after restart %+v", persisted)
	}
}
