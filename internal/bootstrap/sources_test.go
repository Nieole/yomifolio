package bootstrap_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yomifolio/internal/api"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"

	"github.com/go-chi/chi/v5"
)

type controlledSource struct {
	fetch  func(context.Context, string) (*metadata.SeriesMetadata, error)
	search func(context.Context, string, int, int) ([]*metadata.SeriesMetadata, int, error)
}

func (*controlledSource) Name() string { return "controlled" }
func (s *controlledSource) FetchSeriesMetadata(ctx context.Context, title string) (*metadata.SeriesMetadata, error) {
	return s.fetch(ctx, title)
}
func (s *controlledSource) SearchMetadata(ctx context.Context, title string, limit, offset int) ([]*metadata.SeriesMetadata, int, error) {
	return s.search(ctx, title, limit, offset)
}

func sourceHTTP(t *testing.T, process *bootstrap.Process) func(string, string, string, int) *httptest.ResponseRecorder {
	t.Helper()
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	var cookie *http.Cookie
	var csrf string
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api"+path, strings.NewReader(body))
		if cookie != nil {
			req.AddCookie(cookie)
			req.Header.Set("X-CSRF-Token", csrf)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
		}
		return rec
	}
	setup := request("POST", "/auth/setup", `{"username":"admin","password":"correct horse battery staple"}`, 200)
	cookie = setup.Result().Cookies()[0]
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	csrf = session.CSRF
	return request
}

