// 手动刮削与多来源搜索用同一份来源范围：目录型来源加上模型，漏一个或多一个，所有者在两处
// 看到的来源就不一致。每个来源回答的校验规则由 metadata 包的扇出测试守卫。

package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"yomifolio/internal/bootstrap"
)

func TestManualScrapingOffersTheCatalogueSourcesPlusTheModel(t *testing.T) {
	store, cfg := fixture(t)
	snapshot := cfg.Snapshot()
	snapshot.Scrapers.MALClientID = "test"
	snapshot.Scrapers.ComicVineAPIKey = "test"
	cfg.Replace(&snapshot)
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	var listed []map[string]string
	if err := json.Unmarshal(sourceHTTP(t, process)("GET", "/metadata/providers", "", 200).Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, provider := range listed {
		if provider["name"] == "" || provider["description"] == "" {
			t.Errorf("unlabelled provider: %v", provider)
		}
		ids = append(ids, provider["id"])
	}
	if want := "[bangumi anilist mangadex myanimelist comicvine llm]"; fmt.Sprint(ids) != want {
		t.Fatalf("providers=%v, want %s", ids, want)
	}
}
