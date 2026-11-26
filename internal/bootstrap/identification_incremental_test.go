// These tests guard the incremental prefetch of the assembled process: a scan chains a prefetch of
// only the series it created, a claim refreshes a packet that has grown old instead of handing it
// out, and a newly enabled source reopens not-found series for that source alone. Broken, new
// series would wait for a manual backfill, agents would judge on stale evidence, and enabling a
// source would either change nothing or redo the whole backlog.

package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"yomifolio/internal/application/delegated"
	appruntime "yomifolio/internal/application/runtime"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/runhandle"
	"yomifolio/internal/task"
	"yomifolio/internal/taskstore"
)

// incrementalVariant is the run variant of the incremental prefetch.
const incrementalVariant task.Variant = "incremental"

// incrementalRuns lists the incremental prefetch runs of one library, newest first.
func (w *identificationWeb) incrementalRuns(library int64) []task.Run {
	w.t.Helper()
	variant := incrementalVariant
	runs, err := w.process.Application.Tasks.ListRuns(context.Background(), task.RunFilter{Types: []task.Type{"evidence_prefetch"}, Variant: &variant,
		Scope: task.ScopeLibrary, ScopeID: &library, Order: task.OrderIDDesc})
	if err != nil {
		w.t.Fatal(err)
	}
	return runs
}

// awaitIncremental waits for the latest incremental prefetch run of the library to finish.
func (w *identificationWeb) awaitIncremental(library int64) task.Run {
	w.t.Helper()
	runs := w.incrementalRuns(library)
	if len(runs) == 0 {
		w.t.Fatal("no incremental prefetch run was launched")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	run, err := w.process.Application.Engine.Await(ctx, runs[0].ID)
	if err != nil {
		w.t.Fatal(err)
	}
	return run
}

// seriesAt finds the series a scan created for a directory of the library.
func seriesAt(t *testing.T, store database.Store, library database.Library, directory string) int64 {
	t.Helper()
	all, err := store.ListSeriesByLibraryLite(context.Background(), library.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range all {
		if s.Path == filepath.Join(library.Path, directory) {
			return s.ID
		}
	}
	t.Fatalf("no series at %q", directory)
	return 0
}

// oneCandidate answers Bangumi with one entry titled after the query and every other source with
// nothing.
func oneCandidate(source, query string) ([]*metadata.SeriesMetadata, error) {
	if source != "bangumi" {
		return nil, nil
	}
	return []*metadata.SeriesMetadata{{Title: query, SourceID: 700 + len(query), SourceURL: ""}}, nil
}

func TestScanChainsPrefetchOfOnlyTheSeriesItCreated(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Weekly")
	backlog := library.series("Backlog Title", "", "01.cbz")
	sources := &recordingSources{answer: oneCandidate}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)})

	writeComicArchive(t, filepath.Join(library.lib.Path, "Fresh Arrival", "Fresh Arrival 01.cbz"), "")
	if run := web.scan(library.lib.ID, false); run.Status != task.StatusCompleted {
		t.Fatalf("scan %+v", run)
	}
	run := web.awaitIncremental(library.lib.ID)
	if run.Status != task.StatusCompleted || run.Trigger != task.TriggerChained {
		t.Fatalf("incremental prefetch %+v", run)
	}
	fresh := seriesAt(t, store, library.lib, "Fresh Arrival")
	if got := sources.queries("bangumi"); !slices.Equal(got, []string{"Fresh Arrival"}) {
		t.Fatalf("searched %q: only the series the scan created may be searched", got)
	}
	if packet := web.evidence(fresh); packet == nil || len(packet.Candidates) != 1 {
		t.Fatalf("the new series has no packet: %+v", packet)
	}
	if web.evidence(backlog) != nil {
		t.Fatal("the backlog series got a packet; it waits for the owner's backfill")
	}

	// A scan that creates nothing chains nothing.
	if run := web.scan(library.lib.ID, false); run.Status != task.StatusCompleted {
		t.Fatalf("second scan %+v", run)
	}
	if runs := web.incrementalRuns(library.lib.ID); len(runs) != 1 {
		t.Fatalf("incremental runs after a scan without new series: %d", len(runs))
	}
}

