// 来源目录是目录型来源的唯一描述：它的顺序要与配置的默认优先级一致，显示名要与各 provider 的 Name 一致，
// 否则识别追加的来源链接与提案应用的链接会对不上。

package metadata

import (
	"slices"
	"testing"

	"yomifolio/internal/config"
)

func TestCatalogueFollowsTheDefaultPriorityAndTheProvidersNames(t *testing.T) {
	var cfg config.Config
	cfg.Scrapers.MALClientID, cfg.Scrapers.ComicVineAPIKey = "id", "key"
	var keys []string
	for _, source := range Catalogue() {
		keys = append(keys, source.Key)
		if got := NewConfiguredProvider(cfg, source.Key).Name(); got != source.Name {
			t.Errorf("%s: catalogue name %q, provider name %q", source.Key, source.Name, got)
		}
		if found, ok := LookupCatalogueSource(source.Name); !ok || found.Key != source.Key {
			t.Errorf("%s: its display name %q looks up %+v", source.Key, source.Name, found)
		}
	}
	if !slices.Equal(keys, []string(config.DefaultSourcePriority)) {
		t.Fatalf("catalogue order %v, default priority %v", keys, config.DefaultSourcePriority)
	}
	if got := CatalogueSources(config.Config{}); !slices.Equal(got, []string{"bangumi", "anilist", "mangadex"}) {
		t.Fatalf("sources enabled without credentials %v", got)
	}
}
