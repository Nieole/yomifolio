// 守单系列刮削入口的传输语义：响应里的 outcome 结果码——
// 前端按 outcome 分支，换成解析中文 message 会随文案改动一起碎掉。
// 写回系列那条路径（锁定字段、标签作者、来源沿革与外链）归 internal/proposal 的写入器用例。

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"yomifolio/internal/metadata"
)

func TestScrapeSeriesMetadataValidationHandlers(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	invalidRec := httptest.NewRecorder()
	controller.providers.scrapeSeriesMetadata(invalidRec, requestWithRouteParam(http.MethodPost, "/api/series/bad/scrape", nil, "seriesId", "bad"))
	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid series id 400, got %d", invalidRec.Code)
	}

	notFoundRec := httptest.NewRecorder()
	controller.providers.scrapeSeriesMetadata(notFoundRec, requestWithRouteParam(http.MethodPost, "/api/series/999/scrape", nil, "seriesId", "999"))
	if notFoundRec.Code != http.StatusNotFound {
		t.Fatalf("expected missing series 404, got %d", notFoundRec.Code)
	}

	_ = series
}

func TestApplyScrapedMetadataOutcomeCodes(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	body, err := json.Marshal(metadata.SeriesMetadata{
		Summary:  "A brand new summary produced by the scraper",
		Rating:   8.5,
		SourceID: 54321,
	})
	if err != nil {
		t.Fatalf("marshal metadata failed: %v", err)
	}

	post := func() map[string]any {
		req := requestWithRouteParam(http.MethodPost, "/api/series/1/scrape-apply?provider=bangumi", body, "seriesId", strconv.FormatInt(series.ID, 10))
		rec := httptest.NewRecorder()
		controller.providers.applyScrapedMetadata(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response failed: %v", err)
		}
		return resp
	}

	// 首次提交带差异的元数据应入队审核，outcome=queued（前端据此本地化成功提示）。
	first := post()
	if first["outcome"] != "queued" {
		t.Fatalf("first apply: expected outcome=queued, got %v (resp=%v)", first["outcome"], first)
	}

	// 再次提交完全相同的数据应命中重复忽略，outcome=duplicate_ignored。
	second := post()
	if second["outcome"] != "duplicate_ignored" {
		t.Fatalf("second apply: expected outcome=duplicate_ignored, got %v (resp=%v)", second["outcome"], second)
	}
}
