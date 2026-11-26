// These tests guard the owner's pending list of the assembled process: what agents escalated and
// noted, with its evidence and reasons, and the state each decision the owner makes there records.

package bootstrap_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/metadata"
	"yomifolio/internal/task"
)

// pendingPage mirrors the owner's pending list.
type pendingPage struct {
	Items []struct {
		SeriesID       int64  `json:"series_id"`
		LibraryName    string `json:"library_name"`
		DirectoryName  string `json:"directory_name"`
		Title          string `json:"title"`
		BookCount      int    `json:"book_count"`
		Identification struct {
			Status      string `json:"status"`
			Escalations []struct {
				ID       int64  `json:"id"`
				Category string `json:"category"`
				Class    string `json:"class"`
				Reason   string `json:"reason"`
				Clue     string `json:"clue"`
				RaisedBy struct {
					Kind         string `json:"kind"`
					CredentialID string `json:"credential_id"`
				} `json:"raised_by"`
			} `json:"escalations"`
			Evidence *evidencePacket `json:"evidence"`
		} `json:"identification"`
	} `json:"items"`
	Total      int `json:"total"`
	NextOffset int `json:"next_offset"`
	Categories []struct {
		Category string `json:"category"`
		Class    string `json:"class"`
		Series   int    `json:"series"`
	} `json:"categories"`
}

func (p pendingPage) ids() []int64 {
	out := []int64{}
	for _, item := range p.Items {
		out = append(out, item.SeriesID)
	}
	return out
}

// decide posts one owner decision and decodes the answer when it has the wanted status.
func (w *identificationWeb) decide(path, body string, want int, out any) {
	w.t.Helper()
	rec := w.raw("POST", path, body)
	if rec.Code != want {
		w.t.Fatalf("POST %s %s: %d %s", path, body, rec.Code, rec.Body)
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			w.t.Fatalf("POST %s: %v %s", path, err, rec.Body)
		}
	}
}

// conflict posts one owner decision the server must refuse as conflicting, with code.
func (w *identificationWeb) conflict(path, body, code string) {
	w.t.Helper()
	var refused struct {
		Code string `json:"code"`
	}
	w.decide(path, body, http.StatusConflict, &refused)
	if refused.Code != code {
		w.t.Fatalf("POST %s %s refused with %q want %q", path, body, refused.Code, code)
	}
}

func seriesPath(id int64, action string) string {
	return fmt.Sprintf("/api/identification/series/%d/%s", id, action)
}

func (w *identificationWeb) pending(query string) pendingPage {
	w.t.Helper()
	var out pendingPage
	w.call("GET", "/api/identification/pending"+query, http.StatusOK, &out)
	return out
}

// pendingFixture prefetches one library of series "Title 1".."Title n", whose sources answer with
// answer, and connects a main-loop agent that may write to it.
func pendingFixture(t *testing.T, n int, answer func(source, query string) ([]*metadata.SeriesMetadata, error)) (*identificationWeb, *mainLoopAgent, *handClock, []int64) {
	t.Helper()
	store, cfg := fixture(t)
	unpaced(t, cfg)
	directories := make([]string, n)
	for i := range directories {
		directories[i] = fmt.Sprintf("Title %d", i+1)
	}
	library, ids := claimLibrary(t, store, "Pending", directories...)
	clock := &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	web := openClaimWeb(t, store, cfg, &recordingSources{answer: answer}, clock)
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	return web, agent, clock, ids
}

// escalate claims the series and answers each with one verdict, a minute apart.
func (a *mainLoopAgent) escalate(clock *handClock, verdicts ...map[string]any) {
	a.t.Helper()
	batch := a.claim(map[string]any{})
	for i, v := range verdicts {
		clock.advance(time.Minute)
		if r := a.submit(fmt.Sprintf("escalate-%d-%d", i, v["series_id"]), batch.LeaseID, v); !r[0].Accepted {
			a.t.Fatalf("verdict %v refused %+v", v, r[0])
		}
	}
}

