// These tests guard the agent's corrections of its own work: replacing an identification it made
// takes back what that identification supplemented and nobody changed since, then supplements the
// series by the new one, while everything a person decided or a lock protects stays out of reach.

package bootstrap_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/task"
)

// replacement mirrors the result of replacing an identification.
type replacement struct {
	SeriesID      int64    `json:"series_id"`
	Accepted      bool     `json:"accepted"`
	Status        string   `json:"status"`
	Problem       string   `json:"problem"`
	Message       string   `json:"message"`
	RetractionIDs []string `json:"retraction_ids"`
	OperationID   string   `json:"operation_id"`
}

func (a *mainLoopAgent) replace(key string, series int64, candidates []string, reason string) replacement {
	a.t.Helper()
	var out replacement
	args := map[string]any{"request_key": key, "series_id": series, "candidate_ids": candidates, "reason": reason}
	if result := a.call("library_replace_identification", args, &out); result.IsError {
		a.t.Fatalf("replacement %s refused: %s", key, resultText(result))
	}
	return out
}

// twoBangumiEntries answers Bangumi with a wrong entry 1 and the right entry 2 for every series.
func twoBangumiEntries(source, _ string) ([]*metadata.SeriesMetadata, error) {
	if source != "bangumi" {
		return nil, nil
	}
	return []*metadata.SeriesMetadata{
		{Title: "错的作品", Summary: "Wrong summary", SiteRating: &metadata.SiteRating{Value: 6, Scale: 10}, SourceID: 1, SourceURL: "https://bgm.tv/subject/1"},
		{Title: "对的作品", Summary: "Right summary", Publisher: "Right publisher", SiteRating: &metadata.SiteRating{Value: 9, Scale: 10}, SourceID: 2, SourceURL: "https://bgm.tv/subject/2"},
	}, nil
}

// retractionReceipt mirrors the parts of a retraction receipt the tests read.
type retractionReceipt struct {
	Kind           string `json:"kind"`
	Outcome        string `json:"outcome"`
	Identification struct {
		Retracted struct {
			CandidateID string `json:"candidate_id"`
		} `json:"retracted"`
	} `json:"identification"`
	Fields []struct {
		Field   string `json:"field"`
		Changed bool   `json:"changed"`
	} `json:"fields"`
}

func (r retractionReceipt) changed(field string) bool {
	for _, f := range r.Fields {
		if f.Field == field {
			return f.Changed
		}
	}
	return false
}

func (a *mainLoopAgent) retraction(operation string) retractionReceipt {
	a.t.Helper()
	var out retractionReceipt
	if result := a.call("library_get_metadata_operation", map[string]any{"operation_id": operation}, &out); result.IsError {
		a.t.Fatalf("receipt %s refused: %s", operation, resultText(result))
	}
	return out
}

