// These tests guard the owner-started evidence prefetch of the assembled process: which series it
// searches and with which terms, what the stored packet holds, and how source failures and the
// owner's exclusions shape it. Sources are recording stand-ins; the packet is read back through
// the owner's identification route.

package bootstrap_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/api"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/task"
)

// sourceCall is one search a stand-in source received.
type sourceCall struct {
	Source string
	Query  string
	At     time.Time
}

// recordingSources answers every catalogue source from answer and records each search.
type recordingSources struct {
	mu     sync.Mutex
	calls  []sourceCall
	answer func(source, query string) ([]*metadata.SeriesMetadata, error)
}

func (r *recordingSources) options() bootstrap.Options {
	return bootstrap.Options{Sources: api.ExternalSources{Metadata: func(_ config.Config, name string) metadata.Provider {
		return &controlledSource{
			fetch: func(context.Context, string) (*metadata.SeriesMetadata, error) { return nil, nil },
			search: func(_ context.Context, query string, limit, _ int) ([]*metadata.SeriesMetadata, int, error) {
				r.mu.Lock()
				r.calls = append(r.calls, sourceCall{Source: name, Query: query, At: time.Now()})
				r.mu.Unlock()
				results, err := r.answer(name, query)
				if len(results) > limit {
					results = results[:limit]
				}
				return results, len(results), err
			},
		}
	}}}
}

// queries lists, in order, the terms one source received.
func (r *recordingSources) queries(source string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, c := range r.calls {
		if c.Source == source {
			out = append(out, c.Query)
		}
	}
	return out
}

// times lists, in order, when one source received each search.
func (r *recordingSources) times(source string) []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []time.Time
	for _, c := range r.calls {
		if c.Source == source {
			out = append(out, c.At)
		}
	}
	return out
}

// unpaced turns the per-source request spacing off, as a test configuration may.
func unpaced(t *testing.T, cfg *config.Manager) {
	t.Helper()
	snapshot := cfg.Snapshot()
	snapshot.Identification.SourceIntervalsMS = config.SourceIntervals{Bangumi: -1, AniList: -1, MangaDex: -1, MyAnimeList: -1, ComicVine: -1}
	cfg.Replace(&snapshot)
}

// openPrefetchWeb opens the production process with the recording sources and an administrator.
func openPrefetchWeb(t *testing.T, store database.Store, cfg *config.Manager, sources *recordingSources) *identificationWeb {
	t.Helper()
	process, err := bootstrap.Open(context.Background(), store, cfg, sources.options())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(process.Close)
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	web := &identificationWeb{t: t, store: store, process: process, router: router}
	web.setupAdmin()
	return web
}

// prefetchLaunch mirrors the owner's launch response.
type prefetchLaunch struct {
	RunID  int64 `json:"run_id"`
	Series int   `json:"series"`
}

