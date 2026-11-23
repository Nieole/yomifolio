package bootstrap_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"yomifolio/internal/config"
	"yomifolio/internal/covermaterial"
	"yomifolio/internal/database"
	"yomifolio/internal/task"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func verifyCleanup(t *testing.T, session *mcp.ClientSession, call func(string, any) map[string]any, web func(string, string, any, int) []byte, op map[string]any) {
	t.Helper()
	input := map[string]any{"request_key": "cleanup-13-first", "operation_id": op["id"]}
	denied, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "library_cleanup_metadata_operation", Arguments: input})
	if err != nil || !denied.IsError {
		t.Fatalf("metadata write must not grant cleanup: %+v %v", denied, err)
	}
	var result map[string]any
	raw := web("POST", fmt.Sprintf("/mcp/operations/%s/cleanup", op["id"]), input, 200)
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result["outcome"] != "complete" {
		t.Fatal(string(raw))
	}
	audit := call("library_get_metadata_operation", map[string]any{"operation_id": op["id"]})
	if audit["undo_unavailable"] != true || audit["id"] != op["id"] {
		t.Fatal(audit)
	}
	undo := call("library_undo_metadata_operation", map[string]any{"operation_id": op["id"], "request_key": "cleanup-13-undo"})
	if undo["outcome"] != "unavailable" || undo["applied"] != false {
		t.Fatal(undo)
	}
}

