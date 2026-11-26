// Package bootstrap is the production composition root for application resources and HTTP adapters.
// It registers capabilities and restores consistency before exposing a ready process.
package bootstrap

import (
	"context"
	"log/slog"
	"time"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
	"yomifolio/internal/application/identification"

	"yomifolio/internal/api"
	"yomifolio/internal/application/runtime"
	"yomifolio/internal/application/seriesediting"
	"yomifolio/internal/bookcontent"
	"yomifolio/internal/config"
	"yomifolio/internal/dashboard"
	"yomifolio/internal/database"
	"yomifolio/internal/organize"
	"yomifolio/internal/recommendation"
	"yomifolio/internal/runtimecfg"
	"yomifolio/internal/scanner"
	"yomifolio/internal/task"
)

// Options controls process inputs and scheduling at the production assembly seam.
type Options struct {
	Sources     api.ExternalSources
	ConfigPath  string
	Scanner     *scanner.Scanner
	Tasks       task.Config
	WatchConfig func(string, *config.Manager) (runtime.ConfigWatch, error)
	// Clock is the current time of the identification module alone: claim leases and evidence
	// packet ages are measured against it. Nil means the real clock.
	Clock func() time.Time
}

// Process owns the assembled application and its HTTP host. Application is the
// composition result for embedded callers, never an HTTP handler dependency.
type Process struct {
	Application *runtime.Runtime
	HTTP        *api.Host
}

// Open assembles one graph, restores consistency and interrupted runs, then opens inputs.
// On failure every constructed process resource is closed and no HTTP host is returned.
// The caller retains ownership of the supplied database connection and process log file.
func Open(ctx context.Context, store database.Store, cfg *config.Manager, options Options) (*Process, error) {
	resources := runtimecfg.NewResources()
	transport := api.NewTransportWithSources(cfg, options.Sources)
	memory := bookcontent.NewMemoryCache(bookcontent.DefaultImageCacheBytes)
	content := bookcontent.New(func(ctx context.Context, id int64) (bookcontent.Source, error) {
		book, err := store.GetBook(ctx, id)
		return bookcontent.Source{ID: book.ID, LibraryID: book.LibraryID, Path: book.Path, CoverPath: book.CoverPath.String, FileModifiedAt: book.FileModifiedAt, Size: book.Size, PageCount: book.PageCount}, err
	}, cfg.Snapshot, memory, 512, 128)
	effects := transport.RuntimeEffects()
	app, err := runtime.New(store, cfg, runtime.Options{ConfigPath: options.ConfigPath, Scanner: options.Scanner, Resources: resources, Tasks: transport.TaskOptions(options.Tasks), Effects: effects, WatchConfig: options.WatchConfig})
	if err != nil {
		resources.Close()
		return nil, err
	}
	stats := dashboard.New()
	reader := api.NewReaderCapabilities(store, app.Relations, app.Proposals, app.Engine, func() { stats.InvalidateVolatile("reading") })
	delegatedService := delegated.NewWithDiscoveryAndRuns(delegated.NewSQLStore(store.DB()), content, app.Engine, delegated.RunAccess{Engine: app.Engine, Scans: app.Scans, Libraries: store}).WithMatching(&delegated.MatchingAccess{Store: store, Config: cfg.Snapshot, Source: options.Sources.MetadataProvider, Start: app.Runs.Start}).WithModelBudgets(&delegated.ModelBudgetAccess{DB: store.DB(), Config: cfg.Snapshot, AI: options.Sources.ConfiguredAI, Start: app.Runs.Start})
	enrichmentService := enrichment.New(store.DB(), delegatedService, func() { effects.InvalidateStats("metadata_enrichment"); effects.Publish("refresh") }).WithCovers(enrichment.CoverAccess{Images: delegatedService, Config: cfg.Snapshot}).WithCleanup(enrichment.CleanupAccess{Delegated: delegatedService, Start: app.Runs.Start}).WithRelations(app.Relations.Changed)
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	identificationService := identification.New(store.DB(), delegatedService).WithPrefetch(app.Prefetch.WithClock(clock)).WithClock(clock).WithEnrichment(enrichmentService, cfg.Snapshot)
	identificationService.WithImages(delegatedService, options.Sources.CoverTransport())
	if err := identificationService.DeriveFromHistory(ctx); err != nil {
		app.Close()
		return nil, err
	}
	if err := app.Prefetch.SyncSources(ctx); err != nil {
		app.Close()
		return nil, err
	}
	app.Scanner.SetAddedBooksHook(recheckAddedBooks(identificationService))
	app.Scanner.SetCreatedSeriesHook(requestPrefetch(app.Prefetch))
	host := transport.Bind(api.HostDependencies{
		Catalogue: store, CoverQueries: store,
		Reading: api.ReadingDependencies{ContentQueries: store, StatisticsQueries: store, Capabilities: reader, Content: content, ImageCache: memory, Statistics: stats, Recommendations: recommendation.New(24 * time.Hour)},
		Organization: api.OrganizationDependencies{
			Relations: app.Relations, Proposals: app.Proposals, ProposalQueue: app.Proposals,
			Collections: organize.NewCollections(store), Shelves: organize.NewSmartCollections(store), ReadingLists: organize.NewReadingLists(store),
			Grouping: organize.NewAIGrouping(store, func() { effects.Publish("refresh") }), GroupingQueries: store, Series: store, StaticCollections: store,
			Metadata: seriesediting.New(seriesediting.NewSQLStore(store)), Selection: seriesediting.NewSelection(seriesediting.NewSQLStore(store), func() { effects.InvalidateStats("bulk_edit") }),
			UnmatchedCount: store.CountUnmatchedKOReaderProgress, KOReaderEnabled: func() bool { return cfg.Snapshot().KOReader.Enabled }, Publish: effects.Publish,
		},
		Libraries:      api.LibraryDependencies{Removal: store, Initials: store, Queries: store, ComicInfo: store, Management: app.LibraryManagement, Scans: app.Scans, Maintenance: app.Maintenance, Moves: app.Moves, Disk: app.Disk, Intelligence: app.Intelligence, External: app.External, Relocation: app.Relocation, Scanner: app.Scanner, Runs: api.RunDependencies{Engine: app.Engine, Runs: app.Runs, Store: app.Tasks, Registry: app.Registry}},
		Administration: api.AdministrationDependencies{Delegated: delegatedService, Enrichment: enrichmentService, Identification: identificationService, Configuration: app.Configuration, Store: store, Manager: cfg, KOReader: app.KOReader},
		Lifecycle:      app,
	})
	if err := app.Start(ctx); err != nil {
		app.Close()
		return nil, err
	}
	return &Process{Application: app, HTTP: host}, nil
}

