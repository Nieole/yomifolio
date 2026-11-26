// These tests guard the search of one series with a custom term: the owner's route and the agent's
// tool run the same use case, and what the sources return joins the series' evidence packet under
// the packet's rules without losing what it already held. Sources are recording stand-ins.

package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/metadata"
	"yomifolio/internal/task"
)

// researchedPacket is an evidence packet with the searches made for it with custom terms.
type researchedPacket struct {
	evidencePacket
	Searches []struct {
		Query      string   `json:"query"`
		SearchedAt string   `json:"searched_at"`
		Added      []string `json:"added"`
		Dropped    int      `json:"dropped"`
	} `json:"searches"`
}

// researchResult mirrors the outcome of one research.
type researchResult struct {
	SeriesID int64             `json:"series_id"`
	Query    string            `json:"query"`
	Status   string            `json:"status"`
	RunID    int64             `json:"run_id"`
	Added    []string          `json:"added"`
	Dropped  int               `json:"dropped"`
	Packet   *researchedPacket `json:"packet"`
}

// research asks the owner's route to search the series again and decodes the outcome.
func (w *identificationWeb) research(series int64, body string, want int) researchResult {
	w.t.Helper()
	rec := w.raw("POST", "/api/identification/series/"+strconv.FormatInt(series, 10)+"/research", body)
	if rec.Code != want {
		w.t.Fatalf("research %d %s: %d %s", series, body, rec.Code, rec.Body)
	}
	var out researchResult
	if want == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			w.t.Fatalf("research %s: %v", rec.Body, err)
		}
	}
	return out
}

func (w *identificationWeb) researchedEvidence(series int64) *researchedPacket {
	w.t.Helper()
	var out struct {
		Evidence *researchedPacket `json:"evidence"`
	}
	w.call("GET", "/api/identification/series/"+strconv.FormatInt(series, 10), http.StatusOK, &out)
	return out.Evidence
}

func candidateIDs(p *researchedPacket) []string {
	var ids []string
	for _, c := range p.Candidates {
		ids = append(ids, c.ID)
	}
	return ids
}

func bangumiEntry(id int, title string) *metadata.SeriesMetadata {
	return &metadata.SeriesMetadata{Title: title, SourceID: id, SourceURL: fmt.Sprintf("https://bgm.tv/subject/%d", id)}
}

func TestOwnerResearchMergesNewCandidatesIntoThePacket(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Research")
	series := library.series("Kilo [Group]", "", "Kilo 01.cbz", "Kilo 02.cbz")
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		if source != "bangumi" {
			return nil, nil
		}
		if query == "Kilo" {
			return []*metadata.SeriesMetadata{bangumiEntry(701, "Kilo Wrong"), bangumiEntry(702, "Kilo Other")}, nil
		}
		// The custom term finds one entry the packet already holds, three new ones and a fourth that
		// the per-source bound leaves out; the title that matches the directory ranks first.
		return []*metadata.SeriesMetadata{bangumiEntry(702, "Kilo Other"), bangumiEntry(703, "Kilo Side"), bangumiEntry(704, "Kilo Extra"),
			bangumiEntry(705, "Kilo Spin"), bangumiEntry(706, "Kilo")}, nil
	}}
	web := openPrefetchWeb(t, store, cfg, sources)
	web.prefetch("{}")

	before := time.Now().UTC().Add(-time.Second)
	out := web.research(series, `{"query":"  キロ   Kilo  "}`, http.StatusOK)
	if out.Status != "completed" || out.SeriesID != series || out.Query != "キロ Kilo" || out.RunID == 0 {
		t.Fatalf("research outcome %+v", out)
	}
	if got := sources.queries("bangumi"); !slices.Equal(got, []string{"Kilo", "キロ Kilo"}) {
		t.Fatalf("bangumi received %q", got)
	}
	if want := []string{"bangumi:706", "bangumi:703", "bangumi:704"}; !slices.Equal(out.Added, want) {
		t.Fatalf("added %q want %q", out.Added, want)
	}
	packet := web.researchedEvidence(series)
	if want := []string{"bangumi:701", "bangumi:702", "bangumi:706", "bangumi:703", "bangumi:704"}; !slices.Equal(candidateIDs(packet), want) {
		t.Fatalf("candidates %q want %q", candidateIDs(packet), want)
	}
	if !slices.Equal(packet.Queries, []string{"Kilo"}) || len(packet.Searches) != 1 {
		t.Fatalf("queries %q searches %+v", packet.Queries, packet.Searches)
	}
	search := packet.Searches[0]
	searchedAt, err := time.Parse(time.RFC3339, search.SearchedAt)
	if search.Query != "キロ Kilo" || err != nil || searchedAt.Before(before) || !slices.Equal(search.Added, out.Added) {
		t.Fatalf("recorded search %+v %v", search, err)
	}
	for _, c := range packet.Candidates {
		if c.ID == "bangumi:706" && (!c.Signals.TitleMatches || c.Signals.LocalBooks != 2) {
			t.Fatalf("signals of the new candidate %+v", c.Signals)
		}
	}
	if out.Packet == nil || !slices.Equal(candidateIDs(out.Packet), candidateIDs(packet)) {
		t.Fatalf("outcome packet %+v", out.Packet)
	}
}

