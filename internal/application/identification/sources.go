package identification

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Source keys of the catalogues a series can be identified against, in the default priority order.
const (
	Bangumi     = "bangumi"
	AniList     = "anilist"
	MangaDex    = "mangadex"
	MyAnimeList = "myanimelist"
	ComicVine   = "comicvine"
)

// Sources lists every identification source in the default priority order.
var Sources = []string{Bangumi, AniList, MangaDex, MyAnimeList, ComicVine}

// Entry names one entry of one source.
type Entry struct {
	Source string
	ID     string
	URL    string
}

// SourceKey maps a provider name as stored by older code paths ("Bangumi", "Comic Vine", "mal")
// to its source key. Model providers and unknown names are not identification sources.
func SourceKey(name string) (string, bool) {
	key := strings.NewReplacer(" ", "", "-", "", "_", "").Replace(strings.ToLower(strings.TrimSpace(name)))
	switch key {
	case "bangumi", "bgm":
		return Bangumi, true
	case "anilist":
		return AniList, true
	case "mangadex":
		return MangaDex, true
	case "myanimelist", "mal":
		return MyAnimeList, true
	case "comicvine":
		return ComicVine, true
	}
	return "", false
}

var entryPaths = map[string]*regexp.Regexp{
	Bangumi:     regexp.MustCompile(`/subject/(\d+)`),
	AniList:     regexp.MustCompile(`/manga/(\d+)`),
	MangaDex:    regexp.MustCompile(`/title/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`),
	MyAnimeList: regexp.MustCompile(`/manga/(\d+)`),
	ComicVine:   regexp.MustCompile(`/4050-(\d+)`),
}

var entryURLs = map[string]string{
	Bangumi:     "https://bgm.tv/subject/",
	AniList:     "https://anilist.co/manga/",
	MangaDex:    "https://mangadex.org/title/",
	MyAnimeList: "https://myanimelist.net/manga/",
}

// EntryOf names the entry a source result points at. A positive numeric ID wins; otherwise the
// ID is read from the source's own URL shape (MangaDex has only UUIDs there). The URL is kept only
// when it is http(s); without one a canonical URL is built where the source has a stable form.
func EntryOf(source string, sourceID int64, sourceURL string) (Entry, bool) {
	pattern, known := entryPaths[source]
	if !known {
		return Entry{}, false
	}
	link := strings.TrimSpace(sourceURL)
	if parsed, err := url.Parse(link); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		link = ""
	}
	entry := Entry{Source: source}
	if sourceID > 0 && source != MangaDex {
		entry.ID = strconv.FormatInt(sourceID, 10)
	} else if link != "" {
		if match := pattern.FindStringSubmatch(link); match != nil {
			entry.ID = strings.ToLower(match[1])
		}
	}
	if entry.ID == "" {
		return Entry{}, false
	}
	entry.URL = link
	if entry.URL == "" && entryURLs[source] != "" {
		entry.URL = entryURLs[source] + entry.ID
	}
	return entry, true
}