func TestPendingListsEscalationsAndNotesWithEvidence(t *testing.T) {
	web, agent, clock, ids := pendingFixture(t, 4, candidatesBySeries)
	agent.escalate(clock,
		escalateVerdict(ids[3], "uncertain_edition", "two printings share the title"),
		escalateVerdict(ids[0], "ambiguous_candidates", "two candidates fit equally"),
		escalateVerdict(ids[1], "tag_problem", "tags name the shop"))

	page := web.pending("")
	// Oldest escalation first; the untouched series is not waiting for the owner.
	if !reflect.DeepEqual(page.ids(), []int64{ids[3], ids[0], ids[1]}) || page.Total != 3 || page.NextOffset != 0 {
		t.Fatalf("pending page %+v", page)
	}
	first := page.Items[1]
	if first.LibraryName != "Pending" || first.DirectoryName != "Title 1" || first.Title != "Title 1" || first.BookCount != 1 ||
		first.Identification.Status != "awaiting_owner" {
		t.Fatalf("escalated series %+v", first)
	}
	if e := first.Identification.Escalations; len(e) != 1 || e[0].Category != "ambiguous_candidates" || e[0].Class != "identification" ||
		e[0].Reason != "two candidates fit equally" || e[0].RaisedBy.Kind != "agent" || e[0].RaisedBy.CredentialID != agent.credential {
		t.Fatalf("escalation shown %+v", e)
	}
	if ev := first.Identification.Evidence; ev == nil || len(ev.Candidates) != 2 || ev.Candidates[0].ID != "bangumi:1" || ev.Candidates[1].ID != "anilist:1" {
		t.Fatalf("evidence shown %+v", ev)
	}
	if note := page.Items[2]; note.Identification.Status != "unprocessed" || len(note.Identification.Escalations) != 1 ||
		note.Identification.Escalations[0].Class != "note" || note.Identification.Escalations[0].Reason != "tags name the shop" {
		t.Fatalf("note shown %+v", note)
	}
	wantCategories := []struct {
		Category string `json:"category"`
		Class    string `json:"class"`
		Series   int    `json:"series"`
	}{{"ambiguous_candidates", "identification", 1}, {"uncertain_edition", "identification", 1}, {"tag_problem", "note", 1}}
	if !reflect.DeepEqual(page.Categories, wantCategories) {
		t.Fatalf("categories %+v", page.Categories)
	}

	// A category narrows the list but not the counts the filter shows.
	if narrowed := web.pending("?category=tag_problem"); !reflect.DeepEqual(narrowed.ids(), []int64{ids[1]}) || narrowed.Total != 1 ||
		!reflect.DeepEqual(narrowed.Categories, wantCategories) {
		t.Fatalf("narrowed page %+v", narrowed)
	}
	if paged := web.pending("?limit=2"); !reflect.DeepEqual(paged.ids(), []int64{ids[3], ids[0]}) || paged.Total != 3 || paged.NextOffset != 2 {
		t.Fatalf("first page %+v", paged)
	}
	if rest := web.pending("?offset=2&limit=2"); !reflect.DeepEqual(rest.ids(), []int64{ids[1]}) || rest.NextOffset != 0 {
		t.Fatalf("last page %+v", rest)
	}
	if counts := web.pending("?limit=0"); len(counts.Items) != 0 || counts.Total != 3 {
		t.Fatalf("counts only %+v", counts)
	}
	web.call("GET", "/api/identification/pending?limit=51", http.StatusBadRequest, nil)
	web.call("GET", "/api/identification/pending?offset=-1", http.StatusBadRequest, nil)
}