// researchTool calls the agent's research tool, checks that its text copy carries the same
// result, and decodes it; a tool error comes back as its text.
func researchTool(t *testing.T, session *mcp.ClientSession, args map[string]any) (researchResult, string) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "library_research_series", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		return researchResult{}, resultText(result)
	}
	var out, copied researchResult
	raw, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(resultText(result)), &copied); err != nil || !reflect.DeepEqual(copied, out) {
		t.Fatalf("text copy %q differs from the structured result: %v", resultText(result), err)
	}
	return out, ""
}

func TestAgentResearchesThroughTheSameUseCase(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Agent research")
	series := library.series("Lima", "", "01.cbz")
	reviewed := library.series("Mike", "", "01.cbz")
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		if source == "bangumi" && query == "リマ" {
			return []*metadata.SeriesMetadata{bangumiEntry(801, "Lima")}, nil
		}
		return nil, nil
	}}
	web := openPrefetchWeb(t, store, cfg, sources)
	web.prefetch("{}")
	web.call("PUT", "/api/identification/series/"+strconv.FormatInt(reviewed, 10)+"/owner-review", http.StatusOK, nil)
	writer := web.identificationAgent(cfg, 1, []int64{library.lib.ID}, []string{"read", "metadata_write"})

	listed, err := writer.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var tool *mcp.Tool
	for _, candidate := range listed.Tools {
		if candidate.Name == "library_research_series" {
			tool = candidate
		}
	}
	if tool == nil || len(tool.Description) > 400 || !strings.Contains(tool.Description, `Example: {"series_id":`) || tool.Annotations == nil || tool.Annotations.ReadOnlyHint {
		t.Fatalf("research tool %+v", tool)
	}

	out, refused := researchTool(t, writer, map[string]any{"series_id": series, "query": "リマ"})
	if refused != "" || out.Status != "completed" || !slices.Equal(out.Added, []string{"bangumi:801"}) || out.Packet == nil || len(out.Packet.Searches) != 1 {
		t.Fatalf("agent research %+v %s", out, refused)
	}
	if packet := web.researchedEvidence(series); !slices.Contains(candidateIDs(packet), "bangumi:801") {
		t.Fatalf("the owner does not see the agent's candidate: %q", candidateIDs(packet))
	}
	if _, refused := researchTool(t, writer, map[string]any{"series_id": reviewed, "query": "マイク"}); !strings.Contains(refused, "owner-reviewed") {
		t.Fatalf("owner-reviewed series researched: %q", refused)
	}
	if got := sources.queries("bangumi"); slices.Contains(got, "マイク") {
		t.Fatalf("an owner-reviewed series reached the sources: %q", got)
	}

	reader := web.identificationAgent(cfg, 1, []int64{library.lib.ID}, []string{"read"})
	if listed, err := reader.ListTools(context.Background(), nil); err != nil || offersTool(listed, "library_research_series") {
		t.Fatalf("a read-only credential lists the research tool: %v", err)
	}
	if _, refused := researchTool(t, reader, map[string]any{"series_id": series, "query": "Lima again"}); !strings.Contains(refused, "delegated access denied") {
		t.Fatalf("a read-only credential researched: %q", refused)
	}
}

