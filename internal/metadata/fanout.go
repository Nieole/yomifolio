// 多来源搜索扇出：来源范围、单来源状态与候选快照的校验规则只在这里定义，消费方只决定
// 自己的结果预算、候选编号与分页。

package metadata

import (
	"context"
	"encoding/json"
	"strings"

	"yomifolio/internal/config"
)

// CatalogueSources lists the catalogue sources the configuration enables, in fan-out order:
// Bangumi, AniList and MangaDex always, MyAnimeList and Comic Vine only with their credentials.
// Model aliases are never catalogue sources.
func CatalogueSources(cfg config.Config) []string {
	names := []string{"bangumi", "anilist", "mangadex"}
	if strings.TrimSpace(cfg.Scrapers.MALClientID) != "" {
		names = append(names, "myanimelist")
	}
	if strings.TrimSpace(cfg.Scrapers.ComicVineAPIKey) != "" {
		names = append(names, "comicvine")
	}
	return names
}

// SourceStatus is how one catalogue source answered one fan-out query.
type SourceStatus string

const (
	SourceAvailable SourceStatus = "available"
	// SourceFailed covers an unreachable source and a page that breaks the request:
	// more results than asked for, or a negative total.
	SourceFailed SourceStatus = "failed"
	// SourceInvalidResponse means at least one result failed snapshot validation.
	SourceInvalidResponse SourceStatus = "invalid_response"
	// SourceTooLarge means the page was valid but the caller's result budget had no room left.
	SourceTooLarge SourceStatus = "too_large"
)

// A result beyond any of these bounds invalidates its whole page; fields are never truncated.
const (
	maxSnapshotBytes = 32 << 10
	maxSnapshotItems = 100
)

// SourcePage is one catalogue source's answer. Results are detached snapshots stamped with
// the source name, and are empty unless Status is SourceAvailable.
type SourcePage struct {
	Source  string
	Status  SourceStatus
	Total   int
	Results []SeriesMetadata
}

// TooLarge drops the page for a caller whose result budget cannot hold it.
func (p SourcePage) TooLarge() SourcePage {
	return SourcePage{Source: p.Source, Status: SourceTooLarge}
}

// SourceQuery asks every catalogue source for the same page.
type SourceQuery struct {
	Text   string
	Limit  int
	Offset int
}

// SourceFanout searches the catalogue sources one configuration snapshot enables.
// Open resolves a source adapter; nil uses NewConfiguredProvider. Pace, when set, runs before
// every source access to space requests per source.
type SourceFanout struct {
	Config config.Config
	Open   func(config.Config, string) Provider
	Pace   func(ctx context.Context, source string) error
}

// Search asks each source in CatalogueSources order for one page of q and hands every page
// to visit as soon as it is validated, so one failing source never hides the others.
// checkpoint runs before and after each source access (nil observes only ctx), after Pace;
// an error from either stops the fan-out, discards the page in flight and is the only error
// Search returns.
func (f SourceFanout) Search(ctx context.Context, q SourceQuery, checkpoint func(context.Context) error, visit func(SourcePage)) error {
	if checkpoint == nil {
		checkpoint = func(ctx context.Context) error { return ctx.Err() }
	}
	open := f.Open
	if open == nil {
		open = NewConfiguredProvider
	}
	for _, name := range CatalogueSources(f.Config) {
		if f.Pace != nil {
			if err := f.Pace(ctx, name); err != nil {
				return err
			}
		}
		if err := checkpoint(ctx); err != nil {
			return err
		}
		var results []*SeriesMetadata
		total := 0
		failed := true
		if p := open(f.Config, name); p != nil {
			var err error
			results, total, err = p.SearchMetadata(ctx, q.Text, q.Limit, q.Offset)
			failed = err != nil
		}
		if err := checkpoint(ctx); err != nil {
			return err
		}
		if failed || len(results) > q.Limit || total < 0 {
			visit(SourcePage{Source: name, Status: SourceFailed})
			continue
		}
		visit(snapshotPage(name, total, results))
	}
	return nil
}

// snapshotPage detaches every result from the source's objects, or reports the page invalid.
func snapshotPage(name string, total int, results []*SeriesMetadata) SourcePage {
	page := SourcePage{Source: name, Status: SourceAvailable, Total: total}
	for _, result := range results {
		if result == nil || result.MatchEvidence != nil || result.SourceID < 0 || result.VolumeCount < 0 || result.Rating < 0 || result.Rating > 10 {
			return SourcePage{Source: name, Status: SourceInvalidResponse, Total: total}
		}
		raw, err := json.Marshal(result)
		if err != nil || len(raw) > maxSnapshotBytes || len(result.Tags) > maxSnapshotItems || len(result.Authors) > maxSnapshotItems {
			return SourcePage{Source: name, Status: SourceInvalidResponse, Total: total}
		}
		var snapshot SeriesMetadata
		_ = json.Unmarshal(raw, &snapshot)
		snapshot.Provider = name
		page.Results = append(page.Results, snapshot)
	}
	return page
}