func verifyCoverCleanup(t *testing.T, store database.Store, cfg *config.Manager, engine *task.Engine, router http.Handler, call func(string, any) map[string]any, web func(string, string, any, int) []byte, original, undo map[string]any, book int64, cleanupGate *cleanupProgressGate) func(func(string, string, any, int) []byte) {
	t.Helper()
	ctx := t.Context()
	var credential struct{ ID, Token string }
	_ = json.Unmarshal(web("POST", "/mcp/credentials", map[string]any{"user_id": 1, "name": "cleanup-only", "library_ids": []any{original["library_id"]}, "capabilities": []string{"cleanup"}}, 200), &credential)
	server := httptest.NewServer(router)
	defer server.Close()
	previous := cfg.Snapshot()
	defer cfg.Replace(&previous)
	snapshot := previous
	snapshot.MCP.PublicURL = server.URL
	snapshot.MCP.AllowLANHTTP = true
	cfg.Replace(&snapshot)
	client, e := mcp.NewClient(&mcp.Implementation{Name: "cleanup13", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: http.DefaultTransport, token: credential.Token}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	cleanCall := func(name string, args any) map[string]any {
		t.Helper()
		result, e := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil || result.IsError {
			t.Fatalf("%s: %+v %v", name, result, e)
		}
		raw, _ := json.Marshal(result.StructuredContent)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return out
	}

	if _, e = store.DB().Exec(`UPDATE users SET role='regular' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	deniedRole, e := client.CallTool(ctx, &mcp.CallToolParams{Name: "library_cleanup_metadata_operation", Arguments: map[string]any{"request_key": "cleanup13-role-denied", "operation_id": original["id"]}})
	if e != nil || !deniedRole.IsError {
		t.Fatal(deniedRole, e)
	}
	web("POST", "/mcp/operations/"+original["id"].(string)+"/cleanup", map[string]any{"request_key": "cleanup13-web-role-denied"}, 403)
	if _, e = store.DB().Exec(`UPDATE users SET role='admin' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	var otherLibrary int64
	if e = store.DB().QueryRow(`SELECT id FROM libraries WHERE id<>? LIMIT 1`, original["library_id"]).Scan(&otherLibrary); e != nil {
		t.Fatal(e)
	}
	foreignSeries, e := store.CreateSeries(ctx, database.CreateSeriesParams{Name: "Foreign cleanup scope", Path: filepath.Join(t.TempDir(), "foreign"), LibraryID: otherLibrary})
	if e != nil {
		t.Fatal(e)
	}
	foreignFixture := map[string]any{"version": 1, "id": "cleanup13-foreign-fixture", "actor": map[string]any{"user_id": 1, "credential_id": ""}, "library_id": otherLibrary, "series_id": foreignSeries.ID, "kind": "enrich", "outcome": "changed", "fields": []any{}, "before": map[string]any{}, "after": map[string]any{}}
	foreignRaw, _ := json.Marshal(foreignFixture)
	if _, e = store.DB().Exec(`INSERT INTO metadata_operations(id,user_id,credential_id,request_key,request_hash,series_id,library_id,outcome,payload) VALUES('cleanup13-foreign-fixture',1,'','cleanup13-foreign','fixture',?,?,'changed',?)`, foreignSeries.ID, otherLibrary, string(foreignRaw)); e != nil {
		t.Fatal(e)
	}
	deniedForeign, e := client.CallTool(ctx, &mcp.CallToolParams{Name: "library_cleanup_metadata_operation", Arguments: map[string]any{"request_key": "cleanup13-foreign-denied", "operation_id": "cleanup13-foreign-fixture"}})
	if e != nil || !deniedForeign.IsError {
		t.Fatal(deniedForeign, e)
	}
	var foreignAudit map[string]any
	_ = json.Unmarshal(web("GET", "/mcp/operations/cleanup13-foreign-fixture", nil, 200), &foreignAudit)
	if foreignAudit["undo_unavailable"] == true {
		t.Fatal(foreignAudit)
	}
	if _, e = engine.PruneHistory(ctx, task.RetentionPolicy{TerminalAge: time.Nanosecond}); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{original["old_cover_material"].(string), original["new_cover_material"].(string)} {
		web("GET", "/thumbnails/"+key, nil, 200)
	}
	protected := cleanCall("library_cleanup_metadata_operation", map[string]any{"request_key": "cover13-original", "operation_id": original["id"]})
	for _, m := range protected["materials"].([]any) {
		if m.(map[string]any)["outcome"] != "protected" {
			t.Fatal(protected)
		}
	}
	// Both immutable images are retained by the reversal receipt, including the
	// original after-image which is no longer the current catalogue cover.
	for _, key := range []string{original["old_cover_material"].(string), original["new_cover_material"].(string)} {
		web("GET", "/thumbnails/"+key, nil, 200)
	}
	cleanupInput := map[string]any{"request_key": "cover13-undo", "operation_id": undo["id"]}
	result := cleanCall("library_cleanup_metadata_operation", cleanupInput)
	if result["outcome"] != "complete" {
		t.Fatal(result)
	}
	newKey := original["new_cover_material"].(string)
	oldKey := original["old_cover_material"].(string)
	if _, e = os.Stat(covermaterial.FilePath(config.ThumbnailDir(cfg.Snapshot()), newKey)); !os.IsNotExist(e) {
		t.Fatalf("unreferenced material not reclaimed: %v", e)
	}
	web("GET", "/thumbnails/"+newKey, nil, 404)
	im, _, e := image.Decode(bytes.NewReader(web("GET", fmt.Sprintf("/covers/%d", book), nil, 200)))
	if e != nil {
		t.Fatal(e)
	}
	r, _, b, _ := im.At(20, 20).RGBA()
	if r <= b {
		t.Fatal("cleanup damaged current red cover")
	}
	web("GET", "/thumbnails/"+oldKey, nil, 200)
	replay := cleanCall("library_cleanup_metadata_operation", cleanupInput)
	if replay["id"] != result["id"] || replay["replayed"] != true {
		t.Fatal(replay)
	}
	changedIntent, e := client.CallTool(ctx, &mcp.CallToolParams{Name: "library_cleanup_metadata_operation", Arguments: map[string]any{"request_key": "cover13-undo", "operation_id": original["id"]}})
	if e != nil || !changedIntent.IsError {
		t.Fatal("same cleanup key accepted a different target", changedIntent, e)
	}
	// A retired key can never be republished by any writer, including normal cover
	// changes. This DB fault injection exercises the shared publication fence.
	if _, e = store.DB().Exec(`UPDATE books SET cover_path=? WHERE id=?`, newKey, book); e == nil {
		t.Fatal("retired cover resurrected")
	}
	// Cleanup authority alone cannot perform enrichment, nor query another scope.
	denied, e := client.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_enrichment_state", Arguments: map[string]any{"series_id": original["series_id"]}})
	if e != nil || !denied.IsError {
		t.Fatal(denied, e)
	}
	var foreign string
	if e = store.DB().QueryRow(`SELECT id FROM metadata_operations WHERE library_id<>? LIMIT 1`, original["library_id"]).Scan(&foreign); e == nil {
		denied, e = client.CallTool(ctx, &mcp.CallToolParams{Name: "library_cleanup_metadata_operation", Arguments: map[string]any{"request_key": "guess", "operation_id": foreign}})
		if e != nil || !denied.IsError {
			t.Fatal(denied, e)
		}
	}

	// Real file failure: an immutable file replaced by a nonempty directory is not
	// recursively removed. Cleanup loses undo but durably reports partial reclamation.
	makeCover := func(key string) map[string]any {
		cfg.Replace(&previous)
		defer cfg.Replace(&snapshot)
		return call("library_enrich_metadata", map[string]any{"request_key": key, "series_id": original["series_id"], "fields": []string{"cover"}, "proposal_ids": []int64{}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": original["series_id"]}), "cover": original["cover_selection"]})
	}
	failedOp := makeCover("cleanup13-filefail-source")
	badKey := failedOp["old_cover_material"].(string)
	badPath := covermaterial.FilePath(config.ThumbnailDir(cfg.Snapshot()), badKey)
	saved, e := os.ReadFile(badPath)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(badPath); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(badPath, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(badPath, "keep"), []byte("not a material file"), 0600); e != nil {
		t.Fatal(e)
	}
	failInput := map[string]any{"request_key": "cleanup13-filefail", "operation_id": failedOp["id"]}
	partial := cleanupStdio(t, cfg, router, credential.Token, failInput)
	if partial["outcome"] != "partial" {
		t.Fatal(partial)
	}
	// Restore the fault; replay must retain the actual failure, not silently retry.
	if e = os.Remove(filepath.Join(badPath, "keep")); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(badPath); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(badPath, saved, 0600); e != nil {
		t.Fatal(e)
	}
	replay = cleanCall("library_cleanup_metadata_operation", failInput)
	if replay["id"] != partial["id"] || replay["outcome"] != "partial" || replay["replayed"] != true {
		t.Fatal(replay)
	}

	// A second series' current reference protects the material independently of
	// the source operation's audit or source book.
	sharedOp := makeCover("cleanup13-shared-source")
	var library int64
	_ = store.DB().QueryRow(`SELECT library_id FROM books WHERE id=?`, book).Scan(&library)
	otherSeries, e := store.CreateSeries(ctx, database.CreateSeriesParams{Name: "Shared material owner", Path: filepath.Join(t.TempDir(), "s"), LibraryID: library})
	if e != nil {
		t.Fatal(e)
	}
	otherBook, e := store.CreateBook(ctx, database.CreateBookParams{Name: "Other book", Path: filepath.Join(t.TempDir(), "b.cbz"), SeriesID: otherSeries.ID, LibraryID: library})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.DB().Exec(`UPDATE books SET cover_path=? WHERE id=?`, sharedOp["old_cover_material"], otherBook.ID); e != nil {
		t.Fatal(e)
	}
	shared := cleanCall("library_cleanup_metadata_operation", map[string]any{"request_key": "cleanup13-shared", "operation_id": sharedOp["id"]})
	for _, item := range shared["materials"].([]any) {
		if item.(map[string]any)["outcome"] != "protected" {
			t.Fatal(shared)
		}
	}
	web("GET", fmt.Sprintf("/covers/%d", otherBook.ID), nil, 200)
	// An actual competing Undo and cleanup must linearize to either a retained
	// complete reversal or explicit unavailability, never a missing restored file.
	raceOp := makeCover("cleanup13-race-source")
	var both struct{ Token string }
	_ = json.Unmarshal(web("POST", "/mcp/credentials", map[string]any{"user_id": 1, "name": "cleanup-race-writer", "library_ids": []int64{library}, "capabilities": []string{"read", "metadata_write", "cleanup"}}, 200), &both)
	bothClient, e := mcp.NewClient(&mcp.Implementation{Name: "cleanup-race", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: &bearerTransport{base: http.DefaultTransport, token: both.Token}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer bothClient.Close()
	type response struct {
		result *mcp.CallToolResult
		err    error
	}
	undone := make(chan response, 1)
	go func() {
		r, e := bothClient.CallTool(ctx, &mcp.CallToolParams{Name: "library_undo_metadata_operation", Arguments: map[string]any{"request_key": "cleanup13-race-undo", "operation_id": raceOp["id"]}})
		undone <- response{r, e}
	}()
	cleanCall("library_cleanup_metadata_operation", map[string]any{"request_key": "cleanup13-race", "operation_id": raceOp["id"]})
	reversed := <-undone
	if reversed.err != nil || reversed.result.IsError {
		t.Fatal(reversed)
	}
	raw, _ := json.Marshal(reversed.result.StructuredContent)
	var reversal map[string]any
	_ = json.Unmarshal(raw, &reversal)
	if reversal["outcome"] != "changed" && reversal["outcome"] != "unavailable" {
		t.Fatal(reversal)
	}
	if reversal["outcome"] == "changed" {
		web("GET", "/thumbnails/"+reversal["old_cover_material"].(string), nil, 200)
	}
	web("GET", fmt.Sprintf("/covers/%d", book), nil, 200)
	// Same-key and different-key races cannot publish a retired resource; manual
	// cover selection uses the ordinary HTTP route and remains readable.
	manualOp := makeCover("cleanup13-manual-race-source")
	switched := make(chan struct{})
	go func() {
		web("POST", fmt.Sprintf("/books/%d/cover", book), map[string]int{"page": 1}, 200)
		close(switched)
	}()
	cleanCall("library_cleanup_metadata_operation", map[string]any{"request_key": "cleanup13-manual-race", "operation_id": manualOp["id"]})
	<-switched
	web("GET", fmt.Sprintf("/covers/%d", book), nil, 200)
	// Restore automatic source identity for subsequent cover-only operations.
	if _, e = store.DB().Exec(`UPDATE books SET cover_path=?,cover_locked=0 WHERE id=?`, raceOp["new_cover_material"], book); e == nil {
		// It is legal only if a current/retained reference prevented retirement.
		web("GET", fmt.Sprintf("/covers/%d", book), nil, 200)
	} else {
		// The current shared operation after-image is protected by its book/reference.
		if _, e = store.DB().Exec(`UPDATE books SET cover_path=?,cover_locked=0 WHERE id=?`, sharedOp["new_cover_material"], book); e != nil {
			t.Fatal(e)
		}
	}

	// Stop at the public task progress seam, AFTER the first durable item and
	// BEFORE the next checkpoint. No business use case or IO is replaced.
	for _, mode := range []string{"revoke", "password"} {
		first := makeCover("cleanup13-running-first-" + mode)
		second := makeCover("cleanup13-running-second-" + mode)
		operator, err := store.CreateUser(ctx, database.CreateUserParams{Username: "cleanup-operator-" + mode, PasswordHash: "unused", Role: "admin"})
		if err != nil {
			t.Fatal(err)
		}
		var runningCredential struct{ ID, Token string }
		_ = json.Unmarshal(web("POST", "/mcp/credentials", map[string]any{"user_id": operator.ID, "name": "running-" + mode, "library_ids": []any{original["library_id"]}, "capabilities": []string{"cleanup"}}, 200), &runningCredential)
		mixed, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "library_start_metadata_cleanup", Arguments: map[string]any{"library_id": original["library_id"], "operations": []any{
			map[string]any{"request_key": "cleanup13-mixed-" + mode, "operation_id": first["id"]},
			map[string]any{"request_key": "cleanup13-mixed-foreign-" + mode, "operation_id": "cleanup13-foreign-fixture"},
		}}})
		if err != nil || !mixed.IsError {
			t.Fatal("mixed-library batch admitted", mixed, err)
		}
		entered, release := cleanupGate.arm()
		defer release()
		batch := map[string]any{"library_id": original["library_id"], "operations": []any{
			map[string]any{"request_key": "cleanup13-running-first-" + mode, "operation_id": first["id"]},
			map[string]any{"request_key": "cleanup13-running-second-" + mode, "operation_id": second["id"]},
		}}
		// Real stdio process accepts the batch and exits; the shared engine continues.
		started := cleanupStdioTool(t, cfg, router, runningCredential.Token, "library_start_metadata_cleanup", batch)
		select {
		case <-entered:
		case <-time.After(15 * time.Second):
			t.Fatal("cleanup never reached first-item progress barrier")
		}
		var firstAudit, secondAudit map[string]any
		_ = json.Unmarshal(web("GET", "/mcp/operations/"+first["id"].(string), nil, 200), &firstAudit)
		_ = json.Unmarshal(web("GET", "/mcp/operations/"+second["id"].(string), nil, 200), &secondAudit)
		if firstAudit["undo_unavailable"] != true || secondAudit["undo_unavailable"] == true {
			t.Fatal(firstAudit, secondAudit)
		}
		if mode == "revoke" {
			web("DELETE", "/mcp/credentials/"+runningCredential.ID, nil, 200)
		} else {
			web("POST", fmt.Sprintf("/users/%d/password", operator.ID), map[string]string{"password": "reset-cleanup-password"}, 200)
		}
		release()
		runID := int64(started["run_id"].(float64))
		stopped, err := engine.Await(ctx, runID)
		if err != nil || stopped.Status != task.StatusCancelled || stopped.StartedAt.IsZero() || stopped.Current != 1 {
			t.Fatal(stopped, err)
		}
		_ = json.Unmarshal(web("GET", "/mcp/operations/"+second["id"].(string), nil, 200), &secondAudit)
		if secondAudit["undo_unavailable"] == true {
			t.Fatal("running revocation discarded later item", secondAudit)
		}
		for _, field := range []string{"old_cover_material", "new_cover_material"} {
			web("GET", "/thumbnails/"+second[field].(string), nil, 200)
		}
		t.Logf("running cleanup %s: started run %d cancelled at committed prefix=1; later audit/images retained", mode, runID)
		// Library reservation and global worker slot must have been returned. A fresh
		// credential on the same library can complete the untouched remainder.
		finished := cleanCall("library_start_metadata_cleanup", map[string]any{"library_id": original["library_id"], "operations": []any{map[string]any{"request_key": "cleanup13-recovered-" + mode, "operation_id": second["id"]}}})
		if done, err := engine.Await(ctx, int64(finished["run_id"].(float64))); err != nil || done.Status != task.StatusCompleted {
			t.Fatal(done, err)
		}
	}

	// Exercise a REAL filesystem reparse point, not a mocked Lstat. On Windows
	// junctions do not need developer-mode symlink privileges.
	linkPrevious := makeCover("cleanup13-link-previous")
	cleanCall("library_cleanup_metadata_operation", map[string]any{"request_key": "cleanup13-link-previous-clean", "operation_id": linkPrevious["id"]})
	linkOp := makeCover("cleanup13-link-source")
	linkKey := linkOp["old_cover_material"].(string)
	materialDir := filepath.Dir(covermaterial.FilePath(config.ThumbnailDir(cfg.Snapshot()), linkKey))
	backupDir := materialDir + ".held"
	outside := t.TempDir()
	sentinel := filepath.Join(outside, filepath.Base(linkKey))
	if err := os.WriteFile(sentinel, []byte("outside must survive cleanup"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(materialDir, backupDir); err != nil {
		t.Fatal(err)
	}
	var restoreOnce sync.Once
	restoreMaterialDir := func() {
		restoreOnce.Do(func() {
			if err := os.Remove(materialDir); err != nil && !os.IsNotExist(err) {
				t.Error(err)
			}
			if err := os.Rename(backupDir, materialDir); err != nil {
				t.Error(err)
			}
		})
	}
	defer restoreMaterialDir()
	if runtime.GOOS == "windows" {
		command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-Command", "$ErrorActionPreference='Stop'; New-Item -ItemType Junction -Path '"+strings.ReplaceAll(materialDir, "'", "''")+"' -Target '"+strings.ReplaceAll(outside, "'", "''")+"' | Out-Null")
		if out, err := command.CombinedOutput(); err != nil {
			restoreMaterialDir()
			t.Fatalf("real junction creation: %v %s", err, out)
		}
	} else if err := os.Symlink(outside, materialDir); err != nil {
		restoreMaterialDir()
		t.Fatal(err)
	}
	linked := cleanCall("library_cleanup_metadata_operation", map[string]any{"request_key": "cleanup13-link", "operation_id": linkOp["id"]})
	restoreMaterialDir()
	if linked["outcome"] != "partial" {
		t.Fatal("linked material directory must fail closed", linked)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "outside must survive cleanup" {
		t.Fatalf("cleanup followed reparse point: %q %v", data, err)
	}
	t.Logf("real %s directory link rejected: cleanup partial, outside sentinel unchanged, current cover readable", runtime.GOOS)
	web("GET", fmt.Sprintf("/covers/%d", book), nil, 200)

	// A journal failure AFTER unlink leaves a durable pending receipt and a
	// permanent publication fence. Reopening and replay must safely finish it.
	pendingOp := makeCover("cleanup13-pending-source")
	if _, e = store.DB().Exec(`CREATE TRIGGER fail_cleanup_result BEFORE UPDATE ON metadata_cleanup_requests WHEN NEW.payload LIKE '%"deleted"%' BEGIN SELECT RAISE(ABORT,'controlled cleanup result failure'); END`); e != nil {
		t.Fatal(e)
	}
	agentPendingOp := makeCover("cleanup13-agent-pending-source")
	agentFailure, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "library_cleanup_metadata_operation", Arguments: map[string]any{"request_key": "cleanup13-agent-private-retry", "operation_id": agentPendingOp["id"]}})
	if err != nil || !agentFailure.IsError {
		t.Fatal("expected controlled agent result-write failure", agentFailure, err)
	}
	var agentPendingAudit, agentPending map[string]any
	_ = json.Unmarshal(web("GET", "/mcp/operations/"+agentPendingOp["id"].(string), nil, 200), &agentPendingAudit)
	_ = json.Unmarshal(web("GET", "/mcp/cleanups/"+agentPendingAudit["cleaned_by"].(string), nil, 200), &agentPending)
	if agentPending["outcome"] != "pending" || agentPending["retry_request_key"] != nil {
		t.Fatal("admin inspection must not transfer a credential retry namespace", agentPending)
	}
	pendingInput := map[string]any{"request_key": "cleanup13-pending-reopen"}
	web("POST", "/mcp/operations/"+pendingOp["id"].(string)+"/cleanup", pendingInput, 500)
	if _, e = store.DB().Exec(`DROP TRIGGER fail_cleanup_result`); e != nil {
		t.Fatal(e)
	}
	var pendingAudit map[string]any
	_ = json.Unmarshal(web("GET", "/mcp/operations/"+pendingOp["id"].(string), nil, 200), &pendingAudit)
	if pendingAudit["undo_unavailable"] != true {
		t.Fatal(pendingAudit)
	}
	// Both transports hand accepted work to the same runtime: closing the real
	// stdio helper while queued does not cancel the accepted cleanup.
	acceptedOp := makeCover("cleanup13-disconnect-source")
	if _, e = engine.PauseAll(ctx); e != nil {
		t.Fatal(e)
	}
	accepted := cleanupStdioTool(t, cfg, router, credential.Token, "library_start_metadata_cleanup", map[string]any{"library_id": original["library_id"], "operations": []any{map[string]any{"request_key": "cleanup13-disconnected", "operation_id": acceptedOp["id"]}}})
	if _, e = engine.ResumeAll(ctx); e != nil {
		t.Fatal(e)
	}
	if finished, e := engine.Await(ctx, int64(accepted["run_id"].(float64))); e != nil || finished.Status != task.StatusCompleted {
		t.Fatal(finished, e)
	}
	if receipt := cleanCall("library_get_metadata_cleanup", map[string]any{"request_key": "cleanup13-disconnected"}); receipt["outcome"] != "complete" {
		t.Fatal(receipt)
	}
	// A cleanup-only credential's run is discoverable without read/scan grants.
	runs, e := client.CallTool(ctx, &mcp.CallToolParams{Name: "library_list_runs", Arguments: map[string]any{"library_id": original["library_id"], "limit": 20}})
	if e != nil || runs.IsError {
		t.Fatal(runs, e)
	}
	// Existing engine controls govern accepted cleanup runs. Cancellation and queued
	// revocation stop work before a new item loses its reversal material.
	queuedOp := makeCover("cleanup13-queued-source")
	if _, e = engine.PauseAll(ctx); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if _, err := engine.ResumeAll(ctx); err != nil {
			t.Error(err)
		}
	}()
	batch := map[string]any{"library_id": original["library_id"], "operations": []any{map[string]any{"request_key": "cleanup13-queued", "operation_id": queuedOp["id"]}}}
	started := cleanCall("library_start_metadata_cleanup", batch)
	runID := int64(started["run_id"].(float64))
	cleanCall("library_cancel_run", map[string]any{"run_id": runID})
	if stopped, e := engine.Await(ctx, runID); e != nil || stopped.Status != task.StatusCancelled {
		t.Fatal(stopped, e)
	}
	batch["operations"] = []any{map[string]any{"request_key": "cleanup13-revoked", "operation_id": queuedOp["id"]}}
	started = cleanCall("library_start_metadata_cleanup", batch)
	runID = int64(started["run_id"].(float64))
	web("DELETE", "/mcp/credentials/"+credential.ID, nil, 200)
	if _, e = engine.ResumeAll(ctx); e != nil {
		t.Fatal(e)
	}
	if stopped, e := engine.Await(ctx, runID); e != nil || stopped.Status != task.StatusCancelled || !stopped.StartedAt.IsZero() {
		t.Fatal(stopped, e)
	}
	cfg.Replace(&previous)
	audit := call("library_get_metadata_operation", map[string]any{"operation_id": queuedOp["id"]})
	if audit["undo_unavailable"] == true {
		t.Fatal("revoked queued cleanup discarded material", audit)
	}
	return func(web func(string, string, any, int) []byte) {
		var pending map[string]any
		_ = json.Unmarshal(web("GET", "/mcp/cleanups/"+pendingAudit["cleaned_by"].(string), nil, 200), &pending)
		if pending["outcome"] != "pending" || pending["retry_request_key"] != "cleanup13-pending-reopen" {
			t.Fatal(pending)
		}
		// Recovered identities retain the original target binding across reload.
		web("POST", "/mcp/operations/"+failedOp["id"].(string)+"/cleanup", map[string]any{"request_key": pending["retry_request_key"]}, 400)
		_ = json.Unmarshal(web("POST", "/mcp/operations/"+pendingOp["id"].(string)+"/cleanup", map[string]any{"request_key": pending["retry_request_key"]}, 200), &pending)
		if pending["outcome"] != "complete" || pending["replayed"] != true || pending["id"] != pendingAudit["cleaned_by"] {
			t.Fatal(pending)
		}

		var saved map[string]any
		_ = json.Unmarshal(web("GET", "/mcp/operations/"+failedOp["id"].(string), nil, 200), &saved)
		if saved["undo_unavailable"] != true || saved["cleaned_by"] != partial["id"] || saved["old_cover_material"] != nil {
			t.Fatal(saved)
		}
		_ = json.Unmarshal(web("GET", "/mcp/cleanups/"+partial["id"].(string), nil, 200), &saved)
		if saved["outcome"] != "partial" || saved["id"] != partial["id"] {
			t.Fatal(saved)
		}
	}
}

func cleanupStdio(t *testing.T, cfg *config.Manager, router http.Handler, token string, input any) map[string]any {
	t.Helper()
	return cleanupStdioTool(t, cfg, router, token, "library_cleanup_metadata_operation", input)
}
func cleanupStdioTool(t *testing.T, cfg *config.Manager, router http.Handler, token, tool string, input any) map[string]any {
	t.Helper()
	ctx := t.Context()
	binary := filepath.Join(t.TempDir(), "mcp-proxy.exe")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/mcp-proxy")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build stdio: %v %s", e, out)
	}
	server := httptest.NewServer(router)
	defer server.Close()
	previous := cfg.Snapshot()
	defer cfg.Replace(&previous)
	snapshot := previous
	snapshot.MCP.PublicURL = server.URL
	snapshot.MCP.AllowLANHTTP = true
	cfg.Replace(&snapshot)
	command := exec.CommandContext(ctx, binary, "--url", server.URL, "--allow-lan-http")
	command.Dir = t.TempDir()
	command.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN="+token)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	session, e := mcp.NewClient(&mcp.Implementation{Name: "cleanup-stdio", Version: "1"}, nil).Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if e != nil {
		t.Fatalf("stdio: %v %s", e, stderr.String())
	}
	defer session.Close()
	result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: input})
	if e != nil || result.IsError {
		t.Fatalf("stdio cleanup %+v %v %s", result, e, stderr.String())
	}
	raw, _ := json.Marshal(result.StructuredContent)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

// The task publisher is a supported bootstrap input. Blocking a progress frame
// holds no metadata transaction; management HTTP can commit revocation meanwhile.
type cleanupProgressGate struct {
	mu               sync.Mutex
	entered, release chan struct{}
}

func (g *cleanupProgressGate) arm() (<-chan struct{}, func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.entered, g.release = make(chan struct{}), make(chan struct{})
	release := g.release
	var once sync.Once
	return g.entered, func() { once.Do(func() { close(release) }) }
}
func (g *cleanupProgressGate) publish(s task.Snapshot) {
	if s.Run.Phase != "cleanup" || s.Run.Current != 1 || s.Run.Status != task.StatusRunning {
		return
	}
	g.mu.Lock()
	entered, release := g.entered, g.release
	g.entered, g.release = nil, nil
	g.mu.Unlock()
	if entered != nil {
		close(entered)
		<-release
	}
}