func TestAgentScanChainsPrefetchOfTheSeriesItCreated(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Delegated")
	backlog := library.series("Waiting Backlog", "", "01.cbz")
	sources := &recordingSources{answer: oneCandidate}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)})
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.ScanControl})
	writeComicArchive(t, filepath.Join(library.lib.Path, "Agent Found", "Agent Found 01.cbz"), "")

	var started delegated.Run
	if result := agent.call("library_start_scan", map[string]any{"library_id": library.lib.ID}, &started); result.IsError || started.ID == 0 {
		t.Fatalf("start scan %s", resultText(result))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if run, err := web.process.Application.Engine.Await(ctx, started.ID); err != nil || run.Status != task.StatusCompleted {
		t.Fatalf("agent scan %+v %v", run, err)
	}
	if run := web.awaitIncremental(library.lib.ID); run.Status != task.StatusCompleted || run.Trigger != task.TriggerChained {
		t.Fatalf("incremental prefetch %+v", run)
	}
	if got := sources.queries("bangumi"); !slices.Equal(got, []string{"Agent Found"}) {
		t.Fatalf("searched %q", got)
	}
	if web.evidence(seriesAt(t, store, library.lib, "Agent Found")) == nil || web.evidence(backlog) != nil {
		t.Fatal("only the series the agent's scan created may get a packet")
	}
}

// researched reads the series' packet with the custom searches recorded in it.
func (w *identificationWeb) researched(series int64) researchedPacket {
	w.t.Helper()
	var out struct {
		Evidence researchedPacket `json:"evidence"`
	}
	w.call("GET", "/api/identification/series/"+strconv.FormatInt(series, 10), http.StatusOK, &out)
	return out.Evidence
}

func TestClaimRefreshesPacketsOlderThanThirtyDaysBeforeHandingThemOut(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library, ids := claimLibrary(t, store, "Aging", "Title 1")
	old := ids[0]
	var mu sync.Mutex
	searchedOld := 0
	refreshing, release := make(chan struct{}), make(chan struct{})
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		if source == "bangumi" && query == "Title 1" {
			mu.Lock()
			searchedOld++
			second := searchedOld == 2
			mu.Unlock()
			if second {
				close(refreshing)
				<-release
			}
		}
		return candidatesBySeries(source, query)
	}}
	clock := &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	web := openClaimWeb(t, store, cfg, sources, clock)
	var releaseOnce sync.Once
	letGo := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(letGo)
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("first prefetch %+v", run)
	}
	if result := web.research(old, `{"query":"Custom One"}`, http.StatusOK); result.Status != "completed" {
		t.Fatalf("research %+v", result)
	}
	clock.advance(20 * 24 * time.Hour)
	younger := library.series("Title 2", "", "01.cbz")
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("second prefetch %+v", run)
	}
	clock.advance(11 * 24 * time.Hour)
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	var counts struct {
		Prefetch struct {
			Ready   int64 `json:"ready"`
			Pending int64 `json:"pending"`
		} `json:"prefetch"`
	}
	if agent.call("library_identification_progress", map[string]any{}, &counts); counts.Prefetch.Ready != 1 || counts.Prefetch.Pending != 1 {
		t.Fatalf("progress counts the old packet as ready: %+v", counts)
	}

	// The 31-day-old packet is held back and refreshed; the 11-day-old one is handed out.
	if batch := agent.claim(map[string]any{}); !slices.Equal(batch.ids(), []int64{younger}) {
		t.Fatalf("claimed %v want only the current packet's series %d", batch.ids(), younger)
	}
	<-refreshing
	if batch := agent.claim(map[string]any{}); len(batch.Series) != 0 || batch.EmptyReason != "prefetch_pending" {
		t.Fatalf("claim during the refresh %+v", batch)
	}
	letGo()
	if run := web.awaitIncremental(library.lib.ID); run.Status != task.StatusCompleted || run.Trigger != task.TriggerChained {
		t.Fatalf("refresh run %+v", run)
	}
	batch := agent.claim(map[string]any{})
	if !slices.Equal(batch.ids(), []int64{old}) || batch.Series[0].Evidence.FetchedAt != "2026-04-01T09:00:00Z" {
		t.Fatalf("after the refresh claimed %+v", batch)
	}
	// The refresh searched the derived term and the owner's custom term again.
	if got := sources.queries("bangumi"); !slices.Equal(got, []string{"Title 1", "Custom One", "Title 2", "Title 1", "Custom One"}) {
		t.Fatalf("bangumi searches %q", got)
	}
	if packet := web.researched(old); len(packet.Searches) != 1 || packet.Searches[0].Query != "Custom One" || packet.Searches[0].SearchedAt != "2026-04-01T09:00:00Z" {
		t.Fatalf("the research did not survive the refresh: %+v", packet)
	}
}

