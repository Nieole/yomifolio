package intelligence_test

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"yomifolio/internal/application"
	"yomifolio/internal/application/intelligence"
	"yomifolio/internal/database"
	"yomifolio/internal/logger"
	"yomifolio/internal/metadata"
	"yomifolio/internal/organize"
	"yomifolio/internal/runhandle"
	"yomifolio/internal/task"
	"yomifolio/internal/taskstore"
)

type source struct {
	locales []string
	runIDs  []int64
	keys    []string
}

func (p *source) Name() string { return "Controlled" }

func (p *source) GenerateGrouping(ctx context.Context, candidates []metadata.CandidateSeries) ([]metadata.AIGroupCollection, error) {
	p.locales = append(p.locales, metadata.LocaleFromContext(ctx))
	p.runIDs = append(p.runIDs, logger.RunIDFrom(ctx))
	p.keys = append(p.keys, logger.TaskKeyFrom(ctx))
	return []metadata.AIGroupCollection{{Name: "Suggested shelf", Description: "Review this grouping", SeriesIDs: []int64{candidates[0].ID, candidates[0].ID, 99999}}}, nil
}
func TestGroupingQueuesIndependentReviewAndReplaysLocale(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir(), ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Original", Path: filepath.Join(lib.Path, "Series")})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskstore.New(store.(*database.SqlStore).DB())
	engine := task.New(task.Config{Store: tasks, RunBackground: func(fn func()) { fn() }, DecorateRunContext: application.DecorateRunContext})
	provider := &source{}
	groups := organize.NewAIGrouping(store, nil)
	service := intelligence.New(application.NewRuns(engine, nil), store, groups, intelligence.Sources{Grouping: func() intelligence.GroupingProvider { return provider }}, nil)
	launch, err := service.GroupLibrary(ctx, lib.ID, "en-US", task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	run, err := tasks.LoadRun(ctx, launch.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	reviewID, err := strconv.ParseInt(run.MessageParams["reviewId"], 10, 64)
	if err != nil {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	review, err := groups.Review(ctx, reviewID)
	if err != nil {
		t.Fatal(err)
	}
	collections, err := store.ListCollectionsWithSeriesCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	proposals, err := store.ListPendingMetadataReviewsBySeries(ctx, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != task.StatusCompleted || review.Review.Status != "pending" || len(review.Collections) != 1 || review.Collections[0].SeriesIds != "["+strconv.FormatInt(series.ID, 10)+"]" || len(collections) != 0 || len(proposals) != 0 {
		t.Fatalf("run=%+v review=%+v collections=%+v proposals=%+v", run, review, collections, proposals)
	}
	sides, err := tasks.LoadRunSideData(ctx, []int64{run.ID})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.Restart(metadata.WithLocale(ctx, "zh-CN"), task.Identity{Type: "ai_grouping", Scope: task.ScopeLibrary, ScopeID: lib.ID}, sides[run.ID].Args, task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Run.ID == run.ID || len(provider.locales) != 2 || provider.locales[1] != "en-US" {
		t.Fatalf("retry=%+v locales=%v", replay, provider.locales)
	}
	automatic, err := service.Restart(ctx, task.Identity{Type: "ai_grouping", Scope: task.ScopeLibrary, ScopeID: lib.ID}, sides[run.ID].Args, task.TriggerResumed)
	if !errors.Is(err, intelligence.ErrAutomaticResumeUnsupported) || automatic.Run.ID != 0 || len(provider.locales) != 2 {
		t.Fatalf("automatic=%+v err=%v locales=%v", automatic, err, provider.locales)
	}

}

func TestQueuedGenerationCancellationReleasesCoverageWithoutCallingSource(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir(), ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Original", Path: filepath.Join(lib.Path, "Series")})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskstore.New(store.(*database.SqlStore).DB())
	var bodies []func()
	engine := task.New(task.Config{Store: tasks, Slots: func() int { return 1 }, RunBackground: func(fn func()) { bodies = append(bodies, fn) }})
	provider := &source{}
	service := intelligence.New(application.NewRuns(engine, nil), store, organize.NewAIGrouping(store, nil), intelligence.Sources{Grouping: func() intelligence.GroupingProvider { return provider }}, nil)
	_, err = engine.Start(ctx, task.RunSpec{Identity: task.Identity{Type: "block", Scope: task.ScopeSystem}, Trigger: task.TriggerManual, Key: "block"}, func(context.Context, *runhandle.Handle) (task.Result, error) { return task.Result{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	grouping, err := service.GroupLibrary(ctx, lib.ID, "en-US", task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	if grouping.Run.Status != task.StatusQueued {
		t.Fatalf("grouping=%+v", grouping)
	}
	if release, err := engine.AcquireLibraryExclusive(ctx, lib.ID); !errors.Is(err, task.ErrLibraryUnavailable) {
		if release != nil {
			release()
		}
		t.Fatalf("queued generation not covered: %v", err)
	}
	if err = engine.Cancel(grouping.Run.ID); err != nil {
		t.Fatal(err)
	}
	bodies[0]()
	finished, err := tasks.LoadRun(ctx, grouping.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != task.StatusCancelled {
		t.Fatalf("finished=%+v", finished)
	}
	if len(provider.locales) != 0 || len(bodies) != 1 {
		t.Fatalf("cancelled work called source: locales=%v bodies=%d", provider.locales, len(bodies))
	}
	release, err := engine.AcquireLibraryExclusive(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
