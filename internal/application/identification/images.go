package identification

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/bookcontent"
	"yomifolio/internal/metadata"
)

// Image kinds.
const (
	ImageCover     = "cover"
	ImagePage      = "page"
	ImageCandidate = "candidate"
)

// Image statuses. Only an included image carries bytes; every other status says why it was left
// out, and leaving one out never hides the others.
const (
	ImageIncluded = "included"
	// ImageOverBudget: the image was ready but did not fit the result budget; ask for it alone.
	ImageOverBudget = "over_budget"
	// ImageNoCover: the series has no cover, or the source gave the candidate none.
	ImageNoCover = "no_cover"
	// ImagePageNotFound: the book has no such page; pages are one-based.
	ImagePageNotFound = "page_not_found"
	// ImageNotInEvidence: the series' evidence packet holds no such candidate.
	ImageNotInEvidence = "not_in_evidence"
	// ImageHostRejected: the cover address, or a redirect, leaves the source's own image hosts.
	ImageHostRejected = "host_rejected"
	// ImageTooLarge: the source file or its thumbnail exceeds the image limits.
	ImageTooLarge = "too_large"
	// ImageNotImage: the content is not a decodable image.
	ImageNotImage = "not_image"
	// ImageUnavailable: reading or downloading the image failed.
	ImageUnavailable = "unavailable"
)

// ImagesInput selects the images of one series to look at; at least one must be asked for.
type ImagesInput struct {
	SeriesID int64   `json:"series_id" jsonschema:"series to look at"`
	Cover    bool    `json:"cover,omitempty" jsonschema:"include the series' own cover"`
	BookID   int64   `json:"book_id,omitempty" jsonschema:"book of the series whose pages to include"`
	Pages    []int64 `json:"pages,omitempty" jsonschema:"1-3 distinct one-based pages of book_id"`
	// CandidateIDs name candidates of the series' evidence packet, never addresses.
	CandidateIDs []string `json:"candidate_ids,omitempty" jsonschema:"up to 3 distinct candidate ids of the series' evidence packet, like bangumi:101"`
}

// Most pages, and most candidate covers, one call may ask for.
const (
	maxImagePages      = 3
	maxImageCandidates = 3
	candidateIDBytes   = 128
)

func (in ImagesInput) valid() bool {
	if (in.BookID != 0) != (len(in.Pages) > 0) || len(in.Pages) > maxImagePages || len(in.CandidateIDs) > maxImageCandidates {
		return false
	}
	for i, page := range in.Pages {
		if page < 1 || slices.Contains(in.Pages[:i], page) {
			return false
		}
	}
	for i, id := range in.CandidateIDs {
		if id == "" || len(id) > candidateIDBytes || slices.Contains(in.CandidateIDs[:i], id) {
			return false
		}
	}
	return in.Cover || len(in.Pages) > 0 || len(in.CandidateIDs) > 0
}

