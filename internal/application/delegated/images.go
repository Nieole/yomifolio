package delegated

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"

	"yomifolio/internal/bookcontent"
)

const MaxImageResultBytes = 256 << 10

type PageThumbnailsInput struct {
	SeriesID int64   `json:"series_id"`
	BookID   int64   `json:"book_id"`
	Pages    []int64 `json:"pages"`
}
type ImageEvidence struct {
	SeriesID int64                   `json:"series_id"`
	BookID   int64                   `json:"book_id"`
	Images   []bookcontent.Thumbnail `json:"images"`
}

type coverStore interface {
	SeriesCoverBook(context.Context, int64) (int64, error)
}

// SeriesCover reads the same first persisted book cover selected by the catalogue.
func (s *Service) SeriesCover(ctx context.Context, subject Subject, id int64) (ImageEvidence, error) {
	series, err := s.GetSeries(ctx, subject, id)
	if err != nil {
		return ImageEvidence{}, err
	}
	store, ok := s.store.(coverStore)
	if !ok {
		return ImageEvidence{}, ErrDenied
	}
	book, err := store.SeriesCoverBook(ctx, id)
	if err != nil {
		return ImageEvidence{}, err
	}
	return s.imageEvidence(ctx, subject, series, book, nil, true)
}
func (s *Service) PageThumbnails(ctx context.Context, subject Subject, in PageThumbnailsInput) (ImageEvidence, error) {
	if len(in.Pages) < 1 || len(in.Pages) > 3 {
		return ImageEvidence{}, ErrInvalid
	}
	for i, n := range in.Pages {
		if n < 1 || slices.Contains(in.Pages[:i], n) {
			return ImageEvidence{}, ErrInvalid
		}
	}
	series, err := s.GetSeries(ctx, subject, in.SeriesID)
	if err != nil {
		return ImageEvidence{}, err
	}
	return s.imageEvidence(ctx, subject, series, in.BookID, in.Pages, false)
}
func (s *Service) imageEvidence(ctx context.Context, subject Subject, series Series, id int64, pages []int64, cover bool) (ImageEvidence, error) {
	var out ImageEvidence
	store, ok := s.store.(discoveryStore)
	if !ok || s.content == nil || s.access == nil {
		return out, ErrDenied
	}
	book, err := store.Book(ctx, id)
	if err != nil || book.SeriesID != series.ID || book.LibraryID != series.LibraryID {
		return out, ErrDenied
	}
	if err = s.Authorize(ctx, subject, book.LibraryID, Read); err != nil {
		return out, err
	}
	thumbs, err := s.content.Evidence(ctx, id, book.LibraryID, pages, cover, s.access)
	if err != nil {
		return out, err
	}
	if err = s.Authorize(ctx, subject, book.LibraryID, Read); err != nil {
		return out, err
	}
	out = ImageEvidence{SeriesID: series.ID, BookID: id, Images: thumbs}
	// Account for base64 and JSON escaping, not merely compressed image bytes.
	// This conservative complete result surrogate includes metadata, image blocks,
	// and a fixed 4 KiB reserve for text and protocol framing (no caller strings).
	blocks := make([]map[string]string, 0, len(thumbs))
	for _, v := range thumbs {
		blocks = append(blocks, map[string]string{"type": "image", "mimeType": "image/jpeg", "data": base64.StdEncoding.EncodeToString(v.Data)})
	}
	raw, err := json.Marshal(struct {
		Content    any `json:"content"`
		Structured any `json:"structuredContent"`
	}{blocks, out})
	if err != nil || len(raw)+4096 > MaxImageResultBytes {
		return ImageEvidence{}, ErrInvalid
	}
	return out, nil
}