func TestResearchLeavesOutExcludedCandidates(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Research excluded")
	series := library.series("November", "", "01.cbz")
	historicalProposal(t, store, series, "bangumi", 902, "https://bgm.tv/subject/902", `{"Title":"November (wrong)"}`, "rejected")
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		if source != "bangumi" || query == "November" {
			return nil, nil
		}
		return []*metadata.SeriesMetadata{bangumiEntry(901, "November A"), bangumiEntry(902, "November B"), bangumiEntry(903, "November C"), bangumiEntry(904, "November D")}, nil
	}}
	web := openPrefetchWeb(t, store, cfg, sources)
	web.prefetch("{}")

	// The excluded entry never takes one of the source's three places: the next entry gets it.
	out := web.research(series, `{"query":"ノベンバー"}`, http.StatusOK)
	if want := []string{"bangumi:901", "bangumi:903", "bangumi:904"}; out.Status != "completed" || !slices.Equal(out.Added, want) || !slices.Equal(candidateIDs(out.Packet), want) {
		t.Fatalf("research %+v want %q", out, want)
	}
}

func TestResearchSurvivesSourceFailures(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Research failures")
	series := library.series("Oscar", "", "01.cbz")
	var mu sync.Mutex
	down := map[string]bool{"mangadex": true}
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		mu.Lock()
		defer mu.Unlock()
		if down[source] {
			return nil, fmt.Errorf("%s unreachable", source)
		}
		switch {
		case query == "Oscar" && source == "bangumi":
			return []*metadata.SeriesMetadata{bangumiEntry(1001, "Oscar")}, nil
		case query != "Oscar" && source == "anilist":
			return []*metadata.SeriesMetadata{{Title: "Oscar Story", SourceID: 2001, SourceURL: "https://anilist.co/manga/2001"}}, nil
		case query != "Oscar" && source == "mangadex":
			return []*metadata.SeriesMetadata{{Title: "Oscar", SourceURL: "https://mangadex.org/title/0b1c2d3e-4f50-6172-8394-a5b6c7d8e9f0"}}, nil
		}
		return nil, nil
	}}
	web := openPrefetchWeb(t, store, cfg, sources)
	web.prefetch("{}")
	statusOf := func(p *researchedPacket, source string) string {
		for _, s := range p.Sources {
			if s.Source == source {
				return s.Status
			}
		}
		return ""
	}

	// Bangumi fails now; AniList still adds its candidate and the earlier Bangumi one stays.
	mu.Lock()
	down["bangumi"] = true
	mu.Unlock()
	out := web.research(series, `{"query":"Oscar Story"}`, http.StatusOK)
	if out.Status != "completed" || !slices.Equal(out.Added, []string{"anilist:2001"}) || !slices.Equal(candidateIDs(out.Packet), []string{"bangumi:1001", "anilist:2001"}) {
		t.Fatalf("partial research %+v", out)
	}
	if statusOf(out.Packet, "bangumi") != "available" || statusOf(out.Packet, "mangadex") != "unavailable" {
		t.Fatalf("source statuses %+v", out.Packet.Sources)
	}

	// No source answers: the run fails, the packet stays as it was and the term is not recorded.
	mu.Lock()
	down = map[string]bool{"bangumi": true, "anilist": true, "mangadex": true}
	mu.Unlock()
	failed := web.research(series, `{"query":"Oscar Mangadex"}`, http.StatusOK)
	if failed.Status != "failed" || failed.RunID == 0 || len(failed.Added) != 0 || len(failed.Packet.Searches) != 1 || len(failed.Packet.Candidates) != 2 {
		t.Fatalf("unanswered research %+v", failed)
	}
	if run, err := web.process.Application.Engine.SnapshotOf(context.Background(), failed.RunID); err != nil || run.Run.Status != task.StatusFailed {
		t.Fatalf("unanswered research run %+v %v", run.Run, err)
	}

	// Once MangaDex is back, the same term is searched again and its source becomes available.
	mu.Lock()
	down = map[string]bool{}
	mu.Unlock()
	again := web.research(series, `{"query":"Oscar Mangadex"}`, http.StatusOK)
	if again.Status != "completed" || !slices.Equal(again.Added, []string{"mangadex:0b1c2d3e-4f50-6172-8394-a5b6c7d8e9f0"}) || statusOf(again.Packet, "mangadex") != "available" {
		t.Fatalf("retried research %+v", again)
	}
}

