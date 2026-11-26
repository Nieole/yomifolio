// Package intelligence owns AI grouping generation. Collection suggestions retain their own rules
// and storage.
package intelligence

import (
	"context"
	"yomifolio/internal/application"
	"yomifolio/internal/database"
	"yomifolio/internal/organize"
)

// Store names the library a grouping run covers.
type Store interface {
	GetLibrary(context.Context, int64) (database.Library, error)
}

// GroupingProvider is the external generation boundary; it never applies collections.
type GroupingProvider = organize.GroupingGenerator

// Sources creates external clients from current configuration and supports controlled sources in tests.
type Sources struct {
	Grouping func() GroupingProvider
}

// Service launches complete generation use cases using the existing task engine.
type Service struct {
	runs         *application.Runs
	store        Store
	groups       *organize.AIGrouping
	sources      Sources
	publishEvent func(string)
}

// New assembles capabilities before any requests or restart work can enter.
func New(runs *application.Runs, store Store, groups *organize.AIGrouping, sources Sources, publish func(string)) *Service {
	return &Service{runs, store, groups, sources, publish}
}
func (c *Service) publish() {
	if c.publishEvent != nil {
		c.publishEvent("refresh")
	}
}
