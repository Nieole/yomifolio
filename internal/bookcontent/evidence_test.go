package bookcontent_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"yomifolio/internal/bookcontent"
	"yomifolio/internal/config"
	"yomifolio/internal/parser"
)

type cancellingAccess struct {
	bookcontent.LibraryAccess
	cancel context.CancelFunc
	active int
}

func (a *cancellingAccess) AcquireLibraryRead(id int64) (func(), error) {
	release, err := a.LibraryAccess.AcquireLibraryRead(id)
	if err != nil {
		return nil, err
	}
	a.active++
	if a.cancel != nil {
		a.cancel()
	}
	return func() { a.active--; release() }, nil
}

func TestEvidenceReturnsAdmissionAndBorrowOnEveryExit(t *testing.T) {
	for _, kind := range []string{"success", "range", "cancelled", "disconnect", "missing", "wrong-library", "invalid-image", "cover-success", "cover-missing"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			path := filepath.Join(t.TempDir(), "book.cbz")
			pngBytes := archive(t, path, 180)
			cfg := config.Config{}
			cfg.Cache.Dir = t.TempDir()
			if err := os.MkdirAll(config.ThumbnailDir(cfg), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(config.ThumbnailDir(cfg), "cover.png"), pngBytes, 0600); err != nil {
				t.Fatal(err)
			}
			source := bookcontent.Source{ID: 1, LibraryID: 7, Path: path, CoverPath: "cover.png"}
			if kind == "missing" {
				source.Path += ".gone"
			}
			if kind == "cover-missing" {
				source.CoverPath = "missing.png"
			}
			if kind == "invalid-image" {
				source.CoverPath = "bad.png"
				if err := os.WriteFile(filepath.Join(config.ThumbnailDir(cfg), "bad.png"), []byte("bad"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			engine := contentAccess(t)
			access := &cancellingAccess{LibraryAccess: engine}
			if kind == "cancelled" {
				cancel()
			}
			if kind == "disconnect" {
				access.cancel = cancel
			}
			service := bookcontent.New(func(context.Context, int64) (bookcontent.Source, error) { return source, nil }, func() config.Config { return cfg }, nil, 1, 1)
			library := int64(7)
			if kind == "wrong-library" {
				library = 8
			}
			pages := []int64{1}
			if kind == "range" {
				pages = []int64{2}
			}
			cover := kind == "invalid-image" || kind == "cover-success" || kind == "cover-missing"
			if cover {
				pages = nil
			}
			out, err := service.Evidence(ctx, 1, library, pages, cover, access)
			success := kind == "success" || kind == "cover-success"
			if (err == nil) != success {
				t.Fatalf("result %v %v", out, err)
			}
			if kind == "cancelled" || kind == "disconnect" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation: %v", err)
				}
			}
			if success && (len(out) != 1 || out[0].Width != 16 || out[0].Height != 16) {
				t.Fatalf("no-upscale result: %+v", out)
			}
			if access.active != 0 || parser.ActiveArchiveBorrows(path) != 0 {
				t.Fatal("retained content access")
			}
			release, err := engine.AcquireLibraryExclusive(context.Background(), 7)
			if err != nil {
				t.Fatalf("retained library admission: %v", err)
			}
			release()
			parser.EvictArchiveFromPool(path)
			if err = os.Rename(path, path+".moved"); err != nil {
				t.Fatalf("archive handle retained after eviction: %v", err)
			}
		})
	}
}
