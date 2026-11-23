package bootstrap_test

import (
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
	"testing"
	"yomifolio/internal/database"
)

func verifyProposalAdjudication(t *testing.T, store database.Store, makeSeries func(string) int64, call func(string, any) map[string]any, propose func(int64, string) any, web func(string, string, any, int) []byte, session *mcp.ClientSession, gate func() *enrichmentBarrier) func(func(string, any) map[string]any) {
	t.Helper()
	verifyManualAfterReopen := verifyManualPartialAdjudication(t, store, makeSeries, call, web)
	id := makeSeries("Adjudication")
	p := propose(id, "bangumi")
	input := map[string]any{"request_key": "adjudicate-part", "series_id": id, "proposal_ids": []any{p}, "fields": []string{"summary"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": id}), "adjudication": map[string]any{"mode": "partial", "reason": "Accept verified missing description"}}
	partialInput := map[string]any{}
	for k, v := range input {
		partialInput[k] = v
	}
	op := call("library_enrich_metadata", input)
	if op["outcome"] != "changed" || op["adjudication"].(map[string]any)["status"] != "pending" {
		t.Fatal(op)
	}
	verifyAdjudicationReceiptAuthority(t, store, session, call, partialInput, op)
	input["request_key"] = "adjudicate-close"
	input["fields"] = []string{"tags"}
	input["expected"] = call("library_get_enrichment_state", map[string]any{"series_id": id})
	input["adjudication"] = map[string]any{"mode": "supplement_and_close", "reason": "Accept content genres; discard other differences"}
	closed := call("library_enrich_metadata", input)
	if closed["adjudication"].(map[string]any)["status"] != "supplemented_closed" {
		t.Fatal(closed)
	}
	replay := call("library_enrich_metadata", input)
	if replay["replayed"] != true || replay["id"] != closed["id"] {
		t.Fatal(replay)
	}

	original := call("library_enrich_metadata", partialInput)
	if original["id"] != op["id"] || original["replayed"] != true || original["adjudication"].(map[string]any)["status"] != "pending" {
		t.Fatal("partial receipt not immutable after closure", original)
	}
	for _, decision := range []map[string]any{{"mode": "reject", "reason": "Accept content genres; discard other differences"}, {"mode": "supplement_and_close", "reason": "Different decision reason"}} {
		changed := map[string]any{}
		for k, v := range input {
			changed[k] = v
		}
		changed["adjudication"] = decision
		if decision["mode"] == "reject" {
			changed["fields"] = []string{}
		}
		out, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "library_enrich_metadata", Arguments: changed})
		if err != nil || !out.IsError {
			t.Fatal("changed adjudication reused a durable key", out, err)
		}
	}
	input["request_key"] = "adjudicate-terminal"
	conflict := call("library_enrich_metadata", input)
	if conflict["outcome"] != "conflict" || conflict["applied"] != false {
		t.Fatal(conflict)
	}
	state := call("library_get_enrichment_state", map[string]any{"series_id": id})
	if state["summary"] != "Space journey" {
		t.Fatal(state)
	}

	// A provenance failure after metadata writes must roll back the field consume and status too.
	faultID := makeSeries("Adjudication fault")
	faultP := propose(faultID, "bangumi")
	fault := map[string]any{"request_key": "adjudication-fault", "series_id": faultID, "proposal_ids": []any{faultP}, "fields": []string{"summary"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": faultID}), "adjudication": map[string]any{"mode": "supplement_and_close", "reason": "Verified description"}}
	if _, err := store.DB().Exec(`CREATE TRIGGER adjudication_provenance_fault BEFORE INSERT ON series_metadata_provenance BEGIN SELECT RAISE(ABORT,'private fault'); END`); err != nil {
		t.Fatal(err)
	}
	failed := call("library_enrich_metadata", fault)
	if _, err := store.DB().Exec(`DROP TRIGGER adjudication_provenance_fault`); err != nil {
		t.Fatal(err)
	}
	if failed["outcome"] != "failed" || failed["applied"] != false {
		t.Fatal("fault half committed", failed)
	}
	review := call("library_get_match_proposal", map[string]any{"series_id": faultID, "proposal_id": faultP})
	if review["status"] != "pending" || len(review["fields"].([]any)) != 4 {
		t.Fatal(review)
	}
	if state := call("library_get_enrichment_state", map[string]any{"series_id": faultID}); state["summary"] != nil {
		t.Fatal(state)
	}

	rejectedID := makeSeries("Adjudication rejected")
	rejectedP := propose(rejectedID, "bangumi")
	reject := map[string]any{"request_key": "adjudication-reject", "series_id": rejectedID, "proposal_ids": []any{rejectedP}, "fields": []string{}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": rejectedID}), "adjudication": map[string]any{"mode": "reject", "reason": "Wrong edition; decline all differences"}}
	declined := call("library_enrich_metadata", reject)
	if declined["adjudication"].(map[string]any)["status"] != "rejected" || declined["adjudication"].(map[string]any)["accepted"] != false {
		t.Fatal(declined)
	}
	delete(reject, "adjudication")
	reject["request_key"] = "rejected-ordinary-enrichment"
	reject["fields"] = []string{"summary"}
	forbidden := call("library_enrich_metadata", reject)
	if forbidden["outcome"] != "conflict" || forbidden["applied"] != false {
		t.Fatal("ordinary enrichment bypassed terminal", forbidden)
	}

	// A separate close after a successful partial operation preserves its content.
	fault["request_key"] = "adjudication-part-after-fault"
	fault["adjudication"] = map[string]any{"mode": "partial", "reason": "Accept description only"}
	accepted := call("library_enrich_metadata", fault)
	fault["request_key"] = "adjudication-close-only"
	fault["fields"] = []string{}
	fault["adjudication"] = map[string]any{"mode": "supplement_and_close", "reason": "Keep accepted content and close remaining differences"}
	onlyClose := call("library_enrich_metadata", fault)
	if onlyClose["adjudication"].(map[string]any)["status"] != "supplemented_closed" || onlyClose["applied"] != false {
		t.Fatal(onlyClose)
	}
	undo := call("library_undo_metadata_operation", map[string]any{"request_key": "adjudication-undo-part", "operation_id": accepted["id"]})
	if undo["outcome"] != "changed" {
		t.Fatal(undo)
	}
	review = call("library_get_match_proposal", map[string]any{"series_id": faultID, "proposal_id": faultP})
	if review["status"] != "supplemented_closed" {
		t.Fatal("undo reopened proposal", review)
	}

	// Applying the last remaining diff cannot leave an empty pending inbox entry.
	lastID := makeSeries("Adjudication last field")
	search := call("library_search_sources", map[string]any{"series_id": lastID, "query": "Space"})
	call("library_wait_run", map[string]any{"run_id": search["run"].(map[string]any)["run_id"], "timeout_ms": 30000})
	lastP := call("library_propose_match", map[string]any{"search_id": search["search_id"], "identity_confirmed": true, "edition_confirmed": true, "summary": "Same edition and title", "fields": map[string]string{"summary": "bangumi:0"}})["proposal_id"]
	last := call("library_enrich_metadata", map[string]any{"request_key": "adjudication-last-field", "series_id": lastID, "proposal_ids": []any{lastP}, "fields": []string{"summary"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": lastID}), "adjudication": map[string]any{"mode": "partial", "reason": "Accept the final remaining field"}})
	if last["adjudication"].(map[string]any)["status"] != "applied" {
		t.Fatal("empty pending proposal", last)
	}
	// An applied terminal is acceptance even in a conflict receipt.
	lastConflict := call("library_enrich_metadata", map[string]any{"request_key": "adjudication-last-conflict", "series_id": lastID, "proposal_ids": []any{lastP}, "fields": []string{}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": lastID}), "adjudication": map[string]any{"mode": "reject", "reason": "Already applied"}})
	if a := lastConflict["adjudication"].(map[string]any); lastConflict["outcome"] != "conflict" || a["status"] != "applied" || a["accepted"] != true {
		t.Fatal(lastConflict)
	}
	// Identity evidence on old proposals is absent, not implicitly confirmed.
	legacyID := makeSeries("Adjudication legacy")
	legacy, err := store.CreateMetadataReview(t.Context(), database.CreateMetadataReviewParams{SeriesID: legacyID, Provider: "bangumi", Status: "pending", RawPayload: `{"Summary":"Unverified old description"}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateMetadataReviewField(t.Context(), database.CreateMetadataReviewFieldParams{ReviewID: legacy.ID, FieldName: "summary", ProposedValue: "Unverified old description", Source: "bangumi"}); err != nil {
		t.Fatal(err)
	}
	legacyReview := call("library_get_match_proposal", map[string]any{"series_id": legacyID, "proposal_id": legacy.ID})
	if legacyReview["evidence"] != nil {
		t.Fatal("fabricated confirmation", legacyReview)
	}
	legacyInput := map[string]any{"request_key": "adjudication-legacy", "series_id": legacyID, "proposal_ids": []int64{legacy.ID}, "fields": []string{"summary"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": legacyID}), "adjudication": map[string]any{"mode": "partial", "reason": "Cannot invent identity confirmation"}}
	denied, e := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "library_enrich_metadata", Arguments: legacyInput})
	if e != nil || !denied.IsError {
		t.Fatal("unconfirmed legacy proposal applied", denied, e)
	}
	legacyInput["fields"] = []string{}
	legacyInput["adjudication"] = map[string]any{"mode": "reject", "reason": "Unconfirmed old input; do not apply"}
	if out := call("library_enrich_metadata", legacyInput); out["adjudication"].(map[string]any)["status"] != "rejected" {
		t.Fatal(out)
	}
	// Both transports reauthorize reads against the current role, not the token snapshot.
	if _, err = store.DB().Exec(`UPDATE users SET role='user' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	denied, e = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "library_get_match_proposal", Arguments: map[string]any{"series_id": id, "proposal_id": p}})
	if _, err = store.DB().Exec(`UPDATE users SET role='admin' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if e != nil || !denied.IsError {
		t.Fatal("proposal read escaped current metadata_write authority", denied, e)
	}
	// The immutable operation projection shows all three semantic outcomes on the web.
	history := string(web("GET", "/mcp/operations", nil, 200))
	for _, status := range []string{`"status":"pending"`, `"status":"supplemented_closed"`, `"status":"rejected"`} {
		if !strings.Contains(history, status) {
			t.Fatal("missing durable decision", status, history)
		}
	}
	// A protected/no-op partial with no earlier acceptance must stay false.
	noopID := makeSeries("Never accepted protected description")
	noopP := propose(noopID, "bangumi")
	if _, err := store.DB().Exec(`UPDATE series SET summary='Space journey' WHERE id=?`, noopID); err != nil {
		t.Fatal(err)
	}
	noopInput := map[string]any{"request_key": "unaccepted-noop", "series_id": noopID, "proposal_ids": []any{noopP}, "fields": []string{"summary"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": noopID}), "adjudication": map[string]any{"mode": "partial", "reason": "Matching values are not acceptance"}}
	noop := call("library_enrich_metadata", noopInput)
	if a := noop["adjudication"].(map[string]any); a["status"] != "pending" || a["accepted"] != false || noop["applied"] != false {
		t.Fatal(noop)
	}
	noopInput["request_key"] = "unaccepted-noop-reject"
	noopInput["fields"] = []string{}
	noopInput["adjudication"] = map[string]any{"mode": "reject", "reason": "No fields accepted"}
	if r := call("library_enrich_metadata", noopInput)["adjudication"].(map[string]any); r["status"] != "rejected" || r["accepted"] != false {
		t.Fatal(r)
	}
	verifyAdjudicationAuthors(t, store, makeSeries, call, propose)
	verifyAdjudicationFaults(t, store, makeSeries, call, propose, session, web)
	verifyAdjudicationRaces(t, store, makeSeries, call, propose, web, session, gate)
	return func(reopenedCall func(string, any) map[string]any) {
		verifyManualAfterReopen(reopenedCall)
		read := reopenedCall("library_get_match_proposal", map[string]any{"series_id": id, "proposal_id": p})
		if read["status"] != "supplemented_closed" || read["accepted"] != true {
			t.Fatal("lost persistent adjudication", read)
		}
		// The newly issued credential has valid read/write grants, but does not own
		// the original credential's permanent request-key namespace.
		peer := reopenedCall("library_enrich_metadata", partialInput)
		if peer["replayed"] == true || peer["id"] == op["id"] || peer["outcome"] != "conflict" {
			t.Fatal("new credential borrowed the original replay receipt")
		}
		receipt := reopenedCall("library_get_metadata_operation", map[string]any{"operation_id": closed["id"]})
		if receipt["adjudication"].(map[string]any)["status"] != "supplemented_closed" {
			t.Fatal(receipt)
		}
		undo := reopenedCall("library_undo_metadata_operation", map[string]any{"request_key": "adjudication-reopened-undo", "operation_id": op["id"]})
		if undo["outcome"] != "changed" {
			t.Fatal(undo)
		}
		read = reopenedCall("library_get_match_proposal", map[string]any{"series_id": id, "proposal_id": p})
		if read["status"] != "supplemented_closed" {
			t.Fatal("undo reopened closed proposal", read)
		}
		state := reopenedCall("library_get_enrichment_state", map[string]any{"series_id": id})
		if state["summary"] != nil || len(state["tags"].([]any)) != 2 {
			t.Fatal("undo was not field-local", state)
		}
		var sources map[string]any
		if err := json.Unmarshal(web("GET", fmt.Sprintf("/series/%d/metadata-review", id), nil, 200), &sources); err != nil {
			t.Fatal(err)
		}
		for _, entry := range sources["provenance"].([]any) {
			if entry.(map[string]any)["field_name"] == "summary" {
				t.Fatal("undo left a false latest source", sources)
			}
		}

	}
}

func verifyAdjudicationRaces(t *testing.T, store database.Store, makeSeries func(string) int64, call func(string, any) map[string]any, propose func(int64, string) any, web func(string, string, any, int) []byte, session *mcp.ClientSession, gate func() *enrichmentBarrier) {
	t.Helper()
	for _, competitor := range []string{"reject", "manual_reject", "partial", "close", "lock", "role", "library"} {
		t.Run("adjudication-race-"+competitor, func(t *testing.T) {
			id := makeSeries("Adjudication race " + competitor)
			p := propose(id, "bangumi")
			in := map[string]any{"request_key": "adjudication-race-loser-" + competitor, "series_id": id, "proposal_ids": []any{p}, "fields": []string{"summary"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": id}), "adjudication": map[string]any{"mode": "partial", "reason": "Verified missing description"}}
			originalSeries, err := store.GetSeries(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			barrier := gate()
			type response struct {
				result *mcp.CallToolResult
				err    error
			}
			done := make(chan response, 1)
			go func() {
				out, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "library_enrich_metadata", Arguments: in})
				done <- response{out, err}
			}()
			select {
			case <-barrier.entered:
			case <-t.Context().Done():
				t.Fatal("evidence barrier not reached")
			}
			var winner map[string]any
			func() {
				defer close(barrier.release)

				if competitor == "manual_reject" {
					web("POST", fmt.Sprintf("/metadata/reviews/%v/reject", p), nil, 200)
					return
				}
				if competitor == "role" {
					if _, err := store.DB().Exec(`UPDATE users SET role='user' WHERE id=1`); err != nil {
						t.Fatal(err)
					}
					return
				}
				if competitor == "library" {
					outside, err := store.CreateLibrary(t.Context(), database.CreateLibraryParams{Name: "Adjudication outside", Path: t.TempDir(), ScanMode: "none"})
					if err != nil {
						t.Fatal(err)
					}
					if _, err = store.DB().Exec(`UPDATE series SET library_id=? WHERE id=?`, outside.ID, id); err != nil {
						t.Fatal(err)
					}
					return
				}
				if competitor == "lock" {
					if _, err := store.DB().Exec(`UPDATE series SET locked_fields='summary' WHERE id=?`, id); err != nil {
						t.Fatal(err)
					}
					return
				}
				other := map[string]any{}
				for k, v := range in {
					other[k] = v
				}
				other["request_key"] = "adjudication-race-winner-" + competitor
				mode := "partial"
				if competitor == "reject" {
					mode = "reject"
					other["fields"] = []string{}
				}
				if competitor == "close" {
					mode = "supplement_and_close"
				}
				other["adjudication"] = map[string]any{"mode": mode, "reason": "Concurrent authoritative decision"}
				winner = call("library_enrich_metadata", other)
			}()
			received := <-done

			if competitor == "role" || competitor == "library" {
				if _, err := store.DB().Exec(`UPDATE users SET role='admin' WHERE id=1`); err != nil {
					t.Fatal(err)
				}
				if _, err := store.DB().Exec(`UPDATE series SET library_id=? WHERE id=?`, originalSeries.LibraryID, id); err != nil {
					t.Fatal(err)
				}
				if received.err != nil || !received.result.IsError {
					t.Fatal("stale authority wrote metadata", received.result, received.err)
				}
				state := call("library_get_enrichment_state", map[string]any{"series_id": id})
				if state["summary"] != nil {
					t.Fatal(state)
				}
				review := call("library_get_match_proposal", map[string]any{"series_id": id, "proposal_id": p})
				if review["status"] != "pending" || len(review["fields"].([]any)) != 4 {
					t.Fatal(review)
				}
				if strings.Contains(string(web("GET", "/mcp/operations", nil, 200)), in["request_key"].(string)) {
					t.Fatal("denial created a receipt")
				}
				return
			}
			if received.err != nil || received.result.IsError {
				t.Fatal(received.result, received.err)
			}
			raw, _ := json.Marshal(received.result.StructuredContent)
			var loser map[string]any
			_ = json.Unmarshal(raw, &loser)
			if loser["outcome"] != "conflict" || loser["applied"] != false {
				t.Fatal("stale decision wrote metadata", loser)
			}
			replay := call("library_enrich_metadata", in)
			if replay["id"] != loser["id"] || replay["replayed"] != true {
				t.Fatal(replay)
			}
			review := call("library_get_match_proposal", map[string]any{"series_id": id, "proposal_id": p})
			state := call("library_get_enrichment_state", map[string]any{"series_id": id})
			if competitor == "reject" || competitor == "manual_reject" || competitor == "lock" {
				if state["summary"] != nil || len(review["fields"].([]any)) != 4 {
					t.Fatal(state, review)
				}
			} else {
				if state["summary"] != "Space journey" || len(review["fields"].([]any)) != 3 {
					t.Fatal(state, review)
				}
				if competitor == "partial" {
					closeOnly := map[string]any{"request_key": "adjudication-race-close-after-partial", "series_id": id, "proposal_ids": []any{p}, "fields": []string{}, "expected": state, "adjudication": map[string]any{"mode": "reject", "reason": "Discard only remaining differences"}}
					closed := call("library_enrich_metadata", closeOnly)
					if closed["adjudication"].(map[string]any)["status"] != "supplemented_closed" {
						t.Fatal(closed)
					}
					original := call("library_get_metadata_operation", map[string]any{"operation_id": winner["id"]})
					if original["adjudication"].(map[string]any)["status"] != "pending" {
						t.Fatal("receipt rewritten", original)
					}
				}
			}
		})
	}
}

// Exercise persisted grants through the same production HTTP/stdio session, not a
// synthetic subject. A receipt is data even when replay performs no new write.
func verifyAdjudicationReceiptAuthority(t *testing.T, store database.Store, session *mcp.ClientSession, call func(string, any) map[string]any, input, receipt map[string]any) {
	t.Helper()
	credential := receipt["actor"].(map[string]any)["credential_id"]
	var caps, libraries string
	if err := store.DB().QueryRow(`SELECT capabilities,library_ids FROM mcp_credentials WHERE id=?`, credential).Scan(&caps, &libraries); err != nil {
		t.Fatal(err)
	}
	exec := func(t *testing.T, query string, args ...any) {
		t.Helper()
		if _, err := store.DB().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	denied := func(t *testing.T, name string, args any) {
		t.Helper()
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || result == nil || !result.IsError {
			t.Fatalf("%s returned data after authority withdrawal (transport error: %v)", name, err)
		}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		for _, private := range []string{receipt["id"].(string), "Space journey", "Accept verified missing description", credential.(string)} {
			if strings.Contains(string(raw), private) {
				t.Fatalf("%s leaked receipt data", name)
			}
		}
	}
	for _, test := range []struct {
		name, query string
		value       any
		readDenied  bool
	}{
		{"read-revoked", `UPDATE mcp_credentials SET capabilities=? WHERE id=?`, `["metadata_write"]`, true},
		{"write-revoked", `UPDATE mcp_credentials SET capabilities=? WHERE id=?`, `["read"]`, false},
		{"library-revoked", `UPDATE mcp_credentials SET library_ids=? WHERE id=?`, `[]`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			exec(t, test.query, test.value, credential)
			defer exec(t, `UPDATE mcp_credentials SET capabilities=?,library_ids=? WHERE id=?`, caps, libraries, credential)
			denied(t, "library_enrich_metadata", input)
			changed := map[string]any{}
			for k, v := range input {
				changed[k] = v
			}
			delete(changed, "adjudication")
			denied(t, "library_enrich_metadata", changed)
			changed["request_key"] = "adjudication-denied-new"
			changed["adjudication"] = input["adjudication"]
			denied(t, "library_enrich_metadata", changed)
			if test.readDenied {
				denied(t, "library_get_metadata_operation", map[string]any{"operation_id": receipt["id"]})
				denied(t, "library_list_metadata_operations", map[string]any{"series_id": input["series_id"]})
			}
		})
	}
	t.Run("user-demoted", func(t *testing.T) {
		exec(t, `UPDATE users SET role='user' WHERE id=1`)
		defer exec(t, `UPDATE users SET role='admin' WHERE id=1`)
		denied(t, "library_enrich_metadata", input)
	})
	changed := map[string]any{}
	for k, v := range input {
		changed[k] = v
	}
	delete(changed, "adjudication")
	denied(t, "library_enrich_metadata", changed)
	replayed := call("library_enrich_metadata", input)
	if replayed["id"] != receipt["id"] || replayed["replayed"] != true || replayed["applied"] != false {
		t.Fatal("restored read did not replay original receipt")
	}
	for _, field := range []string{"before", "after", "fields", "adjudication"} {
		want, _ := json.Marshal(receipt[field])
		got, _ := json.Marshal(replayed[field])
		if string(got) != string(want) {
			t.Fatalf("replay changed original %s snapshot", field)
		}
	}
	proposal := call("library_get_match_proposal", map[string]any{"series_id": input["series_id"], "proposal_id": input["proposal_ids"].([]any)[0]})
	if proposal["status"] != "pending" || len(proposal["fields"].([]any)) != 3 {
		t.Fatal("replay consumed proposal fields again")
	}
}

func verifyAdjudicationAuthors(t *testing.T, store database.Store, makeSeries func(string) int64, call func(string, any) map[string]any, propose func(int64, string) any) {
	t.Helper()
	a, b := makeSeries("Adjudication authors"), makeSeries("Adjudication shared authors")
	if _, err := store.DB().Exec(`INSERT INTO authors(name,role) VALUES('Adjudication Alias','Artist')`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{a, b} {
		if _, err := store.DB().Exec(`INSERT INTO series_authors SELECT ?,id FROM authors WHERE name='Adjudication Alias'`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.DB().Exec(`UPDATE series SET summary='Protected human description' WHERE id=?`, a); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT OR IGNORE INTO tags(name) VALUES('Personal shelf')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO series_tags SELECT ?,id FROM tags WHERE name='Personal shelf'`, a); err != nil {
		t.Fatal(err)
	}
	p := propose(a, "bangumi")
	input := map[string]any{"request_key": "adjudication-authors", "series_id": a, "proposal_ids": []any{p}, "fields": []string{"summary", "authors", "tags"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": a}), "adjudication": map[string]any{"mode": "partial", "reason": "Confirm original credit and content genres"}, "author_corrections": []any{map[string]any{"from": map[string]string{"name": "Adjudication Alias", "role": "Artist"}, "to": map[string]string{"name": "Canonical", "role": "Writer"}, "reason": "Published credit confirms alias and writing role"}}}
	op := call("library_enrich_metadata", input)
	after := op["after"].(map[string]any)
	authors, _ := json.Marshal(after["authors"])
	tags, _ := json.Marshal(after["tags"])
	if string(authors) != `[{"name":"Artist","role":"Artist"},{"name":"Canonical","role":"Writer"}]` || string(tags) != `["Personal shelf","Science Fiction","冒险"]` || after["summary"] != "Protected human description" {
		t.Fatal("protected content lost", after)
	}
	shared := call("library_get_enrichment_state", map[string]any{"series_id": b})
	sharedAuthors, _ := json.Marshal(shared["authors"])
	if string(sharedAuthors) != `[{"name":"Adjudication Alias","role":"Artist"}]` {
		t.Fatal("shared identity mutated", shared)
	}
	review := call("library_get_match_proposal", map[string]any{"series_id": a, "proposal_id": p})
	if review["accepted"] != true || len(review["fields"].([]any)) != 2 {
		t.Fatal(review)
	}
	closeInput := map[string]any{"request_key": "adjudication-authors-close", "series_id": a, "proposal_ids": []any{p}, "fields": []string{}, "expected": after, "adjudication": map[string]any{"mode": "reject", "reason": "Keep accepted author correction; discard remaining differences"}}
	closed := call("library_enrich_metadata", closeInput)
	if closed["adjudication"].(map[string]any)["status"] != "supplemented_closed" {
		t.Fatal(closed)
	}
	unchanged := call("library_get_enrichment_state", map[string]any{"series_id": a})
	raw, _ := json.Marshal(unchanged)
	old, _ := json.Marshal(after)
	if string(raw) != string(old) {
		t.Fatal("close reverted accepted fields", unchanged)
	}
	undo := call("library_undo_metadata_operation", map[string]any{"request_key": "adjudication-authors-undo", "operation_id": op["id"]})
	undoAfter := undo["after"].(map[string]any)
	restoredAuthors, _ := json.Marshal(undoAfter["authors"])
	if undo["outcome"] != "changed" || string(restoredAuthors) != string(sharedAuthors) || undoAfter["summary"] != "Protected human description" {
		t.Fatal(undo)
	}
	review = call("library_get_match_proposal", map[string]any{"series_id": a, "proposal_id": p})
	if review["status"] != "supplemented_closed" {
		t.Fatal("undo rewound proposal", review)
	}
}

func verifyAdjudicationFaults(t *testing.T, store database.Store, makeSeries func(string) int64, call func(string, any) map[string]any, propose func(int64, string) any, session *mcp.ClientSession, web func(string, string, any, int) []byte) {
	t.Helper()
	for _, stage := range []string{"consume", "terminal", "receipt"} {
		id := makeSeries("Adjudication fault " + stage)
		p := propose(id, "bangumi")
		input := map[string]any{"request_key": "adjudication-fault-" + stage, "series_id": id, "proposal_ids": []any{p}, "fields": []string{"summary", "tags"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": id}), "adjudication": map[string]any{"mode": "supplement_and_close", "reason": "Verified selected fields"}}
		triggers := map[string]string{
			"consume":  `CREATE TRIGGER adjudication_write_fault BEFORE DELETE ON metadata_review_fields BEGIN SELECT RAISE(ABORT,'private consume failure'); END`,
			"terminal": `CREATE TRIGGER adjudication_write_fault BEFORE UPDATE OF status ON metadata_reviews WHEN NEW.status='supplemented_closed' BEGIN SELECT RAISE(ABORT,'private status failure'); END`,
			"receipt":  `CREATE TRIGGER adjudication_write_fault BEFORE INSERT ON metadata_operations BEGIN SELECT RAISE(ABORT,'private journal failure'); END`,
		}
		if _, err := store.DB().Exec(triggers[stage]); err != nil {
			t.Fatal(err)
		}
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "library_enrich_metadata", Arguments: input})
		if _, e := store.DB().Exec(`DROP TRIGGER adjudication_write_fault`); e != nil {
			t.Fatal(e)
		}
		if err != nil {
			t.Fatal(err)
		}
		if stage == "receipt" {
			if !result.IsError {
				t.Fatal("unpersisted receipt reported success", result)
			}
		} else {
			raw, _ := json.Marshal(result.StructuredContent)
			var failed map[string]any
			_ = json.Unmarshal(raw, &failed)
			if result.IsError || failed["outcome"] != "failed" || failed["applied"] != false || strings.Contains(string(raw), "private") {
				t.Fatal(result)
			}
			replay := call("library_enrich_metadata", input)
			if replay["id"] != failed["id"] || replay["replayed"] != true || replay["outcome"] != "failed" {
				t.Fatal(replay)
			}
		}
		review := call("library_get_match_proposal", map[string]any{"series_id": id, "proposal_id": p})
		state := call("library_get_enrichment_state", map[string]any{"series_id": id})
		if review["status"] != "pending" || review["accepted"] != false || len(review["fields"].([]any)) != 4 || state["summary"] != nil || len(state["tags"].([]any)) != 0 {
			t.Fatal("half committed field consume", review, state)
		}
		var projection map[string]any
		if err := json.Unmarshal(web("GET", fmt.Sprintf("/series/%d/metadata-review", id), nil, 200), &projection); err != nil {
			t.Fatal(err)
		}
		if len(projection["provenance"].([]any)) != 0 {
			t.Fatal("provenance half committed", projection)
		}
	}
}

// The same real HTTP manual workflow feeds both MCP transports. Provenance is
// the only acceptance evidence here: ordinary partial Apply leaves applied_at nil.
func verifyManualPartialAdjudication(t *testing.T, store database.Store, makeSeries func(string) int64, call func(string, any) map[string]any, web func(string, string, any, int) []byte) func(func(string, any) map[string]any) {
	t.Helper()
	id := makeSeries("Manual partial provenance")
	if _, err := store.DB().Exec(`UPDATE series SET title='Existing title' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	search := call("library_search_sources", map[string]any{"series_id": id, "query": "Space"})
	call("library_wait_run", map[string]any{"run_id": search["run"].(map[string]any)["run_id"], "timeout_ms": 30000})
	p := call("library_propose_match", map[string]any{"search_id": search["search_id"], "identity_confirmed": true, "edition_confirmed": true, "summary": "Same edition", "fields": map[string]string{"title": "bangumi:0", "summary": "bangumi:0"}})["proposal_id"]
	read := func(c func(string, any) map[string]any) map[string]any {
		return c("library_get_match_proposal", map[string]any{"series_id": id, "proposal_id": p})
	}
	if r := read(call); r["accepted"] != false {
		t.Fatal("queued is not accepted", r)
	}
	var bulk map[string]any
	if err := json.Unmarshal(web("POST", "/metadata/reviews/bulk-apply", map[string]any{"review_ids": []any{p}, "mode": "fill_empty"}, 200), &bulk); err != nil {
		t.Fatal(err)
	}
	if len(bulk["partial"].([]any)) != 1 {
		t.Fatal(bulk)
	}
	if r := read(call); r["status"] != "pending" || r["accepted"] != true || len(r["fields"].([]any)) != 1 {
		t.Fatal("manual partial lost acceptance", r)
	}
	// Assert the fixture really exercises provenance alone, not a new artificial marker.
	var marked bool
	if err := store.DB().QueryRow(`SELECT applied_at IS NOT NULL FROM metadata_reviews WHERE id=?`, p).Scan(&marked); err != nil || marked {
		t.Fatal("ordinary manual semantics changed", marked, err)
	}
	input := map[string]any{"request_key": "manual-partial-noop", "series_id": id, "proposal_ids": []any{p}, "fields": []string{"title"}, "expected": call("library_get_enrichment_state", map[string]any{"series_id": id}), "adjudication": map[string]any{"mode": "partial", "reason": "Preserve existing title"}}
	op := call("library_enrich_metadata", input)
	assertPartial := func(r map[string]any) {
		t.Helper()
		a := r["adjudication"].(map[string]any)
		if a["status"] != "pending" || a["accepted"] != true || r["applied"] != false {
			t.Fatal("wrong immutable partial receipt", r)
		}
	}
	assertPartial(op)
	replay := call("library_enrich_metadata", input)
	assertPartial(replay)
	if replay["id"] != op["id"] || replay["replayed"] != true {
		t.Fatal(replay)
	}
	// Web history consumes exactly this public projection for its partial state.
	var history map[string]any
	if err := json.Unmarshal(web("GET", "/mcp/operations/"+op["id"].(string), nil, 200), &history); err != nil {
		t.Fatal(err)
	}
	assertPartial(history)
	// Cleaning snapshots must retain both the immutable decision and provenance.
	web("POST", "/mcp/operations/"+op["id"].(string)+"/cleanup", map[string]any{"request_key": "manual-partial-cleanup"}, 200)
	cleaned := call("library_get_metadata_operation", map[string]any{"operation_id": op["id"]})
	assertPartial(cleaned)
	if cleaned["undo_unavailable"] != true {
		t.Fatal(cleaned)
	}
	assertPartial(call("library_enrich_metadata", input))
	if r := read(call); r["status"] != "pending" || r["accepted"] != true {
		t.Fatal("cleanup lost provenance acceptance", r)
	}
	return func(c func(string, any) map[string]any) {
		if r := read(c); r["status"] != "pending" || r["accepted"] != true {
			t.Fatal("reopen lost provenance-only acceptance", r)
		}
		assertPartial(c("library_get_metadata_operation", map[string]any{"operation_id": op["id"]}))
		input["request_key"] = "manual-partial-reject"
		input["fields"] = []string{}
		input["adjudication"] = map[string]any{"mode": "reject", "reason": "Discard remaining title difference"}
		closed := c("library_enrich_metadata", input)
		if a := closed["adjudication"].(map[string]any); a["status"] != "supplemented_closed" || a["accepted"] != true {
			t.Fatal(closed)
		}
		if state := c("library_get_enrichment_state", map[string]any{"series_id": id}); state["title"] != "Existing title" || state["summary"] != "Space journey" {
			t.Fatal("reject removed manual content", state)
		}
		if r := read(c); r["status"] != "supplemented_closed" || r["accepted"] != true {
			t.Fatal(r)
		}
		assertPartial(c("library_get_metadata_operation", map[string]any{"operation_id": op["id"]}))
	}
}