func TestAgentReplacesItsIdentificationAndKeepsWhatWasChangedSince(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Replace")
	id := library.series("Title 1", "", "01.cbz")
	setSeries(t, store, id, func(p *database.UpdateSeriesMetadataParams) {
		p.Title, p.LockedFields = sqlText("Title 1"), sqlText("title")
	})
	web := openClaimWeb(t, store, cfg, &recordingSources{answer: twoBangumiEntries}, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	agent.identifyAll("identify", map[int64][]string{id: {"bangumi:1"}})
	if got := web.series(id); got.Title.String != "错的作品" || got.Summary.String != "Wrong summary" || got.Rating.Float64 != 6 {
		t.Fatalf("identified series %+v", got)
	}
	// The owner rates the series by hand after the identification.
	setSeries(t, store, id, func(p *database.UpdateSeriesMetadataParams) { p.Rating = sql.NullFloat64{Float64: 7, Valid: true} })

	result := agent.replace("fix", id, []string{"bangumi:2"}, "the summary describes another work")
	if !result.Accepted || result.Status != "identified" || len(result.RetractionIDs) != 1 || result.OperationID == "" || result.Problem != "" {
		t.Fatalf("replacement %+v", result)
	}
	// What the wrong entry brought is taken back and filled again from the right one: the
	// placeholder title is replaced under its lock again. The owner's rating stays.
	got := web.series(id)
	if got.Title.String != "对的作品" || got.LockedFields.String != "title" || got.Summary.String != "Right summary" || got.Publisher.String != "Right publisher" || got.Rating.Float64 != 7 {
		t.Fatalf("replaced series %+v", got)
	}
	var view struct {
		Links []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"links"`
	}
	web.call("GET", fmt.Sprintf("/api/series/%d/context", id), 200, &view)
	if len(view.Links) != 1 || view.Links[0].URL != "https://bgm.tv/subject/2" {
		t.Fatalf("links after replacement %+v", view.Links)
	}
	identification := web.identification(id)
	if len(identification.Conclusions) != 1 || identification.Conclusions[0].EntryID != "2" || identification.Conclusions[0].Confirmer.Kind != "agent" ||
		identification.Conclusions[0].Reason != "the summary describes another work" {
		t.Fatalf("conclusions after replacement %+v", identification.Conclusions)
	}

	// Both operations leave receipts: the retraction takes back what the old entry supplemented,
	// and the new supplementation is an ordinary identification receipt.
	retraction := agent.retraction(result.RetractionIDs[0])
	if retraction.Kind != "enrich" || retraction.Identification.Retracted.CandidateID != "bangumi:1" || retraction.Outcome != "changed" ||
		!retraction.changed("title") || !retraction.changed("summary") || !retraction.changed("source_links") || retraction.changed("rating") {
		t.Fatalf("retraction receipt %+v", retraction)
	}
	if r := agent.receipt(result.OperationID); r.Kind != "enrich" || r.Identification == nil || !r.Identification.PlaceholderTitle || !r.changed("summary") || r.changed("rating") {
		t.Fatalf("supplementation receipt %+v", r)
	}

	// The same request replays its result without writing again.
	if again := agent.replace("fix", id, []string{"bangumi:2"}, "the summary describes another work"); !reflect.DeepEqual(again, result) {
		t.Fatalf("replay %+v want %+v", again, result)
	}
}

// twoEntriesPerSource answers Bangumi with entries 1-3 and AniList with entries 1-2 for every
// series, each with its own summary.
func twoEntriesPerSource(source, _ string) ([]*metadata.SeriesMetadata, error) {
	var out []*metadata.SeriesMetadata
	switch source {
	case "bangumi":
		for n := 1; n <= 3; n++ {
			out = append(out, &metadata.SeriesMetadata{Title: fmt.Sprintf("番组 %d", n), Summary: fmt.Sprintf("Bangumi summary %d", n), SourceID: n, SourceURL: fmt.Sprintf("https://bgm.tv/subject/%d", n)})
		}
	case "anilist":
		for n := 1; n <= 2; n++ {
			out = append(out, &metadata.SeriesMetadata{Title: fmt.Sprintf("AniList %d", n), Publisher: fmt.Sprintf("AniList publisher %d", n), SourceID: n, SourceURL: fmt.Sprintf("https://anilist.co/manga/%d", n)})
		}
	}
	return out, nil
}

func wantRefused(t *testing.T, got replacement, problem string) {
	t.Helper()
	if got.Accepted || got.Problem != problem || got.Message == "" || got.OperationID != "" || len(got.RetractionIDs) != 0 {
		t.Fatalf("replacement %+v want refusal %s with a next step", got, problem)
	}
}

func TestAgentReplacesOnlyAgentIdentificationsOfIdentifiedSeries(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Refusals")
	owned := library.series("Owned", "", "01.cbz")
	historicalProposal(t, store, owned, "Bangumi", 4711, "https://bgm.tv/subject/4711", `{"Title":"怪物"}`, "applied")
	id := library.series("Title 1", "", "01.cbz")
	historicalProposal(t, store, id, "bangumi", 3, "https://bgm.tv/subject/3", `{}`, "rejected")
	web := openClaimWeb(t, store, cfg, &recordingSources{answer: twoEntriesPerSource}, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	agent.identifyAll("identify", map[int64][]string{id: {"bangumi:1", "anilist:1"}})
	identified := web.series(id)

	// A person's identification is out of reach; the refusal points at the report instead.
	refused := agent.replace("owner", owned, []string{"bangumi:2"}, "another work")
	wantRefused(t, refused, "owner_confirmed")
	if !strings.Contains(refused.Message, "library_report_series") || !strings.Contains(refused.Message, "disputed_owner_identification") {
		t.Fatalf("refusal names no report: %s", refused.Message)
	}
	if got := web.identification(owned); got.Status != "identified" || len(got.Conclusions) != 1 || got.Conclusions[0].EntryID != "4711" {
		t.Fatalf("owner's identification after a refused replacement %+v", got)
	}

	wantRefused(t, agent.replace("unknown", id, []string{"bangumi:2", "mangadex:5"}, "another work"), "not_concluded")
	wantRefused(t, agent.replace("same", id, []string{"bangumi:2", "anilist:1"}, "another work"), "already_identified")
	wantRefused(t, agent.replace("excluded", id, []string{"bangumi:3"}, "another work"), "excluded_candidate")
	wantRefused(t, agent.replace("missing", id, []string{"bangumi:9"}, "another work"), "not_in_evidence")
	if got := web.series(id); !reflect.DeepEqual(got, identified) {
		t.Fatalf("a refused replacement changed the series: %+v want %+v", got, identified)
	}

	// While the owner holds the series, the agent leaves it alone.
	review := fmt.Sprintf("/api/identification/series/%d/owner-review", id)
	web.call("PUT", review, 200, nil)
	if got := agent.replace("reviewed", id, []string{"bangumi:2"}, "another work"); got.Status != "owner_reviewed" {
		t.Fatalf("replacement of an owner-reviewed series %+v", got)
	} else {
		wantRefused(t, got, "owner_holds")
	}
	web.call("DELETE", review, 200, nil)
	writeComicArchive(t, library.lib.Path+"/Title 1/02.cbz", `<ComicInfo><Series>Pluto</Series></ComicInfo>`)
	if run := web.scan(library.lib.ID, false); run.Status != task.StatusCompleted {
		t.Fatalf("scan %+v", run)
	}
	if got := agent.replace("recheck", id, []string{"bangumi:2"}, "another work"); got.Status != "needs_recheck" {
		t.Fatalf("replacement of a series awaiting recheck %+v", got)
	} else {
		wantRefused(t, got, "owner_holds")
	}
	if got := web.identification(id); len(got.Conclusions) != 2 || got.Conclusions[0].EntryID != "1" || got.Conclusions[1].EntryID != "1" {
		t.Fatalf("conclusions after refused replacements %+v", got.Conclusions)
	}
}

func TestReplacingOneSourceKeepsTheOthersIdentification(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Partial")
	id := library.series("Title 1", "", "01.cbz")
	web := openClaimWeb(t, store, cfg, &recordingSources{answer: twoEntriesPerSource}, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	agent.identifyAll("identify", map[int64][]string{id: {"bangumi:1", "anilist:1"}})

	result := agent.replace("fix", id, []string{"bangumi:2"}, "entry 1 is another volume set")
	if !result.Accepted || result.Status != "identified" {
		t.Fatalf("replacement %+v", result)
	}
	// The series reads as if it had been identified with Bangumi 2 and AniList 1 from the start.
	if got := web.series(id); got.Title.String != "番组 2" || got.Summary.String != "Bangumi summary 2" || got.Publisher.String != "AniList publisher 1" {
		t.Fatalf("replaced series %+v", got)
	}
	got := web.identification(id)
	if len(got.Conclusions) != 2 || got.Conclusions[0].Source != "bangumi" || got.Conclusions[0].EntryID != "2" || got.Conclusions[1].Source != "anilist" || got.Conclusions[1].EntryID != "1" {
		t.Fatalf("conclusions %+v", got.Conclusions)
	}

	// A later replacement takes back what the remaining entry supplemented, through the earlier
	// replacement's supplementation, and leaves what the new Bangumi entry brought.
	again := agent.replace("fix-anilist", id, []string{"anilist:2"}, "AniList 1 is the novel")
	if !again.Accepted || len(again.RetractionIDs) != 1 || again.RetractionIDs[0] == result.RetractionIDs[0] {
		t.Fatalf("second replacement %+v after %+v", again, result)
	}
	if r := agent.retraction(again.RetractionIDs[0]); r.Identification.Retracted.CandidateID != "anilist:1" || !r.changed("publisher") || r.changed("title") {
		t.Fatalf("second retraction %+v", r)
	}
	if got := web.series(id); got.Title.String != "番组 2" || got.Summary.String != "Bangumi summary 2" || got.Publisher.String != "AniList publisher 2" {
		t.Fatalf("series after the second replacement %+v", got)
	}
}

// fieldCorrection mirrors the result of correcting a field.
type fieldCorrection struct {
	SeriesID    int64  `json:"series_id"`
	Accepted    bool   `json:"accepted"`
	Status      string `json:"status"`
	Problem     string `json:"problem"`
	Message     string `json:"message"`
	OperationID string `json:"operation_id"`
}

func (a *mainLoopAgent) correct(key string, series int64, field, value, reason string) fieldCorrection {
	a.t.Helper()
	var out fieldCorrection
	args := map[string]any{"request_key": key, "series_id": series, "field": field, "value": value, "reason": reason}
	if result := a.call("library_correct_series_field", args, &out); result.IsError {
		a.t.Fatalf("correction %s refused: %s", key, resultText(result))
	}
	return out
}

func TestAgentCorrectsASourceValueAndCanUndoIt(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Correct")
	id := library.series("Title 1", "", "01.cbz")
	web := openClaimWeb(t, store, cfg, &recordingSources{answer: twoBangumiEntries}, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	agent.identifyAll("identify", map[int64][]string{id: {"bangumi:2"}})

	corrected := agent.correct("publisher", id, "publisher", "Right Press", "the source names the imprint, the colophon names Right Press")
	if !corrected.Accepted || corrected.OperationID == "" || corrected.Problem != "" {
		t.Fatalf("correction %+v", corrected)
	}
	if rated := agent.correct("rating", id, "rating", "8.5", "the source rating counts an anime adaptation"); !rated.Accepted {
		t.Fatalf("rating correction %+v", rated)
	}
	if got := web.series(id); got.Publisher.String != "Right Press" || got.Rating.Float64 != 8.5 || got.Summary.String != "Right summary" {
		t.Fatalf("corrected series %+v", got)
	}
	r := agent.receipt(corrected.OperationID)
	if r.Kind != "enrich" || r.Outcome != "changed" || !r.changed("publisher") || r.changed("summary") {
		t.Fatalf("correction receipt %+v", r)
	}
	// A correction is kept like the source value it replaced: a new book's ComicInfo does not wash it out.
	writeComicArchive(t, library.lib.Path+"/Title 1/02.cbz", `<ComicInfo><Publisher>ComicInfo publisher</Publisher></ComicInfo>`)
	if run := web.scan(library.lib.ID, false); run.Status != task.StatusCompleted {
		t.Fatalf("scan %+v", run)
	}
	if got := web.series(id); got.Publisher.String != "Right Press" {
		t.Fatalf("a scan washed out the correction: %+v", got)
	}

	// Undoing restores the source value together with its source record, so the value is again
	// one a scan keeps and the agent may correct.
	var undone receipt
	if result := agent.call("library_undo_metadata_operation", map[string]any{"request_key": "undo-publisher", "operation_id": corrected.OperationID}, &undone); result.IsError || undone.Outcome != "changed" {
		t.Fatalf("undo %+v %s", undone, resultText(result))
	}
	if got := web.series(id); got.Publisher.String != "Right publisher" {
		t.Fatalf("publisher after undo %+v", got)
	}
	writeComicArchive(t, library.lib.Path+"/Title 1/03.cbz", `<ComicInfo><Publisher>ComicInfo publisher</Publisher></ComicInfo>`)
	if run := web.scan(library.lib.ID, false); run.Status != task.StatusCompleted {
		t.Fatalf("scan %+v", run)
	}
	if got := web.series(id); got.Publisher.String != "Right publisher" {
		t.Fatalf("a scan washed out the restored source value: %+v", got)
	}
	if again := agent.correct("publisher-again", id, "publisher", "Right Press", "the colophon names Right Press"); !again.Accepted {
		t.Fatalf("correcting the restored source value %+v", again)
	}

	// The same request replays its result without writing again.
	if replay := agent.correct("publisher", id, "publisher", "Right Press", "the source names the imprint, the colophon names Right Press"); !reflect.DeepEqual(replay, corrected) {
		t.Fatalf("replay %+v want %+v", replay, corrected)
	}
}

func wantCorrectionRefused(t *testing.T, got fieldCorrection, problem string) {
	t.Helper()
	if got.Accepted || got.Problem != problem || got.Message == "" || got.OperationID != "" {
		t.Fatalf("correction %+v want refusal %s with a next step", got, problem)
	}
}

func TestAgentMayNotCorrectWhatAPersonOrComicInfoOrALockDecided(t *testing.T) {
	store, cfg := fixture(t)
	// The owner applied a Bangumi proposal long ago and then changed the publisher by hand.
	lib, series := identifiedSeries(t, store, `<ComicInfo><Series>Monster</Series></ComicInfo>`)
	web := openIdentificationWeb(t, store, cfg)
	defer web.process.Close()
	web.setupAdmin()
	agent := web.mainLoopAgent(cfg, 1, []int64{lib.ID}, []string{delegated.Read, delegated.MetadataWrite})

	wantCorrectionRefused(t, agent.correct("owner-edit", series.ID, "publisher", "Shogakukan", "the colophon names Shogakukan"), "not_from_source")
	// The scan's directory-name title is locked: the placeholder exception is the server's alone.
	wantCorrectionRefused(t, agent.correct("placeholder", series.ID, "title", "怪物", "Bangumi names it 怪物"), "locked")
	wantCorrectionRefused(t, agent.correct("unchanged", series.ID, "summary", "Source summary", "it is right"), "unchanged")
	// A new book's ComicInfo replaces the owner's publisher, and a ComicInfo value is no source value either.
	writeComicArchive(t, filepath.Join(lib.Path, "Monster", "Monster 02.cbz"), `<ComicInfo><Series>Monster</Series><Publisher>ComicInfo publisher</Publisher></ComicInfo>`)
	if run := web.scan(lib.ID, false); run.Status != task.StatusCompleted {
		t.Fatalf("scan %+v", run)
	}
	wantCorrectionRefused(t, agent.correct("comicinfo", series.ID, "publisher", "Shogakukan", "the colophon names Shogakukan"), "not_from_source")
	setSeries(t, store, series.ID, func(p *database.UpdateSeriesMetadataParams) { p.LockedFields = sqlText("title,status") })
	wantCorrectionRefused(t, agent.correct("locked", series.ID, "status", "ongoing", "the series continues"), "locked")
	for _, bad := range []map[string]any{
		{"request_key": "cover", "series_id": series.ID, "field": "cover", "value": "1", "reason": "a better cover"},
		{"request_key": "status", "series_id": series.ID, "field": "status", "value": "maybe", "reason": "unknown status"},
		{"request_key": "rating", "series_id": series.ID, "field": "rating", "value": "11", "reason": "beyond the scale"},
		{"request_key": "title", "series_id": series.ID, "field": "title", "value": "", "reason": "no title"},
	} {
		if result, err := agent.session.CallTool(t.Context(), &mcp.CallToolParams{Name: "library_correct_series_field", Arguments: bad}); err == nil && !result.IsError {
			t.Fatalf("correction %v accepted: %s", bad, resultText(result))
		}
	}
	before := web.series(series.ID)
	if before.Publisher.String != "ComicInfo publisher" || before.Status.String != "completed" || before.Title.String != "Monster" || before.Summary.String != "Source summary" {
		t.Fatalf("refused corrections changed the series: %+v", before)
	}

	// While the owner keeps the series, even a source value is out of reach.
	review := fmt.Sprintf("/api/identification/series/%d/owner-review", series.ID)
	web.call("PUT", review, 200, nil)
	if got := agent.correct("reviewed", series.ID, "summary", "A better summary", "the source summary spoils the ending"); got.Status != "owner_reviewed" {
		t.Fatalf("correction of an owner-reviewed series %+v", got)
	} else {
		wantCorrectionRefused(t, got, "owner_holds")
	}
	web.call("DELETE", review, 200, nil)
	// A value the owner's applied proposal brought is a source value nobody changed since.
	if got := agent.correct("summary", series.ID, "summary", "A better summary", "the source summary spoils the ending"); !got.Accepted {
		t.Fatalf("correction of a source value %+v", got)
	}
	if got := web.series(series.ID); got.Summary.String != "A better summary" {
		t.Fatalf("corrected summary %+v", got)
	}
}

func TestAgentDisputesAPersonsIdentificationForTheOwnerToRecheck(t *testing.T) {
	store, cfg := fixture(t)
	lib, series := identifiedSeries(t, store, `<ComicInfo><Series>Monster</Series></ComicInfo>`)
	agentMade := newPrefetchLibrary(t, store, "Agent made")
	unconcluded := agentMade.series("Unconcluded", "", "01.cbz")
	web := openIdentificationWeb(t, store, cfg)
	defer web.process.Close()
	web.setupAdmin()
	before := web.series(series.ID)
	conclusions := web.identification(series.ID).Conclusions
	// Reporting needs only read, so a read-only inspection agent can question it too.
	agent := web.mainLoopAgent(cfg, 1, []int64{lib.ID, agentMade.lib.ID}, []string{delegated.Read})

	got, result := agent.report(series.ID, "disputed_owner_identification", "  ComicInfo and every volume name the 2004 anime book, not the manga  ")
	if result.IsError || got != (seriesReport{SeriesID: series.ID, Category: "disputed_owner_identification", Recorded: true, Status: "needs_recheck"}) {
		t.Fatalf("dispute %+v %s", got, resultText(result))
	}
	escalations := web.ownerVerdicts(series.ID).Escalations
	if len(escalations) != 1 || escalations[0].Class != "recheck" || escalations[0].Reason != "ComicInfo and every volume name the 2004 anime book, not the manga" ||
		escalations[0].RaisedBy.Kind != "agent" || escalations[0].RaisedBy.CredentialID != agent.credential {
		t.Fatalf("dispute escalation %+v", escalations)
	}
	// The series waits for the owner with its identification and fields as they were.
	if after := web.identification(series.ID); after.Status != "needs_recheck" || !reflect.DeepEqual(after.Conclusions, conclusions) {
		t.Fatalf("identification after the dispute %+v", after)
	}
	if after := web.series(series.ID); !reflect.DeepEqual(after, before) {
		t.Fatalf("the dispute changed the series %+v, was %+v", after, before)
	}
	if again, _ := agent.report(series.ID, "disputed_owner_identification", "still the anime book"); again.Recorded || again.Status != "needs_recheck" {
		t.Fatalf("repeated dispute %+v", again)
	}

	// Without a person's identification there is nothing to dispute.
	refused, err := agent.session.CallTool(t.Context(), &mcp.CallToolParams{Name: "library_report_series", Arguments: map[string]any{"series_id": unconcluded, "category": "disputed_owner_identification", "reason": "looks wrong"}})
	if err != nil || !refused.IsError || !strings.Contains(resultText(refused), "library_replace_identification") {
		t.Fatalf("dispute without a person's identification %+v %v", refused, err)
	}
	if got := web.identification(unconcluded); got.Status != "unprocessed" {
		t.Fatalf("refused dispute changed the status %+v", got)
	}
}

// pointsAtProtectedValueNote reports whether a refusal tells the agent to note the value instead.
func pointsAtProtectedValueNote(message string) bool {
	return strings.Contains(message, "library_report_series") && strings.Contains(message, "protected_value_doubt")
}

func TestAgentNotesAProtectedValueInsteadOfChangingIt(t *testing.T) {
	store, cfg := fixture(t)
	// The owner applied a Bangumi proposal long ago and then changed the publisher by hand.
	lib, series := identifiedSeries(t, store, `<ComicInfo><Series>Monster</Series></ComicInfo>`)
	web := openIdentificationWeb(t, store, cfg)
	defer web.process.Close()
	web.setupAdmin()
	agent := web.mainLoopAgent(cfg, 1, []int64{lib.ID}, []string{delegated.Read, delegated.MetadataWrite})

	// Refusals of a person's edit, a ComicInfo value or a lock point at the note.
	if got := agent.correct("owner-edit", series.ID, "publisher", "Shogakukan", "the colophon names Shogakukan"); !pointsAtProtectedValueNote(got.Message) {
		t.Fatalf("refusal of a person's edit %+v", got)
	}
	if got := agent.correct("locked", series.ID, "title", "怪物", "Bangumi names it 怪物"); !pointsAtProtectedValueNote(got.Message) {
		t.Fatalf("refusal of a locked field %+v", got)
	}
	before := web.series(series.ID)

	got, result := agent.report(series.ID, "protected_value_doubt", "the publisher is Shogakukan by the colophon, not the one set")
	if result.IsError || got != (seriesReport{SeriesID: series.ID, Category: "protected_value_doubt", Recorded: true, Status: "identified"}) {
		t.Fatalf("note %+v %s", got, resultText(result))
	}
	notes := web.ownerVerdicts(series.ID).Escalations
	if len(notes) != 1 || notes[0].Category != "protected_value_doubt" || notes[0].Class != "note" {
		t.Fatalf("note %+v", notes)
	}
	if after := web.series(series.ID); !reflect.DeepEqual(after, before) {
		t.Fatalf("the note changed the series %+v, was %+v", after, before)
	}
	// Like every note, one the owner closed is not raised again.
	closeNotes(t, store, series.ID, "protected_value_doubt")
	if again, _ := agent.report(series.ID, "protected_value_doubt", "still the wrong publisher"); again.Recorded || again.Status != "identified" {
		t.Fatalf("note after the owner closed it %+v", again)
	}
}

func TestRefusedCoverImprovementPointsAtTheProtectedValueNote(t *testing.T) {
	w := openCoverWorld(t)
	w.exec(`UPDATE books SET cover_locked = 1 WHERE id = ?`, w.book)
	var out struct {
		Outcome string `json:"outcome"`
		Message string `json:"message"`
	}
	input := map[string]any{"request_key": "locked", "series_id": w.series, "book_id": w.book, "page": 2, "reason": "page 2 is the full cover art"}
	if result := w.agent.call("library_improve_cover", input, &out); result.IsError || out.Outcome != "conflict" || !pointsAtProtectedValueNote(out.Message) {
		t.Fatalf("protected cover %+v %s", out, resultText(result))
	}
	w.exec(`UPDATE books SET cover_locked = 0 WHERE id = ?`, w.book)
	out.Message = ""
	if result := w.agent.call("library_improve_cover", map[string]any{"request_key": "free", "series_id": w.series, "book_id": w.book, "page": 2, "reason": "page 2 is the full cover art"}, &out); result.IsError || out.Outcome != "changed" || out.Message != "" {
		t.Fatalf("improved cover %+v %s", out, resultText(result))
	}
}
