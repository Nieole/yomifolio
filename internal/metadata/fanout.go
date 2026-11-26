// 多来源搜索扇出：来源范围、单来源状态与候选快照的校验规则只在这里定义，消费方只决定
// 自己的结果预算、候选编号与分页。

package metadata

import (
	"context"
	"encoding/json"
	"slices"
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
// every source access to space requests per source. Only, when not empty, narrows the fan-out to
// those of the enabled sources it names.
type SourceFanout struct {
	Config config.Config
	Open   func(config.Config, string) Provider
	Pace   func(ctx context.Context, source string) error
	Only   []string
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
		if len(f.Only) > 0 && !slices.Contains(f.Only, name) {
			continue
		}
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
		snapshot, ok := Snapshot(name, result)
		if !ok {
			return SourcePage{Source: name, Status: SourceInvalidResponse, Total: total}
		}
		page.Results = append(page.Results, snapshot)
	}
	return page
}

// Snapshot detaches one source result from the source's objects and stamps it with the source
// name, or reports it invalid: a result carrying a decision record, a negative ID, volume count,
// rating or tag vote count, a rating above ten, more than the bounded tags, tag votes, authors or
// aliases, or more than the bounded bytes.
func Snapshot(source string, result *SeriesMetadata) (SeriesMetadata, bool) {
	if result == nil || result.MatchEvidence != nil || result.SourceID < 0 || result.VolumeCount < 0 || result.Rating < 0 || result.Rating > 10 {
		return SeriesMetadata{}, false
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > maxSnapshotBytes || len(result.Tags) > maxSnapshotItems || len(result.TagVotes) > maxSnapshotItems || len(result.Authors) > maxSnapshotItems || len(result.Aliases) > maxSnapshotItems {
		return SeriesMetadata{}, false
	}
	if slices.ContainsFunc(result.TagVotes, func(v TagVote) bool { return v.Votes < 0 }) {
		return SeriesMetadata{}, false
	}
	var snapshot SeriesMetadata
	_ = json.Unmarshal(raw, &snapshot)
	snapshot.Provider = source
	return snapshot, true
}