// gate holds one source search until the test lets it go.
type gate struct {
	entered, release chan struct{}
	once             sync.Once
}

func newGate() *gate { return &gate{entered: make(chan struct{}), release: make(chan struct{})} }

func (g *gate) hold() { g.once.Do(func() { close(g.entered) }); <-g.release }

func (g *gate) open() {
	select {
	case <-g.release:
	default:
		close(g.release)
	}
}

func TestResearchRunsOnAfterTheCallAndCanBeCancelled(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Research runs")
	papa, quebec, romeo := library.series("Papa", "", "01.cbz"), library.series("Quebec", "", "01.cbz"), library.series("Romeo", "", "01.cbz")
	gates := map[string]*gate{"Papa Long": newGate(), "Quebec Long": newGate(), "Romeo Long": newGate()}
	for _, g := range gates {
		t.Cleanup(g.open)
	}
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		if g := gates[query]; g != nil && source == "bangumi" {
			g.hold()
			return []*metadata.SeriesMetadata{bangumiEntry(len(query)*100, query)}, nil
		}
		return nil, nil
	}}
	web := openPrefetchWeb(t, store, cfg, sources)
	web.prefetch("{}")
	agent := web.identificationAgent(cfg, 1, []int64{library.lib.ID}, []string{"read", "metadata_write"})
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result, err := agent.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("%s %v: %+v %v", name, args, result, err)
		}
		out := map[string]any{}
		raw, _ := json.Marshal(result.StructuredContent)
		_ = json.Unmarshal(raw, &out)
		return out
	}
	await := func(runID int64) task.Run {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		run, err := web.process.Application.Engine.Await(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		return run
	}

	// Not finished within the wait: the run is reported and goes on; waiting on it and repeating
	// the call reads what it stored without searching again.
	started, _ := researchTool(t, agent, map[string]any{"series_id": papa, "query": "Papa Long", "wait_ms": 0})
	if started.Status != "running" || started.RunID == 0 || started.Packet != nil {
		t.Fatalf("unfinished research %+v", started)
	}
	if run := call("library_get_run", map[string]any{"run_id": started.RunID}); run["status"] == "" {
		t.Fatalf("run read %+v", run)
	}
	<-gates["Papa Long"].entered
	gates["Papa Long"].open()
	if run := call("library_wait_run", map[string]any{"run_id": started.RunID, "timeout_ms": 10000}); run["status"] != "completed" {
		t.Fatalf("waited run %+v", run)
	}
	searched := len(sources.queries("bangumi"))
	repeated, _ := researchTool(t, agent, map[string]any{"series_id": papa, "query": "papa  long"})
	if repeated.Status != "already_searched" || len(repeated.Added) != 1 || repeated.Packet == nil || !slices.Contains(candidateIDs(repeated.Packet), repeated.Added[0]) {
		t.Fatalf("repeated research %+v", repeated)
	}
	if len(sources.queries("bangumi")) != searched {
		t.Fatal("a repeated term was searched again")
	}

	// The caller going away does not cancel the search.
	ctx, hangUp := context.WithCancel(context.Background())
	called := make(chan error, 1)
	go func() {
		_, err := agent.CallTool(ctx, &mcp.CallToolParams{Name: "library_research_series", Arguments: map[string]any{"series_id": quebec, "query": "Quebec Long"}})
		called <- err
	}()
	<-gates["Quebec Long"].entered
	hangUp()
	if err := <-called; err == nil {
		t.Fatal("the abandoned call returned a result")
	}
	runs, err := web.process.Application.Tasks.ListRuns(context.Background(), task.RunFilter{Types: []task.Type{"evidence_research"}})
	if err != nil || len(runs) != 2 {
		t.Fatalf("research runs %+v %v", runs, err)
	}
	abandoned := runs[slices.IndexFunc(runs, func(r task.Run) bool { return r.ID != started.RunID })].ID
	gates["Quebec Long"].open()
	if run := await(abandoned); run.Status != task.StatusCompleted {
		t.Fatalf("abandoned research run %+v", run)
	}
	if packet := web.researchedEvidence(quebec); len(packet.Searches) != 1 || packet.Searches[0].Query != "Quebec Long" {
		t.Fatalf("abandoned research did not store its candidates: %+v", packet)
	}

	// Cancelling the run stores nothing.
	cancelled, _ := researchTool(t, agent, map[string]any{"series_id": romeo, "query": "Romeo Long", "wait_ms": 0})
	<-gates["Romeo Long"].entered
	call("library_cancel_run", map[string]any{"run_id": cancelled.RunID})
	gates["Romeo Long"].open()
	if run := await(cancelled.RunID); run.Status != task.StatusCancelled {
		t.Fatalf("cancelled research run %+v", run)
	}
	if packet := web.researchedEvidence(romeo); len(packet.Searches) != 0 || len(packet.Candidates) != 0 {
		t.Fatalf("a cancelled research stored %+v", packet)
	}
}

