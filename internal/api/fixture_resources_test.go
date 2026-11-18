package api

import (
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
	"yomifolio/internal/diskwork"
	"yomifolio/internal/external"
	"yomifolio/internal/koreader"
	"yomifolio/internal/organize"
	"yomifolio/internal/proposal"
	"yomifolio/internal/recommendation"
	"yomifolio/internal/relocation"
	"yomifolio/internal/scanner"
)

// apiFixture retains explicit SQLite/application resources for assertions and independently bound adapters.
// Resource replacement is not handler injection: tests must rebind the consumed port.
type apiFixture struct {
	*Host
	admin          *administrationHTTP
	media          *contentHTTP
	store          database.Store
	imageCache     *bookcontent.MemoryCache
	content        *bookcontent.Service
	stats          *dashboard.Service
	scanner        *scanner.Scanner
	config         *config.Manager
	diskWork       *diskwork.Runner
	koreader       *koreader.Service
	external       *external.Manager
	diskJobs       *diskjobs.Service
	intelligence   *intelligence.Service
	proposals      *proposal.Service
	relocation     *relocation.Service
	configPath     string
	scans          *scanning.Service
	maintenance    *maintenance.Service
	watcher        *scanner.FileWatcher
	watcherInitErr error
	moves          *relocating.Service

	sse *sseBroker

	recommendations *recommendation.Service

	taskEngine *taskEngine

	auth *authState

	runtime       *appruntime.Runtime
	process       Lifecycle
	configuration *configuration.Service

	relations         *organize.Relations
	libraryManagement *libraries.Service
}