// prefetch launches the owner's prefetch and waits for its run to finish.
func (w *identificationWeb) prefetch(body string) (prefetchLaunch, task.Run) {
	w.t.Helper()
	rec := w.raw("POST", "/api/identification/prefetch", body)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("prefetch %s: %d %s", body, rec.Code, rec.Body)
	}
	var launch prefetchLaunch
	if err := json.Unmarshal(rec.Body.Bytes(), &launch); err != nil {
		w.t.Fatal(err)
	}
	if launch.RunID == 0 {
		return launch, task.Run{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	run, err := w.process.Application.Engine.Await(ctx, launch.RunID)
	if err != nil {
		w.t.Fatal(err)
	}
	return launch, run
}

// evidencePacket mirrors the packet the owner's identification route serves.
type evidencePacket struct {
	FetchedAt string `json:"fetched_at"`
	Local     struct {
		DirectoryName string   `json:"directory_name"`
		BookCount     int      `json:"book_count"`
		FileNames     []string `json:"file_names"`
		ComicInfo     *struct {
			Series  string   `json:"series"`
			Authors []string `json:"authors"`
			Year    int      `json:"year"`
		} `json:"comic_info"`
	} `json:"local"`
	Queries []string `json:"queries"`
	Sources []struct {
		Source string `json:"source"`
		Status string `json:"status"`
	} `json:"sources"`
	Candidates []struct {
		ID            string   `json:"id"`
		Source        string   `json:"source"`
		EntryURL      string   `json:"entry_url"`
		Title         string   `json:"title"`
		OriginalTitle string   `json:"original_title"`
		Authors       []string `json:"authors"`
		Year          int      `json:"year"`
		Summary       string   `json:"summary"`
		Signals       struct {
			TitleMatches  bool  `json:"title_matches"`
			AuthorMatches *bool `json:"author_matches"`
			SourceVolumes int   `json:"source_volumes"`
			LocalBooks    int   `json:"local_books"`
		} `json:"signals"`
	} `json:"candidates"`
}

func (w *identificationWeb) evidence(series int64) *evidencePacket {
	w.t.Helper()
	var out struct {
		Evidence *evidencePacket `json:"evidence"`
	}
	w.call("GET", "/api/identification/series/"+strconv.FormatInt(series, 10), http.StatusOK, &out)
	return out.Evidence
}

// prefetchLibrary is a library whose series sit in directories under its root.
type prefetchLibrary struct {
	t     *testing.T
	store database.Store
	lib   database.Library
}

func newPrefetchLibrary(t *testing.T, store database.Store, name string) *prefetchLibrary {
	t.Helper()
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: name, Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	return &prefetchLibrary{t: t, store: store, lib: lib}
}

// series creates a series named after its directory, with one book per file name. comicInfo, when
// not empty, is the ComicInfo.xml every book carries.
func (l *prefetchLibrary) series(directory, comicInfo string, files ...string) int64 {
	l.t.Helper()
	ctx := context.Background()
	dir := filepath.Join(l.lib.Path, directory)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		l.t.Fatal(err)
	}
	s, err := l.store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: l.lib.ID, Name: directory, Path: dir})
	if err != nil {
		l.t.Fatal(err)
	}
	for i, file := range files {
		var archive bytes.Buffer
		z := zip.NewWriter(&archive)
		entries := map[string][]byte{"01.png": {0x89, 'P', 'N', 'G'}}
		if comicInfo != "" {
			entries["ComicInfo.xml"] = []byte(comicInfo)
		}
		for name, data := range entries {
			w, err := z.Create(name)
			if err != nil {
				l.t.Fatal(err)
			}
			if _, err := w.Write(data); err != nil {
				l.t.Fatal(err)
			}
		}
		if err := z.Close(); err != nil {
			l.t.Fatal(err)
		}
		path := filepath.Join(dir, file)
		if err := os.WriteFile(path, archive.Bytes(), 0o600); err != nil {
			l.t.Fatal(err)
		}
		if _, err := l.store.CreateBook(ctx, database.CreateBookParams{LibraryID: l.lib.ID, SeriesID: s.ID, Name: file, Path: path, Size: int64(archive.Len()), FileModifiedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Volume: fmt.Sprintf("%03d", i+1), PageCount: 1}); err != nil {
			l.t.Fatal(err)
		}
	}
	return s.ID
}

