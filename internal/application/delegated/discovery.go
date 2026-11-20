package delegated

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"yomifolio/internal/bookcontent"
)

// MaxDiscoveryBytes leaves protocol envelope headroom under a 256 KiB tool result.
const MaxDiscoveryBytes = 240 << 10

type FindSeriesInput struct {
	LibraryIDs    []int64  `json:"library_ids,omitempty"`
	SeriesIDs     []int64  `json:"series_ids,omitempty"`
	AddedFrom     string   `json:"added_from,omitempty"`
	AddedBefore   string   `json:"added_before,omitempty"`
	MissingFields []string `json:"missing_fields,omitempty"`
	AfterID       int64    `json:"after_id,omitempty"`
	Limit         int      `json:"limit,omitempty"`
}
type SeriesPage struct {
	Items       []Series `json:"items"`
	NextAfterID int64    `json:"next_after_id,omitempty"`
}
type BooksInput struct {
	SeriesID int64 `json:"series_id"`
	AfterID  int64 `json:"after_id,omitempty"`
	Limit    int   `json:"limit,omitempty"`
}
type Book struct {
	ID            int64  `json:"id"`
	SeriesID      int64  `json:"series_id"`
	LibraryID     int64  `json:"library_id"`
	Name          string `json:"name"`
	Filename      string `json:"filename"`
	DirectoryName string `json:"directory_name"`
	Title         string `json:"title"`
	Number        string `json:"number"`
	Volume        string `json:"volume"`
	PageCount     int64  `json:"page_count"`
}
type BooksPage struct {
	Items       []Book `json:"items"`
	NextAfterID int64  `json:"next_after_id,omitempty"`
}
type discoveryStore interface {
	FindSeries(context.Context, FindSeriesInput) ([]Series, error)
	Books(context.Context, BooksInput) ([]Book, error)
	Book(context.Context, int64) (Book, error)
}

// NewWithDiscovery binds identity-based content access; no paths reach the adapter.
func NewWithDiscovery(store Store, content *bookcontent.Service, access bookcontent.LibraryAccess) *Service {
	return &Service{store: store, content: content, access: access}
}
func pageLimit(limit int, after int64) (int, error) {
	if limit < 0 || limit > 50 || after < 0 {
		return 0, ErrInvalid
	}
	if limit == 0 {
		limit = 20
	}
	return limit, nil
}
func bounded(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(raw) > MaxDiscoveryBytes {
		return ErrInvalid
	}
	return nil
}
func (s *Service) FindSeries(ctx context.Context, subject Subject, in FindSeriesInput) (SeriesPage, error) {
	var out SeriesPage
	limit, err := pageLimit(in.Limit, in.AfterID)
	if err != nil {
		return out, err
	}
	if len(in.LibraryIDs) > 256 || len(in.SeriesIDs) > 50 || len(in.MissingFields) > 10 {
		return out, ErrInvalid
	}
	var from, before time.Time
	for _, v := range []struct {
		raw    string
		target *time.Time
	}{{in.AddedFrom, &from}, {in.AddedBefore, &before}} {
		if v.raw != "" {
			*v.target, err = time.Parse(time.RFC3339, v.raw)
			if err != nil {
				return out, ErrInvalid
			}
		}
	}
	if in.AddedFrom != "" && in.AddedBefore != "" && !from.Before(before) {
		return out, ErrInvalid
	}
	for _, f := range in.MissingFields {
		if !slices.Contains([]string{"title", "summary", "publisher", "status", "language", "rating", "authors", "tags", "source_links"}, f) {
			return out, ErrInvalid
		}
	}
	authority, err := s.store.Authority(ctx, subject)
	if err != nil {
		return out, ErrDenied
	}
	if len(in.LibraryIDs) == 0 {
		in.LibraryIDs = slices.Clone(authority.Credential.LibraryIDs)
	}
	for _, id := range in.LibraryIDs {
		if id <= 0 {
			return out, ErrInvalid
		}
		if err = s.Authorize(ctx, subject, id, Read); err != nil {
			return out, err
		}
	}
	// Validate every explicit target, even when other filters would exclude it.
	for _, id := range in.SeriesIDs {
		if id <= 0 {
			return out, ErrInvalid
		}
		if _, err = s.GetSeries(ctx, subject, id); err != nil {
			return out, err
		}
	}
	if authority.Credential.Revoked || authority.MustChangePassword || !slices.Contains(authority.Credential.Capabilities, Read) {
		return out, ErrDenied
	}
	in.Limit = limit + 1
	store, ok := s.store.(discoveryStore)
	if !ok {
		return out, ErrDenied
	}
	items, err := store.FindSeries(ctx, in)
	if err != nil {
		return out, err
	}
	out.Items = items
	if len(items) > limit {
		out.Items = items[:limit]
		out.NextAfterID = out.Items[limit-1].ID
	}
	for _, v := range out.Items {
		if err = s.Authorize(ctx, subject, v.LibraryID, Read); err != nil {
			return SeriesPage{}, err
		}
	}
	return out, bounded(out)
}
func (s *Service) ListBooks(ctx context.Context, subject Subject, in BooksInput) (BooksPage, error) {
	var out BooksPage
	limit, err := pageLimit(in.Limit, in.AfterID)
	if err != nil {
		return out, err
	}
	if _, err = s.GetSeries(ctx, subject, in.SeriesID); err != nil {
		return out, err
	}
	store, ok := s.store.(discoveryStore)
	if !ok {
		return out, ErrDenied
	}
	in.Limit = limit + 1
	items, err := store.Books(ctx, in)
	if err != nil {
		return out, err
	}
	out.Items = items
	if len(items) > limit {
		out.Items = items[:limit]
		out.NextAfterID = out.Items[limit-1].ID
	}
	for _, v := range out.Items {
		if err = s.Authorize(ctx, subject, v.LibraryID, Read); err != nil {
			return BooksPage{}, err
		}
	}
	return out, bounded(out)
}

type BookClues struct {
	Book      Book                      `json:"book"`
	ComicInfo bookcontent.ComicInfoClue `json:"comic_info"`
}

func (s *Service) GetBookClues(ctx context.Context, subject Subject, id int64) (BookClues, error) {
	var out BookClues
	store, ok := s.store.(discoveryStore)
	if !ok {
		return out, ErrDenied
	}
	book, err := store.Book(ctx, id)
	if err != nil {
		return out, ErrDenied
	}
	if err = s.Authorize(ctx, subject, book.LibraryID, Read); err != nil {
		return out, err
	}
	if _, err = s.GetSeries(ctx, subject, book.SeriesID); err != nil {
		return out, err
	}
	if s.content == nil || s.access == nil {
		return out, ErrDenied
	}
	clue, err := s.content.ComicInfo(ctx, id, book.LibraryID, s.access)
	if err != nil {
		return out, err
	}
	if err = s.Authorize(ctx, subject, book.LibraryID, Read); err != nil {
		return out, err
	}
	out = BookClues{Book: book, ComicInfo: clue}
	return out, bounded(out)
}
