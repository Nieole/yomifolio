package api

import (
	"context"
	"net/http"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
	"yomifolio/internal/application/identification"

	"sync"
	"time"
	"yomifolio/internal/application"
	"yomifolio/internal/application/configuration"
	"yomifolio/internal/application/diskjobs"
	"yomifolio/internal/application/intelligence"
	"yomifolio/internal/application/libraries"
	"yomifolio/internal/application/maintenance"
	"yomifolio/internal/application/relocating"
	appruntime "yomifolio/internal/application/runtime"
	"yomifolio/internal/application/scanning"
	"yomifolio/internal/bookcontent"
	"yomifolio/internal/config"
	"yomifolio/internal/dashboard"
	"yomifolio/internal/database"
	"yomifolio/internal/external"
	"yomifolio/internal/koreader"

	"yomifolio/internal/recommendation"
	"yomifolio/internal/relocation"
	"yomifolio/internal/scanner"
	"yomifolio/internal/task"

	"github.com/go-chi/chi/v5"
)

// Lifecycle supplies transport background ownership without exposing the application graph.
type Lifecycle interface {
	Done() <-chan struct{}
	Go(func())
	Close()
	Shutdown(context.Context, func(context.Context) error) error
}

// ReadingDependencies supplies content and reader query projections.
type ReadingDependencies struct {
	ContentQueries    ContentQueries
	StatisticsQueries dashboard.Store
	Capabilities      *ReaderCapabilities
	Content           *bookcontent.Service
	ImageCache        *bookcontent.MemoryCache
	Statistics        *dashboard.Service
	Recommendations   *recommendation.Service
}

// RunDependencies joins the transport projection to the same admitted queue.
type RunDependencies struct {
	Engine   *task.Engine
	Runs     *application.Runs
	Store    task.Store
	Registry *appruntime.Registry
}

// LibraryDependencies supplies library and disk-use cases to their transport family.
type LibraryDependencies struct {
	Removal      BookRemovalStore
	Initials     interface{ BackfillSeriesInitials(context.Context) error }
	Queries      LibraryQueries
	ComicInfo    ComicInfoQueries
	Management   *libraries.Service
	Scans        *scanning.Service
	Maintenance  *maintenance.Service
	Moves        *relocating.Service
	Disk         *diskjobs.Service
	Intelligence *intelligence.Service
	External     *external.Manager
	Relocation   *relocation.Service
	Scanner      *scanner.Scanner
	Runs         RunDependencies
}

// AdministrationDependencies supplies configuration publication, not a Runtime.
type AdministrationDependencies struct {
	Delegated      *delegated.Service
	Enrichment     *enrichment.Service
	Identification *identification.Service
	Configuration  *configuration.Service
	Store          AdministrationStore
	Manager        *config.Manager
	KOReader       *koreader.Service
}

// HostDependencies supplies explicit family capabilities and shared transport leaves.
type HostDependencies struct {
	Catalogue    CatalogueMaintenanceStore
	CoverQueries interface {
		GetBook(context.Context, int64) (database.Book, error)
	}
	Reading        ReadingDependencies
	Organization   OrganizationDependencies
	Libraries      LibraryDependencies
	Administration AdministrationDependencies
	Lifecycle      Lifecycle
}

// Transport owns independent technical HTTP leaves, not an application graph.
// Bind installs effect consumers before recovery or background inputs open.
type Transport struct {
	cfg        *config.Manager
	auth       *authState
	warning    sync.Once
	events     *sseBroker
	projection *taskEngine
	effects    transportEffects
	clients    *providerClients
	media      *contentHTTP
}

func NewTransport(cfg *config.Manager) *Transport {
	return NewTransportWithSources(cfg, ExternalSources{})
}

// NewTransportWithSources binds external client factories before assembly and recovery.
func NewTransportWithSources(cfg *config.Manager, sources ExternalSources) *Transport {
	t := &Transport{cfg: cfg, auth: newAuthState(), events: newSSEBroker()}
	snapshot := cfg.Snapshot
	t.clients = &providerClients{currentConfig: snapshot, sources: sources}
	t.projection = &taskEngine{configSnapshot: snapshot, identities: make(map[int64]TaskIdentity),
		slots:          func() int { return snapshot().Tasks.RunSlots },
		backoff:        func() task.BackoffPolicy { return backoffPolicyOf(snapshot()) },
		sampleInterval: func() time.Duration { return sampleIntervalOf(snapshot()) },
		resumeEnabled:  func() bool { b := snapshot().Tasks.ResumeAfterRestart; return b == nil || *b },
	}
	return t
}
func (t *Transport) RuntimeEffects() appruntime.Effects {
	e := &t.effects
	return appruntime.Effects{Publish: t.events.publish, PurgeReadingPaths: func() { t.media.content.PurgeSources() },
		InvalidateStats: func(reason string) { e.stats.InvalidateAll(reason) }, WarmStats: e.warm,
		InvalidateRecommendations: func() { e.recommendations.Purge() },
		InvalidateReading:         func() { e.stats.InvalidateVolatile("koreader_progress") },
		RunEvents:                 t.events.run, StopEvents: t.events.closeClients,
		TrimPageCache: func() { t.media.enforcePageCacheBudget() }, ObserveIdentity: t.projection.observeIdentity,
		Sources: intelligence.Sources{Metadata: t.clients.getMetadataFetcher, Grouping: func() intelligence.GroupingProvider { return t.clients.sources.configuredAI(t.cfg.Snapshot()) }}}
}