func TestManualPrefetchStoresEvidencePacketForUnprocessedSeries(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Manga")
	series := library.series("[Circle] Alpha Story Vol.01-03 [Moon汉化组]",
		`<ComicInfo><Series>Alpha Story</Series><Writer>Ann Writer</Writer><Penciller>Bo Artist</Penciller><Year>2001</Year></ComicInfo>`,
		"Alpha Story 01.cbz", "Alpha Story 02.cbz", "Alpha Story 03.cbz", "Alpha Story 04.cbz")
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		switch source {
		case "bangumi":
			return []*metadata.SeriesMetadata{
				{Title: "阿尔法物语", OriginalTitle: "Alpha Story", Authors: []metadata.SeriesAuthor{{Name: "Ann Writer", Role: "作者"}}, ReleaseDate: "2001-04-01", VolumeCount: 4, Summary: "A long summary about the story that keeps going well past any excerpt the packet would keep for an agent to read in one go.", SourceID: 101, SourceURL: "https://bgm.tv/subject/101"},
				{Title: "Beta Story", Authors: []metadata.SeriesAuthor{{Name: "Someone Else"}}, VolumeCount: 9, SourceID: 102, SourceURL: "https://bgm.tv/subject/102"},
			}, nil
		case "anilist":
			return []*metadata.SeriesMetadata{{Title: "Alpha Story", SourceID: 201, SourceURL: "https://anilist.co/manga/201"}}, nil
		}
		return nil, fmt.Errorf("%s is down", source)
	}}
	web := openPrefetchWeb(t, store, cfg, sources)

	launch, run := web.prefetch(fmt.Sprintf(`{"library_id":%d}`, library.lib.ID))
	if launch.Series != 1 || run.Status != task.StatusCompleted {
		t.Fatalf("launch %+v run %+v", launch, run)
	}
	packet := web.evidence(series)
	if packet == nil {
		t.Fatal("no evidence packet after prefetch")
	}
	if packet.FetchedAt == "" || packet.Local.DirectoryName != "[Circle] Alpha Story Vol.01-03 [Moon汉化组]" || packet.Local.BookCount != 4 ||
		!slices.Equal(packet.Local.FileNames, []string{"Alpha Story 01.cbz", "Alpha Story 02.cbz", "Alpha Story 03.cbz"}) {
		t.Fatalf("local clues %+v", packet.Local)
	}
	if ci := packet.Local.ComicInfo; ci == nil || ci.Series != "Alpha Story" || !slices.Equal(ci.Authors, []string{"Ann Writer", "Bo Artist"}) || ci.Year != 2001 {
		t.Fatalf("ComicInfo clues %+v", packet.Local.ComicInfo)
	}
	if want := []string{"Alpha Story", "Alpha Story Ann Writer"}; !slices.Equal(packet.Queries, want) || !slices.Equal(sources.queries("bangumi"), want) {
		t.Fatalf("queries packet=%q bangumi=%q", packet.Queries, sources.queries("bangumi"))
	}
	statuses := map[string]string{}
	for _, s := range packet.Sources {
		statuses[s.Source] = s.Status
	}
	if !maps.Equal(statuses, map[string]string{"bangumi": "available", "anilist": "available", "mangadex": "unavailable"}) {
		t.Fatalf("source statuses %v", statuses)
	}
	if len(packet.Candidates) != 3 {
		t.Fatalf("candidates %+v", packet.Candidates)
	}
	alpha, beta, ani := packet.Candidates[0], packet.Candidates[1], packet.Candidates[2]
	if alpha.ID != "bangumi:101" || alpha.Source != "bangumi" || alpha.EntryURL != "https://bgm.tv/subject/101" || alpha.Title != "阿尔法物语" || alpha.OriginalTitle != "Alpha Story" ||
		!slices.Equal(alpha.Authors, []string{"Ann Writer"}) || alpha.Year != 2001 || alpha.Summary == "" || len([]rune(alpha.Summary)) > 61 {
		t.Fatalf("first candidate %+v", alpha)
	}
	if !alpha.Signals.TitleMatches || alpha.Signals.AuthorMatches == nil || !*alpha.Signals.AuthorMatches || alpha.Signals.SourceVolumes != 4 || alpha.Signals.LocalBooks != 4 {
		t.Fatalf("first candidate signals %+v", alpha.Signals)
	}
	if beta.ID != "bangumi:102" || beta.Signals.TitleMatches || beta.Signals.AuthorMatches == nil || *beta.Signals.AuthorMatches || beta.Signals.SourceVolumes != 9 {
		t.Fatalf("second candidate %+v", beta)
	}
	if ani.ID != "anilist:201" || !ani.Signals.TitleMatches || ani.Signals.AuthorMatches != nil {
		t.Fatalf("AniList candidate %+v", ani)
	}
}

