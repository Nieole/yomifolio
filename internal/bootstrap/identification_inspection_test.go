// These tests guard the inspection primitives of the assembled process: a credential with read
// finds suspected duplicates and counts tags across its libraries, and leaves the owner notes that
// change neither the identification status nor any metadata.

package bootstrap_test

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/task"
)

// duplicatePage mirrors one page of suspected duplicates.
type duplicatePage struct {
	LibraryIDs []int64 `json:"library_ids"`
	Groups     []struct {
		Match       string            `json:"match"`
		Key         string            `json:"key"`
		SeriesCount int64             `json:"series_count"`
		Series      []duplicateMember `json:"series"`
	} `json:"groups"`
	NextOffset int `json:"next_offset"`
}

type duplicateMember struct {
	SeriesID  int64  `json:"series_id"`
	LibraryID int64  `json:"library_id"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	BookCount int64  `json:"book_count"`
	Status    string `json:"status"`
}

// groupKeys lists each group of the page as match/key.
func (p duplicatePage) groupKeys() []string {
	out := []string{}
	for _, g := range p.Groups {
		out = append(out, g.Match+"/"+g.Key)
	}
	return out
}

func TestReadOnlyAgentFindsSuspectedDuplicatesAcrossItsLibraries(t *testing.T) {
	store, cfg := fixture(t)
	shelfA, shelfB, hidden := newPrefetchLibrary(t, store, "Shelf A"), newPrefetchLibrary(t, store, "Shelf B"), newPrefetchLibrary(t, store, "Hidden")
	onePiece := shelfA.series("One Piece", "", "01.cbz", "02.cbz")
	onePieceUpper := shelfB.series("ONE PIECE", "", "03.cbz")
	// An enriched series whose title, not its directory, carries the shared name.
	retitled := shelfB.series("ワンピース", "")
	if _, err := store.DB().Exec(`UPDATE series SET title = 'One Piece' WHERE id = ?`, retitled); err != nil {
		t.Fatal(err)
	}
	hidden.series("One Piece", "", "01.cbz")
	naruto := shelfA.series("Naruto", "")
	narutoBang := shelfA.series("Naruto!", "")
	fullmetal := shelfA.series("Fullmetal", "")
	hagane := shelfB.series("鋼の錬金術師", "")
	hiddenHagane := hidden.series("Hagane", "")
	for _, series := range []int64{fullmetal, hagane, hiddenHagane} {
		historicalProposal(t, store, series, "bangumi", 7, "https://bgm.tv/subject/7", `{}`, "applied")
	}
	web := openIdentificationWeb(t, store, cfg)
	t.Cleanup(web.process.Close)
	web.setupAdmin()
	agent := web.mainLoopAgent(cfg, 1, []int64{shelfA.lib.ID, shelfB.lib.ID}, []string{delegated.Read})

	var all duplicatePage
	if result := agent.call("library_find_duplicate_series", map[string]any{}, &all); result.IsError {
		t.Fatalf("duplicates refused: %s", resultText(result))
	}
	// Shared source entries come first, then shared names; the hidden library joins neither.
	if want := []string{"same_entry/bangumi:7", "same_name/naruto", "same_name/onepiece"}; !reflect.DeepEqual(all.groupKeys(), want) || all.NextOffset != 0 {
		t.Fatalf("groups %v next %d, want %v", all.groupKeys(), all.NextOffset, want)
	}
	if !reflect.DeepEqual(all.LibraryIDs, []int64{shelfA.lib.ID, shelfB.lib.ID}) {
		t.Fatalf("libraries %v", all.LibraryIDs)
	}
	members := map[string][]duplicateMember{
		"bangumi:7": {
			{SeriesID: fullmetal, LibraryID: shelfA.lib.ID, Name: "Fullmetal", Status: "identified"},
			{SeriesID: hagane, LibraryID: shelfB.lib.ID, Name: "鋼の錬金術師", Status: "identified"},
		},
		"naruto": {
			{SeriesID: naruto, LibraryID: shelfA.lib.ID, Name: "Naruto", Status: "unprocessed"},
			{SeriesID: narutoBang, LibraryID: shelfA.lib.ID, Name: "Naruto!", Status: "unprocessed"},
		},
		"onepiece": {
			{SeriesID: onePiece, LibraryID: shelfA.lib.ID, Name: "One Piece", BookCount: 2, Status: "unprocessed"},
			{SeriesID: onePieceUpper, LibraryID: shelfB.lib.ID, Name: "ONE PIECE", BookCount: 1, Status: "unprocessed"},
			{SeriesID: retitled, LibraryID: shelfB.lib.ID, Name: "ワンピース", Title: "One Piece", Status: "unprocessed"},
		},
	}
	for _, group := range all.Groups {
		if want := members[group.Key]; !reflect.DeepEqual(group.Series, want) || group.SeriesCount != int64(len(want)) {
			t.Fatalf("group %s: %d %+v, want %+v", group.Key, group.SeriesCount, group.Series, want)
		}
	}

	var first, rest duplicatePage
	agent.call("library_find_duplicate_series", map[string]any{"limit": 2}, &first)
	if !reflect.DeepEqual(first.groupKeys(), all.groupKeys()[:2]) || first.NextOffset != 2 {
		t.Fatalf("first page %v next %d", first.groupKeys(), first.NextOffset)
	}
	agent.call("library_find_duplicate_series", map[string]any{"limit": 2, "offset": first.NextOffset}, &rest)
	if !reflect.DeepEqual(rest.groupKeys(), all.groupKeys()[2:]) || rest.NextOffset != 0 {
		t.Fatalf("second page %v next %d", rest.groupKeys(), rest.NextOffset)
	}

	var narrowed duplicatePage
	agent.call("library_find_duplicate_series", map[string]any{"library_ids": []int64{shelfA.lib.ID}}, &narrowed)
	if want := []string{"same_name/naruto"}; !reflect.DeepEqual(narrowed.groupKeys(), want) {
		t.Fatalf("one library's groups %v, want %v", narrowed.groupKeys(), want)
	}
	for _, args := range []map[string]any{
		{"library_ids": []int64{shelfA.lib.ID, hidden.lib.ID}},
		{"limit": 21},
		{"offset": -1},
	} {
		var refused duplicatePage
		if result := agent.call("library_find_duplicate_series", args, &refused); !result.IsError {
			t.Fatalf("duplicates %v answered %+v", args, refused)
		}
	}
}

func TestDuplicatePagesStayWithinTheirByteBudget(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Crowded", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	// Each group shares one long name across eleven directories that differ only in punctuation.
	marks := []string{"", "!", "?", ".", ",", "(", ")", "[", "]", "-", "_"}
	var groups []string
	for g := range 7 {
		name := strings.Repeat("漫", 80) + string(rune('a'+g))
		groups = append(groups, "same_name/"+name)
		for _, mark := range marks {
			if _, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: name + mark, Path: filepath.Join(lib.Path, name+mark),
				Title: sqlText(mark + name)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	web := openIdentificationWeb(t, store, cfg)
	t.Cleanup(web.process.Close)
	web.setupAdmin()
	agent := web.mainLoopAgent(cfg, 1, []int64{lib.ID}, []string{delegated.Read})

	var seen []string
	offset := 0
	for pages := 0; ; pages++ {
		result, err := agent.session.CallTool(ctx, &mcp.CallToolParams{Name: "library_find_duplicate_series", Arguments: map[string]any{"limit": 20, "offset": offset}})
		if err != nil || result.IsError {
			t.Fatalf("page at %d: %+v %v", offset, result, err)
		}
		var page duplicatePage
		if err := json.Unmarshal([]byte(resultText(result)), &page); err != nil {
			t.Fatal(err)
		}
		if size := len(resultText(result)); size > 30<<10 || len(page.Groups) == 0 {
			t.Fatalf("page at %d holds %d groups in %d bytes", offset, len(page.Groups), size)
		}
		for _, group := range page.Groups {
			if group.SeriesCount != int64(len(marks)) || len(group.Series) != 10 {
				t.Fatalf("group %s lists %d of %d series", group.Key, len(group.Series), group.SeriesCount)
			}
		}
		seen = append(seen, page.groupKeys()...)
		if page.NextOffset == 0 {
			if pages == 0 {
				t.Fatal("seven long groups fit one page")
			}
			break
		}
		offset = page.NextOffset
	}
	if !reflect.DeepEqual(seen, groups) {
		t.Fatalf("pages listed %v, want %v", seen, groups)
	}
}

// tagPage mirrors one page of tag usage.
type tagPage struct {
	LibraryIDs []int64 `json:"library_ids"`
	Total      int64   `json:"total"`
	Tags       []struct {
		Tag             string  `json:"tag"`
		SeriesCount     int64   `json:"series_count"`
		SampleSeriesIDs []int64 `json:"sample_series_ids"`
	} `json:"tags"`
	NextOffset int `json:"next_offset"`
}

func (p tagPage) names() []string {
	out := []string{}
	for _, tag := range p.Tags {
		out = append(out, tag.Tag)
	}
	return out
}

func tagSeries(t *testing.T, store database.Store, series int64, tags ...string) {
	t.Helper()
	for _, name := range tags {
		tag, err := store.UpsertTag(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.LinkSeriesTag(context.Background(), database.LinkSeriesTagParams{SeriesID: series, TagID: tag.ID}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadOnlyAgentCountsTagUsageAcrossItsLibraries(t *testing.T) {
	store, cfg := fixture(t)
	shelfA, shelfB, hidden := newPrefetchLibrary(t, store, "Shelf A"), newPrefetchLibrary(t, store, "Shelf B"), newPrefetchLibrary(t, store, "Hidden")
	first, second, third := shelfA.series("First", ""), shelfA.series("Second", ""), shelfA.series("Third", "")
	shelfA.series("Untagged", "")
	other := shelfB.series("Other", "")
	tagSeries(t, store, first, "Adventure", "Sci-Fi")
	tagSeries(t, store, second, "Adventure", "Ninja", "Thriller")
	tagSeries(t, store, third, "Adventure", "Zombie")
	tagSeries(t, store, other, "Adventure", "sci fi", "Zombie")
	tagSeries(t, store, hidden.series("Hidden", ""), "Adventure", "Secret")
	web := openIdentificationWeb(t, store, cfg)
	t.Cleanup(web.process.Close)
	web.setupAdmin()
	agent := web.mainLoopAgent(cfg, 1, []int64{shelfA.lib.ID, shelfB.lib.ID}, []string{delegated.Read})

	var byCount tagPage
	if result := agent.call("library_count_tags", map[string]any{}, &byCount); result.IsError {
		t.Fatalf("tag usage refused: %s", resultText(result))
	}
	// Most used first; the hidden library's series and tags count for nothing.
	if want := []string{"Adventure", "Zombie", "Ninja", "Sci-Fi", "Thriller", "sci fi"}; !reflect.DeepEqual(byCount.names(), want) || byCount.Total != 6 || byCount.NextOffset != 0 {
		t.Fatalf("tags by count %v of %d next %d, want %v", byCount.names(), byCount.Total, byCount.NextOffset, want)
	}
	adventure, zombie := byCount.Tags[0], byCount.Tags[1]
	if adventure.SeriesCount != 4 || !reflect.DeepEqual(adventure.SampleSeriesIDs, []int64{first, second, third}) ||
		zombie.SeriesCount != 2 || !reflect.DeepEqual(zombie.SampleSeriesIDs, []int64{third, other}) {
		t.Fatalf("usage %+v %+v", adventure, zombie)
	}
	if !reflect.DeepEqual(byCount.LibraryIDs, []int64{shelfA.lib.ID, shelfB.lib.ID}) {
		t.Fatalf("libraries %v", byCount.LibraryIDs)
	}

	// Ordered by normalized name, spelling variants sit together.
	var byName tagPage
	agent.call("library_count_tags", map[string]any{"sort": "name"}, &byName)
	if want := []string{"Adventure", "Ninja", "Sci-Fi", "sci fi", "Thriller", "Zombie"}; !reflect.DeepEqual(byName.names(), want) {
		t.Fatalf("tags by name %v, want %v", byName.names(), want)
	}

	var head, tail tagPage
	agent.call("library_count_tags", map[string]any{"limit": 4}, &head)
	agent.call("library_count_tags", map[string]any{"limit": 4, "offset": head.NextOffset}, &tail)
	if !reflect.DeepEqual(append(head.names(), tail.names()...), byCount.names()) || head.NextOffset != 4 || tail.NextOffset != 0 || tail.Total != 6 {
		t.Fatalf("pages %v (next %d) and %v (next %d)", head.names(), head.NextOffset, tail.names(), tail.NextOffset)
	}

	var narrowed tagPage
	agent.call("library_count_tags", map[string]any{"library_ids": []int64{shelfB.lib.ID}}, &narrowed)
	if want := []string{"Adventure", "Zombie", "sci fi"}; !reflect.DeepEqual(narrowed.names(), want) || narrowed.Total != 3 || narrowed.Tags[0].SeriesCount != 1 {
		t.Fatalf("one library's tags %+v, want %v", narrowed, want)
	}
	for _, args := range []map[string]any{
		{"library_ids": []int64{shelfA.lib.ID, hidden.lib.ID}},
		{"sort": "random"},
		{"limit": 101},
		{"offset": -1},
	} {
		var refused tagPage
		if result := agent.call("library_count_tags", args, &refused); !result.IsError {
			t.Fatalf("tag usage %v answered %+v", args, refused)
		}
	}
}

// seriesReport mirrors the result of one note.
type seriesReport struct {
	SeriesID int64  `json:"series_id"`
	Category string `json:"category"`
	Recorded bool   `json:"recorded"`
	Status   string `json:"status"`
}

func (a *mainLoopAgent) report(series int64, category, reason string) (seriesReport, *mcp.CallToolResult) {
	a.t.Helper()
	var out seriesReport
	result := a.call("library_report_series", map[string]any{"series_id": series, "category": category, "reason": reason}, &out)
	return out, result
}

func TestReadOnlyAgentLeavesNotesThatChangeNothing(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	shelf, hidden := newPrefetchLibrary(t, store, "Shelf"), newPrefetchLibrary(t, store, "Hidden")
	onePiece := shelf.series("One Piece", "", "01.cbz")
	fullmetal := shelf.series("Fullmetal", "")
	historicalProposal(t, store, fullmetal, "bangumi", 7, "https://bgm.tv/subject/7", `{}`, "applied")
	elsewhere := hidden.series("Elsewhere", "")
	web := openIdentificationWeb(t, store, cfg)
	t.Cleanup(web.process.Close)
	web.setupAdmin()
	agent := web.mainLoopAgent(cfg, 1, []int64{shelf.lib.ID}, []string{delegated.Read})
	before := web.series(onePiece)

	got, _ := agent.report(onePiece, "suspected_duplicate", "  same work as series 40 in Shelf B  ")
	if got != (seriesReport{SeriesID: onePiece, Category: "suspected_duplicate", Recorded: true, Status: "unprocessed"}) {
		t.Fatalf("report %+v", got)
	}
	notes := web.ownerVerdicts(onePiece).Escalations
	if len(notes) != 1 || notes[0].Category != "suspected_duplicate" || notes[0].Class != "note" || notes[0].Reason != "same work as series 40 in Shelf B" ||
		notes[0].RaisedBy.Kind != "agent" || notes[0].RaisedBy.CredentialID != agent.credential {
		t.Fatalf("note %+v", notes)
	}
	// An open note of the category is not repeated; another category is a note of its own.
	if again, _ := agent.report(onePiece, "suspected_duplicate", "still the same work"); again.Recorded || again.Status != "unprocessed" {
		t.Fatalf("repeated report %+v", again)
	}
	if grouping, _ := agent.report(onePiece, "directory_grouping", "the directory holds two works"); !grouping.Recorded {
		t.Fatalf("second category %+v", grouping)
	}
	if identified, _ := agent.report(fullmetal, "tag_problem", "tags name the shop"); !identified.Recorded || identified.Status != "identified" {
		t.Fatalf("note on an identified series %+v", identified)
	}

	var denied []string
	for _, args := range []struct {
		series           int64
		category, reason string
	}{
		{onePiece, "ambiguous_candidates", "only the main loop escalates for identification"},
		{onePiece, "new_book_contradiction", "a system category"},
		{onePiece, "elsewhere", "not a category"},
		{onePiece, "tag_problem", "   "},
		{onePiece, "tag_problem", strings.Repeat("x", 1001)},
		{0, "tag_problem", "no series"},
		{elsewhere, "tag_problem", "outside the credential"},
		{999999, "tag_problem", "no such series"},
	} {
		result, err := agent.session.CallTool(ctx, &mcp.CallToolParams{Name: "library_report_series", Arguments: map[string]any{"series_id": args.series, "category": args.category, "reason": args.reason}})
		if err == nil && !result.IsError {
			t.Fatalf("report %+v answered %s", args, resultText(result))
		}
		if args.series == elsewhere || args.series == 999999 {
			denied = append(denied, resultText(result))
		}
	}
	if denied[0] != denied[1] || !strings.HasPrefix(denied[0], "delegated access denied") {
		t.Fatalf("a series outside the credential reads differently from a missing one: %q", denied)
	}

	owner := web.ownerVerdicts(onePiece).Escalations
	if len(owner) != 2 || owner[0].Reason != "same work as series 40 in Shelf B" || owner[1].Category != "directory_grouping" {
		t.Fatalf("notes after refusals %+v", owner)
	}
	if got := web.identification(onePiece); got.Status != "unprocessed" {
		t.Fatalf("notes changed the status %+v", got)
	}
	if got := web.identification(fullmetal); got.Status != "identified" || len(got.Conclusions) != 1 {
		t.Fatalf("a note changed the identification %+v", got)
	}
	if after := web.series(onePiece); !reflect.DeepEqual(after, before) {
		t.Fatalf("notes changed the series %+v, was %+v", after, before)
	}
	if notes := web.ownerVerdicts(elsewhere).Escalations; len(notes) != 0 {
		t.Fatalf("a refused note was recorded %+v", notes)
	}
}

func TestInspectionNeedsOnlyReadAndGrantsNoWrite(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library, ids := claimLibrary(t, store, "Inspected", "Title 1", "Title 2")
	sources := &recordingSources{answer: candidatesBySeries}
	clock := &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	web := openClaimWeb(t, store, cfg, sources, clock)
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	regular, err := store.CreateUser(ctx, database.CreateUserParams{Username: "regular", PasswordHash: "unused", Role: database.RoleRegular})
	if err != nil {
		t.Fatal(err)
	}
	inspection := map[string]map[string]any{
		"library_find_duplicate_series": {},
		"library_count_tags":            {},
		"library_report_series":         {"series_id": ids[0], "category": "directory_grouping", "reason": "the directory holds two works"},
	}
	writes := map[string]map[string]any{
		"library_claim_series":        {},
		"library_submit_verdicts":     {"request_key": "k", "lease_id": "l", "verdicts": []any{notFoundVerdict(ids[0], "read only")}},
		"library_add_series_relation": {"request_key": "r", "series_id": ids[0], "target_series_id": ids[1], "relation_type": "sequel", "reason": "read only"},
	}
	refused := func(agent *mainLoopAgent, name string, args map[string]any) bool {
		t.Helper()
		result, err := agent.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		return err != nil || result.IsError
	}
	for _, tc := range []struct {
		name         string
		owner        int64
		capabilities []string
	}{
		{"read only", 1, []string{delegated.Read}},
		// A regular account's credential acts with read alone, whatever else it names.
		{"regular account", regular.ID, []string{delegated.Read, delegated.MetadataWrite}},
	} {
		agent := web.mainLoopAgent(cfg, tc.owner, []int64{library.lib.ID}, tc.capabilities)
		listed, err := agent.session.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for name, args := range inspection {
			if !offersTool(listed, name) || refused(agent, name, args) {
				t.Fatalf("%s: %s is not usable", tc.name, name)
			}
		}
		for name, args := range writes {
			if !refused(agent, name, args) {
				t.Fatalf("%s: %s was taken", tc.name, name)
			}
		}
	}
	writer := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.MetadataWrite})
	listed, err := writer.session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, args := range inspection {
		if offersTool(listed, name) || !refused(writer, name, args) {
			t.Fatalf("%s is usable without read", name)
		}
	}

	// Nothing was claimed or decided: the owner's agent still gets both series.
	for _, id := range ids {
		if got := web.identification(id); got.Status != "unprocessed" || len(got.Conclusions) != 0 {
			t.Fatalf("series %d changed %+v", id, got)
		}
	}
	if batch := writer.claim(map[string]any{}); !reflect.DeepEqual(batch.ids(), ids) {
		t.Fatalf("claim after inspection %v, want %v", batch.ids(), ids)
	}
	if notes := web.ownerVerdicts(ids[0]).Escalations; len(notes) != 1 || notes[0].Category != "directory_grouping" {
		t.Fatalf("notes %+v", notes)
	}
}

// closeNotes closes the series' open notes of one category the way the owner does, from the
// pending list.
func (w *identificationWeb) closeNotes(series int64, category string) {
	w.t.Helper()
	for _, item := range w.pending("").Items {
		for _, e := range item.Identification.Escalations {
			if item.SeriesID == series && e.Category == category {
				w.decide(notePath(series, e.ID), ``, http.StatusOK, nil)
			}
		}
	}
}

func TestNoteTheOwnerClosedIsNotRaisedAgain(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library, ids := claimLibrary(t, store, "Closed", "Title 1", "Title 2")
	sources := &recordingSources{answer: candidatesBySeries}
	clock := &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	web := openClaimWeb(t, store, cfg, sources, clock)
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	if first, _ := agent.report(ids[0], "suspected_duplicate", "same work as series 40"); !first.Recorded {
		t.Fatalf("first report %+v", first)
	}
	web.closeNotes(ids[0], "suspected_duplicate")

	// Closed by the owner, the category stays closed for the series on both ways of noting it.
	if again, _ := agent.report(ids[0], "suspected_duplicate", "same work as series 40 still"); again.Recorded || again.Status != "unprocessed" {
		t.Fatalf("report after the owner closed the note %+v", again)
	}
	batch := agent.claim(map[string]any{})
	wantResult(t, agent.submit("closed", batch.LeaseID, escalateVerdict(ids[0], "suspected_duplicate", "the same work as series 40"))[0], ids[0], true, "unprocessed")
	if open := web.ownerVerdicts(ids[0]).Escalations; len(open) != 0 {
		t.Fatalf("a closed note was raised again %+v", open)
	}
	// Another category, or the same one on another series, is a note of its own.
	if other, _ := agent.report(ids[0], "tag_problem", "tags name the shop"); !other.Recorded {
		t.Fatalf("another category %+v", other)
	}
	if elsewhere, _ := agent.report(ids[1], "suspected_duplicate", "same work as series 41"); !elsewhere.Recorded {
		t.Fatalf("the category on another series %+v", elsewhere)
	}
	if open := web.ownerVerdicts(ids[0]).Escalations; len(open) != 1 || open[0].Category != "tag_problem" {
		t.Fatalf("open notes %+v", open)
	}
	for _, id := range ids {
		if got := web.identification(id); got.Status != "unprocessed" {
			t.Fatalf("notes changed series %d: %+v", id, got)
		}
	}
}