func TestResearchTermsAreBounded(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Research terms")
	series := library.series("Sierra", "", "01.cbz")
	sources := &recordingSources{answer: func(string, string) ([]*metadata.SeriesMetadata, error) { return nil, nil }}
	web := openPrefetchWeb(t, store, cfg, sources)
	agent := web.identificationAgent(cfg, 1, []int64{library.lib.ID}, []string{"read", "metadata_write"})

	for _, body := range []string{
		`{"query":""}`, `{"query":"   "}`, `{"query":"!!! ---"}`,
		`{"query":"` + strings.Repeat("长", 101) + `"}`,
		`{"query":"Sierra\nDROP"}`, `{"query":"Sierra\u0000"}`, `{"query":"Sierra\u202eevil"}`,
		`{"query":"Sierra","wait_ms":-1}`, `{"query":"Sierra","wait_ms":30001}`, `not json`,
	} {
		web.research(series, body, http.StatusBadRequest)
	}
	if _, refused := researchTool(t, agent, map[string]any{"series_id": series, "query": strings.Repeat("x", 101)}); !strings.Contains(refused, "invalid research") {
		t.Fatalf("an overlong term was accepted: %q", refused)
	}
	if got := sources.queries("bangumi"); len(got) != 0 {
		t.Fatalf("rejected terms reached the sources: %q", got)
	}
	// The longest allowed term is searched as given, apart from collapsed whitespace.
	longest := strings.Repeat("长", 100)
	if out := web.research(series, `{"query":"`+longest+`"}`, http.StatusOK); out.Status != "completed" {
		t.Fatalf("longest term %+v", out)
	}
	if got := sources.queries("bangumi"); !slices.Contains(got, longest) {
		t.Fatalf("searched %q", got)
	}
	web.research(999999, `{"query":"Sierra"}`, http.StatusNotFound)
	if _, refused := researchTool(t, agent, map[string]any{"series_id": 999999, "query": "Sierra"}); !strings.Contains(refused, "delegated access denied") {
		t.Fatalf("an unknown series was not a denial: %q", refused)
	}
}

