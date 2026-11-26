// A credential's run reads follow each run's own capability: a credential lists only the runs it
// started, and once narrowed, the runs of a withdrawn capability leave its listing and cannot be
// read, while paging past them still reaches the older runs it may see.

package bootstrap_test

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/task"
)

// listedRun mirrors one run of a run listing.
type listedRun struct {
	ID        int64 `json:"run_id"`
	LibraryID int64 `json:"library_id"`
}

// pages lists the agent's runs of one library page by page, newest first.
func (a *mainLoopAgent) pages(library int64, limit int) []int64 {
	a.t.Helper()
	var ids []int64
	var before int64
	for {
		var page []listedRun
		if result := a.call("library_list_runs", map[string]any{"library_id": library, "limit": limit, "before_id": before}, &page); result.IsError {
			a.t.Fatalf("list runs refused: %s", resultText(result))
		}
		if len(page) > limit {
			a.t.Fatalf("page of %d runs for limit %d", len(page), limit)
		}
		if len(page) == 0 {
			return ids
		}
		for _, run := range page {
			if run.LibraryID != library || (before > 0 && run.ID >= before) {
				a.t.Fatalf("run %+v out of library or cursor %d", run, before)
			}
			ids = append(ids, run.ID)
			before = run.ID
		}
	}
}

func TestRunListingFollowsEachRunsCapability(t *testing.T) {
	for _, final := range []string{delegated.ScanControl, delegated.Cleanup, delegated.MetadataWrite} {
		t.Run(final, func(t *testing.T) {
			ctx := context.Background()
			store, cfg := fixture(t)
			unpaced(t, cfg)
			library, ids := claimLibrary(t, store, "Runs", "Title 1")
			outside, _ := claimLibrary(t, store, "Outside", "Title 2")
			web := openClaimWeb(t, store, cfg, &recordingSources{answer: candidatesBySeries}, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
			if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
				t.Fatalf("prefetch run %+v", run)
			}
			every := []string{delegated.Read, delegated.MetadataWrite, delegated.ScanControl, delegated.Cleanup}
			agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, every)
			engine := web.process.Application.Engine
			started := map[int64]string{}
			research := func(a *mainLoopAgent, term string) int64 {
				t.Helper()
				var out researchResult
				if result := a.call("library_research_series", map[string]any{"series_id": ids[0], "query": term}, &out); result.IsError || out.RunID == 0 {
					t.Fatalf("research %q: %+v %s", term, out, resultText(result))
				}
				if _, err := engine.Await(ctx, out.RunID); err != nil {
					t.Fatal(err)
				}
				return out.RunID
			}

			// A paused engine keeps the scan from touching the fixture while it still yields a run.
			if _, err := engine.PauseAll(ctx); err != nil {
				t.Fatal(err)
			}
			var scan struct {
				RunID int64 `json:"run_id"`
			}
			if result := agent.call("library_start_scan", map[string]any{"library_id": library.lib.ID}, &scan); result.IsError {
				t.Fatalf("scan refused: %s", resultText(result))
			}
			agent.call("library_cancel_run", map[string]any{"run_id": scan.RunID}, &map[string]any{})
			if _, err := engine.ResumeAll(ctx); err != nil {
				t.Fatal(err)
			}
			started[scan.RunID] = delegated.ScanControl
			started[research(agent, "Title 1 first look")] = delegated.MetadataWrite
			operation := agent.identifyAll("identify", map[int64][]string{ids[0]: {"bangumi:1"}})[ids[0]].OperationID
			var cleanup struct {
				RunID int64 `json:"run_id"`
			}
			if result := agent.call("library_start_metadata_cleanup", map[string]any{"library_id": library.lib.ID, "operations": []any{map[string]any{"request_key": "clean", "operation_id": operation}}}, &cleanup); result.IsError {
				t.Fatalf("cleanup refused: %s", resultText(result))
			}
			if _, err := engine.Await(ctx, cleanup.RunID); err != nil {
				t.Fatal(err)
			}
			started[cleanup.RunID] = delegated.Cleanup
			started[research(agent, "Title 1 second look")] = delegated.MetadataWrite
			// Another credential of the same owner and library keeps its runs to itself.
			sibling := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, every)
			research(sibling, "Title 1 sibling look")

			want := slices.Sorted(maps.Keys(started))
			slices.Reverse(want)
			for _, limit := range []int{1, 2} {
				if got := agent.pages(library.lib.ID, limit); !slices.Equal(got, want) {
					t.Fatalf("limit %d listed %v, want %v", limit, got, want)
				}
			}
			if result := agent.call("library_list_runs", map[string]any{"library_id": outside.lib.ID}, &[]listedRun{}); !result.IsError {
				t.Fatal("listed the runs of a library outside the credential")
			}

			// Narrowed to one capability, only the runs it started stay visible. Listing runs needs read,
			// scan_control or cleanup, so metadata_write keeps read beside it.
			narrowed := []string{final}
			if final == delegated.MetadataWrite {
				narrowed = append(narrowed, delegated.Read)
			}
			body, _ := json.Marshal(map[string]any{"user_id": 1, "name": "main loop agent", "library_ids": []int64{library.lib.ID}, "capabilities": narrowed})
			if rec := web.raw("PATCH", "/api/mcp/credentials/"+agent.credential, string(body)); rec.Code != 200 {
				t.Fatalf("narrow %d %s", rec.Code, rec.Body)
			}
			var kept []int64
			for _, id := range want {
				if started[id] == final {
					kept = append(kept, id)
				}
			}
			for _, limit := range []int{1, 2} {
				if got := agent.pages(library.lib.ID, limit); !slices.Equal(got, kept) {
					t.Fatalf("narrowed to %s, limit %d listed %v, want %v", final, limit, got, kept)
				}
			}
			for id, capability := range started {
				result := agent.call("library_get_run", map[string]any{"run_id": id}, &map[string]any{})
				if result.IsError != (capability != final) {
					t.Errorf("run %d of %s readable=%v after narrowing to %s", id, capability, !result.IsError, final)
				}
			}
		})
	}
}
