package identification

import (
	"net/url"
	"strconv"
	"strings"

	"yomifolio/internal/metadata"
)

// Sources lists every identification source, the catalogue sources in the default priority order.
var Sources = func() []string {
	var keys []string
	for _, source := range metadata.Catalogue() {
		keys = append(keys, source.Key)
	}
	return keys
}()

// Entry names one entry of one source.
type Entry struct {
	Source string
	ID     string
	URL    string
}

// SourceKey maps a provider name a proposal or search result carries ("Bangumi", "Comic Vine",
// "mal") to its source key. Model providers and unknown names are not identification sources.
func SourceKey(name string) (string, bool) {
	source, ok := metadata.LookupCatalogueSource(name)
	return source.Key, ok
}

// EntryOf names the entry a source result points at. A positive numeric ID wins where the source
// numbers its entries; otherwise the ID is read from the source's own URL shape. The URL is kept
// only when it is http(s); without one a canonical URL is built where the source has a stable form.
func EntryOf(source string, sourceID int64, sourceURL string) (Entry, bool) {
	catalogue, known := metadata.LookupCatalogueSource(source)
	if !known || catalogue.Key != source {
		return Entry{}, false
	}
	link := strings.TrimSpace(sourceURL)
	if parsed, err := url.Parse(link); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		link = ""
	}
	entry := Entry{Source: source}
	if sourceID > 0 && catalogue.NumericEntries {
		entry.ID = strconv.FormatInt(sourceID, 10)
	} else if link != "" {
		if match := catalogue.EntryPath.FindStringSubmatch(link); match != nil {
			entry.ID = strings.ToLower(match[1])
		}
	}
	if entry.ID == "" {
		return Entry{}, false
	}
	entry.URL = link
	if entry.URL == "" && catalogue.EntryURL != "" {
		entry.URL = catalogue.EntryURL + entry.ID
	}
	return entry, true
}
