package parser

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nwaples/rardecode/v2"
)

// BoundedPageDictionaryBytes limits evidence-only RAR decoder dictionaries.
// Ordinary reading and scanning retain the dependency's default.
const BoundedPageDictionaryBytes int64 = 16 << 20

var openBoundedRarReader = rardecode.OpenReader

// boundedRar creates the reader with limits before even parsing initial headers.
// Wrapping every volume also observes cancellation during initialization and IO.
func boundedRar(ctx context.Context, path string) (*rardecode.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rr, err := openBoundedRarReader(path,
		rardecode.MaxDictionarySize(BoundedPageDictionaryBytes),
		rardecode.FileSystem(pageContextFS{ctx}),
	)
	return rr, pageBudgetError(err)
}

func pageBudgetError(err error) error {
	if errors.Is(err, rardecode.ErrDictionaryTooLarge) {
		return ErrMetadataTooLarge
	}
	return err
}

type pageContextFS struct{ ctx context.Context }

func (f pageContextFS) Open(name string) (fs.File, error) {
	if err := f.ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	return pageContextFile{File: file, ctx: f.ctx}, nil
}

type pageContextFile struct {
	fs.File
	ctx context.Context
}

func (f pageContextFile) Read(p []byte) (int, error) {
	return (pageContextReader{f.ctx, f.File}).Read(p)
}

// skipBoundedPage drains explicitly rather than allowing Next to invisibly decode
// solid predecessors. Each skip has the same byte ceiling and cancellation checks
// as a selected entry, and retains no decompressed input buffer.
func skipBoundedPage(ctx context.Context, rr io.Reader, declared, limit int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if declared > limit {
		return ErrMetadataTooLarge
	}
	n, err := io.Copy(io.Discard, io.LimitReader(pageContextReader{ctx, rr}, limit+1))
	if err != nil {
		return pageBudgetError(err)
	}
	if n > limit {
		return ErrMetadataTooLarge
	}
	return nil
}

// GetPagesLimited enumerates evidence pages with fixed decoder/skip budgets and
// cancellation. It neither changes nor populates the ordinary reading session.
func GetPagesLimited(ctx context.Context, arc Archive) ([]PageMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var pages []PageMetadata
	add := func(name string, size int64) {
		if strings.HasPrefix(filepath.Base(name), ".") {
			return
		}
		ext := strings.ToLower(filepath.Ext(name))
		if getMediaType(ext) != "application/octet-stream" {
			pages = append(pages, PageMetadata{Name: name, Size: size, MediaType: getMediaType(ext)})
		}
	}
	switch a := arc.(type) {
	case *ZipArchive:
		a.mu.RLock()
		defer a.mu.RUnlock()
		for _, f := range a.reader.File {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !f.FileInfo().IsDir() {
				add(f.Name, f.FileInfo().Size())
			}
		}
	case *RarArchive:
		if err := a.mu.lockContext(ctx); err != nil {
			return nil, err
		}
		defer a.mu.Unlock()
		if a.closed {
			return nil, ErrArchiveClosed
		}
		rr, err := boundedRar(ctx, a.path)
		if err != nil {
			return nil, err
		}
		defer rr.Close()
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			h, err := rr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, pageBudgetError(err)
			}
			if !h.IsDir {
				add(h.Name, h.UnPackedSize)
			}
			if err := skipBoundedPage(ctx, rr, h.UnPackedSize, BoundedPageDictionaryBytes); err != nil {
				return nil, err
			}
		}
	default:
		return nil, errors.New("unsupported bounded page reader")
	}
	sort.Slice(pages, func(i, j int) bool { return naturalCompare(pages[i].Name, pages[j].Name) })
	return pages, ctx.Err()
}
