package parser

import (
	"errors"
	"io"

	"github.com/nwaples/rardecode/v2"
)

// ErrMetadataTooLarge indicates a metadata entry exceeds the caller's byte budget.
var ErrMetadataTooLarge = errors.New("metadata exceeds byte limit")

// ReadMetadataLimited applies a smaller metadata budget before allocating or decompressing.
// It preserves the format's metadata selection semantics and leaves ordinary reads unchanged.
func ReadMetadataLimited(arc Archive, name string, limit int64) ([]byte, error) {
	if limit < 0 || limit > maxPageUncompressedBytes {
		return nil, ErrMetadataTooLarge
	}
	switch a := arc.(type) {
	case *ZipArchive:
		a.mu.RLock()
		defer a.mu.RUnlock()
		f := pickZipMetadataEntry(a.reader.File, name)
		if f == nil {
			return nil, ErrMetadataNotFound
		}
		if f.UncompressedSize64 > uint64(limit) {
			return nil, ErrMetadataTooLarge
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return readMetadataBytes(rc, limit)
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
			header, err := rr.Next()
			if err == io.EOF {
				return nil, ErrMetadataNotFound
			}
			if err != nil {
				return nil, err
			}
			if header.IsDir || !matchesArchiveEntry(header.Name, name, true) {
				continue
			}
			if header.UnPackedSize > limit {
				return nil, ErrMetadataTooLarge
			}
			return readMetadataBytes(rr, limit)
		}
	default:
		return nil, errors.New("unsupported metadata reader")
	}
}
func readMetadataBytes(r io.Reader, limit int64) ([]byte, error) {
	if limit < 0 || limit > maxPageUncompressedBytes {
		return nil, ErrMetadataTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrMetadataTooLarge
	}
	return data, nil
}
