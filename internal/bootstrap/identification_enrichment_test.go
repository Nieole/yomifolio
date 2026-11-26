// These tests guard that an agent's identification immediately supplements the series' metadata by
// the fixed policy and leaves an ordinary enrichment receipt: fields the identification wrote keep
// their source records, so a later scan does not wash them out, and the receipt can be undone.

package bootstrap_test

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/task"
)

// enrichedVerdict mirrors a verdict result with the receipt of its enrichment.
type enrichedVerdict struct {
	SeriesID    int64  `json:"series_id"`
	Accepted    bool   `json:"accepted"`
	Status      string `json:"status"`
	Problem     string `json:"problem"`
	OperationID string `json:"operation_id"`
}

// receipt mirrors the enrichment receipt the operation tools read.
type receipt struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Outcome string `json:"outcome"`
	Fields  []struct {
		Field   string `json:"field"`
		Changed bool   `json:"changed"`
		Formula string `json:"formula"`
		Sources []struct {
			CandidateID string `json:"candidate_id"`
			Source      string `json:"source"`
		} `json:"sources"`
	} `json:"fields"`
	Identification *struct {
		PlaceholderTitle    bool    `json:"placeholder_title"`
		SupersededProposals []int64 `json:"superseded_proposals"`
	} `json:"identification"`
}

func (r receipt) changed(field string) bool {
	for _, f := range r.Fields {
		if f.Field == field {
			return f.Changed
		}
	}
	return false
}

// identifyAll claims every ready series and identifies each with the candidates cite names for it.
func (a *mainLoopAgent) identifyAll(key string, cite map[int64][]string) map[int64]enrichedVerdict {
	a.t.Helper()
	batch := a.claim(map[string]any{})
	var verdicts []map[string]any
	for _, id := range batch.ids() {
		if candidates, ok := cite[id]; ok {
			verdicts = append(verdicts, identifyVerdict(id, "the titles match", candidates...))
		}
	}
	if len(verdicts) != len(cite) {
		a.t.Fatalf("claimed %v, want every series of %v", batch.ids(), cite)
	}
	var out struct {
		Results []enrichedVerdict `json:"results"`
	}
	if result := a.call("library_submit_verdicts", map[string]any{"request_key": key, "lease_id": batch.LeaseID, "verdicts": verdicts}, &out); result.IsError {
		a.t.Fatalf("submission refused: %s", resultText(result))
	}
	results := map[int64]enrichedVerdict{}
	for _, r := range out.Results {
		if !r.Accepted || r.Status != "identified" || r.OperationID == "" {
			a.t.Fatalf("identify verdict %+v", r)
		}
		results[r.SeriesID] = r
	}
	return results
}

func (a *mainLoopAgent) receipt(operation string) receipt {
	a.t.Helper()
	var out receipt
	if result := a.call("library_get_metadata_operation", map[string]any{"operation_id": operation}, &out); result.IsError {
		a.t.Fatalf("receipt %s refused: %s", operation, resultText(result))
	}
	return out
}

// catalogue answers each source with the entries listed for the query, all others with nothing.
func catalogue(entries map[string]map[string]*metadata.SeriesMetadata) func(source, query string) ([]*metadata.SeriesMetadata, error) {
	return func(source, query string) ([]*metadata.SeriesMetadata, error) {
		if hit := entries[source][query]; hit != nil {
			copied := *hit
			return []*metadata.SeriesMetadata{&copied}, nil
		}
		return nil, nil
	}
}