// openConfigurableWeb opens the production process like openClaimWeb, with a configuration file the
// owner can save settings to.
func openConfigurableWeb(t *testing.T, store database.Store, cfg *config.Manager, sources *recordingSources, clock *handClock) *identificationWeb {
	t.Helper()
	options := sources.options()
	options.Clock = clock.Now
	options.ConfigPath = filepath.Join(t.TempDir(), "config.yaml")
	options.WatchConfig = func(string, *config.Manager) (appruntime.ConfigWatch, error) { return &watch{}, nil }
	process, err := bootstrap.Open(context.Background(), store, cfg, options)
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

// saveSettings changes the configuration through the owner's settings page.
func (w *identificationWeb) saveSettings(change func(*config.Config)) {
	w.t.Helper()
	read := w.raw("GET", "/api/system/config", "")
	if read.Code != http.StatusOK {
		w.t.Fatalf("read settings %d %s", read.Code, read.Body)
	}
	var view struct {
		Config config.Config `json:"config"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &view); err != nil {
		w.t.Fatal(err)
	}
	change(&view.Config)
	payload, _ := json.Marshal(view.Config)
	r := httptest.NewRequest("POST", "/api/system/config", strings.NewReader(string(payload)))
	r.AddCookie(w.cookie)
	r.Header.Set("X-CSRF-Token", w.csrf)
	r.Header.Set("If-Match", read.Header().Get("ETag"))
	saved := httptest.NewRecorder()
	w.router.ServeHTTP(saved, r)
	if saved.Code != http.StatusOK {
		w.t.Fatalf("save settings %d %s", saved.Code, saved.Body)
	}
}

func TestEnablingASourceReopensNotFoundSeriesForThatSourceOnly(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library, ids := claimLibrary(t, store, "Reopen", "Title 1", "Title 2", "Title 3")
	missing, identified, found := ids[0], ids[1], ids[2]
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		if source == "myanimelist" {
			return []*metadata.SeriesMetadata{{Title: query, SourceID: 9001, SourceURL: "https://myanimelist.net/manga/9001"}}, nil
		}
		return candidatesBySeries(source, query)
	}}
	clock := &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	web := openConfigurableWeb(t, store, cfg, sources, clock)
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.MetadataWrite})
	batch := agent.claim(map[string]any{})
	results := agent.submit("first", batch.LeaseID, notFoundVerdict(missing, "no candidate is this work"),
		identifyVerdict(identified, "the Bangumi title matches", "bangumi:2"), notFoundVerdict(found, "no candidate is this work"))
	wantResult(t, results[0], missing, true, "not_found")
	wantResult(t, results[1], identified, true, "identified")
	wantResult(t, results[2], found, true, "not_found")

	clock.advance(time.Hour)
	web.saveSettings(func(c *config.Config) { c.Scrapers.MALClientID = "client-id" })
	if got := web.identification(missing).Status; got != "unprocessed" {
		t.Fatalf("not-found series after enabling MyAnimeList: %s", got)
	}
	if got := web.identification(identified).Status; got != "identified" {
		t.Fatalf("identified series after enabling MyAnimeList: %s", got)
	}

	// The packet predates the new source, so the claim holds the series back and refreshes it
	// against MyAnimeList alone.
	if batch := agent.claim(map[string]any{}); len(batch.Series) != 0 || batch.EmptyReason != "prefetch_pending" {
		t.Fatalf("claim before the refresh %+v", batch)
	}
	if run := web.awaitIncremental(library.lib.ID); run.Status != task.StatusCompleted {
		t.Fatalf("refresh %+v", run)
	}
	if got := sources.queries("myanimelist"); !slices.Equal(got, []string{"Title 1", "Title 3"}) {
		t.Fatalf("MyAnimeList searches %q", got)
	}
	for _, source := range []string{"bangumi", "anilist", "mangadex"} {
		if got := sources.queries(source); !slices.Equal(got, []string{"Title 1", "Title 2", "Title 3"}) {
			t.Fatalf("%s was searched again: %q", source, got)
		}
	}
	batch = agent.claim(map[string]any{})
	if !slices.Equal(batch.ids(), []int64{missing, found}) {
		t.Fatalf("claimed %v", batch.ids())
	}
	packet := batch.Series[0].Evidence
	if len(packet.Sources) != 1 || packet.Sources[0].Source != "myanimelist" || len(packet.Candidates) != 1 || packet.Candidates[0].ID != "myanimelist:9001" {
		t.Fatalf("reopened packet %+v", packet)
	}

	// Concluding not-found again covers every source searched so far; identifying through the new
	// source leaves no not-found verdict behind.
	results = agent.submit("second", batch.LeaseID, notFoundVerdict(missing, "the MyAnimeList entry is another work"),
		identifyVerdict(found, "the MyAnimeList title matches", "myanimelist:9001"))
	wantResult(t, results[0], missing, true, "not_found")
	wantResult(t, results[1], found, true, "identified")
	if got := web.ownerVerdicts(found).NotFound; got != nil {
		t.Fatalf("identified series keeps a not-found verdict %+v", got)
	}
	if got := web.ownerVerdicts(missing).NotFound; got == nil || !slices.Equal(got.SearchedSources, []string{"bangumi", "anilist", "myanimelist"}) {
		t.Fatalf("not-found verdict %+v", got)
	}
	clock.advance(time.Hour)
	web.saveSettings(func(c *config.Config) { c.LLM.Model = fmt.Sprintf("%s-again", c.LLM.Model) })
	if got := web.identification(missing).Status; got != "not_found" {
		t.Fatalf("saving unrelated settings reopened the series: %s", got)
	}
}

func TestIncrementalPrefetchResumesAfterARestart(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Resumed")
	var mu sync.Mutex
	online := false
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		mu.Lock()
		defer mu.Unlock()
		if !online {
			return nil, fmt.Errorf("%s unreachable", source)
		}
		return oneCandidate(source, query)
	}}
	clock := &handClock{now: time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)}
	first := openClaimWeb(t, store, cfg, sources, clock)
	writeComicArchive(t, filepath.Join(library.lib.Path, "Arrived Offline", "01.cbz"), "")
	first.scan(library.lib.ID, false)
	if run := first.awaitIncremental(library.lib.ID); run.Status != task.StatusCompleted {
		t.Fatalf("offline prefetch %+v", run)
	}
	series := seriesAt(t, store, library.lib, "Arrived Offline")
	if first.evidence(series) != nil {
		t.Fatal("a packet nobody answered for was stored")
	}
	first.process.Close()

	// The process is lost while a new incremental run of the library is running.
	engine := task.New(task.Config{Store: taskstore.New(store.DB()), RunBackground: func(func()) {}})
	if _, err := engine.Start(context.Background(), task.RunSpec{
		Identity: task.Identity{Type: "evidence_prefetch", Scope: task.ScopeLibrary, ScopeID: library.lib.ID, Variant: incrementalVariant},
		Key:      "evidence_prefetch_incremental", Trigger: task.TriggerChained, LibraryIDs: []int64{library.lib.ID}, Total: 1,
	}, func(context.Context, *runhandle.Handle) (task.Result, error) {
		t.Error("the lost process's body must not execute")
		return task.Result{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	online = true
	mu.Unlock()
	options := sources.options()
	options.Clock = clock.Now
	process, err := bootstrap.Open(context.Background(), store, cfg, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(process.Close)
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	second := &identificationWeb{t: t, store: store, process: process, router: router}
	second.loginAs("admin", "password123")
	run := second.awaitIncremental(library.lib.ID)
	if run.Status != task.StatusCompleted || run.Trigger != task.TriggerResumed {
		t.Fatalf("resumed run %+v", run)
	}
	if second.evidence(series) == nil {
		t.Fatal("the resumed run did not prepare the requested packet")
	}
}