func TestPrefetchDerivesSearchTermsFromDirectoryAndComicInfo(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Terms")
	cases := []struct {
		directory, comicInfo string
		want                 []string
	}{
		{"[作者] 进击的巨人 第01-34卷 [XX汉化组]", "", []string{"进击的巨人"}},
		{"Holyland 1-18", "", []string{"Holyland"}},
		{"《海贼王》 全100卷 完结", "", []string{"海贼王"}},
		{"One Piece v01-v10", "", []string{"One Piece"}},
		{"[Circle] (C99) Title Name (Original)", "", []string{"Title Name"}},
		{"Old Days 03 TestScans", "", []string{"Old Days"}},
		{"[进击的巨人]", "", []string{"进击的巨人"}},
		{"Shingeki no Kyojin", `<ComicInfo><Series>進撃の巨人</Series><Writer>諫山創</Writer></ComicInfo>`, []string{"Shingeki no Kyojin", "進撃の巨人", "進撃の巨人 諫山創"}},
		{"Some Title 03", `<ComicInfo><Writer>Author A, Author B</Writer></ComicInfo>`, []string{"Some Title", "Some Title Author A"}},
		{"Same Name", `<ComicInfo><Series>same name</Series></ComicInfo>`, []string{"Same Name"}},
	}
	ids := make([]int64, len(cases))
	for i, c := range cases {
		ids[i] = library.series(c.directory, c.comicInfo, "01.cbz")
	}
	sources := &recordingSources{answer: func(string, string) ([]*metadata.SeriesMetadata, error) { return nil, nil }}
	web := openPrefetchWeb(t, store, cfg, sources)

	launch, run := web.prefetch("{}")
	if launch.Series != len(cases) || run.Status != task.StatusCompleted {
		t.Fatalf("launch %+v run %+v", launch, run)
	}
	var everyTerm []string
	for i, c := range cases {
		packet := web.evidence(ids[i])
		if packet == nil || !slices.Equal(packet.Queries, c.want) {
			t.Errorf("%q: queries %+v want %q", c.directory, packet, c.want)
		}
		everyTerm = append(everyTerm, c.want...)
	}
	slices.Sort(everyTerm)
	for _, source := range []string{"bangumi", "anilist", "mangadex"} {
		got := sources.queries(source)
		slices.Sort(got)
		if !slices.Equal(got, everyTerm) {
			t.Errorf("%s received %q want %q", source, got, everyTerm)
		}
	}
}

func TestPrefetchLeavesOutExcludedCandidates(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Excluded")
	series := library.series("Gamma", "", "01.cbz")
	historicalProposal(t, store, series, "bangumi", 301, "https://bgm.tv/subject/301", `{"Title":"Gamma (wrong)"}`, "rejected")
	sources := &recordingSources{answer: func(source, _ string) ([]*metadata.SeriesMetadata, error) {
		if source != "bangumi" {
			return nil, nil
		}
		return []*metadata.SeriesMetadata{
			{Title: "Gamma", SourceID: 301, SourceURL: "https://bgm.tv/subject/301"},
			{Title: "Gamma", SourceID: 302, SourceURL: "https://bgm.tv/subject/302"},
			{Title: "Gamma Two", SourceID: 303, SourceURL: "https://bgm.tv/subject/303"},
			{Title: "Gamma Three", SourceID: 304, SourceURL: "https://bgm.tv/subject/304"},
		}, nil
	}}
	web := openPrefetchWeb(t, store, cfg, sources)

	if _, run := web.prefetch(fmt.Sprintf(`{"library_id":%d}`, library.lib.ID)); run.Status != task.StatusCompleted {
		t.Fatalf("run %+v", run)
	}
	// The excluded entry never takes one of the source's three places: the next entry gets it.
	packet := web.evidence(series)
	var ids []string
	for _, c := range packet.Candidates {
		ids = append(ids, c.ID)
	}
	if want := []string{"bangumi:302", "bangumi:303", "bangumi:304"}; !slices.Equal(ids, want) {
		t.Fatalf("candidates %q want %q", ids, want)
	}
}

