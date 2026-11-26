// These tests guard the tag policy of identification enrichment: source tags are united across the
// identified sources, then pass a vote threshold, a blocklist and a per-series limit before they are
// appended, so medium, region, year and ownership noise never reaches the library.

package bootstrap_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/task"
)

// setTags gives a series the tags a scan or an owner left on it.
func setTags(t *testing.T, store database.Store, id int64, names ...string) {
	t.Helper()
	ctx := context.Background()
	err := store.ExecTx(ctx, func(q *database.Queries) error {
		for _, name := range names {
			tag, err := q.UpsertTag(ctx, name)
			if err != nil {
				return err
			}
			if err := q.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: id, TagID: tag.ID}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// seriesTags reads the series page's tags, sorted.
func (w *identificationWeb) seriesTags(id int64) []string {
	w.t.Helper()
	var view struct {
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	w.call("GET", fmt.Sprintf("/api/series/%d/context", id), 200, &view)
	out := []string{}
	for _, tag := range view.Tags {
		out = append(out, tag.Name)
	}
	slices.Sort(out)
	return out
}

// voted is a Bangumi entry for query whose tags carry the given votes, in the source's order.
func voted(id int, votes ...metadata.TagVote) *metadata.SeriesMetadata {
	entry := &metadata.SeriesMetadata{Title: "标题", SourceID: id, SourceURL: fmt.Sprintf("https://bgm.tv/subject/%d", id), TagVotes: votes}
	for _, v := range votes {
		entry.Tags = append(entry.Tags, v.Name)
	}
	return entry
}

func TestIdentificationAppendsOnlySourceTagsWithEnoughVotes(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Votes")
	id := library.series("Title 1", "", "01.cbz")
	setTags(t, store, id, "Owned label")
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{
		"bangumi": {"Title 1": voted(1, metadata.TagVote{Name: "热血", Votes: 120}, metadata.TagVote{Name: "冒险", Votes: 3}, metadata.TagVote{Name: "一人一票", Votes: 2})},
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	operation := agent.identifyAll("identify", map[int64][]string{id: {"bangumi:1"}})[id].OperationID

	// The evidence packet and the conclusion keep the votes, so the default threshold of three votes
	// drops the tag only two people applied. Tags the series had stay.
	if got, want := web.seriesTags(id), []string{"Owned label", "冒险", "热血"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tags %v, want %v", got, want)
	}
	r := agent.receipt(operation)
	if !r.changed("tags") {
		t.Fatalf("receipt %+v", r)
	}
	for _, f := range r.Fields {
		if f.Field == "tags" && (len(f.Sources) != 1 || f.Sources[0].CandidateID != "bangumi:1") {
			t.Fatalf("tags evidence %+v", f)
		}
	}

	// The appended tags are an ordinary part of the receipt: undoing it removes just them.
	var undone receipt
	if result := agent.call("library_undo_metadata_operation", map[string]any{"request_key": "undo-tags", "operation_id": operation}, &undone); result.IsError || undone.Outcome != "changed" {
		t.Fatalf("undo %+v %s", undone, resultText(result))
	}
	if got := web.seriesTags(id); !reflect.DeepEqual(got, []string{"Owned label"}) {
		t.Fatalf("tags after undo %v", got)
	}
}

func TestIdentificationLeavesBlockedTagsOutAndFollowsAChangedBlocklistAtOnce(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	first := newPrefetchLibrary(t, store, "Defaults")
	defaults := first.series("Title 1", "", "01.cbz")
	second := newPrefetchLibrary(t, store, "Configured")
	configured := second.series("Title 2", "", "01.cbz")
	popular := []metadata.TagVote{{Name: "漫画", Votes: 900}, {Name: "日本", Votes: 500}, {Name: "2010年", Votes: 300}, {Name: "已购", Votes: 200},
		{Name: "Yuri", Votes: 160}, {Name: "百合", Votes: 150}, {Name: "校园", Votes: 80}}
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{
		"bangumi": {"Title 1": voted(1, popular...), "Title 2": voted(2, popular...)},
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}

	// The default blocklist covers the medium, the region, years and the reader's own status, which
	// are the most voted tags of almost every entry.
	web.mainLoopAgent(cfg, 1, []int64{first.lib.ID}, []string{delegated.Read, delegated.MetadataWrite}).
		identifyAll("identify", map[int64][]string{defaults: {"bangumi:1"}})
	if got, want := web.seriesTags(defaults), []string{"Yuri", "校园", "百合"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tags under the default blocklist %v, want %v", got, want)
	}

	// A changed blocklist replaces the default one at once. Entries compare as catalogue search text,
	// so a full-width entry blocks its half-width tag; an entry between slashes is a regular expression.
	snapshot := cfg.Snapshot()
	snapshot.Identification.Tags.Blocklist = []string{"ｙｕｒｉ", "/^校/"}
	cfg.Replace(&snapshot)
	web.mainLoopAgent(cfg, 1, []int64{second.lib.ID}, []string{delegated.Read, delegated.MetadataWrite}).
		identifyAll("identify", map[int64][]string{configured: {"bangumi:2"}})
	if got, want := web.seriesTags(configured), []string{"2010年", "已购", "日本", "漫画", "百合"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tags under the changed blocklist %v, want %v", got, want)
	}
}

func TestIdentificationTakesTheMostVotedTagsUpToTheSeriesLimit(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Limit")
	id := library.series("Title 1", "", "01.cbz")
	setTags(t, store, id, "tag 02")
	// Twelve tags pass the threshold, listed least voted first.
	var votes []metadata.TagVote
	for n := 12; n >= 1; n-- {
		votes = append(votes, metadata.TagVote{Name: fmt.Sprintf("Tag %02d", n), Votes: 100 - n})
	}
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{"bangumi": {"Title 1": voted(1, votes...)}})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite}).
		identifyAll("identify", map[int64][]string{id: {"bangumi:1"}})

	// The series takes the ten most voted. One of them it already has under another spelling: it
	// counts toward the ten and is not added twice.
	want := []string{"Tag 01", "Tag 03", "Tag 04", "Tag 05", "Tag 06", "Tag 07", "Tag 08", "Tag 09", "Tag 10", "tag 02"}
	if got := web.seriesTags(id); !reflect.DeepEqual(got, want) {
		t.Fatalf("tags %v, want %v", got, want)
	}
}

// tagEvidence lists, per candidate, the tags a receipt's tag audit credits to it.
func (a *mainLoopAgent) tagEvidence(operation string) map[string][]string {
	a.t.Helper()
	var out struct {
		Fields []struct {
			Field   string `json:"field"`
			Sources []struct {
				CandidateID string `json:"candidate_id"`
				Metadata    struct {
					Tags     []string           `json:"Tags"`
					TagVotes []metadata.TagVote `json:"TagVotes"`
				} `json:"metadata"`
			} `json:"sources"`
		} `json:"fields"`
	}
	if result := a.call("library_get_metadata_operation", map[string]any{"operation_id": operation}, &out); result.IsError {
		a.t.Fatalf("receipt %s refused: %s", operation, resultText(result))
	}
	credited := map[string][]string{}
	for _, f := range out.Fields {
		if f.Field != "tags" {
			continue
		}
		for _, s := range f.Sources {
			credited[s.CandidateID] = s.Metadata.Tags
			for _, v := range s.Metadata.TagVotes {
				credited[s.CandidateID+" votes"] = append(credited[s.CandidateID+" votes"], fmt.Sprintf("%s=%d", v.Name, v.Votes))
			}
		}
	}
	return credited
}

func TestIdentificationUnitesTheTagsOfEveryIdentifiedSourceBeforeFiltering(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Union")
	id := library.series("Title 1", "", "01.cbz")
	const dex = "0b0e4e1c-5a37-4c39-8a2e-1d3c8b3f6a10"
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{
		"bangumi": {"Title 1": voted(1, metadata.TagVote{Name: "漫画", Votes: 900}, metadata.TagVote{Name: "热血", Votes: 50}, metadata.TagVote{Name: "Action", Votes: 2})},
		// AniList and MangaDex tags are curated categories without votes: each one vouches for its own.
		"anilist":  {"Title 1": {Title: "Title", SourceID: 1, SourceURL: "https://anilist.co/manga/1", Tags: []string{"Action", "Comedy", "Manga"}}},
		"mangadex": {"Title 1": {Title: "Title", SourceURL: "https://mangadex.org/title/" + dex, Tags: []string{"comedy", "Long Strip", "Romance"}}},
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	operation := agent.identifyAll("identify", map[int64][]string{id: {"bangumi:1", "anilist:1", "mangadex:" + dex}})[id].OperationID

	// The union is filtered as one: Bangumi's two votes for Action do not keep it out once AniList
	// lists it, a spelling already taken is not added again, and blocked tags stay out from every source.
	if got, want := web.seriesTags(id), []string{"Action", "Comedy", "Romance", "热血"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tags %v, want %v", got, want)
	}
	want := map[string][]string{"bangumi:1": {"热血"}, "bangumi:1 votes": {"热血=50"}, "anilist:1": {"Action", "Comedy"}, "mangadex:" + dex: {"Romance"}}
	if got := agent.tagEvidence(operation); !reflect.DeepEqual(got, want) {
		t.Fatalf("tag evidence %v, want %v", got, want)
	}
}

func TestIdentificationTakesNoBangumiTagsFromEvidenceKeptWithoutVotes(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	first := newPrefetchLibrary(t, store, "Thresholded")
	thresholded := first.series("Title 1", "", "01.cbz")
	second := newPrefetchLibrary(t, store, "Unthresholded")
	unthresholded := second.series("Title 2", "", "01.cbz")
	// Evidence prefetched before Bangumi results kept their votes holds Bangumi tags without counts.
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		var n int
		if _, err := fmt.Sscanf(query, "Title %d", &n); err != nil {
			return nil, nil
		}
		switch source {
		case "bangumi":
			return []*metadata.SeriesMetadata{{Title: query, SourceID: n, SourceURL: fmt.Sprintf("https://bgm.tv/subject/%d", n), Tags: []string{"漫画", "热血"}}}, nil
		case "anilist":
			return []*metadata.SeriesMetadata{{Title: query, SourceID: n, SourceURL: fmt.Sprintf("https://anilist.co/manga/%d", n), Tags: []string{"Action"}}}, nil
		}
		return nil, nil
	}}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}

	// Unknown counts never meet the threshold, so only the other source's tags are appended.
	web.mainLoopAgent(cfg, 1, []int64{first.lib.ID}, []string{delegated.Read, delegated.MetadataWrite}).
		identifyAll("identify", map[int64][]string{thresholded: {"bangumi:1", "anilist:1"}})
	if got := web.seriesTags(thresholded); !reflect.DeepEqual(got, []string{"Action"}) {
		t.Fatalf("tags under the threshold %v", got)
	}

	// With the threshold turned off they need no votes; the blocklist still applies.
	snapshot := cfg.Snapshot()
	snapshot.Identification.Tags.MinVotes = -1
	cfg.Replace(&snapshot)
	web.mainLoopAgent(cfg, 1, []int64{second.lib.ID}, []string{delegated.Read, delegated.MetadataWrite}).
		identifyAll("identify", map[int64][]string{unthresholded: {"bangumi:2", "anilist:2"}})
	if got, want := web.seriesTags(unthresholded), []string{"Action", "热血"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tags without a threshold %v, want %v", got, want)
	}
}
