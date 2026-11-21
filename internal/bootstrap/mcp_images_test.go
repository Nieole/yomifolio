package bootstrap_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"yomifolio/internal/config"
	"yomifolio/internal/parser"
	"yomifolio/internal/storageio"
	"yomifolio/internal/task"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/database"
)

func assertImageEvidence(t *testing.T, session *mcp.ClientSession, store database.Store, library, series int64, root, thumbnails string, wireChecks ...func(int64)) {
	t.Helper()
	ctx := context.Background()
	img := image.NewRGBA(image.Rect(0, 0, 800, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{R: 230, G: 20, B: 40, A: 255})
		}
	}
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "evidence.cbz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"001.png", "002.png", "003.png", "004.png"} {
		w, e := zw.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		data := pngBytes.Bytes()
		if name == "004.png" {
			blue := image.NewRGBA(image.Rect(0, 0, 800, 400))
			for i := 0; i < len(blue.Pix); i += 4 {
				blue.Pix[i+2] = 230
				blue.Pix[i+3] = 255
			}
			var encoded bytes.Buffer
			if e = png.Encode(&encoded, blue); e != nil {
				t.Fatal(e)
			}
			data = encoded.Bytes()
		}
		if _, e = w.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	bomb := append([]byte(nil), pngBytes.Bytes()...)
	binary.BigEndian.PutUint32(bomb[16:20], 20000)
	binary.BigEndian.PutUint32(bomb[20:24], 20000)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	noise := image.NewRGBA(image.Rect(0, 0, 384, 384))
	rng := rand.New(rand.NewSource(5))
	for i := 0; i < len(noise.Pix); i += 4 {
		noise.Pix[i] = byte(rng.Intn(256))
		noise.Pix[i+1] = byte(rng.Intn(256))
		noise.Pix[i+2] = byte(rng.Intn(256))
		noise.Pix[i+3] = 255
	}
	var noisy bytes.Buffer
	if err = png.Encode(&noisy, noise); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		name string
		data []byte
	}{{"005.png", bomb}, {"006.png", make([]byte, 16<<20|1)}, {"007.png", []byte("not an image")}, {"008.png", noisy.Bytes()}, {"009.png", noisy.Bytes()}, {"010.png", noisy.Bytes()}} {
		w, e := zw.Create(entry.name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(entry.data); e != nil {
			t.Fatal(e)
		}
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(thumbnails, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(thumbnails, "evidence.png"), pngBytes.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	row, err := store.DB().ExecContext(ctx, `INSERT INTO books(series_id,library_id,name,path,cover_path,size,file_modified_at) VALUES(?,?,'Evidence',?,'evidence.png',0,CURRENT_TIMESTAMP)`, series, library, path)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := row.LastInsertId()
	defer func() {
		_, e := store.DB().ExecContext(ctx, `DELETE FROM books WHERE id=?`, id)
		if e != nil {
			t.Error(e)
		}
	}()
	call := func(name string, args map[string]any, wantError bool) *mcp.CallToolResult {
		t.Helper()
		out, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil || out.IsError != wantError {
			t.Fatalf("%s: %+v %v", name, out, e)
		}
		raw, _ := json.Marshal(out)
		if len(raw) > 256<<10 {
			t.Fatalf("unbounded image result: %d", len(raw))
		}
		if bytes.Contains(raw, []byte(root)) {
			t.Fatal("image path leak")
		}
		return out
	}
	check := func(out *mcp.CallToolResult, n int) {
		t.Helper()
		count := 0
		for _, c := range out.Content {
			if v, ok := c.(*mcp.ImageContent); ok {
				count++
				if len(v.Data) > 96<<10 {
					t.Fatal("per-image byte limit exceeded")
				}
				if v.MIMEType != "image/jpeg" {
					t.Fatalf("MIME %s", v.MIMEType)
				}
				decoded, format, e := image.Decode(bytes.NewReader(v.Data))
				if e != nil || format != "jpeg" {
					t.Fatalf("not JPEG: %v %s", e, format)
				}
				if decoded.Bounds().Dx() != 384 || decoded.Bounds().Dy() != 192 {
					t.Fatalf("size %v", decoded.Bounds())
				}
				r, g, b, _ := decoded.At(100, 100).RGBA()
				if n == 2 && count == 2 {
					if b < 50000 || r > 10000 {
						t.Fatal("wrong requested page content")
					}
					continue
				}
				if r < 50000 || g > 10000 {
					t.Fatal("lost image content")
				}
			}
		}
		if count != n {
			t.Fatalf("images %d want %d", count, n)
		}
	}
	pagesResult := call("library_get_page_thumbnails", map[string]any{"series_id": series, "book_id": id, "pages": []int{2, 4}}, false)
	check(pagesResult, 2)
	var metadata struct {
		Images []struct {
			Page          int
			Width, Height int
		} `json:"images"`
	}
	raw, _ := json.Marshal(pagesResult.StructuredContent)
	if err = json.Unmarshal(raw, &metadata); err != nil || len(metadata.Images) != 2 || metadata.Images[0].Page != 2 || metadata.Images[1].Page != 4 || metadata.Images[0].Width != 384 || metadata.Images[0].Height != 192 {
		t.Fatalf("image metadata: %s %v", raw, err)
	}
	check(call("library_get_series_cover", map[string]any{"series_id": series}, false), 1)
	for _, pages := range [][]int{{}, {0}, {-1}, {11}, {1, 2, 3, 4}, {1, 1}} {
		call("library_get_page_thumbnails", map[string]any{"series_id": series, "book_id": id, "pages": pages}, true)
	}
	call("library_get_page_thumbnails", map[string]any{"series_id": series + 99999, "book_id": id, "pages": []int{1}}, true)
	call("library_get_page_thumbnails", map[string]any{"series_id": series, "book_id": 999999, "pages": []int{1}}, true)
	call("library_get_series_cover", map[string]any{"series_id": 999999}, true)
	for _, pages := range [][]int{{5}, {6}, {7}, {8, 9, 10}} {
		out := call("library_get_page_thumbnails", map[string]any{"series_id": series, "book_id": id, "pages": pages}, true)
		if pages[0] == 6 {
			raw, _ := json.Marshal(out)
			if !strings.Contains(string(raw), "budget") {
				t.Fatalf("missing actionable byte budget: %s", raw)
			}
		}
	}
	for _, check := range wireChecks {
		check(id)
	}
	if parser.ActiveArchiveBorrows(path) != 0 {
		t.Fatal("image request retained archive borrow")
	}
	other, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Image hidden", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: other.ID, Name: "Hidden images", Path: other.Path})
	if err != nil {
		t.Fatal(err)
	}
	call("library_get_series_cover", map[string]any{"series_id": hidden.ID}, true)
	// An authorized book identifier cannot bypass the explicitly selected series.
	call("library_get_page_thumbnails", map[string]any{"series_id": hidden.ID, "book_id": id, "pages": []int{1}}, true)
	result, err := store.DB().ExecContext(ctx, `INSERT INTO books(series_id,library_id,name,path,cover_path,size,file_modified_at) VALUES(?,?,'Hidden',?,'evidence.png',0,CURRENT_TIMESTAMP)`, hidden.ID, other.ID, filepath.Join(other.Path, "hidden.cbz"))
	if err != nil {
		t.Fatal(err)
	}
	hiddenID, _ := result.LastInsertId()
	call("library_get_page_thumbnails", map[string]any{"series_id": series, "book_id": hiddenID, "pages": []int{1}}, true)
	if _, err = store.DB().ExecContext(ctx, `UPDATE books SET cover_path='../outside.png' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	call("library_get_series_cover", map[string]any{"series_id": series}, true)

}

func assertMCPWireBudget(t *testing.T, client *http.Client, endpoint string, series, book int64) {
	t.Helper()
	// The final wire budget includes a legal but unusually long caller ID.
	request, err := http.NewRequest("POST", endpoint, strings.NewReader(fmt.Sprintf(`{"jsonrpc":"2.0","id":"%s","method":"tools/call","params":{"name":"library_get_page_thumbnails","arguments":{"series_id":%d,"book_id":%d,"pages":[8,9]}}}`, strings.Repeat("x", 63000), series, book)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("MCP-Protocol-Version", "2025-11-25")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > 256<<10 || response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("wire budget status=%d bytes=%d", response.StatusCode, len(body))
	}
}

func assertImageRequestRelease(t *testing.T, session *mcp.ClientSession, engine *task.Engine, cfg config.Config, path string, library, series, book int64, disconnect func()) {
	t.Helper()
	policy := config.ResolveStoragePolicy(cfg, path)
	limit := policy.IOPolicy.ArchiveOpenConcurrency
	if limit < 1 {
		t.Fatal("test needs active volume concurrency policy")
	}
	for _, mode := range []string{"cancel", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			waitCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			var leases []storageio.Lease
			for range limit {
				lease, err := storageio.Default.Acquire(waitCtx, storageio.Request{VolumeKey: policy.VolumeKey, Limit: limit, Kind: storageio.WorkKindReader})
				if err != nil {
					t.Fatal(err)
				}
				leases = append(leases, lease)
			}
			defer func() {
				for _, lease := range leases {
					lease.Release()
				}
			}()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			returned := make(chan struct{})
			go func() {
				defer close(returned)
				_, _ = session.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_page_thumbnails", Arguments: map[string]any{"series_id": series, "book_id": book, "pages": []int{1}}})
			}()
			waitReads := func(want bool) {
				t.Helper()
				ticker := time.NewTicker(5 * time.Millisecond)
				defer ticker.Stop()
				for {
					conflicts, err := engine.LibraryConflicts(waitCtx, library, 0)
					if err != nil {
						t.Fatal(err)
					}
					reads := false
					for _, c := range conflicts {
						if c.Reason == "wait_for_reads" {
							reads = true
						}
					}
					if reads == want {
						return
					}
					select {
					case <-waitCtx.Done():
						t.Fatalf("content read admission retained=%t wanted=%t", reads, want)
					case <-ticker.C:
					}
				}
			}
			// A real production image call holds admission while waiting on the existing IO gate.
			waitReads(true)
			if mode == "cancel" {
				cancel()
			} else {
				disconnect()
			}
			waitReads(false)
			select {
			case <-returned:
			case <-waitCtx.Done():
				t.Fatal("image transport request did not return")
			}
			if parser.ActiveArchiveBorrows(path) != 0 {
				t.Fatal("disconnected image call retained archive borrow")
			}
		})
	}
}