func TestPrefetchKeepsSeriesPendingWhenNoSourceAnswers(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Offline")
	series := library.series("Delta", "", "01.cbz")
	var down sync.Mutex
	offline := true
	sources := &recordingSources{answer: func(source, _ string) ([]*metadata.SeriesMetadata, error) {
		down.Lock()
		defer down.Unlock()
		if offline {
			return nil, fmt.Errorf("%s unreachable", source)
		}
		return []*metadata.SeriesMetadata{{Title: "Delta", SourceID: 401, SourceURL: "https://bgm.tv/subject/401"}}, nil
	}}
	web := openPrefetchWeb(t, store, cfg, sources)

	launch, run := web.prefetch("{}")
	if launch.Series != 1 || run.Status != task.StatusCompleted {
		t.Fatalf("launch %+v run %+v", launch, run)
	}
	if packet := web.evidence(series); packet != nil {
		t.Fatalf("a packet nobody answered for was stored: %+v", packet)
	}
	down.Lock()
	offline = false
	down.Unlock()
	launch, _ = web.prefetch("{}")
	if packet := web.evidence(series); launch.Series != 1 || packet == nil || len(packet.Candidates) == 0 {
		t.Fatalf("relaunch did not pick the series up again: %+v %+v", launch, packet)
	}
}