// rivalCandidates answers Bangumi with two entries for "Title 1" and AniList with one, each with
// its own summary, and every other query with nothing; MangaDex is down.
func rivalCandidates(source, query string) ([]*metadata.SeriesMetadata, error) {
	switch {
	case source == "mangadex":
		return nil, fmt.Errorf("%s is down", source)
	case source == "bangumi" && query == "Title 1":
		return []*metadata.SeriesMetadata{
			{Title: "中文名", OriginalTitle: "Title 1", Summary: "Bangumi summary", SourceID: 1, SourceURL: "https://bgm.tv/subject/1"},
			{Title: "Title 1 Remake", Summary: "Remake summary", SourceID: 2, SourceURL: "https://bgm.tv/subject/2"},
		}, nil
	case source == "anilist" && query == "Title 1":
		return []*metadata.SeriesMetadata{{Title: "English Title", Summary: "AniList summary", SourceID: 1, SourceURL: "https://anilist.co/manga/1"}}, nil
	case source == "bangumi" && query == "Title 2":
		return []*metadata.SeriesMetadata{{Title: "Title 2", SourceID: 3, SourceURL: "https://bgm.tv/subject/3"}}, nil
	}
	return nil, nil
}

// ownerIdentified mirrors the answer to the owner picking candidates.
type ownerIdentified struct {
	Identification seriesIdentification `json:"identification"`
	OperationIDs   []string             `json:"operation_ids"`
}

func TestOwnerPicksCandidatesAndTheSeriesIsSupplemented(t *testing.T) {
	web, agent, clock, ids := pendingFixture(t, 2, rivalCandidates)
	agent.escalate(clock,
		escalateVerdict(ids[0], "ambiguous_candidates", "the original and the remake both fit"),
		escalateVerdict(ids[1], "missing_from_sources", "the only candidate is another work"))
	clock.advance(time.Hour)

	var picked ownerIdentified
	web.decide(seriesPath(ids[0], "identify"), `{"candidate_ids":["bangumi:1","anilist:1"]}`, http.StatusOK, &picked)
	got := picked.Identification
	// One receipt per candidate, the Bangumi one first by source priority.
	if got.Status != "identified" || len(got.Conclusions) != 2 || len(picked.OperationIDs) != 2 {
		t.Fatalf("picked %+v", picked)
	}
	for i, want := range []struct{ source, entry, url string }{{"bangumi", "1", "https://bgm.tv/subject/1"}, {"anilist", "1", "https://anilist.co/manga/1"}} {
		c := got.Conclusions[i]
		if c.Source != want.source || c.EntryID != want.entry || c.EntryURL != want.url || c.Confirmer.Kind != "owner" || c.Confirmer.UserID != 1 ||
			c.Confirmer.CredentialID != "" || c.ConfirmedAt != "2026-03-01T10:02:00Z" {
			t.Fatalf("owner conclusion %d %+v", i, c)
		}
	}
	// The pick supplemented the series at once: the placeholder title gave way to the Bangumi name.
	if s := web.series(ids[0]); s.Title.String != "中文名" || s.Summary.String != "Bangumi summary" {
		t.Fatalf("supplemented series %+v", s)
	}
	var supplemented receipt
	web.call("GET", "/api/mcp/operations/"+picked.OperationIDs[0], http.StatusOK, &supplemented)
	if supplemented.Kind != "enrich" || !supplemented.changed("title") || !supplemented.changed("summary") {
		t.Fatalf("receipt %+v", supplemented)
	}
	// The escalation is settled and the series has left the list.
	if open := web.ownerVerdicts(ids[0]).Escalations; len(open) != 0 {
		t.Fatalf("escalation still open %+v", open)
	}
	if page := web.pending(""); !reflect.DeepEqual(page.ids(), []int64{ids[1]}) {
		t.Fatalf("pending after the pick %+v", page.ids())
	}

	// A second pick finds the series decided; a candidate that is not in the evidence is not offered.
	web.conflict(seriesPath(ids[0], "identify"), `{"candidate_ids":["bangumi:2"]}`, "not_pending")
	web.conflict(seriesPath(ids[1], "identify"), `{"candidate_ids":["bangumi:9"]}`, "candidate_unavailable")
	for _, malformed := range []string{`{}`, `{"candidate_ids":[]}`, `{"candidate_ids":["bangumi:3","bangumi:4"]}`, `{"candidate_ids":["nowhere:3"]}`, `not json`} {
		web.decide(seriesPath(ids[1], "identify"), malformed, http.StatusBadRequest, nil)
	}
	web.decide(seriesPath(999999, "identify"), `{"candidate_ids":["bangumi:3"]}`, http.StatusNotFound, nil)
	if got := web.identification(ids[1]); got.Status != "awaiting_owner" || len(got.Conclusions) != 0 {
		t.Fatalf("refused picks changed the series %+v", got)
	}
}