// TaskOptions attaches transport projections while retaining caller-controlled clocks and scheduling.
func (t *Transport) TaskOptions(options task.Config) task.Config {
	e, c := t.projection, t.events
	if options.Publish == nil {
		options.Publish = e.publisher(c.publishAdmin)
	}
	if options.PublishLive == nil {
		options.PublishLive = e.livePublisher(c.publishAdmin)
	}
	if options.RunBackground == nil {
		options.RunBackground = func(fn func()) { e.runBackground(fn) }
	}
	if options.Now == nil {
		options.Now = e.clock
	}
	if options.Slots == nil {
		options.Slots = e.slotLimit
	}
	if options.Backoff == nil {
		options.Backoff = e.backoffPolicy
	}
	if options.SampleInterval == nil {
		options.SampleInterval = e.samplingInterval
	}
	return options
}

// Host owns only bound HTTP adapters and their single route composition.
type Host struct {
	mcp              http.Handler
	reader           *ReaderHTTP
	organizationHTTP *organizationHTTP
	families         httpFamilies
	administration   *administrationHTTP
	content          *contentHTTP
	catalogue        *catalogMaintenanceHTTP
	covers           *coverHTTP
	providers        *providerHTTP
	events           *sseBroker
}

// Bind installs capability ports before the composition root recovers or opens inputs.
func (t *Transport) Bind(d HostDependencies) *Host {
	snapshot := t.cfg.Snapshot
	e := t.projection
	e.engine, e.runs, e.runStore, e.registry = d.Libraries.Runs.Engine, d.Libraries.Runs.Runs, d.Libraries.Runs.Store, d.Libraries.Runs.Registry
	e.runBackground = d.Lifecycle.Go
	series := d.Organization.Series
	e.libraryIDs = func(ctx context.Context, id TaskIdentity) ([]int64, error) {
		return taskLibraries(ctx, series, id)
	}
	t.effects = transportEffects{stats: d.Reading.Statistics, recommendations: d.Reading.Recommendations, store: d.Reading.StatisticsQueries, background: d.Lifecycle.Go}
	t.media = &contentHTTP{store: d.Reading.ContentQueries, content: d.Reading.Content, imageCache: d.Reading.ImageCache, currentConfig: snapshot, engine: e.engine}
	providers := &providerHTTP{providerClients: t.clients, store: d.Organization.Series, proposals: d.Organization.ProposalQueue}
	admin := newAdministration(d.Administration, t.auth, &t.warning)
	admin.sources = t.clients.sources
	organization := newOrganizationHTTP(d.Organization)
	media := t.media
	reader := &ReaderHTTP{sources: t.clients.sources, ReaderCapabilities: d.Reading.Capabilities, store: d.Reading.Capabilities.queries,
		stats: d.Reading.Statistics, recommendations: d.Reading.Recommendations, content: d.Reading.Content,
		currentConfig: snapshot, failedRuns: e.statusesFrom,
		requireProtocolEnabled: admin.requireProtocolEnabled, requireBasicAuth: admin.requireBasicAuth,
		serveBookFile: media.serveBookFile, serveCoverImage: media.serveCoverImage, serveThumbnailImage: media.serveThumbnailImage,
		servePageImage: media.servePageImage, servePageImageByNumber: media.servePageImageByNumber, guardBookRead: media.guardBookRead,
		readingLists: organization.readingLists, loadCollectionViews: organization.loadCollectionViews,
		loadStaticCollectionSeries: organization.loadStaticCollectionSeries, loadSmartCollectionSeries: organization.loadSmartCollectionSeries, getSmartFilterByID: organization.getSmartFilterByID}
	invalidate := d.Reading.Statistics.InvalidateAll
	return &Host{mcp: newMCPHandler(d.Administration.Delegated, d.Administration.Enrichment, d.Administration.Identification, t.cfg), reader: reader, organizationHTTP: organization, administration: admin, content: media, events: t.events,
		catalogue: &catalogMaintenanceHTTP{store: d.Catalogue, engine: e.engine, openPath: openPathInDefaultFileManager, invalidateDashboardStatsCache: invalidate},
		covers:    &coverHTTP{store: d.CoverQueries, scanner: d.Libraries.Scanner, invalidateDashboardStatsCache: invalidate}, providers: providers,
		families: bindHTTPFamilies(d.Libraries, d.Reading.Content, snapshot, e, t.clients.getMetadataFetcher, d.Administration.Configuration.DatabasePath, invalidate)}
}
func (h *Host) SetupOPDSRoutes(r chi.Router)     { h.reader.SetupOPDSRoutes(r) }
func (h *Host) SetupKOReaderRoutes(r chi.Router) { h.administration.SetupKOReaderRoutes(r) }