func TestIdentificationFillsEmptyTextFieldsBySourcePriority(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Priority")
	id := library.series("Title 1", "", "01.cbz")
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{
		"bangumi": {"Title 1": {Title: "中文名", OriginalTitle: "日本語名", Publisher: "Bangumi Publisher", Status: "unknown", SourceID: 1, SourceURL: "https://bgm.tv/subject/1"}},
		"anilist": {"Title 1": {Title: "English Title", OriginalTitle: "日本語名", Summary: "AniList summary", Publisher: "AniList Publisher", Status: "完結", SourceID: 1, SourceURL: "https://anilist.co/manga/1"}},
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})

	verdict := agent.identifyAll("identify", map[int64][]string{id: {"bangumi:1", "anilist:1"}})[id]
	got := web.series(id)
	// Each text field comes from the first source in priority order that has a value: Bangumi has
	// no summary and no known status, so AniList supplies them.
	if got.Title.String != "中文名" || got.Summary.String != "AniList summary" || got.Publisher.String != "Bangumi Publisher" || got.Status.String != "completed" {
		t.Fatalf("supplemented series %+v", got)
	}
	r := agent.receipt(verdict.OperationID)
	if r.Kind != "enrich" || r.Outcome != "changed" || !r.changed("title") || !r.changed("summary") || !r.changed("publisher") || !r.changed("status") {
		t.Fatalf("enrichment receipt %+v", r)
	}
	for _, f := range r.Fields {
		if f.Field == "summary" && (len(f.Sources) != 1 || f.Sources[0].CandidateID != "anilist:1") {
			t.Fatalf("summary evidence %+v", f)
		}
	}

	// The supplemented values carry their source records, so a new book's ComicInfo keeps them.
	writeComicArchive(t, library.lib.Path+"/Title 1/02.cbz", `<ComicInfo><Series>Title 1</Series><Summary>ComicInfo summary</Summary><Publisher>ComicInfo publisher</Publisher></ComicInfo>`)
	if run := web.scan(library.lib.ID, false); run.Status != task.StatusCompleted {
		t.Fatalf("scan %+v", run)
	}
	if after := web.series(id); after.Summary.String != "AniList summary" || after.Publisher.String != "Bangumi Publisher" || after.Status.String != "completed" {
		t.Fatalf("a scan washed out supplemented values: %+v", after)
	}
}