// recheckAddedBooks hands the books a scan added to the identification check. The check only
// reads local state; its failure is logged and never reaches the scan.
func recheckAddedBooks(service *identification.Service) func(context.Context, []scanner.AddedBook) {
	return func(ctx context.Context, added []scanner.AddedBook) {
		books := make([]identification.NewBook, 0, len(added))
		for _, book := range added {
			clue := identification.NewBook{SeriesID: book.SeriesID, Name: book.Name}
			if book.ComicInfo != nil {
				clue.ComicInfoSeries = book.ComicInfo.Series
			}
			books = append(books, clue)
		}
		if err := service.RecheckNewBooks(ctx, books); err != nil {
			slog.WarnContext(ctx, "Identification recheck of added books failed", "books", len(books), "error", err)
		}
	}
}

// requestPrefetch asks the incremental prefetch for packets of the series a scan created. Its
// failure is logged and never reaches the scan; the series then wait for a backfill.
func requestPrefetch(prefetch *identification.Prefetcher) func(context.Context, []int64) {
	return func(ctx context.Context, created []int64) {
		if err := prefetch.Request(ctx, created); err != nil {
			slog.WarnContext(ctx, "Evidence prefetch request for created series failed", "series", len(created), "error", err)
		}
	}
}

func (p *Process) Close() { p.Application.Close() }
func (p *Process) Shutdown(ctx context.Context, drain func(context.Context) error) error {
	return p.Application.Shutdown(ctx, drain)
}
