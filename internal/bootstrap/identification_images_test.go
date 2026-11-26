// These tests guard the identification image primitive of the assembled process: a read
// credential sees the series' own cover and pages under the content admission rules, and candidate
// covers come only from the evidence packet's sources, downloaded by the server from their known
// hosts and shrunk, within the one image result budget. A broken guard lets the tool fetch
// arbitrary addresses, hand out a corrupt or oversized result, or leak another library's images.

package bootstrap_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
)

// coverHost stands in for the sources' image hosts: it records every URL that reached it and
// answers with serve.
type coverHost struct {
	mu       sync.Mutex
	requests []string
	serve    func(*http.Request) (*http.Response, error)
}

func (h *coverHost) RoundTrip(r *http.Request) (*http.Response, error) {
	h.mu.Lock()
	h.requests = append(h.requests, r.URL.String())
	h.mu.Unlock()
	return h.serve(r)
}

func (h *coverHost) received() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.requests...)
}

// solidPNG is a w×h picture of one colour.
func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// noisyPNG is a picture JPEG cannot compress, so its thumbnail lands near the per-image limit.
func noisyPNG(t *testing.T, seed int64) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 384, 384))
	rng := rand.New(rand.NewSource(seed))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256)), 255
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

var (
	red   = color.RGBA{R: 230, G: 20, B: 40, A: 255}
	green = color.RGBA{R: 20, G: 200, B: 40, A: 255}
	blue  = color.RGBA{R: 20, G: 40, B: 230, A: 255}
)

// imageWorld is one opened process holding a series whose only book has the given pages and a
// red automatic cover, sources that answer with candidates, and a stand-in cover host.
type imageWorld struct {
	t       *testing.T
	web     *identificationWeb
	cfg     *config.Manager
	library *prefetchLibrary
	series  int64
	book    int64
	archive string
	host    *coverHost
}