func TestResearchBuildsAMissingPacketFromTheDerivedTermsFirst(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Research new packet")
	series := library.series("[Circle] Tango 01-05", `<ComicInfo><Series>Tango</Series><Writer>Uma</Writer></ComicInfo>`, "Tango 01.cbz")
	historicalProposal(t, store, series, "bangumi", 1101, "https://bgm.tv/subject/1101", `{}`, "applied")
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		if source != "bangumi" {
			return nil, nil
		}
		switch query {
		case "Tango":
			return []*metadata.SeriesMetadata{bangumiEntry(1101, "Tango")}, nil
		case "タンゴ":
			return []*metadata.SeriesMetadata{bangumiEntry(1102, "Tango Remix")}, nil
		}
		return nil, nil
	}}
	web := openPrefetchWeb(t, store, cfg, sources)
	// An identified series gets no prefetch, so the research is the first search it sees.
	if launch, _ := web.prefetch("{}"); launch.Series != 0 {
		t.Fatalf("prefetch picked an identified series %+v", launch)
	}

	out := web.research(series, `{"query":"タンゴ"}`, http.StatusOK)
	if got := sources.queries("bangumi"); !slices.Equal(got, []string{"Tango", "Tango Uma", "タンゴ"}) {
		t.Fatalf("searched %q", got)
	}
	if out.Status != "completed" || !slices.Equal(out.Added, []string{"bangumi:1102"}) || out.Packet == nil {
		t.Fatalf("research %+v", out)
	}
	if p := out.Packet; !slices.Equal(p.Queries, []string{"Tango", "Tango Uma"}) || p.Local.ComicInfo == nil || p.Local.ComicInfo.Series != "Tango" ||
		!slices.Equal(candidateIDs(p), []string{"bangumi:1101", "bangumi:1102"}) || p.FetchedAt == "" {
		t.Fatalf("built packet %+v", p)
	}
	// A term that is one of the derived terms is covered already.
	if again := web.research(series, `{"query":"tango uma"}`, http.StatusOK); again.Status != "already_searched" || len(again.Added) != 0 {
		t.Fatalf("derived term %+v", again)
	}
}

func TestResearchKeepsTheHeldCandidatesWhenThePacketIsFull(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Research budget")
	series := library.series("Victor", "", "01.cbz")
	bulky := func(source string, id int) *metadata.SeriesMetadata {
		url := map[string]string{"bangumi": "https://bgm.tv/subject/%d", "anilist": "https://anilist.co/manga/%d", "mangadex": ""}[source]
		entry := &metadata.SeriesMetadata{Title: strings.Repeat("长", 99) + strconv.Itoa(id%10), OriginalTitle: strings.Repeat("原", 99) + strconv.Itoa(id%10),
			Summary: strings.Repeat("简介", 80), SourceID: id, Authors: []metadata.SeriesAuthor{{Name: strings.Repeat("作", 40)}, {Name: strings.Repeat("画", 40)}, {Name: strings.Repeat("者", 40)}}}
		if url != "" {
			entry.SourceURL = fmt.Sprintf(url, id)
		} else {
			entry.SourceID, entry.SourceURL = 0, fmt.Sprintf("https://mangadex.org/title/%08d-0000-4000-8000-000000000000", id)
		}
		return entry
	}
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		base := map[string]int{"bangumi": 1000, "anilist": 2000, "mangadex": 3000}[source]
		if query != "Victor" {
			base += 500
		}
		return []*metadata.SeriesMetadata{bulky(source, base+1), bulky(source, base+2), bulky(source, base+3)}, nil
	}}
	web := openPrefetchWeb(t, store, cfg, sources)
	web.prefetch("{}")
	held := candidateIDs(web.researchedEvidence(series))

	rec := web.raw("POST", "/api/identification/series/"+strconv.FormatInt(series, 10)+"/research", `{"query":"Victor Bulky"}`)
	var raw struct {
		Packet json.RawMessage `json:"packet"`
	}
	var out researchResult
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		t.Fatalf("research %d %s", rec.Code, rec.Body)
	}
	if out.Status != "completed" || out.Dropped == 0 || len(out.Added) == 0 || len(out.Added)+out.Dropped != 9 {
		t.Fatalf("research added %q dropped %d", out.Added, out.Dropped)
	}
	if len(raw.Packet) > 8<<10 {
		t.Fatalf("packet is %d bytes", len(raw.Packet))
	}
	ids := candidateIDs(out.Packet)
	for _, id := range held {
		if !slices.Contains(ids, id) {
			t.Fatalf("held candidate %s was dropped: %q", id, ids)
		}
	}
	for _, c := range out.Packet.Candidates {
		if !slices.Contains(out.Added, c.ID) && !slices.Contains(held, c.ID) {
			t.Fatalf("candidate %s is neither held nor added", c.ID)
		}
	}
	if search := out.Packet.Searches[0]; search.Dropped != out.Dropped || !slices.Equal(search.Added, out.Added) {
		t.Fatalf("recorded search %+v", search)
	}
}