// setSeries edits a series' stored metadata the way a scan or an owner's edit leaves it.
func setSeries(t *testing.T, store database.Store, id int64, edit func(*database.UpdateSeriesMetadataParams)) {
	t.Helper()
	ctx := context.Background()
	err := store.ExecTx(ctx, func(q *database.Queries) error {
		current, err := q.GetSeries(ctx, id)
		if err != nil {
			return err
		}
		params := database.UpdateSeriesMetadataParams{ID: id, Title: current.Title, Summary: current.Summary, Publisher: current.Publisher, Status: current.Status,
			Rating: current.Rating, Language: current.Language, LockedFields: current.LockedFields, NameInitial: current.NameInitial}
		edit(&params)
		_, err = q.UpdateSeriesMetadata(ctx, params)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// numbered answers Bangumi with entry n for the series "Title n", carrying the given text.
func numbered(entry func(n int) *metadata.SeriesMetadata) func(source, query string) ([]*metadata.SeriesMetadata, error) {
	return func(source, query string) ([]*metadata.SeriesMetadata, error) {
		var n int
		if _, err := fmt.Sscanf(query, "Title %d", &n); err != nil || source != "bangumi" {
			return nil, nil
		}
		hit := entry(n)
		hit.SourceID, hit.SourceURL = n, fmt.Sprintf("https://bgm.tv/subject/%d", n)
		return []*metadata.SeriesMetadata{hit}, nil
	}
}

func TestIdentificationReplacesOnlyThePlaceholderTitleAndKeepsLockedOrFilledFields(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Placeholders")
	placeholder := library.series("Title 1", "", "01.cbz")
	setSeries(t, store, placeholder, func(p *database.UpdateSeriesMetadataParams) {
		p.Title, p.LockedFields = sqlText("Title 1"), sqlText("title")
	})
	owned := library.series("Title 2", "", "01.cbz")
	setSeries(t, store, owned, func(p *database.UpdateSeriesMetadataParams) {
		p.Title, p.Publisher, p.LockedFields = sqlText("Owner title"), sqlText("Owner publisher"), sqlText("summary")
		p.Rating = sql.NullFloat64{Float64: 7, Valid: true}
	})
	sources := &recordingSources{answer: numbered(func(n int) *metadata.SeriesMetadata {
		return &metadata.SeriesMetadata{Title: fmt.Sprintf("来源标题 %d", n), Summary: "Source summary", Publisher: "Source publisher", SiteRating: &metadata.SiteRating{Value: 9, Scale: 10}}
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})

	verdicts := agent.identifyAll("identify", map[int64][]string{placeholder: {"bangumi:1"}, owned: {"bangumi:2"}})
	// The scan's directory-name title is a placeholder: replaced, and still locked.
	got := web.series(placeholder)
	if got.Title.String != "来源标题 1" || got.LockedFields.String != "title" || got.Summary.String != "Source summary" || got.Rating.Float64 != 9 {
		t.Fatalf("placeholder series %+v", got)
	}
	if r := agent.receipt(verdicts[placeholder].OperationID); r.Identification == nil || !r.Identification.PlaceholderTitle || !r.changed("title") {
		t.Fatalf("placeholder receipt %+v", r)
	}
	// A title that differs from the directory name, filled values, a manual rating and a locked
	// field stay as they were; only the empty, unlocked fields are filled.
	kept := web.series(owned)
	if kept.Title.String != "Owner title" || kept.Publisher.String != "Owner publisher" || kept.Summary.Valid || kept.Rating.Float64 != 7 || kept.LockedFields.String != "summary" {
		t.Fatalf("owner's series %+v", kept)
	}
	if r := agent.receipt(verdicts[owned].OperationID); r.Outcome != "changed" || r.changed("title") || r.changed("summary") || r.changed("publisher") || r.changed("rating") || !r.changed("source_links") {
		t.Fatalf("owner's series receipt %+v", r)
	}

	// The replacement keeps its lock, so a forced rescan does not turn it back into the directory name.
	if run := web.scan(library.lib.ID, true); run.Status != task.StatusCompleted {
		t.Fatalf("rescan %+v", run)
	}
	if again := web.series(placeholder); again.Title.String != "来源标题 1" || again.LockedFields.String != "title" {
		t.Fatalf("rescanned placeholder series %+v", again)
	}
}

func TestUndoingIdentificationEnrichmentRestoresThePlaceholderUnderItsLock(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Undo")
	id := library.series("Title 1", "", "01.cbz")
	setSeries(t, store, id, func(p *database.UpdateSeriesMetadataParams) {
		p.Title, p.LockedFields = sqlText("Title 1"), sqlText("title")
	})
	sources := &recordingSources{answer: numbered(func(n int) *metadata.SeriesMetadata {
		return &metadata.SeriesMetadata{Title: "来源标题", Summary: "Source summary", SiteRating: &metadata.SiteRating{Value: 8, Scale: 10}}
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	operation := agent.identifyAll("identify", map[int64][]string{id: {"bangumi:1"}})[id].OperationID

	var undone receipt
	if result := agent.call("library_undo_metadata_operation", map[string]any{"request_key": "undo-identify", "operation_id": operation}, &undone); result.IsError || undone.Outcome != "changed" {
		t.Fatalf("undo %+v %s", undone, resultText(result))
	}
	got := web.series(id)
	if got.Title.String != "Title 1" || got.LockedFields.String != "title" || got.Summary.Valid || got.Rating.Valid {
		t.Fatalf("undone series %+v", got)
	}
	if status := web.identification(id).Status; status != "identified" {
		t.Fatalf("undoing the supplementation changed the identification: %s", status)
	}
	// The undone fields no longer carry source records, so ComicInfo fills them again as before.
	writeComicArchive(t, library.lib.Path+"/Title 1/02.cbz", `<ComicInfo><Series>Title 1</Series><Summary>ComicInfo summary</Summary></ComicInfo>`)
	if run := web.scan(library.lib.ID, false); run.Status != task.StatusCompleted {
		t.Fatalf("scan %+v", run)
	}
	if after := web.series(id); after.Summary.String != "ComicInfo summary" {
		t.Fatalf("scan after undo %+v", after)
	}
}

func TestIdentificationRatesFromEverySourceAndUnitesTheirLinks(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Sources")
	id := library.series("Title 1", "", "01.cbz")
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO series_links (series_id, name, url) VALUES (?, 'Shop', 'https://shop.example/title-1')`, id); err != nil {
		t.Fatal(err)
	}
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{
		"bangumi":  {"Title 1": {Title: "标题", SiteRating: &metadata.SiteRating{Value: 9, Scale: 10}, SourceID: 1, SourceURL: "https://bgm.tv/subject/1"}},
		"anilist":  {"Title 1": {Title: "Title", SiteRating: &metadata.SiteRating{Value: 80, Scale: 100}, SourceID: 2, SourceURL: "https://anilist.co/manga/2"}},
		"mangadex": {"Title 1": {Title: "Title", SourceURL: "https://mangadex.org/title/0b0e4e1c-5a37-4c39-8a2e-1d3c8b3f6a10"}},
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	operation := agent.identifyAll("identify", map[int64][]string{id: {"bangumi:1", "anilist:2", "mangadex:0b0e4e1c-5a37-4c39-8a2e-1d3c8b3f6a10"}})[id].OperationID

	// Every identified source with a valid rating counts once, on a ten-point scale.
	if got := web.series(id); got.Rating.Float64 != 8.5 {
		t.Fatalf("rating %+v", got.Rating)
	}
	r := agent.receipt(operation)
	for _, f := range r.Fields {
		if f.Field == "rating" && (f.Formula != "(9/10*10 + 80/100*10)/2 = 8.5" || len(f.Sources) != 2) {
			t.Fatalf("rating audit %+v", f)
		}
	}
	var view struct {
		Links []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"links"`
	}
	web.call("GET", fmt.Sprintf("/api/series/%d/context", id), 200, &view)
	links := map[string]string{}
	for _, l := range view.Links {
		links[l.Name] = l.URL
	}
	want := map[string]string{"Shop": "https://shop.example/title-1", "Bangumi": "https://bgm.tv/subject/1", "AniList": "https://anilist.co/manga/2",
		"MangaDex": "https://mangadex.org/title/0b0e4e1c-5a37-4c39-8a2e-1d3c8b3f6a10"}
	if len(view.Links) != len(want) || !reflect.DeepEqual(links, want) {
		t.Fatalf("links %+v want %v", view.Links, want)
	}
}

func TestIdentificationSupersedesTheSeriesPendingProposals(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Backlog")
	id := library.series("Title 1", "", "01.cbz")
	older := historicalProposal(t, store, id, "Bangumi", 9, "https://bgm.tv/subject/9", `{"Title":"Another work"}`, "pending")
	newer := historicalProposal(t, store, id, "AniList", 1, "https://anilist.co/manga/1", `{"Title":"Title 1"}`, "pending")
	sources := &recordingSources{answer: numbered(func(int) *metadata.SeriesMetadata { return &metadata.SeriesMetadata{Title: "标题"} })}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if status := web.identification(id).Status; status != "unprocessed" {
		t.Fatalf("a series with only pending proposals is %s", status)
	}
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	operation := agent.identifyAll("identify", map[int64][]string{id: {"bangumi:1"}})[id].OperationID

	if r := agent.receipt(operation); r.Identification == nil || !reflect.DeepEqual(r.Identification.SupersededProposals, []int64{older, newer}) {
		t.Fatalf("receipt %+v", r)
	}
	var reviews struct {
		Reviews []struct {
			ID int64 `json:"id"`
		} `json:"reviews"`
	}
	web.call("GET", fmt.Sprintf("/api/series/%d/metadata-review", id), 200, &reviews)
	if len(reviews.Reviews) != 0 {
		t.Fatalf("pending proposals after identification %+v", reviews)
	}
	// Superseded is its own terminal status: neither applied nor rejected, and no longer decidable.
	for _, proposal := range []int64{older, newer} {
		row, err := store.GetMetadataReview(ctx, proposal)
		if err != nil || row.Status != "superseded" || row.AppliedAt.Valid || row.RejectedAt.Valid {
			t.Fatalf("proposal %d %+v %v", proposal, row, err)
		}
		if rec := web.raw("POST", fmt.Sprintf("/api/metadata/reviews/%d/apply", proposal), ""); rec.Code != 409 {
			t.Fatalf("applying superseded proposal %d: %d %s", proposal, rec.Code, rec.Body)
		}
	}
}

func TestIdentificationFollowsTheConfiguredSourcePriority(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Configured")
	id := library.series("Title 1", "", "01.cbz")
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{
		"bangumi": {"Title 1": {Title: "中文名", Summary: "Bangumi summary", SourceID: 1, SourceURL: "https://bgm.tv/subject/1"}},
		"anilist": {"Title 1": {Title: "English Title", Publisher: "AniList Publisher", SourceID: 1, SourceURL: "https://anilist.co/manga/1"}},
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	// The priority takes effect at once; sources it leaves out follow in the default order.
	snapshot := cfg.Snapshot()
	snapshot.Identification.SourcePriority = config.SourcePriority{"anilist"}
	cfg.Replace(&snapshot)
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	agent.identifyAll("identify", map[int64][]string{id: {"bangumi:1", "anilist:1"}})

	if got := web.series(id); got.Title.String != "English Title" || got.Publisher.String != "AniList Publisher" || got.Summary.String != "Bangumi summary" {
		t.Fatalf("series under AniList-first priority %+v", got)
	}
}
