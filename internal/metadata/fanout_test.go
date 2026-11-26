// 每个目录型来源的回答各自校验、各自报状态：一个来源的坏数据只让它自己不可用，留下的候选是
// 脱离来源对象的快照。破了，坏来源会污染整次搜索或让候选在保存后被改写。

package metadata

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"yomifolio/internal/config"
)

// answeringSource answers every search with what its source's answer function returns.
type answeringSource struct {
	answer func(query string, limit int) ([]*SeriesMetadata, int, error)
}

func (answeringSource) Name() string { return "answering" }
func (answeringSource) FetchSeriesMetadata(context.Context, string) (*SeriesMetadata, error) {
	return nil, nil
}
func (s answeringSource) SearchMetadata(_ context.Context, query string, limit, _ int) ([]*SeriesMetadata, int, error) {
	return s.answer(query, limit)
}

// allSources enables every catalogue source, MyAnimeList and Comic Vine through their credentials.
func allSources() config.Config {
	var cfg config.Config
	cfg.Scrapers.MALClientID = "test"
	cfg.Scrapers.ComicVineAPIKey = "test"
	return cfg
}

// searchAll fans q out to sources answering through answer and returns every visited page.
func searchAll(t *testing.T, q SourceQuery, answer func(source, query string, limit int) ([]*SeriesMetadata, int, error)) []SourcePage {
	t.Helper()
	fanout := SourceFanout{Config: allSources(), Open: func(_ config.Config, name string) Provider {
		return answeringSource{answer: func(query string, limit int) ([]*SeriesMetadata, int, error) { return answer(name, query, limit) }}
	}}
	var pages []SourcePage
	if err := fanout.Search(context.Background(), q, nil, func(page SourcePage) { pages = append(pages, page) }); err != nil {
		t.Fatal(err)
	}
	return pages
}

func statuses(pages []SourcePage) string {
	var out []string
	for _, page := range pages {
		out = append(out, page.Source+"="+string(page.Status))
	}
	return strings.Join(out, " ")
}

func TestEachCatalogueSourceKeepsItsOwnStatus(t *testing.T) {
	returned := map[string]*SeriesMetadata{}
	pages := searchAll(t, SourceQuery{Text: "Mixed", Limit: 10}, func(source, _ string, _ int) ([]*SeriesMetadata, int, error) {
		switch source {
		case "anilist":
			return []*SeriesMetadata{{Title: "tagged", Tags: make([]string, 101)}}, 1, nil
		case "mangadex":
			return nil, 0, errors.New("unavailable")
		}
		item := &SeriesMetadata{Title: "Kept " + source, Tags: []string{"kept"}, SourceID: 7, Provider: "spoofed"}
		returned[source] = item
		return []*SeriesMetadata{item}, 1, nil
	})
	want := "bangumi=available anilist=invalid_response mangadex=failed myanimelist=available comicvine=available"
	if got := statuses(pages); got != want {
		t.Fatalf("statuses %s, want %s", got, want)
	}
	for _, page := range pages {
		if page.Status != SourceAvailable {
			if len(page.Results) != 0 {
				t.Errorf("%s kept results: %+v", page.Source, page.Results)
			}
			continue
		}
		if len(page.Results) != 1 || page.Results[0].Provider != page.Source {
			t.Errorf("%s result not attributed to its source: %+v", page.Source, page.Results)
		}
	}
	// Kept results are snapshots: the source changing its own objects later changes nothing.
	for _, item := range returned {
		item.Title = "rewritten"
		item.Tags[0] = "rewritten"
	}
	for _, page := range pages {
		for _, kept := range page.Results {
			if kept.Title != "Kept "+page.Source || kept.Tags[0] != "kept" {
				t.Errorf("%s result followed the source object: %+v", page.Source, kept)
			}
		}
	}
}

func TestAnInvalidAnswerMakesOnlyItsSourceUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(limit int) ([]*SeriesMetadata, int)
		status SourceStatus
	}{
		{"oversized result", func(int) ([]*SeriesMetadata, int) {
			return []*SeriesMetadata{{Summary: strings.Repeat("x", 33<<10)}}, 1
		}, SourceInvalidResponse},
		{"too many tags", func(int) ([]*SeriesMetadata, int) { return []*SeriesMetadata{{Tags: make([]string, 101)}}, 1 }, SourceInvalidResponse},
		{"too many tag votes", func(int) ([]*SeriesMetadata, int) { return []*SeriesMetadata{{TagVotes: make([]TagVote, 101)}}, 1 }, SourceInvalidResponse},
		{"negative tag votes", func(int) ([]*SeriesMetadata, int) {
			return []*SeriesMetadata{{TagVotes: []TagVote{{Name: "tag", Votes: -1}}}}, 1
		}, SourceInvalidResponse},
		{"too many authors", func(int) ([]*SeriesMetadata, int) { return []*SeriesMetadata{{Authors: make([]SeriesAuthor, 101)}}, 1 }, SourceInvalidResponse},
		{"negative source ID", func(int) ([]*SeriesMetadata, int) { return []*SeriesMetadata{{SourceID: -1}}, 1 }, SourceInvalidResponse},
		{"negative volumes", func(int) ([]*SeriesMetadata, int) { return []*SeriesMetadata{{VolumeCount: -1}}, 1 }, SourceInvalidResponse},
		{"rating above scale", func(int) ([]*SeriesMetadata, int) { return []*SeriesMetadata{{Rating: 10.5}}, 1 }, SourceInvalidResponse},
		{"negative rating", func(int) ([]*SeriesMetadata, int) { return []*SeriesMetadata{{Rating: -1}}, 1 }, SourceInvalidResponse},
		{"non-finite rating", func(int) ([]*SeriesMetadata, int) { return []*SeriesMetadata{{Rating: math.NaN()}}, 1 }, SourceInvalidResponse},
		{"decision record", func(int) ([]*SeriesMetadata, int) {
			return []*SeriesMetadata{{MatchEvidence: &MatchEvidence{Summary: "source decided"}}}, 1
		}, SourceInvalidResponse},
		{"missing result", func(int) ([]*SeriesMetadata, int) { return []*SeriesMetadata{nil}, 1 }, SourceInvalidResponse},
		{"more than requested", func(limit int) ([]*SeriesMetadata, int) {
			out := make([]*SeriesMetadata, limit+1)
			for i := range out {
				out[i] = &SeriesMetadata{}
			}
			return out, limit + 1
		}, SourceFailed},
		{"negative total", func(int) ([]*SeriesMetadata, int) { return nil, -1 }, SourceFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Only AniList answers badly; every other source still answers.
			pages := searchAll(t, SourceQuery{Text: "query", Limit: 2}, func(source, _ string, limit int) ([]*SeriesMetadata, int, error) {
				if source == "anilist" {
					results, total := tc.answer(limit)
					return results, total, nil
				}
				return []*SeriesMetadata{{Title: "Kept " + source}}, 1, nil
			})
			want := "bangumi=available anilist=" + string(tc.status) + " mangadex=available myanimelist=available comicvine=available"
			if got := statuses(pages); got != want {
				t.Fatalf("statuses %s, want %s", got, want)
			}
			if len(pages[1].Results) != 0 {
				t.Errorf("anilist kept results: %+v", pages[1].Results)
			}
		})
	}
}