// fullIdentification mirrors the owner's identification route with its evidence and verdicts.
type fullIdentification struct {
	seriesIdentification
	Evidence *evidencePacket `json:"evidence"`
	ownerVerdicts
}

func TestOwnerExcludesACandidateFromTheEvidence(t *testing.T) {
	web, agent, clock, ids := pendingFixture(t, 1, rivalCandidates)
	agent.escalate(clock, escalateVerdict(ids[0], "ambiguous_candidates", "the original and the remake both fit"))
	clock.advance(time.Hour)

	var got fullIdentification
	web.decide(seriesPath(ids[0], "exclusions"), `{"candidate_id":"bangumi:2"}`, http.StatusOK, &got)
	if got.Status != "awaiting_owner" || len(got.Excluded) != 1 {
		t.Fatalf("after the exclusion %+v", got)
	}
	if x := got.Excluded[0]; x.Source != "bangumi" || x.EntryID != "2" || x.EntryURL != "https://bgm.tv/subject/2" || x.Title != "Title 1 Remake" || x.ExcludedAt != "2026-03-01T10:01:00Z" {
		t.Fatalf("excluded candidate %+v", x)
	}
	var offered []string
	for _, c := range got.Evidence.Candidates {
		offered = append(offered, c.ID)
	}
	if !reflect.DeepEqual(offered, []string{"bangumi:1", "anilist:1"}) {
		t.Fatalf("evidence still offers %v", offered)
	}
	// The series still waits for the owner, and the excluded entry is gone from what the list shows.
	page := web.pending("")
	if !reflect.DeepEqual(page.ids(), ids) || len(page.Items[0].Identification.Evidence.Candidates) != 2 {
		t.Fatalf("pending after the exclusion %+v", page)
	}
	// Excluding it again changes nothing; it can no longer be picked.
	web.decide(seriesPath(ids[0], "exclusions"), `{"candidate_id":"bangumi:2"}`, http.StatusOK, &got)
	if len(got.Excluded) != 1 {
		t.Fatalf("excluded twice %+v", got.Excluded)
	}
	web.conflict(seriesPath(ids[0], "identify"), `{"candidate_ids":["bangumi:2"]}`, "candidate_unavailable")
	web.conflict(seriesPath(ids[0], "exclusions"), `{"candidate_id":"bangumi:9"}`, "candidate_unavailable")
	web.decide(seriesPath(ids[0], "exclusions"), `{"candidate_id":"bangumi"}`, http.StatusBadRequest, nil)
}

func TestOwnerConcludesNotFoundWithTheSourcesSearched(t *testing.T) {
	web, agent, clock, ids := pendingFixture(t, 2, rivalCandidates)
	agent.escalate(clock,
		escalateVerdict(ids[1], "missing_from_sources", "the only candidate is another work"),
		escalateVerdict(ids[0], "ambiguous_candidates", "the original and the remake both fit"))
	clock.advance(time.Hour)

	var got fullIdentification
	web.decide(seriesPath(ids[1], "not-found"), ``, http.StatusOK, &got)
	if got.Status != "not_found" || len(got.Escalations) != 0 || got.NotFound == nil {
		t.Fatalf("after the not-found decision %+v", got)
	}
	// The sources that answered are the ones searched; MangaDex was down.
	if nf := got.NotFound; !reflect.DeepEqual(nf.SearchedSources, []string{"bangumi", "anilist"}) || nf.Confirmer.Kind != "owner" || nf.Confirmer.CredentialID != "" {
		t.Fatalf("not-found verdict %+v", nf)
	}
	if page := web.pending(""); !reflect.DeepEqual(page.ids(), []int64{ids[0]}) {
		t.Fatalf("pending after the not-found decision %v", page.ids())
	}
	web.conflict(seriesPath(ids[1], "not-found"), ``, "not_pending")
	web.conflict(seriesPath(ids[1], "identify"), `{"candidate_ids":["bangumi:3"]}`, "not_pending")
}

