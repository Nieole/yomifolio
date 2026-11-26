// 目录型来源的唯一描述：键、显示名、来源列表里的说明、历史提案里的其它写法、条目地址的形状与封面所在的
// 图片域名。扇出、识别、补充的来源链接与网页来源列表都从这里取，新增来源只改这一处。

package metadata

import (
	"regexp"
	"slices"
	"strings"

	"yomifolio/internal/config"
)

// CatalogueSource describes one catalogue source.
type CatalogueSource struct {
	// Key names the source in configuration, evidence packets, candidate IDs and conclusions.
	Key string
	// Name is the display name. It equals the source provider's Name(), which a proposal stores as
	// its provider and names the source link by, so a link identification appends matches one a
	// proposal applied.
	Name string
	// Description is the line the web source picker shows for it.
	Description string
	// Spellings are the other provider names a stored proposal may carry for the source; a name
	// matches ignoring case, spaces, hyphens and underscores.
	Spellings []string
	// EntryPath finds the entry ID in one of the source's entry URLs.
	EntryPath *regexp.Regexp
	// NumericEntries says the source's numeric SourceID is its entry ID; MangaDex names entries by
	// UUID, which only its URLs carry.
	NumericEntries bool
	// EntryURL prefixed to an entry ID is the entry's canonical URL; empty where the source has no
	// stable form.
	EntryURL string
	// CoverHosts are the image hosts its covers come from; each also matches its subdomains.
	CoverHosts []string
	// credentialed reports whether the configuration holds the credential the source needs; nil
	// means it needs none.
	credentialed func(config.Config) bool
}

// catalogue lists the catalogue sources in config.DefaultSourcePriority order.
var catalogue = []CatalogueSource{
	{Key: "bangumi", Name: "Bangumi", Description: "从 Bangumi 番组计划获取漫画元数据", Spellings: []string{"bgm"},
		EntryPath: regexp.MustCompile(`/subject/(\d+)`), NumericEntries: true, EntryURL: "https://bgm.tv/subject/", CoverHosts: []string{"bgm.tv", "bangumi.tv", "chii.in"}},
	{Key: "anilist", Name: "AniList", Description: "从 AniList 获取漫画元数据（英文/罗马音/原名、评分、标签、作者）",
		EntryPath: regexp.MustCompile(`/manga/(\d+)`), NumericEntries: true, EntryURL: "https://anilist.co/manga/", CoverHosts: []string{"anilist.co"}},
	{Key: "mangadex", Name: "MangaDex", Description: "从 MangaDex 获取漫画元数据（多语言标题、标签、作者、封面）",
		EntryPath: regexp.MustCompile(`/title/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`), EntryURL: "https://mangadex.org/title/", CoverHosts: []string{"mangadex.org"}},
	{Key: "myanimelist", Name: "MyAnimeList", Description: "从 MyAnimeList 获取漫画元数据（需配置 Client ID）", Spellings: []string{"mal"},
		EntryPath: regexp.MustCompile(`/manga/(\d+)`), NumericEntries: true, EntryURL: "https://myanimelist.net/manga/", CoverHosts: []string{"myanimelist.net"},
		credentialed: func(cfg config.Config) bool { return strings.TrimSpace(cfg.Scrapers.MALClientID) != "" }},
	{Key: "comicvine", Name: "Comic Vine", Description: "从 Comic Vine 获取欧美 comics 元数据（需配置 API Key）",
		EntryPath: regexp.MustCompile(`/4050-(\d+)`), NumericEntries: true, CoverHosts: []string{"comicvine.gamespot.com"},
		credentialed: func(cfg config.Config) bool { return strings.TrimSpace(cfg.Scrapers.ComicVineAPIKey) != "" }},
}

// Catalogue lists every catalogue source in the default priority order.
func Catalogue() []CatalogueSource { return slices.Clone(catalogue) }

// LookupCatalogueSource returns the catalogue source a key or one of its spellings names. Model
// providers and unknown names are no catalogue source.
func LookupCatalogueSource(name string) (CatalogueSource, bool) {
	fold := func(s string) string {
		return strings.NewReplacer(" ", "", "-", "", "_", "").Replace(strings.ToLower(strings.TrimSpace(s)))
	}
	name = fold(name)
	for _, source := range catalogue {
		if name == fold(source.Key) || name == fold(source.Name) || slices.ContainsFunc(source.Spellings, func(s string) bool { return name == fold(s) }) {
			return source, true
		}
	}
	return CatalogueSource{}, false
}

// CatalogueSources lists the catalogue sources the configuration enables, in fan-out order:
// Bangumi, AniList and MangaDex always, MyAnimeList and Comic Vine only with their credentials.
// Model aliases are never catalogue sources.
func CatalogueSources(cfg config.Config) []string {
	var names []string
	for _, source := range catalogue {
		if source.credentialed == nil || source.credentialed(cfg) {
			names = append(names, source.Key)
		}
	}
	return names
}
