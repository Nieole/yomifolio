package parser

import (
	"context"
	"errors"
	"io"

	"github.com/nwaples/rardecode/v2"
)

// ReadPageLimited reads only an exact selected entry, with a caller-owned smaller
// decompression budget. It never falls back to an ordinary unbounded page read.
func ReadPageLimited(ctx context.Context, arc Archive, name string, limit int64) ([]byte, error) {
	if limit < 0 || limit > maxPageUncompressedBytes {
		return nil, ErrMetadataTooLarge
	}
	read := func(r io.Reader, declared int64) ([]byte, error) {
		if declared > limit {
			return nil, ErrMetadataTooLarge
		}
		return readMetadataBytes(pageContextReader{ctx, r}, limit)
	}
	switch a := arc.(type) {
	case *ZipArchive:
		a.mu.RLock()
		defer a.mu.RUnlock()
		for _, f := range a.reader.File {
			if f.Name == name {
				if f.UncompressedSize64 > uint64(limit) {
					return nil, ErrMetadataTooLarge
				}
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return read(rc, int64(f.UncompressedSize64))
			}
		}
	case *RarArchive:
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.closed {
			return nil, ErrArchiveClosed
		}
		rr, err := rardecode.OpenReader(a.path)
		if err != nil {
			return nil, err
		}
		defer rr.Close()
		for {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			h, e := rr.Next()
			if e != nil {
				return nil, e
			}
			if !h.IsDir && h.Name == name {
				return read(rr, h.UnPackedSize)
			}
		}
	default:
		return nil, errors.New("unsupported bounded page reader")
	}
	return nil, errors.New("page not found")
}

type pageContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r pageContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