func TestPrefetchSearchesOnlyUnprocessedSeries(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Mixed")
	identified := library.series("Identified One", "", "01.cbz")
	historicalProposal(t, store, identified, "bangumi", 501, "https://bgm.tv/subject/501", `{}`, "applied")
	reviewed := library.series("Reviewed One", "", "01.cbz")
	fresh := library.series("Fresh One", "", "01.cbz")
	other := newPrefetchLibrary(t, store, "Other")
	elsewhere := other.series("Elsewhere One", "", "01.cbz")
	sources := &recordingSources{answer: func(string, string) ([]*metadata.SeriesMetadata, error) { return nil, nil }}
	web := openPrefetchWeb(t, store, cfg, sources)
	web.call("PUT", "/api/identification/series/"+strconv.FormatInt(reviewed, 10)+"/owner-review", http.StatusOK, nil)

	launch, run := web.prefetch(fmt.Sprintf(`{"library_id":%d}`, library.lib.ID))
	if launch.Series != 1 || run.Status != task.StatusCompleted {
		t.Fatalf("launch %+v run %+v", launch, run)
	}
	if got := sources.queries("bangumi"); !slices.Equal(got, []string{"Fresh One"}) {
		t.Fatalf("searched %q", got)
	}
	for _, id := range []int64{identified, reviewed, elsewhere} {
		if packet := web.evidence(id); packet != nil {
			t.Fatalf("series %d outside the unprocessed ones of the library got a packet", id)
		}
	}
	if web.evidence(fresh) == nil {
		t.Fatal("the unprocessed series has no packet")
	}
	// Everything left is ready, so launching again starts nothing.
	if again, _ := web.prefetch(fmt.Sprintf(`{"library_id":%d}`, library.lib.ID)); again.Series != 0 || again.RunID != 0 {
		t.Fatalf("relaunch with nothing pending %+v", again)
	}
	if rec := web.raw("POST", "/api/identification/prefetch", `{"library_id":999999}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown library %d %s", rec.Code, rec.Body)
	}
}

func TestPrefetchPausesCancelsAndContinuesWhereItStopped(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Long")
	ids := map[string]int64{}
	for _, name := range []string{"Alpha", "Bravo", "Charlie"} {
		ids[name] = library.series(name, "", "01.cbz")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		if source == "bangumi" && query == "Bravo" {
			once.Do(func() { close(entered); <-release })
		}
		return nil, nil
	}}
	web := openPrefetchWeb(t, store, cfg, sources)
	var releaseOnce sync.Once
	letGo := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(letGo)

	rec := web.raw("POST", "/api/identification/prefetch", "{}")
	var launch prefetchLaunch
	if err := json.Unmarshal(rec.Body.Bytes(), &launch); err != nil || launch.RunID == 0 || launch.Series != 3 {
		t.Fatalf("launch %d %s", rec.Code, rec.Body)
	}
	<-entered
	runPath := "/api/system/runs/" + strconv.FormatInt(launch.RunID, 10)
	web.call("POST", runPath+"/pause", http.StatusAccepted, nil)
	snapshot, err := web.process.Application.Engine.SnapshotOf(context.Background(), launch.RunID)
	if err != nil || snapshot.Run.Status != task.StatusPaused || !snapshot.Capabilities.CanCancel {
		t.Fatalf("paused run %+v %v", snapshot, err)
	}
	letGo()
	web.call("POST", runPath+"/cancel", http.StatusAccepted, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if run, err := web.process.Application.Engine.Await(ctx, launch.RunID); err != nil || run.Status != task.StatusCancelled {
		t.Fatalf("cancelled run %+v %v", run, err)
	}
	if web.evidence(ids["Alpha"]) == nil || web.evidence(ids["Bravo"]) != nil || web.evidence(ids["Charlie"]) != nil {
		t.Fatal("only the series finished before the cancel may have a packet")
	}

	again, run := web.prefetch("{}")
	if again.Series != 2 || run.Status != task.StatusCompleted {
		t.Fatalf("relaunch %+v %+v", again, run)
	}
	if got := sources.queries("bangumi"); !slices.Equal(got, []string{"Alpha", "Bravo", "Bravo", "Charlie"}) {
		t.Fatalf("searches %q: the relaunch must skip the series that already had a packet", got)
	}
}

func TestPrefetchSpacesRequestsToEachSource(t *testing.T) {
	store, cfg := fixture(t)
	const interval = 200 * time.Millisecond
	snapshot := cfg.Snapshot()
	snapshot.Identification.SourceIntervalsMS = config.SourceIntervals{Bangumi: int(interval / time.Millisecond), AniList: -1, MangaDex: -1, MyAnimeList: -1, ComicVine: -1}
	cfg.Replace(&snapshot)
	library := newPrefetchLibrary(t, store, "Paced")
	library.series("Echo", `<ComicInfo><Writer>Writer E</Writer></ComicInfo>`, "01.cbz")
	library.series("Foxtrot", "", "01.cbz")
	sources := &recordingSources{answer: func(string, string) ([]*metadata.SeriesMetadata, error) { return nil, nil }}
	web := openPrefetchWeb(t, store, cfg, sources)

	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("run %+v", run)
	}
	// The pacer spaces the reserved slots; the stand-in stamps each search a moment after its slot,
	// so a gap may fall short of the interval by that scheduling jitter, never by a whole interval.
	const jitter = 50 * time.Millisecond
	bangumi := sources.times("bangumi")
	if len(bangumi) != 3 {
		t.Fatalf("bangumi searches %d", len(bangumi))
	}
	for i := 1; i < len(bangumi); i++ {
		if gap := bangumi[i].Sub(bangumi[i-1]); gap < interval-jitter {
			t.Fatalf("bangumi searches %d and %d only %v apart", i-1, i, gap)
		}
	}
}

func TestProgressReportsPrefetchReadiness(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Counted")
	library.series("Golf", "", "01.cbz")
	library.series("Hotel", "", "01.cbz")
	library.series("Zulu", "", "01.cbz")
	historicalProposal(t, store, library.series("India", "", "01.cbz"), "bangumi", 601, "https://bgm.tv/subject/601", `{}`, "applied")
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		if query == "Zulu" {
			return nil, fmt.Errorf("%s unreachable", source)
		}
		return nil, nil
	}}
	web := openPrefetchWeb(t, store, cfg, sources)
	agent := web.identificationAgent(cfg, 1, []int64{library.lib.ID}, []string{"read"})
	type counts struct {
		Prefetch struct {
			Ready   int64 `json:"ready"`
			Pending int64 `json:"pending"`
		} `json:"prefetch"`
	}
	read := func() counts {
		t.Helper()
		result, err := agent.CallTool(context.Background(), &mcp.CallToolParams{Name: "library_identification_progress", Arguments: map[string]any{}})
		if err != nil || result.IsError {
			t.Fatalf("progress %+v %v", result, err)
		}
		var out counts
		raw, _ := json.Marshal(result.StructuredContent)
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if before := read(); before.Prefetch.Ready != 0 || before.Prefetch.Pending != 3 {
		t.Fatalf("before prefetch %+v", before)
	}
	web.prefetch("{}")
	if after := read(); after.Prefetch.Ready != 2 || after.Prefetch.Pending != 1 {
		t.Fatalf("after prefetch %+v", after)
	}
}
