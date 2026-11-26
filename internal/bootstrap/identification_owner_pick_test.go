// These tests guard the owner's own identifications from the web: picking an entry in a source's
// manual search records an owner-confirmed identification and supplements the series by the
// identification policy instead of queueing a proposal, and picking another entry of an identified
// source replaces that identification, excluding the old entry and taking back what it brought.

package bootstrap_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/task"
)

// ownerPick mirrors the response of the owner's identification route.
type ownerPick struct {
	Identification seriesIdentification `json:"identification"`
	Replaced       *struct {
		Source  string `json:"source"`
		EntryID string `json:"entry_id"`
	} `json:"replaced"`
	RetractionID string `json:"retraction_id"`
	OperationID  string `json:"operation_id"`
}

// manualSearch runs the series' manual search on one source, as the search dialog does, and
// returns the results it lists.
func (w *identificationWeb) manualSearch(series int64, source, query string) []json.RawMessage {
	w.t.Helper()
	var out struct {
		Results []json.RawMessage `json:"results"`
	}
	w.call("GET", fmt.Sprintf("/api/series/%d/scrape-search?provider=%s&q=%s", series, source, url.QueryEscape(query)), http.StatusOK, &out)
	return out.Results
}

// pickEntry records the owner's pick of one manual search result for the series.
func (w *identificationWeb) pickEntry(series int64, source string, result json.RawMessage, want int) ownerPick {
	w.t.Helper()
	body, _ := json.Marshal(map[string]json.RawMessage{"result": result})
	rec := w.raw("PUT", fmt.Sprintf("/api/identification/series/%d/conclusions/%s", series, source), string(body))
	if rec.Code != want {
		w.t.Fatalf("pick %s for series %d: %d %s", source, series, rec.Code, rec.Body)
	}
	var out ownerPick
	if want == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			w.t.Fatal(err)
		}
	}
	return out
}

// reviewCount counts the proposals of every status a series has.
func reviewCount(t *testing.T, store database.Store, series int64) int {
	t.Helper()
	var n int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM metadata_reviews WHERE series_id = ?`, series).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestOwnerPickInManualSearchIdentifiesAndSupplementsTheSeries(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Manual")
	id := library.series("Title 1", "", "01.cbz")
	setSeries(t, store, id, func(p *database.UpdateSeriesMetadataParams) {
		p.Title, p.LockedFields = sqlText("Title 1"), sqlText("title")
	})
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{
		"bangumi": {"Monster": {Title: "怪物", OriginalTitle: "MONSTER", Summary: "Source summary", Publisher: "Shogakukan", SourceID: 7, SourceURL: "https://bgm.tv/subject/7"}},
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})

	results := web.manualSearch(id, "bangumi", "Monster")
	if len(results) != 1 {
		t.Fatalf("manual search results %s", results)
	}
	pick := web.pickEntry(id, "bangumi", results[0], http.StatusOK)

	got := pick.Identification
	if got.Status != "identified" || len(got.Conclusions) != 1 {
		t.Fatalf("identification after the pick %+v", got)
	}
	c := got.Conclusions[0]
	if c.Source != "bangumi" || c.EntryID != "7" || c.EntryURL != "https://bgm.tv/subject/7" || c.Title != "怪物" || c.Confirmer.Kind != "owner" || c.Confirmer.UserID != 1 {
		t.Fatalf("owner conclusion %+v", c)
	}
	if pick.OperationID == "" || pick.Replaced != nil || pick.RetractionID != "" {
		t.Fatalf("pick receipts %+v", pick)
	}
	if again := web.identification(id); again.Status != "identified" || len(again.Conclusions) != 1 {
		t.Fatalf("stored identification %+v", again)
	}
	// The pick supplements by the identification policy: the directory-name placeholder takes the
	// source title under its lock, and empty fields fill. No proposal is queued for review.
	series := web.series(id)
	if series.Title.String != "怪物" || series.LockedFields.String != "title" || series.Summary.String != "Source summary" || series.Publisher.String != "Shogakukan" {
		t.Fatalf("series after the pick %+v", series)
	}
	if n := reviewCount(t, store, id); n != 0 {
		t.Fatalf("the pick queued %d proposals", n)
	}
}

// seriesLinks reads the source links the series page shows, by link name.
func (w *identificationWeb) seriesLinks(id int64) map[string]string {
	w.t.Helper()
	var view struct {
		Links []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"links"`
	}
	w.call("GET", fmt.Sprintf("/api/series/%d/context", id), http.StatusOK, &view)
	out := map[string]string{}
	for _, l := range view.Links {
		out[l.Name] = l.URL
	}
	return out
}