func notePath(series, note int64) string {
	return fmt.Sprintf("/api/identification/series/%d/notes/%d/close", series, note)
}

func TestOwnerClosesNotesWithoutChangingTheStatus(t *testing.T) {
	web, agent, clock, ids := pendingFixture(t, 2, candidatesBySeries)
	agent.escalate(clock,
		escalateVerdict(ids[0], "tag_problem", "tags name the shop"),
		escalateVerdict(ids[1], "suspected_duplicate", "same work as series 40"),
		escalateVerdict(ids[0], "ambiguous_candidates", "two candidates fit equally"))
	page := web.pending("")
	escalations := map[string]int64{}
	for _, item := range page.Items {
		for _, e := range item.Identification.Escalations {
			escalations[e.Category] = e.ID
		}
	}
	if len(escalations) != 3 {
		t.Fatalf("escalations shown %+v", page)
	}

	var got fullIdentification
	web.decide(notePath(ids[1], escalations["suspected_duplicate"]), ``, http.StatusOK, &got)
	if got.Status != "unprocessed" || len(got.Escalations) != 0 {
		t.Fatalf("after closing the only note %+v", got)
	}
	web.decide(notePath(ids[0], escalations["tag_problem"]), ``, http.StatusOK, &got)
	if got.Status != "awaiting_owner" || len(got.Escalations) != 1 || got.Escalations[0].Category != "ambiguous_candidates" {
		t.Fatalf("after closing the note beside an escalation %+v", got)
	}
	// The series whose only note is closed has left the list; the other still waits on its escalation.
	if page := web.pending(""); !reflect.DeepEqual(page.ids(), []int64{ids[0]}) {
		t.Fatalf("pending after closing notes %v", page.ids())
	}
	if again := web.pending("?category=tag_problem"); again.Total != 0 {
		t.Fatalf("a closed note still listed %+v", again)
	}

	// Closing it again changes nothing; only notes close, and only on their own series.
	web.decide(notePath(ids[1], escalations["suspected_duplicate"]), ``, http.StatusOK, &got)
	web.conflict(notePath(ids[0], escalations["ambiguous_candidates"]), ``, "not_a_note")
	web.decide(notePath(ids[0], escalations["suspected_duplicate"]), ``, http.StatusNotFound, nil)
	web.decide(notePath(ids[0], 999999), ``, http.StatusNotFound, nil)
	if got := web.identification(ids[0]); got.Status != "awaiting_owner" {
		t.Fatalf("refused closes changed the series %+v", got)
	}
}