// SeriesImage is one requested image. Data is the JPEG thumbnail of an included image; it never
// enters JSON.
type SeriesImage struct {
	Kind        string `json:"kind"`
	BookID      int64  `json:"book_id,omitempty"`
	Page        int64  `json:"page,omitempty"`
	CandidateID string `json:"candidate_id,omitempty"`
	Status      string `json:"status"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Data        []byte `json:"-"`
}

// ImageSet lists the requested images of a series in request order: cover, pages, candidates.
type ImageSet struct {
	SeriesID int64         `json:"series_id"`
	Images   []SeriesImage `json:"images"`
}

// ImageAccess reads a series' own images under delegated authority and the content admission
// rules of the library.
type ImageAccess interface {
	GetSeries(context.Context, delegated.Subject, int64) (delegated.Series, error)
	Authorize(context.Context, delegated.Subject, int64, string) error
	SeriesCover(context.Context, delegated.Subject, int64) (delegated.ImageEvidence, error)
	PageThumbnails(context.Context, delegated.Subject, delegated.PageThumbnailsInput) (delegated.ImageEvidence, error)
}

var errNoImages = errors.New("identification images are not assembled")

type imageSources struct {
	local  ImageAccess
	covers *coverFetcher
}

// WithImages lets agents look at a series' own images and its candidates' covers. covers carries
// the candidate cover downloads; nil uses the default transport.
func (s *Service) WithImages(local ImageAccess, covers http.RoundTripper) *Service {
	s.images = &imageSources{local: local, covers: newCoverFetcher(covers)}
	return s
}

// Images reads the requested images of one series the credential may read. A denial, invalid
// selection or cancellation fails the call; any other trouble with one image is its status.
func (s *Service) Images(ctx context.Context, subject delegated.Subject, in ImagesInput) (ImageSet, error) {
	if s.images == nil {
		return ImageSet{}, errNoImages
	}
	if !in.valid() {
		return ImageSet{}, delegated.ErrInvalid
	}
	series, err := s.images.local.GetSeries(ctx, subject, in.SeriesID)
	if err != nil {
		return ImageSet{}, err
	}
	out := ImageSet{SeriesID: series.ID, Images: []SeriesImage{}}
	if in.Cover {
		evidence, err := s.images.local.SeriesCover(ctx, subject, series.ID)
		image := SeriesImage{Kind: ImageCover, BookID: evidence.BookID}
		if err = localStatus(ctx, &image, evidence, err); err != nil {
			return ImageSet{}, err
		}
		out.Images = append(out.Images, image)
	}
	// One page at a time, so a page outside the book leaves the others readable.
	for _, page := range in.Pages {
		evidence, err := s.images.local.PageThumbnails(ctx, subject, delegated.PageThumbnailsInput{SeriesID: series.ID, BookID: in.BookID, Pages: []int64{page}})
		image := SeriesImage{Kind: ImagePage, BookID: in.BookID, Page: page}
		if err = localStatus(ctx, &image, evidence, err); err != nil {
			return ImageSet{}, err
		}
		out.Images = append(out.Images, image)
	}
	candidates, err := s.candidateImages(ctx, series.ID, in.CandidateIDs)
	if err != nil {
		return ImageSet{}, err
	}
	out.Images = append(out.Images, candidates...)
	// Re-check after the reads, so a grant withdrawn meanwhile sees nothing.
	if err := s.images.local.Authorize(ctx, subject, series.LibraryID, delegated.Read); err != nil {
		return ImageSet{}, err
	}
	return fitImages(out), nil
}

// localStatus fills one local image from what the content read returned. It returns the errors
// that must fail the whole call.
func localStatus(ctx context.Context, image *SeriesImage, evidence delegated.ImageEvidence, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	switch {
	case err == nil && len(evidence.Images) == 1:
		thumb := evidence.Images[0]
		image.Status, image.Width, image.Height, image.Data = ImageIncluded, thumb.Width, thumb.Height, thumb.Data
	case errors.Is(err, delegated.ErrDenied):
		return err
	case errors.Is(err, sql.ErrNoRows):
		image.Status = ImageNoCover
	case errors.Is(err, bookcontent.ErrPageNotFound):
		image.Status = ImagePageNotFound
	case errors.Is(err, bookcontent.ErrEvidenceLimit):
		image.Status = ImageTooLarge
	default:
		image.Status = ImageUnavailable
	}
	return nil
}

// imageReserve is left for the text block's framing, the protocol envelope and the caller's
// request ID.
const imageReserve = 4 << 10

// fitImages keeps, in request order, every included image that still fits the result budget and
// marks the rest over budget, so a later smaller image may still fit. The measure is the whole
// result: each kept image as a base64 block plus the metadata twice, as structured content and as
// its JSON text copy.
func fitImages(set ImageSet) ImageSet {
	fitted := ImageSet{SeriesID: set.SeriesID, Images: make([]SeriesImage, 0, len(set.Images))}
	blocks := 0
	size := func() int {
		raw, _ := json.Marshal(fitted)
		return 2*len(raw) + blocks + imageReserve
	}
	for _, image := range set.Images {
		fitted.Images = append(fitted.Images, image)
		if image.Status != ImageIncluded {
			continue
		}
		block := base64.StdEncoding.EncodedLen(len(image.Data)) + 64
		if blocks += block; size() > delegated.MaxImageResultBytes {
			blocks -= block
			fitted.Images[len(fitted.Images)-1] = SeriesImage{Kind: image.Kind, BookID: image.BookID, Page: image.Page, CandidateID: image.CandidateID, Status: ImageOverBudget}
		}
	}
	return fitted
}

// candidateImages downloads the covers of the named candidates of the series' evidence packet,
// concurrently within the process-wide download slots.
func (s *Service) candidateImages(ctx context.Context, seriesID int64, ids []string) ([]SeriesImage, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	covers, err := candidateCovers(ctx, s.db, seriesID)
	if err != nil {
		return nil, err
	}
	out := make([]SeriesImage, len(ids))
	errs := make([]error, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		out[i] = SeriesImage{Kind: ImageCandidate, CandidateID: id}
		address, known := covers[id]
		if !known {
			out[i].Status = ImageNotInEvidence
			continue
		}
		source, _, _ := strings.Cut(id, ":")
		wg.Go(func() { errs[i] = s.images.covers.fetch(ctx, source, address, &out[i]) })
	}
	wg.Wait()
	return out, errors.Join(errs...)
}

// candidateCovers maps the candidates of the series' evidence packet to the cover address their
// source gave, leaving out the candidates excluded since the packet was fetched.
func candidateCovers(ctx context.Context, q queryer, seriesID int64) (map[string]string, error) {
	out := map[string]string{}
	var raw string
	err := q.QueryRowContext(ctx, `SELECT source_facts FROM identification_evidence WHERE series_id = ?`, seriesID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var facts map[string]metadata.SeriesMetadata
	if err := json.Unmarshal([]byte(raw), &facts); err != nil {
		return nil, err
	}
	excluded, err := excludedIDs(ctx, q, seriesID)
	if err != nil {
		return nil, err
	}
	for id, fact := range facts {
		if !excluded[id] {
			out[id] = fact.CoverURL
		}
	}
	return out, nil
}