// ownerReceipt reads one metadata operation receipt as the owner's operations page does.
func (w *identificationWeb) ownerReceipt(id string) receipt {
	w.t.Helper()
	var out receipt
	w.call("GET", "/api/mcp/operations/"+id, http.StatusOK, &out)
	return out
}

func TestOwnerReplacingAnIdentificationExcludesTheOldEntryAndTakesBackWhatItBrought(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Replace")
	id := library.series("Title 1", "", "01.cbz")
	setSeries(t, store, id, func(p *database.UpdateSeriesMetadataParams) {
		p.Title, p.LockedFields = sqlText("Title 1"), sqlText("title")
	})
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{
		"bangumi": {
			"Title 1": {Title: "错的作品", Summary: "Wrong summary", Publisher: "Wrong publisher", SiteRating: &metadata.SiteRating{Value: 6, Scale: 10},
				Authors: []metadata.SeriesAuthor{{Name: "Wrong Writer", Role: "Writer"}}, SourceID: 1, SourceURL: "https://bgm.tv/subject/1"},
			"Monster": {Title: "怪物", Summary: "Right summary", Publisher: "Shogakukan", SiteRating: &metadata.SiteRating{Value: 9, Scale: 10},
				Authors: []metadata.SeriesAuthor{{Name: "Urasawa", Role: "Writer"}}, SourceID: 7, SourceURL: "https://bgm.tv/subject/7"},
		},
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	agent.identifyAll("identify", map[int64][]string{id: {"bangumi:1"}})
	// The owner rewrites the summary the wrong entry brought; that value is now the owner's.
	setSeries(t, store, id, func(p *database.UpdateSeriesMetadataParams) { p.Summary = sqlText("Owner summary") })

	pick := web.pickEntry(id, "bangumi", web.manualSearch(id, "bangumi", "Monster")[0], http.StatusOK)

	if pick.Replaced == nil || pick.Replaced.Source != "bangumi" || pick.Replaced.EntryID != "1" || pick.RetractionID == "" || pick.OperationID == "" {
		t.Fatalf("replacement %+v", pick)
	}
	got := pick.Identification
	if got.Status != "identified" || len(got.Conclusions) != 1 || got.Conclusions[0].EntryID != "7" || got.Conclusions[0].Confirmer.Kind != "owner" {
		t.Fatalf("identification after the replacement %+v", got)
	}
	if len(got.Excluded) != 1 || got.Excluded[0].Source != "bangumi" || got.Excluded[0].EntryID != "1" || got.Excluded[0].Title != "错的作品" {
		t.Fatalf("the replaced entry is not excluded: %+v", got.Excluded)
	}
	// What the wrong entry brought and nobody touched is taken back and supplemented from the new
	// entry; the summary the owner rewrote stays.
	series := web.series(id)
	if series.Title.String != "怪物" || series.LockedFields.String != "title" || series.Summary.String != "Owner summary" ||
		series.Publisher.String != "Shogakukan" || series.Rating.Float64 != 9 {
		t.Fatalf("series after the replacement %+v", series)
	}
	if authors := web.seriesAuthors(id); len(authors) != 1 || !authors["Urasawa/Writer"] {
		t.Fatalf("authors after the replacement %v", authors)
	}
	if links := web.seriesLinks(id); len(links) != 1 || links["Bangumi"] != "https://bgm.tv/subject/7" {
		t.Fatalf("links after the replacement %v", links)
	}
	retraction := web.ownerReceipt(pick.RetractionID)
	if retraction.Kind != "enrich" || retraction.Outcome != "changed" || !retraction.changed("title") || !retraction.changed("publisher") ||
		!retraction.changed("rating") || !retraction.changed("authors") || !retraction.changed("source_links") || retraction.changed("summary") {
		t.Fatalf("retraction receipt %+v", retraction)
	}
	// The excluded entry no longer reaches an agent, nor the owner's own pick.
	if evidence := web.evidence(id); evidence != nil {
		for _, c := range evidence.Candidates {
			if c.ID == "bangumi:1" {
				t.Fatalf("the excluded entry is still evidence: %+v", evidence.Candidates)
			}
		}
	}
	web.pickEntry(id, "bangumi", web.manualSearch(id, "bangumi", "Title 1")[0], http.StatusConflict)
}

func TestOwnerReplacementKeepsWhatOtherSourcesBroughtAndRecomputesTheRating(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Sources")
	id := library.series("Title 1", "", "01.cbz")
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{
		"bangumi": {
			"Title 1": {Title: "错的作品", Summary: "Wrong summary", SiteRating: &metadata.SiteRating{Value: 6, Scale: 10}, SourceID: 1, SourceURL: "https://bgm.tv/subject/1"},
			"Monster": {Title: "怪物", Summary: "Right summary", SiteRating: &metadata.SiteRating{Value: 9, Scale: 10}, SourceID: 7, SourceURL: "https://bgm.tv/subject/7"},
		},
		"anilist": {"Monster": {Title: "Monster", Publisher: "Viz", SiteRating: &metadata.SiteRating{Value: 80, Scale: 100}, SourceID: 5, SourceURL: "https://anilist.co/manga/5"}},
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	agent.identifyAll("identify", map[int64][]string{id: {"bangumi:1"}})

	// A pick of another source adds to the identification: the formula rating, still untouched,
	// is computed again over both sources.
	added := web.pickEntry(id, "anilist", web.manualSearch(id, "anilist", "Monster")[0], http.StatusOK)
	if added.Replaced != nil || len(added.Identification.Conclusions) != 2 {
		t.Fatalf("adding a source %+v", added)
	}
	if series := web.series(id); series.Rating.Float64 != 7 || series.Publisher.String != "Viz" {
		t.Fatalf("series with two sources %+v", series)
	}

	// Replacing the wrong Bangumi entry takes back the values it brought, even those a later
	// supplementation built on, and supplements again from every source now identified.
	replaced := web.pickEntry(id, "bangumi", web.manualSearch(id, "bangumi", "Monster")[0], http.StatusOK)
	if replaced.Replaced == nil || replaced.Replaced.EntryID != "1" {
		t.Fatalf("replacement %+v", replaced)
	}
	series := web.series(id)
	if series.Title.String != "怪物" || series.Summary.String != "Right summary" || series.Publisher.String != "Viz" || series.Rating.Float64 != 8.5 {
		t.Fatalf("series after the replacement %+v", series)
	}
	want := map[string]string{"Bangumi": "https://bgm.tv/subject/7", "AniList": "https://anilist.co/manga/5"}
	if links := web.seriesLinks(id); !reflect.DeepEqual(links, want) {
		t.Fatalf("links after the replacement %v want %v", links, want)
	}
}

// openEscalations reads the classes and categories of the series' open escalations.
func (w *identificationWeb) openEscalations(series int64) []string {
	w.t.Helper()
	var out struct {
		Escalations []struct {
			Class    string `json:"class"`
			Category string `json:"category"`
		} `json:"escalations"`
	}
	w.call("GET", fmt.Sprintf("/api/identification/series/%d", series), http.StatusOK, &out)
	var open []string
	for _, e := range out.Escalations {
		open = append(open, e.Class+"/"+e.Category)
	}
	return open
}

func TestOwnerPickSettlesTheSeriesEscalationsButKeepsItsNotes(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Escalated")
	id := library.series("Title 1", "", "01.cbz")
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{
		"bangumi": {"Title 1": {Title: "怪物", SourceID: 7, SourceURL: "https://bgm.tv/subject/7"}},
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	batch := agent.claim(map[string]any{})
	results := agent.submit("escalate", batch.LeaseID,
		escalateVerdict(id, "suspected_duplicate", "another series holds the same books"))
	wantResult(t, results[0], id, true, "unprocessed")
	results = agent.submit("escalate-2", batch.LeaseID, escalateVerdict(id, "ambiguous_candidates", "two candidates read the same"))
	wantResult(t, results[0], id, true, "awaiting_owner")

	pick := web.pickEntry(id, "bangumi", web.manualSearch(id, "bangumi", "Title 1")[0], http.StatusOK)

	if pick.Identification.Status != "identified" {
		t.Fatalf("status after the owner's pick %+v", pick.Identification)
	}
	if open := web.openEscalations(id); !reflect.DeepEqual(open, []string{"note/suspected_duplicate"}) {
		t.Fatalf("open escalations after the pick %v", open)
	}
	// The lease the agent held answers nothing any more: the owner decided the series.
	late := agent.submit("late", batch.LeaseID, identifyVerdict(id, "the titles match", "bangumi:7"))
	wantResult(t, late[0], id, false, "not_unprocessed")
}

func TestOwnerPickOfTheIdentifiedEntryMakesItTheOwners(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Confirm")
	id := library.series("Title 1", "", "01.cbz")
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{
		"bangumi": {"Title 1": {Title: "怪物", Summary: "Source summary", SourceID: 7, SourceURL: "https://bgm.tv/subject/7"}},
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	agent.identifyAll("identify", map[int64][]string{id: {"bangumi:7"}})

	pick := web.pickEntry(id, "bangumi", web.manualSearch(id, "bangumi", "Title 1")[0], http.StatusOK)

	got := pick.Identification
	if pick.Replaced != nil || pick.RetractionID != "" || len(got.Excluded) != 0 || len(got.Conclusions) != 1 ||
		got.Conclusions[0].EntryID != "7" || got.Conclusions[0].Confirmer.Kind != "owner" {
		t.Fatalf("confirming the identified entry %+v", pick)
	}
	if series := web.series(id); series.Title.String != "怪物" || series.Summary.String != "Source summary" {
		t.Fatalf("series after confirming %+v", series)
	}
}

func TestOwnerPickOnAnOwnerReviewedSeriesLeavesItsMetadataAlone(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Reviewed")
	id := library.series("Title 1", "", "01.cbz")
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{
		"bangumi": {"Title 1": {Title: "怪物", Summary: "Source summary", SourceID: 7, SourceURL: "https://bgm.tv/subject/7"}},
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	web.call("PUT", fmt.Sprintf("/api/identification/series/%d/owner-review", id), http.StatusOK, nil)

	pick := web.pickEntry(id, "bangumi", web.manualSearch(id, "bangumi", "Title 1")[0], http.StatusOK)

	if pick.Identification.Status != "owner_reviewed" || len(pick.Identification.Conclusions) != 1 || pick.OperationID != "" {
		t.Fatalf("pick on an owner-reviewed series %+v", pick)
	}
	if series := web.series(id); series.Title.Valid || series.Summary.Valid {
		t.Fatalf("the pick supplemented an owner-reviewed series %+v", series)
	}
}

func TestOwnerPickRefusesWhatIsNoIdentification(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Refused")
	id := library.series("Title 1", "", "01.cbz")
	web := openClaimWeb(t, store, cfg, &recordingSources{answer: catalogue(nil)}, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	entry := json.RawMessage(`{"Title":"怪物","SourceID":7,"SourceURL":"https://bgm.tv/subject/7"}`)

	// A model's answer names no catalogue entry, and a result has to name the entry it is.
	web.pickEntry(id, "llm", entry, http.StatusBadRequest)
	web.pickEntry(id, "bangumi", json.RawMessage(`{"Title":"怪物"}`), http.StatusBadRequest)
	web.pickEntry(id, "bangumi", json.RawMessage(`{"Title":"怪物","SourceID":7,"Rating":11}`), http.StatusBadRequest)
	if rec := web.raw("PUT", fmt.Sprintf("/api/identification/series/%d/conclusions/bangumi", id), `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("pick without a result: %d %s", rec.Code, rec.Body)
	}
	web.pickEntry(999999, "bangumi", entry, http.StatusNotFound)
	if got := web.identification(id); got.Status != "unprocessed" || len(got.Conclusions) != 0 {
		t.Fatalf("a refused pick changed the identification %+v", got)
	}

	if rec := web.raw("POST", "/api/users", `{"username":"reader","password":"initialpass"}`); rec.Code != http.StatusOK {
		t.Fatalf("create reader %d %s", rec.Code, rec.Body)
	}
	web.loginAs("reader", "initialpass")
	if rec := web.raw("POST", "/api/auth/change-password", `{"current_password":"initialpass","new_password":"readerpass"}`); rec.Code != http.StatusOK {
		t.Fatalf("change password %d %s", rec.Code, rec.Body)
	}
	web.loginAs("reader", "readerpass")
	web.pickEntry(id, "bangumi", entry, http.StatusForbidden)
}

// browse lists the ids of the library's series the library page shows for a query.
func (w *identificationWeb) browse(library int64, query string, want int) []int64 {
	w.t.Helper()
	rec := w.raw("GET", fmt.Sprintf("/api/series/search?libraryId=%d&%s", library, query), "")
	if rec.Code != want {
		w.t.Fatalf("browse %s: %d %s", query, rec.Code, rec.Body)
	}
	var out struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if want != http.StatusOK {
		return nil
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		w.t.Fatal(err)
	}
	ids := []int64{}
	for _, item := range out.Items {
		ids = append(ids, item.ID)
	}
	if out.Total != len(ids) {
		w.t.Fatalf("browse %s: total %d for %v", query, out.Total, ids)
	}
	return ids
}

func TestLibraryBrowsingFiltersByIdentificationStatus(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Browse")
	identified := library.series("Alpha One", "", "01.cbz")
	unprocessed := library.series("Alpha Two", "", "01.cbz")
	reviewed := library.series("Beta", "", "01.cbz")
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{
		"bangumi": {"Alpha": {Title: "Alpha", SourceID: 7, SourceURL: "https://bgm.tv/subject/7"}},
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	web.pickEntry(identified, "bangumi", web.manualSearch(identified, "bangumi", "Alpha")[0], http.StatusOK)
	web.call("PUT", fmt.Sprintf("/api/identification/series/%d/owner-review", reviewed), http.StatusOK, nil)
	lib := library.lib.ID

	for status, want := range map[string][]int64{
		"identified":     {identified},
		"unprocessed":    {unprocessed},
		"owner_reviewed": {reviewed},
		"not_found":      {},
	} {
		if got := web.browse(lib, "identification="+status, http.StatusOK); !reflect.DeepEqual(got, want) {
			t.Fatalf("identification=%s lists %v want %v", status, got, want)
		}
	}
	// The status combines with the other conditions of the library page.
	if got := web.browse(lib, "identification=unprocessed&q=Beta", http.StatusOK); len(got) != 0 {
		t.Fatalf("unprocessed series named Beta %v", got)
	}
	if got := web.browse(lib, "identification=identified&q=Alpha", http.StatusOK); !reflect.DeepEqual(got, []int64{identified}) {
		t.Fatalf("identified series named Alpha %v", got)
	}
	if got := web.browse(lib, "", http.StatusOK); len(got) != 3 {
		t.Fatalf("unfiltered library %v", got)
	}
	web.browse(lib, "identification=matched", http.StatusBadRequest)

	// Identification is the owner's business: a regular account cannot filter by it.
	if rec := web.raw("POST", "/api/users", `{"username":"reader","password":"initialpass"}`); rec.Code != http.StatusOK {
		t.Fatalf("create reader %d %s", rec.Code, rec.Body)
	}
	web.loginAs("reader", "initialpass")
	if rec := web.raw("POST", "/api/auth/change-password", `{"current_password":"initialpass","new_password":"readerpass"}`); rec.Code != http.StatusOK {
		t.Fatalf("change password %d %s", rec.Code, rec.Body)
	}
	web.loginAs("reader", "readerpass")
	web.browse(lib, "identification=identified", http.StatusForbidden)
	if got := web.browse(lib, "", http.StatusOK); len(got) != 3 {
		t.Fatalf("regular account's library %v", got)
	}
}