func TestOwnerResearchesAndReviewsSeriesOnTheList(t *testing.T) {
	web, agent, clock, ids := pendingFixture(t, 2, candidatesBySeries)
	agent.escalate(clock,
		escalateVerdict(ids[0], "missing_from_sources", "no candidate is this work"),
		escalateVerdict(ids[1], "directory_grouping", "the directory mixes two works"),
		escalateVerdict(ids[1], "uncertain_edition", "two printings share the title"))

	// A custom search joins the evidence the list shows, and the series keeps waiting.
	if out := web.research(ids[0], `{"query":"Title 7"}`, http.StatusOK); out.Status != "completed" || !reflect.DeepEqual(out.Added, []string{"bangumi:7", "anilist:7"}) {
		t.Fatalf("research %+v", out)
	}
	page := web.pending("")
	var offered []string
	for _, c := range page.Items[0].Identification.Evidence.Candidates {
		offered = append(offered, c.ID)
	}
	if page.Items[0].SeriesID != ids[0] || page.Items[0].Identification.Status != "awaiting_owner" || !reflect.DeepEqual(offered, []string{"bangumi:1", "bangumi:7", "anilist:1", "anilist:7"}) {
		t.Fatalf("pending after the research %+v", page.Items[0])
	}
	web.decide(seriesPath(ids[0], "identify"), `{"candidate_ids":["bangumi:7"]}`, http.StatusOK, nil)

	// Owner-reviewed, the series no longer waits on its escalation; its note still waits.
	web.call("PUT", seriesPath(ids[1], "owner-review"), http.StatusOK, nil)
	page = web.pending("")
	if !reflect.DeepEqual(page.ids(), []int64{ids[1]}) || page.Items[0].Identification.Status != "owner_reviewed" ||
		len(page.Items[0].Identification.Escalations) != 1 || page.Items[0].Identification.Escalations[0].Category != "directory_grouping" {
		t.Fatalf("pending with an owner-reviewed series %+v", page)
	}
	if narrowed := web.pending("?category=uncertain_edition"); narrowed.Total != 0 {
		t.Fatalf("an owner-reviewed series waits on its escalation %+v", narrowed)
	}
	web.conflict(seriesPath(ids[1], "not-found"), ``, "not_pending")
	// Withdrawn, it waits on the escalation it had again.
	web.call("DELETE", seriesPath(ids[1], "owner-review"), http.StatusOK, nil)
	if narrowed := web.pending("?category=uncertain_edition"); !reflect.DeepEqual(narrowed.ids(), []int64{ids[1]}) || narrowed.Items[0].Identification.Status != "awaiting_owner" {
		t.Fatalf("after withdrawing owner-reviewed %+v", narrowed)
	}
}

func TestPendingListAndDecisionsNeedAnAdministrator(t *testing.T) {
	web, agent, clock, ids := pendingFixture(t, 1, rivalCandidates)
	agent.escalate(clock,
		escalateVerdict(ids[0], "tag_problem", "tags name the shop"),
		escalateVerdict(ids[0], "ambiguous_candidates", "the original and the remake both fit"))
	note := web.pending("").Items[0].Identification.Escalations[0]
	if note.Category != "tag_problem" {
		t.Fatalf("escalations %+v", web.pending(""))
	}

	if rec := web.raw("POST", "/api/users", `{"username":"reader","password":"initialpass"}`); rec.Code != http.StatusOK {
		t.Fatalf("create reader %d %s", rec.Code, rec.Body)
	}
	web.loginAs("reader", "initialpass")
	if rec := web.raw("POST", "/api/auth/change-password", `{"current_password":"initialpass","new_password":"readerpass"}`); rec.Code != http.StatusOK {
		t.Fatalf("change password %d %s", rec.Code, rec.Body)
	}
	web.loginAs("reader", "readerpass")
	web.call("GET", "/api/identification/pending", http.StatusForbidden, nil)
	for path, body := range map[string]string{
		seriesPath(ids[0], "identify"):   `{"candidate_ids":["bangumi:1"]}`,
		seriesPath(ids[0], "exclusions"): `{"candidate_id":"bangumi:2"}`,
		seriesPath(ids[0], "not-found"):  ``,
		notePath(ids[0], note.ID):        ``,
	} {
		web.decide(path, body, http.StatusForbidden, nil)
	}
	web.loginAs("admin", "password123")
	if got := web.ownerVerdicts(ids[0]); len(got.Escalations) != 2 || got.NotFound != nil {
		t.Fatalf("a regular account decided %+v", got)
	}
	if got := web.identification(ids[0]); got.Status != "awaiting_owner" || len(got.Conclusions) != 0 || len(got.Excluded) != 0 {
		t.Fatalf("a regular account decided %+v", got)
	}
}

