package identification

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"yomifolio/internal/images"
)

// Bounds of one candidate cover download.
const (
	coverTimeout   = 10 * time.Second
	coverRedirects = 3
	// coverDownloads caps the downloads, and so the decodes they feed, across the process.
	coverDownloads = 2
)

// coverHosts are the image hosts each source serves its covers from. A host matches itself and
// its subdomains; a candidate's cover is fetched only from its own source's hosts.
var coverHosts = map[string][]string{
	Bangumi:     {"bgm.tv", "bangumi.tv", "chii.in"},
	AniList:     {"anilist.co"},
	MangaDex:    {"mangadex.org"},
	MyAnimeList: {"myanimelist.net"},
	ComicVine:   {"comicvine.gamespot.com"},
}

// coverURL is the HTTPS address of a source's cover when it lies on one of the source's hosts; a
// plain-HTTP address on such a host is upgraded. It reports false for every other address.
func coverURL(source, raw string) (*url.URL, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Opaque != "" {
		return nil, false
	}
	switch u.Scheme {
	case "https":
	case "http":
		if u.Port() != "" {
			return nil, false
		}
		u.Scheme = "https"
	default:
		return nil, false
	}
	return u, coverHost(source, u)
}

func coverHost(source string, u *url.URL) bool {
	if u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	for _, known := range coverHosts[source] {
		if host == known || strings.HasSuffix(host, "."+known) {
			return true
		}
	}
	return false
}

var errCoverRedirect = errors.New("cover redirect leaves the source's hosts")

// coverFetcher downloads candidate covers and shrinks them to evidence thumbnails.
type coverFetcher struct {
	transport http.RoundTripper
	slots     chan struct{}
}

func newCoverFetcher(transport http.RoundTripper) *coverFetcher {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &coverFetcher{transport: transport, slots: make(chan struct{}, coverDownloads)}
}

// fetch fills one candidate image from the source's cover address. It returns an error only when
// ctx ended; every other failure is the image's status.
func (f *coverFetcher) fetch(ctx context.Context, source, raw string, image *SeriesImage) error {
	if strings.TrimSpace(raw) == "" {
		image.Status = ImageNoCover
		return nil
	}
	target, ok := coverURL(source, raw)
	if !ok {
		image.Status = ImageHostRejected
		return nil
	}
	select {
	case f.slots <- struct{}{}:
		defer func() { <-f.slots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	thumb, status := f.download(ctx, source, target)
	if err := ctx.Err(); err != nil {
		return err
	}
	image.Status = status
	if status == ImageIncluded {
		image.Width, image.Height, image.Data = thumb.Width, thumb.Height, thumb.Data
	} else {
		slog.InfoContext(ctx, "Candidate cover left out", "source", source, "host", target.Hostname(), "status", status)
	}
	return nil
}

func (f *coverFetcher) download(ctx context.Context, source string, target *url.URL) (images.EvidenceImage, string) {
	ctx, cancel := context.WithTimeout(ctx, coverTimeout)
	defer cancel()
	client := &http.Client{Transport: f.transport, CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if !coverHost(source, next.URL) {
			return errCoverRedirect
		}
		if len(via) > coverRedirects {
			return errors.New("too many cover redirects")
		}
		return nil
	}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return images.EvidenceImage{}, ImageUnavailable
	}
	request.Header.Set("User-Agent", "Yomifolio/1.0 (https://github.com/Nieole/yomifolio)")
	request.Header.Set("Accept", "image/*")
	response, err := client.Do(request)
	if errors.Is(err, errCoverRedirect) {
		return images.EvidenceImage{}, ImageHostRejected
	}
	if err != nil {
		return images.EvidenceImage{}, ImageUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return images.EvidenceImage{}, ImageUnavailable
	}
	if kind, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil || !strings.HasPrefix(kind, "image/") {
		return images.EvidenceImage{}, ImageNotImage
	}
	if response.ContentLength > images.EvidenceSourceBytes {
		return images.EvidenceImage{}, ImageTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, images.EvidenceSourceBytes+1))
	if err != nil {
		return images.EvidenceImage{}, ImageUnavailable
	}
	if len(data) > images.EvidenceSourceBytes {
		return images.EvidenceImage{}, ImageTooLarge
	}
	thumb, err := images.EvidenceThumbnail(ctx, data)
	switch {
	case err == nil:
		return thumb, ImageIncluded
	case errors.Is(err, images.ErrEvidenceLimit):
		return images.EvidenceImage{}, ImageTooLarge
	case ctx.Err() != nil:
		return images.EvidenceImage{}, ImageUnavailable
	}
	return images.EvidenceImage{}, ImageNotImage
}