func TestProductionSourceFeedsHTTPAndApplicationScrape(t *testing.T) {
	store, cfg := fixture(t)
	root := t.TempDir()
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Library", Path: root, ScanMode: "none", ScanFormats: "cbz"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{LibraryID: lib.ID, Name: "Local series", Path: filepath.Join(root, "series")})
	if err != nil {
		t.Fatal(err)
	}
	var picture, archive bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(&archive)
	page, err := z.Create("01.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.Write(picture.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "01.cbz")
	if err := os.WriteFile(path, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	book, err := store.CreateBook(context.Background(), database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID, Name: "01.cbz", Path: path, Size: int64(archive.Len()), PageCount: 1, FileModifiedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}, 1), make(chan struct{})
	source := &controlledSource{fetch: func(ctx context.Context, title string) (*metadata.SeriesMetadata, error) {
		if title != "Local series" {
			return nil, fmt.Errorf("unexpected title %q", title)
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &metadata.SeriesMetadata{Title: "Source title", Summary: "Source summary", Confidence: 1}, nil
	}, search: func(_ context.Context, title string, limit, offset int) ([]*metadata.SeriesMetadata, int, error) {
		if title != "Local series" || limit != 2 || offset != 3 {
			return nil, 0, fmt.Errorf("unexpected search %q %d %d", title, limit, offset)
		}
		return []*metadata.SeriesMetadata{{Title: "Candidate one"}, {Title: "Candidate two"}}, 5, nil
	}}
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{Sources: api.ExternalSources{Metadata: func(_ config.Config, name string) metadata.Provider {
		if name != "bangumi" {
			t.Errorf("source key=%q", name)
		}
		return source
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	request := sourceHTTP(t, process)
	content := request("GET", fmt.Sprintf("/pages/%d/1?format=png", book.ID), "", 200)
	if _, err := png.Decode(bytes.NewReader(content.Body.Bytes())); err != nil {
		t.Fatal(err)
	}
	found := request("GET", fmt.Sprintf("/series/%d/scrape-search?provider=bangumi&limit=2&offset=3", series.ID), "", 200)
	if !strings.Contains(found.Body.String(), "Candidate two") || !strings.Contains(found.Body.String(), `"total":5`) {
		t.Fatal(found.Body.String())
	}
	request("POST", fmt.Sprintf("/libraries/%d/scrape", lib.ID), `{"provider":"bangumi"}`, 200)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("application did not reach injected source")
	}
	runs := request("GET", "/system/tasks?type=scrape", "", 200)
	if !strings.Contains(runs.Body.String(), `"status":"running"`) {
		t.Fatal(runs.Body.String())
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		reviews := request("GET", "/metadata/reviews", "", 200)
		if strings.Contains(reviews.Body.String(), "Source summary") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("source result not visible via HTTP: " + reviews.Body.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type controlledAI struct {
	*controlledSource
	grouping func(context.Context, []metadata.CandidateSeries) ([]metadata.AIGroupCollection, error)
}

func (s *controlledAI) GenerateGrouping(ctx context.Context, candidates []metadata.CandidateSeries) ([]metadata.AIGroupCollection, error) {
	return s.grouping(ctx, candidates)
}
func (*controlledAI) GenerateRecommendations(context.Context, []string, []metadata.CandidateSeries, int) ([]metadata.AIRecommendation, error) {
	return nil, fmt.Errorf("controlled recommendation error")
}
func (*controlledAI) TestLLM(context.Context, string) (string, error) {
	return "controlled connection response", nil
}

func TestProductionLLMSourceCancellationAndConnectionTest(t *testing.T) {
	store, cfg := fixture(t)
	root := t.TempDir()
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Library", Path: root, ScanMode: "none", ScanFormats: "cbz"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"One", "Two"} {
		if _, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{LibraryID: lib.ID, Name: name, Path: filepath.Join(root, name)}); err != nil {
			t.Fatal(err)
		}
	}
	entered, cancelled := make(chan struct{}), make(chan struct{})
	ai := &controlledAI{controlledSource: &controlledSource{}, grouping: func(ctx context.Context, candidates []metadata.CandidateSeries) ([]metadata.AIGroupCollection, error) {
		if len(candidates) != 2 {
			return nil, fmt.Errorf("expected two candidates, got %d", len(candidates))
		}
		close(entered)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}}
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{Sources: api.ExternalSources{AI: func(_, _, _, _, _, _ string, _ int) metadata.AIProvider { return ai }}})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	request := sourceHTTP(t, process)
	response := request("POST", "/system/test-llm", `{"provider":"openai","base_url":"https://example.invalid","model":"test"}`, 200)
	if !strings.Contains(response.Body.String(), "controlled connection response") {
		t.Fatal(response.Body.String())
	}
	request("POST", fmt.Sprintf("/libraries/%d/ai-grouping", lib.ID), "{}", 202)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("LLM source not reached")
	}
	listed := request("GET", "/system/tasks?type=ai_grouping", "", 200)
	var runs []struct {
		RunID  int64  `json:"run_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != "running" {
		t.Fatal(listed.Body.String())
	}
	request("POST", fmt.Sprintf("/system/runs/%d/cancel", runs[0].RunID), "{}", 202)
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP cancellation did not reach source context")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		rec := request("GET", "/system/tasks?type=ai_grouping", "", 200)
		if strings.Contains(rec.Body.String(), `"status":"cancelled"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(rec.Body.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	reviews := request("GET", "/ai-grouping/reviews", "", 200)
	if strings.Contains(reviews.Body.String(), `"id":`) {
		t.Fatal("cancelled generation created a review: " + reviews.Body.String())
	}
}

func TestProductionSourceErrorsAreVisibleOverHTTP(t *testing.T) {
	store, cfg := fixture(t)
	source := &controlledSource{fetch: func(context.Context, string) (*metadata.SeriesMetadata, error) {
		return nil, fmt.Errorf("controlled upstream unavailable")
	}}
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{Sources: api.ExternalSources{Metadata: func(config.Config, string) metadata.Provider { return source }}})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	request := sourceHTTP(t, process)
	rec := request("GET", "/metadata/search?provider=llm&q=Example", "", 500)
	if !strings.Contains(rec.Body.String(), "controlled upstream unavailable") {
		t.Fatal(rec.Body.String())
	}
}