// questionedLibrary prefetches one library of series "Title 1".."Title n" for an agent that may
// write to it, and returns the library so a test can add books and scan it.
func questionedLibrary(t *testing.T, n int) (*identificationWeb, *mainLoopAgent, *handClock, *prefetchLibrary, []int64) {
	t.Helper()
	store, cfg := fixture(t)
	unpaced(t, cfg)
	directories := make([]string, n)
	for i := range directories {
		directories[i] = fmt.Sprintf("Title %d", i+1)
	}
	library, ids := claimLibrary(t, store, "Questioned", directories...)
	clock := &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	web := openClaimWeb(t, store, cfg, &recordingSources{answer: candidatesBySeries}, clock)
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	return web, agent, clock, library, ids
}

// addBook writes one more book with ComicInfo naming series into a series directory and scans the
// library for it.
func (w *identificationWeb) addBook(library *prefetchLibrary, directory, file, series string) {
	w.t.Helper()
	writeComicArchive(w.t, filepath.Join(library.lib.Path, directory, file), `<ComicInfo><Series>`+series+`</Series></ComicInfo>`)
	if run := w.scan(library.lib.ID, false); run.Status != task.StatusCompleted {
		w.t.Fatalf("scan %+v", run)
	}
}

func TestOwnerKeepsAQuestionedIdentificationAndItsName(t *testing.T) {
	web, agent, clock, library, ids := questionedLibrary(t, 2)
	batch := agent.claim(map[string]any{})
	for i, r := range agent.submit("identify", batch.LeaseID,
		identifyVerdict(ids[0], "the Bangumi title equals the directory", "bangumi:1"),
		identifyVerdict(ids[1], "the Bangumi title equals the directory", "bangumi:2")) {
		wantResult(t, r, ids[i], true, "identified")
	}
	clock.advance(time.Hour)
	web.addBook(library, "Title 1", "02.cbz", "Pluto")

	page := web.pending("?category=new_book_contradiction")
	if !reflect.DeepEqual(page.ids(), []int64{ids[0]}) || page.Items[0].Identification.Status != "needs_recheck" {
		t.Fatalf("recheck on the list %+v", page)
	}
	if e := page.Items[0].Identification.Escalations; len(e) != 1 || e[0].Class != "recheck" || e[0].Clue != "Pluto" {
		t.Fatalf("recheck escalation %+v", e)
	}
	// Only a series needing a recheck can be kept as it is.
	web.conflict(seriesPath(ids[1], "keep-identification"), ``, "not_pending")

	clock.advance(time.Hour)
	var kept fullIdentification
	web.decide(seriesPath(ids[0], "keep-identification"), ``, http.StatusOK, &kept)
	if kept.Status != "identified" || len(kept.Escalations) != 0 || len(kept.Conclusions) != 1 {
		t.Fatalf("kept identification %+v", kept)
	}
	// The questioned agent conclusion is the owner's now, its reason kept.
	if c := kept.Conclusions[0]; c.EntryID != "1" || c.Confirmer.Kind != "owner" || c.Confirmer.UserID != 1 || c.Confirmer.CredentialID != "" ||
		c.ConfirmedAt != "2026-03-01T11:00:00Z" || c.Reason != "the Bangumi title equals the directory" {
		t.Fatalf("kept conclusion %+v", c)
	}
	if page := web.pending(""); page.Total != 0 {
		t.Fatalf("pending after keeping %+v", page)
	}

	// The name the owner accepted no longer questions the series, however it is written; another
	// name still does.
	web.addBook(library, "Title 1", "03.cbz", "PLUTO ")
	if got := web.identification(ids[0]); got.Status != "identified" {
		t.Fatalf("an accepted name questioned the series again: %+v", got)
	}
	web.addBook(library, "Title 1", "04.cbz", "Astro Boy")
	page = web.pending("")
	if !reflect.DeepEqual(page.ids(), []int64{ids[0]}) || page.Items[0].Identification.Escalations[0].Clue != "Astro Boy" {
		t.Fatalf("a new name on the list %+v", page)
	}
	// The name accepted for one series accepts nothing for another.
	web.addBook(library, "Title 2", "02.cbz", "Pluto")
	if got := web.identification(ids[1]); got.Status != "needs_recheck" {
		t.Fatalf("another series with the accepted name: %+v", got)
	}
}