func openImageWorld(t *testing.T, pages [][]byte, answer func(source, query string) ([]*metadata.SeriesMetadata, error), host *coverHost) *imageWorld {
	t.Helper()
	ctx := context.Background()
	store, cfg := fixture(t)
	unpaced(t, cfg)
	snapshot := cfg.Snapshot()
	snapshot.Scrapers.MALClientID, snapshot.Scrapers.ComicVineAPIKey = "client", "key"
	cfg.Replace(&snapshot)
	library := newPrefetchLibrary(t, store, "Pictures")
	dir := filepath.Join(library.lib.Path, "Alpha")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: library.lib.ID, Name: "Alpha", Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	for i, data := range pages {
		w, e := z.Create(string(rune('a'+i)) + ".png")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Alpha 01.cbz")
	if err = os.WriteFile(path, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	modified := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	book, err := store.CreateBook(ctx, database.CreateBookParams{LibraryID: library.lib.ID, SeriesID: series.ID, Name: "Alpha 01.cbz", Path: path, Size: int64(archive.Len()), FileModifiedAt: modified, PageCount: int64(len(pages))})
	if err != nil {
		t.Fatal(err)
	}
	cover := automaticCoverPath(path, modified, int64(archive.Len()))
	coverFile := filepath.Join(config.ThumbnailDir(cfg.Snapshot()), cover)
	if err = os.MkdirAll(filepath.Dir(coverFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(coverFile, solidPNG(t, 800, 400, red), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().Exec(`UPDATE books SET cover_path = ? WHERE id = ?`, cover, book.ID); err != nil {
		t.Fatal(err)
	}
	sources := &recordingSources{answer: answer}
	options := sources.options()
	options.Sources.Covers = host
	process, err := bootstrap.Open(ctx, store, cfg, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(process.Close)
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	web := &identificationWeb{t: t, store: store, process: process, router: router}
	web.setupAdmin()
	return &imageWorld{t: t, web: web, cfg: cfg, library: library, series: series.ID, book: book.ID, archive: path, host: host}
}

// identificationImage mirrors one entry of the tool's images list.
type identificationImage struct {
	Kind        string `json:"kind"`
	BookID      int64  `json:"book_id"`
	Page        int64  `json:"page"`
	CandidateID string `json:"candidate_id"`
	Status      string `json:"status"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

type identificationImages struct {
	SeriesID int64                 `json:"series_id"`
	Images   []identificationImage `json:"images"`
}

// imagesCall is one successful call: the structured result and the decoded image blocks.
type imagesCall struct {
	out     identificationImages
	decoded []image.Image
	raw     []byte
}

// images calls the tool and checks the result contract shared by every successful call: the
// first block repeats the structured result as JSON, one JPEG follows per included image in list
// order, and the whole result stays within the 256 KiB image budget.
func (w *imageWorld) images(session *mcp.ClientSession, args map[string]any) imagesCall {
	w.t.Helper()
	// Bounded, so a download slot that never came back fails the call instead of hanging it.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_identification_images", Arguments: args})
	if err != nil || result.IsError {
		w.t.Fatalf("images %v: %+v %v", args, result, err)
	}
	var call imagesCall
	call.raw, _ = json.Marshal(result)
	if len(call.raw) > 256<<10 {
		w.t.Fatalf("image result %d bytes over budget", len(call.raw))
	}
	structured, _ := json.Marshal(result.StructuredContent)
	if err = json.Unmarshal(structured, &call.out); err != nil {
		w.t.Fatal(err)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		w.t.Fatalf("first block %T is not the JSON text copy", result.Content[0])
	}
	var copied, want any
	_ = json.Unmarshal(structured, &want)
	if json.Unmarshal([]byte(text.Text), &copied) != nil || !reflect.DeepEqual(copied, want) {
		w.t.Fatalf("text copy %s differs from structured %s", text.Text, structured)
	}
	for _, block := range result.Content[1:] {
		picture, ok := block.(*mcp.ImageContent)
		if !ok || picture.MIMEType != "image/jpeg" || len(picture.Data) > 96<<10 {
			w.t.Fatalf("image block %+v", block)
		}
		decoded, format, e := image.Decode(bytes.NewReader(picture.Data))
		if e != nil || format != "jpeg" {
			w.t.Fatalf("image block is not a JPEG: %v %s", e, format)
		}
		call.decoded = append(call.decoded, decoded)
	}
	included := 0
	for _, entry := range call.out.Images {
		if entry.Status == "included" {
			if b := call.decoded[included].Bounds(); b.Dx() != entry.Width || b.Dy() != entry.Height {
				w.t.Fatalf("%+v listed, image is %v", entry, b)
			}
			included++
		}
	}
	if included != len(call.decoded) {
		w.t.Fatalf("%d included entries, %d image blocks", included, len(call.decoded))
	}
	return call
}

// colourAt reports which of red, green or blue dominates the middle of an image.
func colourAt(img image.Image) string {
	b := img.Bounds()
	r, g, bl, _ := img.At(b.Dx()/2, b.Dy()/2).RGBA()
	switch {
	case r > 40000 && g < 20000 && bl < 20000:
		return "red"
	case g > 40000 && r < 20000 && bl < 20000:
		return "green"
	case bl > 40000 && r < 20000 && g < 20000:
		return "blue"
	}
	return "other"
}

func TestIdentificationImagesShowTheSeriesOwnCover(t *testing.T) {
	world := openImageWorld(t, [][]byte{solidPNG(t, 600, 900, blue)}, func(string, string) ([]*metadata.SeriesMetadata, error) { return nil, nil }, &coverHost{serve: func(*http.Request) (*http.Response, error) {
		t.Error("no candidate cover was asked for")
		return nil, http.ErrHandlerTimeout
	}})
	agent := world.web.identificationAgent(world.cfg, 1, []int64{world.library.lib.ID}, []string{"read"})

	call := world.images(agent, map[string]any{"series_id": world.series, "cover": true})
	want := []identificationImage{{Kind: "cover", BookID: world.book, Status: "included", Width: 384, Height: 192}}
	if call.out.SeriesID != world.series || !reflect.DeepEqual(call.out.Images, want) {
		t.Fatalf("images %+v", call.out)
	}
	if colourAt(call.decoded[0]) != "red" {
		t.Fatal("cover image lost its content")
	}
}

func TestIdentificationImagesShowChosenPagesOfTheSeriesBook(t *testing.T) {
	world := openImageWorld(t, [][]byte{solidPNG(t, 600, 900, blue), solidPNG(t, 900, 600, green)}, func(string, string) ([]*metadata.SeriesMetadata, error) { return nil, nil }, &coverHost{serve: func(*http.Request) (*http.Response, error) {
		t.Error("no candidate cover was asked for")
		return nil, http.ErrHandlerTimeout
	}})
	agent := world.web.identificationAgent(world.cfg, 1, []int64{world.library.lib.ID}, []string{"read"})

	call := world.images(agent, map[string]any{"series_id": world.series, "cover": true, "book_id": world.book, "pages": []int{2, 7, 1}})
	want := []identificationImage{
		{Kind: "cover", BookID: world.book, Status: "included", Width: 384, Height: 192},
		{Kind: "page", BookID: world.book, Page: 2, Status: "included", Width: 384, Height: 256},
		{Kind: "page", BookID: world.book, Page: 7, Status: "page_not_found"},
		{Kind: "page", BookID: world.book, Page: 1, Status: "included", Width: 256, Height: 384},
	}
	if !reflect.DeepEqual(call.out.Images, want) {
		t.Fatalf("images %+v", call.out.Images)
	}
	if got := []string{colourAt(call.decoded[0]), colourAt(call.decoded[1]), colourAt(call.decoded[2])}; !reflect.DeepEqual(got, []string{"red", "green", "blue"}) {
		t.Fatalf("image contents %v", got)
	}

	for _, args := range []map[string]any{
		{"series_id": world.series},
		{"series_id": world.series, "book_id": world.book, "pages": []int{}},
		{"series_id": world.series, "book_id": world.book, "pages": []int{1, 2, 1}},
		{"series_id": world.series, "book_id": world.book, "pages": []int{0}},
		{"series_id": world.series, "book_id": world.book, "pages": []int{1, 2, 3, 4}},
		{"series_id": world.series, "pages": []int{1}},
	} {
		result, err := agent.CallTool(context.Background(), &mcp.CallToolParams{Name: "library_get_identification_images", Arguments: args})
		if err != nil || !result.IsError || len(result.Content) != 1 {
			t.Fatalf("%v accepted: %+v %v", args, result, err)
		}
	}
}

// pictureResponse answers with an image body of the given content type.
func pictureResponse(r *http.Request, contentType string, body []byte) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: r}
}

func redirectResponse(r *http.Request, location string) *http.Response {
	return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {location}}, Body: http.NoBody, Request: r}
}

const mangaDexEntry = "0b2a6d2e-3f43-4f0e-9a3c-6c1d3e8f9a01"

func TestIdentificationImagesShowEvidenceCandidateCovers(t *testing.T) {
	greenCover, blueCover := solidPNG(t, 600, 900, green), solidPNG(t, 900, 600, blue)
	host := &coverHost{}
	host.serve = func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case "https://lain.bgm.tv/pic/cover/l/101.jpg":
			return pictureResponse(r, "image/png", greenCover), nil
		case "https://s4.anilist.co/file/cover/201.png":
			return pictureResponse(r, "image/png", blueCover), nil
		case "https://uploads.mangadex.org/covers/old.png":
			return redirectResponse(r, "https://uploads.mangadex.org/covers/new.png"), nil
		case "https://uploads.mangadex.org/covers/new.png":
			return pictureResponse(r, "image/png", greenCover), nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody, Request: r}, nil
	}
	world := openImageWorld(t, [][]byte{solidPNG(t, 600, 900, blue)}, func(source, _ string) ([]*metadata.SeriesMetadata, error) {
		switch source {
		case "bangumi":
			return []*metadata.SeriesMetadata{{Title: "Alpha", SourceID: 101, SourceURL: "https://bgm.tv/subject/101", CoverURL: "https://lain.bgm.tv/pic/cover/l/101.jpg"}}, nil
		case "anilist":
			// A plain-HTTP cover address is fetched over HTTPS.
			return []*metadata.SeriesMetadata{{Title: "Alpha", SourceID: 201, SourceURL: "https://anilist.co/manga/201", CoverURL: "http://s4.anilist.co/file/cover/201.png"}}, nil
		case "mangadex":
			return []*metadata.SeriesMetadata{{Title: "Alpha", SourceURL: "https://mangadex.org/title/" + mangaDexEntry, CoverURL: "https://uploads.mangadex.org/covers/old.png"}}, nil
		}
		return nil, nil
	}, host)
	world.web.prefetch("{}")
	agent := world.web.identificationAgent(world.cfg, 1, []int64{world.library.lib.ID}, []string{"read"})

	call := world.images(agent, map[string]any{"series_id": world.series, "cover": true, "candidate_ids": []string{"anilist:201", "mangadex:" + mangaDexEntry, "bangumi:101"}})
	want := []identificationImage{
		{Kind: "cover", BookID: world.book, Status: "included", Width: 384, Height: 192},
		{Kind: "candidate", CandidateID: "anilist:201", Status: "included", Width: 384, Height: 256},
		{Kind: "candidate", CandidateID: "mangadex:" + mangaDexEntry, Status: "included", Width: 256, Height: 384},
		{Kind: "candidate", CandidateID: "bangumi:101", Status: "included", Width: 256, Height: 384},
	}
	if !reflect.DeepEqual(call.out.Images, want) {
		t.Fatalf("images %+v", call.out.Images)
	}
	if got := []string{colourAt(call.decoded[0]), colourAt(call.decoded[1]), colourAt(call.decoded[2]), colourAt(call.decoded[3])}; !reflect.DeepEqual(got, []string{"red", "blue", "green", "green"}) {
		t.Fatalf("image contents %v", got)
	}
}

// zeros is an endless body, so an oversized download costs the stand-in nothing.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestIdentificationImagesRefuseCoversOutsideTheSourcesKnownHosts(t *testing.T) {
	host := &coverHost{}
	host.serve = func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case "https://lain.bgm.tv/pic/103.jpg":
			return redirectResponse(r, "https://evil.example.com/103.jpg"), nil
		case "https://s4.anilist.co/201.png":
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(io.LimitReader(zeros{}, 17<<20)), ContentLength: 17 << 20, Request: r}, nil
		case "https://s4.anilist.co/202.png":
			// No declared length: the download itself is cut at the limit.
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(io.LimitReader(zeros{}, 16<<20+1)), ContentLength: -1, Request: r}, nil
		case "https://s4.anilist.co/203.png":
			return pictureResponse(r, "text/html", []byte("<html>not a cover</html>")), nil
		case "https://uploads.mangadex.org/covers/garbage.png":
			return pictureResponse(r, "image/png", []byte("not really a png")), nil
		case "https://comicvine.gamespot.com/a/uploads/501.jpg":
			return &http.Response{StatusCode: http.StatusInternalServerError, Body: http.NoBody, Request: r}, nil
		case "https://comicvine.gamespot.com/a/uploads/502.jpg":
			return nil, errors.New("connection reset")
		}
		t.Errorf("unexpected cover request %s", r.URL)
		return nil, errors.New("unexpected")
	}
	world := openImageWorld(t, [][]byte{solidPNG(t, 600, 900, blue)}, func(source, _ string) ([]*metadata.SeriesMetadata, error) {
		entry := func(id int, url, cover string) *metadata.SeriesMetadata {
			return &metadata.SeriesMetadata{Title: "Alpha", SourceID: id, SourceURL: url, CoverURL: cover}
		}
		switch source {
		case "bangumi":
			return []*metadata.SeriesMetadata{
				entry(102, "https://bgm.tv/subject/102", "https://evil.example.com/102.jpg"),
				entry(103, "https://bgm.tv/subject/103", "https://lain.bgm.tv/pic/103.jpg"),
				entry(104, "https://bgm.tv/subject/104", "https://lain.bgm.tv.evil.example/104.jpg"),
			}, nil
		case "anilist":
			return []*metadata.SeriesMetadata{
				entry(201, "https://anilist.co/manga/201", "https://s4.anilist.co/201.png"),
				entry(202, "https://anilist.co/manga/202", "https://s4.anilist.co/202.png"),
				entry(203, "https://anilist.co/manga/203", "https://s4.anilist.co/203.png"),
			}, nil
		case "mangadex":
			return []*metadata.SeriesMetadata{{Title: "Alpha", SourceURL: "https://mangadex.org/title/" + mangaDexEntry, CoverURL: "https://uploads.mangadex.org/covers/garbage.png"}}, nil
		case "myanimelist":
			return []*metadata.SeriesMetadata{entry(401, "https://myanimelist.net/manga/401", "ftp://cdn.myanimelist.net/401.jpg")}, nil
		case "comicvine":
			return []*metadata.SeriesMetadata{
				entry(501, "https://comicvine.gamespot.com/alpha/4050-501/", "https://comicvine.gamespot.com/a/uploads/501.jpg"),
				entry(502, "https://comicvine.gamespot.com/alpha/4050-502/", "https://comicvine.gamespot.com/a/uploads/502.jpg"),
				entry(503, "https://comicvine.gamespot.com/alpha/4050-503/", ""),
			}, nil
		}
		return nil, nil
	}, host)
	world.web.prefetch("{}")
	if packet := world.web.evidence(world.series); packet == nil || len(packet.Candidates) != 11 {
		t.Fatalf("evidence %+v", packet)
	}
	agent := world.web.identificationAgent(world.cfg, 1, []int64{world.library.lib.ID}, []string{"read"})

	for id, want := range map[string]string{
		"bangumi:102":               "host_rejected",
		"bangumi:103":               "host_rejected",
		"bangumi:104":               "host_rejected",
		"anilist:201":               "too_large",
		"anilist:202":               "too_large",
		"anilist:203":               "not_image",
		"mangadex:" + mangaDexEntry: "not_image",
		"myanimelist:401":           "host_rejected",
		"comicvine:501":             "unavailable",
		"comicvine:502":             "unavailable",
		"comicvine:503":             "no_cover",
		// Callers name candidates, never addresses.
		"https://lain.bgm.tv/pic/103.jpg": "not_in_evidence",
		"bangumi:999":                     "not_in_evidence",
	} {
		call := world.images(agent, map[string]any{"series_id": world.series, "candidate_ids": []string{id}})
		if got := call.out.Images; len(got) != 1 || got[0] != (identificationImage{Kind: "candidate", CandidateID: id, Status: want}) {
			t.Errorf("%s: %+v, want %s", id, got, want)
		}
	}
	for _, url := range host.received() {
		if !strings.HasPrefix(url, "https://lain.bgm.tv/") && !strings.HasPrefix(url, "https://s4.anilist.co/") &&
			!strings.HasPrefix(url, "https://uploads.mangadex.org/") && !strings.HasPrefix(url, "https://comicvine.gamespot.com/") {
			t.Errorf("a cover request left the sources' hosts: %s", url)
		}
	}
}

func TestIdentificationImagesLeaveOutWholeImagesBeyondTheResultBudget(t *testing.T) {
	noisy, plain := noisyPNG(t, 9), solidPNG(t, 600, 900, green)
	host := &coverHost{}
	host.serve = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/noisy.png" {
			return pictureResponse(r, "image/png", noisy), nil
		}
		return pictureResponse(r, "image/png", plain), nil
	}
	world := openImageWorld(t, [][]byte{noisyPNG(t, 1), noisyPNG(t, 2), noisyPNG(t, 3)}, func(source, _ string) ([]*metadata.SeriesMetadata, error) {
		if source != "bangumi" {
			return nil, nil
		}
		return []*metadata.SeriesMetadata{
			{Title: "Alpha", SourceID: 101, SourceURL: "https://bgm.tv/subject/101", CoverURL: "https://lain.bgm.tv/noisy.png"},
			{Title: "Alpha", SourceID: 102, SourceURL: "https://bgm.tv/subject/102", CoverURL: "https://lain.bgm.tv/plain.png"},
		}, nil
	}, host)
	world.web.prefetch("{}")
	agent := world.web.identificationAgent(world.cfg, 1, []int64{world.library.lib.ID}, []string{"read"})

	// Two noisy thumbnails nearly fill the budget, so the third and the noisy candidate stay out
	// whole while the small candidate after them still fits.
	call := world.images(agent, map[string]any{"series_id": world.series, "cover": true, "book_id": world.book, "pages": []int{1, 2, 3}, "candidate_ids": []string{"bangumi:101", "bangumi:102"}})
	var statuses []string
	for _, image := range call.out.Images {
		statuses = append(statuses, image.Kind+":"+image.Status)
	}
	if want := []string{"cover:included", "page:included", "page:included", "page:over_budget", "candidate:over_budget", "candidate:included"}; !reflect.DeepEqual(statuses, want) {
		t.Fatalf("statuses %v want %v", statuses, want)
	}
	if left := call.out.Images[3]; left.Width != 0 || left.Height != 0 {
		t.Fatalf("an image left out still describes a thumbnail: %+v", left)
	}
	alone := world.images(agent, map[string]any{"series_id": world.series, "book_id": world.book, "pages": []int{3}})
	if len(alone.out.Images) != 1 || alone.out.Images[0].Status != "included" {
		t.Fatalf("page asked for alone %+v", alone.out.Images)
	}
}

func TestIdentificationImagesStayInsideTheCredentialsReadGrant(t *testing.T) {
	host := &coverHost{}
	host.serve = func(r *http.Request) (*http.Response, error) {
		t.Errorf("a denied call reached the cover host: %s", r.URL)
		return nil, errors.New("unexpected")
	}
	world := openImageWorld(t, [][]byte{solidPNG(t, 600, 900, blue)}, func(source, _ string) ([]*metadata.SeriesMetadata, error) {
		if source != "bangumi" {
			return nil, nil
		}
		return []*metadata.SeriesMetadata{{Title: "Alpha", SourceID: 101, SourceURL: "https://bgm.tv/subject/101", CoverURL: "https://lain.bgm.tv/101.jpg"}}, nil
	}, host)
	world.web.prefetch("{}")
	other := newPrefetchLibrary(t, world.web.store, "Elsewhere")
	hidden := other.series("Hidden", "", "01.cbz")
	denied := func(session *mcp.ClientSession, args map[string]any) {
		t.Helper()
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "library_get_identification_images", Arguments: args})
		if err != nil || !result.IsError || !strings.HasPrefix(resultText(result), "delegated access denied") {
			t.Fatalf("%v not denied: %+v %v", args, result, err)
		}
	}

	reader := world.web.identificationAgent(world.cfg, 1, []int64{world.library.lib.ID}, []string{"read"})
	denied(reader, map[string]any{"series_id": hidden, "candidate_ids": []string{"bangumi:101"}})
	denied(reader, map[string]any{"series_id": 999999, "cover": true})
	// A readable book of another series cannot be looked at through this one.
	var foreign int64
	if err := world.web.store.DB().QueryRow(`SELECT id FROM books WHERE series_id = ?`, hidden).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	wide := world.web.identificationAgent(world.cfg, 1, []int64{world.library.lib.ID, other.lib.ID}, []string{"read"})
	denied(wide, map[string]any{"series_id": world.series, "book_id": foreign, "pages": []int{1}})

	writer := world.web.identificationAgent(world.cfg, 1, []int64{world.library.lib.ID}, []string{"metadata_write"})
	listed, err := writer.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if offersTool(listed, "library_get_identification_images") {
		t.Fatal("a credential without read lists the image tool")
	}
	denied(writer, map[string]any{"series_id": world.series, "candidate_ids": []string{"bangumi:101"}})
}

func TestIdentificationImagesReturnWhatTheyHoldWhenTheCallerLeaves(t *testing.T) {
	var hold sync.Map // request URL -> chan struct{} closed when the download was given up
	started := make(chan string, 8)
	var both sync.WaitGroup
	host := &coverHost{}
	host.serve = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/hang.png" {
			started <- r.URL.String()
			<-r.Context().Done()
			gone, _ := hold.LoadOrStore(r.URL.String(), make(chan struct{}))
			close(gone.(chan struct{}))
			return nil, r.Context().Err()
		}
		// The two later downloads answer only once both are under way, which needs both slots.
		both.Done()
		waited := make(chan struct{})
		go func() { both.Wait(); close(waited) }()
		select {
		case <-waited:
			return pictureResponse(r, "image/png", solidPNG(t, 600, 900, green)), nil
		case <-time.After(5 * time.Second):
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody, Request: r}, nil
		}
	}
	both.Add(2)
	world := openImageWorld(t, [][]byte{solidPNG(t, 600, 900, blue)}, func(source, _ string) ([]*metadata.SeriesMetadata, error) {
		if source != "bangumi" {
			return nil, nil
		}
		return []*metadata.SeriesMetadata{
			{Title: "Alpha", SourceID: 101, SourceURL: "https://bgm.tv/subject/101", CoverURL: "https://lain.bgm.tv/hang.png"},
			{Title: "Alpha", SourceID: 102, SourceURL: "https://bgm.tv/subject/102", CoverURL: "https://lain.bgm.tv/fine.png"},
			{Title: "Alpha", SourceID: 103, SourceURL: "https://bgm.tv/subject/103", CoverURL: "https://lain.bgm.tv/also-fine.png"},
		}, nil
	}, host)
	world.web.prefetch("{}")
	agent := world.web.identificationAgent(world.cfg, 1, []int64{world.library.lib.ID}, []string{"read"})

	t.Run("local reads", func(t *testing.T) {
		snapshot := world.cfg.Snapshot()
		snapshot.Library.IOPolicy.ArchiveOpenConcurrency = 1
		world.cfg.Replace(&snapshot)
		pages := &mcp.CallToolParams{Name: "library_get_identification_images", Arguments: map[string]any{"series_id": world.series, "book_id": world.book, "pages": []int{1}}}
		assertImageRequestRelease(t, agent, world.web.process.Application.Engine, world.cfg.Snapshot(), world.archive, world.library.lib.ID, pages, world.web.server.CloseClientConnections)
	})
	for _, mode := range []string{"cancel", "disconnect"} {
		t.Run("cover download on "+mode, func(t *testing.T) {
			hold.Clear()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			returned := make(chan struct{})
			go func() {
				defer close(returned)
				_, _ = agent.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_identification_images", Arguments: map[string]any{"series_id": world.series, "candidate_ids": []string{"bangumi:101"}}})
			}()
			url := <-started
			if mode == "cancel" {
				cancel()
			} else {
				world.web.server.CloseClientConnections()
			}
			gone, _ := hold.LoadOrStore(url, make(chan struct{}))
			select {
			case <-gone.(chan struct{}):
			case <-time.After(5 * time.Second):
				t.Fatal("the download outlived its caller")
			}
			<-returned
		})
	}
	// Both download slots came back.
	call := world.images(agent, map[string]any{"series_id": world.series, "candidate_ids": []string{"bangumi:102", "bangumi:103"}})
	for _, image := range call.out.Images {
		if image.Status != "included" {
			t.Fatalf("after the callers left %+v", call.out.Images)
		}
	}
}
