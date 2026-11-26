// 本文件验证各目录型来源把条目的原名与别名带进 SeriesMetadata：识别结论保存它们，复核判据拿它们比对。

package metadata

import (
	"context"
	"net/http"
	"reflect"
	"testing"
)

func TestSourcesNameTheEntrysOriginalTitleAndAliases(t *testing.T) {
	cases := []struct {
		name         string
		provider     func(roundTripFunc) Provider
		body         string
		wantOriginal string
		wantAliases  []string
	}{
		{
			name: "bangumi infobox aliases",
			provider: func(fn roundTripFunc) Provider {
				p := NewBangumiProvider()
				p.httpClient = &http.Client{Transport: fn}
				return p
			},
			body: `{"total":1,"data":[{"id":1,"name":"MONSTER","name_cn":"怪物","infobox":[` +
				`{"key":"中文名","value":"怪物"},{"key":"别名","value":[{"v":"Monster"},{"v":" 莫扎特 "},{"v":"monster"},{"v":""}]},{"key":"作者","value":"浦沢直樹"}]}]}`,
			wantOriginal: "MONSTER",
			wantAliases:  []string{"莫扎特"},
		},
		{
			name: "anilist title variants and synonyms",
			provider: func(fn roundTripFunc) Provider {
				p := NewAniListProvider()
				p.httpClient = &http.Client{Transport: fn}
				return p
			},
			body: `{"data":{"Page":{"pageInfo":{"total":1},"media":[{"id":1,"title":{"romaji":"Monster","english":"Monster","native":"MONSTER"},` +
				`"synonyms":["Monstre","Naoki Urasawa's Monster"],"siteUrl":"https://anilist.co/manga/1"}]}}}`,
			wantOriginal: "MONSTER",
			wantAliases:  []string{"Monstre", "Naoki Urasawa's Monster"},
		},
		{
			name: "mangadex localized titles and altTitles",
			provider: func(fn roundTripFunc) Provider {
				p := NewMangaDexProvider()
				p.httpClient = &http.Client{Transport: fn}
				return p
			},
			body: `{"data":[{"id":"0a1b2c3d-0000-4000-8000-000000000001","attributes":{"title":{"en":"Monster"},` +
				`"altTitles":[{"ja":"MONSTER"},{"ja-ro":"Monsutā"},{"zh":"怪物"},{"en":"Monster"}]},"relationships":[]}],"total":1}`,
			wantOriginal: "MONSTER",
			wantAliases:  []string{"Monsutā", "怪物"},
		},
		{
			name: "myanimelist alternative titles",
			provider: func(fn roundTripFunc) Provider {
				p := NewMyAnimeListProvider("client")
				p.httpClient = &http.Client{Transport: fn}
				return p
			},
			body:         `{"data":[{"node":{"id":1,"title":"Monster","alternative_titles":{"synonyms":["Naoki Urasawa's Monster"],"en":"Monster","ja":"MONSTER"}}}],"paging":{}}`,
			wantOriginal: "MONSTER",
			wantAliases:  []string{"Naoki Urasawa's Monster"},
		},
		{
			name: "comicvine newline separated aliases",
			provider: func(fn roundTripFunc) Provider {
				p := NewComicVineProvider("key")
				p.httpClient = &http.Client{Transport: fn}
				return p
			},
			body:        `{"error":"OK","number_of_total_results":1,"results":[{"id":1,"name":"Monster","aliases":"Naoki Urasawa's Monster\r\nMonster\n\nPerfect Edition"}]}`,
			wantAliases: []string{"Naoki Urasawa's Monster", "Perfect Edition"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.provider(func(req *http.Request) (*http.Response, error) { return metaJSONResponse(req, tc.body), nil })
			results, _, err := p.SearchMetadata(context.Background(), "monster", 5, 0)
			if err != nil || len(results) != 1 {
				t.Fatalf("search %+v %v", results, err)
			}
			if got := results[0]; got.OriginalTitle != tc.wantOriginal || !reflect.DeepEqual(got.Aliases, tc.wantAliases) {
				t.Fatalf("original %q aliases %q, want %q %q", got.OriginalTitle, got.Aliases, tc.wantOriginal, tc.wantAliases)
			}
			// Aliases survive the fan-out snapshot the evidence packets and conclusions keep.
			if snapshot, ok := Snapshot("source", results[0]); !ok || !reflect.DeepEqual(snapshot.Aliases, tc.wantAliases) {
				t.Fatalf("snapshot aliases %q valid=%v", snapshot.Aliases, ok)
			}
		})
	}
}
