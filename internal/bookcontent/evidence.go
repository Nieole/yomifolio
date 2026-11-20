package bookcontent

import (
	"context"
	"errors"
	"io"
	"os"

	"yomifolio/internal/config"
	"yomifolio/internal/images"
	"yomifolio/internal/parser"
	"yomifolio/internal/storageio"
)

const (
	EvidenceEdge        = images.EvidenceEdge
	EvidenceSourceBytes = images.EvidenceSourceBytes
	EvidenceImageBytes  = images.EvidenceImageBytes
	EvidencePixels      = images.EvidencePixels
)

var ErrEvidenceLimit = images.ErrEvidenceLimit

// Two concurrent evidence requests bound retained input/decode memory independently
// of archive safety limits and CPU count. Waiting requests retain no input buffers.
var evidenceSlots = make(chan struct{}, 2)

type Thumbnail struct {
	Page   int64  `json:"page,omitempty"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Data   []byte `json:"-"`
}

// Evidence materializes strict JPEG thumbnails under current library admission.
// Cover selects the persisted cover cache, not page one; every borrow is returned
// before this call returns, including cancellation and decoding failures.
func (s *Service) Evidence(ctx context.Context, id, libraryID int64, pages []int64, cover bool, access LibraryAccess) ([]Thumbnail, error) {
	if (!cover && (len(pages) < 1 || len(pages) > 3)) || (cover && len(pages) != 0) {
		return nil, ErrEvidenceLimit
	}
	select {
	case evidenceSlots <- struct{}{}:
		defer func() { <-evidenceSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		version := access.LibraryAccessVersion()
		source, err := s.lookup(ctx, id)
		if err != nil || source.LibraryID != libraryID {
			return nil, ErrSourceMissing
		}
		release, err := access.AcquireLibraryRead(libraryID)
		if err != nil {
			return nil, ErrBusy
		}
		if version != access.LibraryAccessVersion() {
			release()
			continue
		}
		defer release()
		policy := config.ResolveStoragePolicy(s.config(), source.Path)
		lease, err := storageio.Default.Acquire(ctx, storageio.Request{VolumeKey: policy.VolumeKey, Limit: policy.IOPolicy.ArchiveOpenConcurrency, Kind: storageio.WorkKindReader, PauseWhenReading: policy.IOPolicy.PauseBackgroundWhenReading})
		if err != nil {
			return nil, err
		}
		defer lease.Release()
		if cover {
			if source.CoverPath == "" {
				return nil, ErrSourceMissing
			}
			// The server-owned cache key cannot escape the configured cache, even via symlinks.
			root, err := os.OpenRoot(config.ThumbnailDir(s.config()))
			if err != nil {
				return nil, err
			}
			defer root.Close()
			file, err := root.Open(source.CoverPath)
			if err != nil {
				return nil, err
			}
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, EvidenceSourceBytes+1))
			if err != nil {
				return nil, err
			}
			thumb, err := evidenceThumbnail(ctx, data)
			if err != nil {
				return nil, err
			}
			return []Thumbnail{thumb}, nil
		}
		arc, done, err := s.reader.borrow(source.Path)
		if err != nil {
			return nil, err
		}
		defer done()
		manifest, err := arc.GetPages()
		if err != nil {
			return nil, err
		}
		for _, n := range pages {
			if n < 1 || n > int64(len(manifest)) {
				return nil, ErrPageNotFound
			}
		}
		out := make([]Thumbnail, 0, len(pages))
		for _, n := range pages {
			raw, err := parser.ReadPageLimited(ctx, arc, manifest[n-1].Name, EvidenceSourceBytes)
			if err != nil {
				if errors.Is(err, parser.ErrMetadataTooLarge) {
					return nil, ErrEvidenceLimit
				}
				return nil, err
			}
			thumb, err := evidenceThumbnail(ctx, raw)
			if err != nil {
				return nil, err
			}
			thumb.Page = n
			out = append(out, thumb)
		}
		return out, nil
	}
}

func evidenceThumbnail(ctx context.Context, data []byte) (Thumbnail, error) {
	out, err := images.EvidenceThumbnail(ctx, data)
	return Thumbnail{Width: out.Width, Height: out.Height, Data: out.Data}, err
}
