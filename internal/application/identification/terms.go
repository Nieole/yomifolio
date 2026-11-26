package identification

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"yomifolio/internal/database"
)

// maxTermRunes bounds one search term; a longer cleaned name is cut at a word boundary.
const maxTermRunes = 100

var (
	// Bracketed segments carry circles, groups, editions and tags, never the title itself.
	bracketed = regexp.MustCompile(`\[[^\]]*\]|【[^】]*】|\([^)]*\)|（[^）]*）|\{[^}]*\}|〔[^〕]*〕`)
	// Title quotes keep their content: 《进击的巨人》 is the title.
	titleQuotes = strings.NewReplacer("《", " ", "》", " ", "「", " ", "」", " ", "『", " ", "』", " ", "_", " ")
	// Volume and chapter markers, in Chinese, Japanese and Latin forms.
	volumeMarkers = []*regexp.Regexp{
		regexp.MustCompile(`第\s*[0-9０-９一二三四五六七八九十百零〇]+\s*(?:[-~～至到]\s*[0-9０-９一二三四五六七八九十百零〇]+\s*)?[卷巻册冊話话集部]`),
		regexp.MustCompile(`[全共]\s*[0-9０-９一二三四五六七八九十百零〇]+\s*[卷巻册冊話话集]`),
		regexp.MustCompile(`[卷巻]\s*[0-9０-９]+`),
		regexp.MustCompile(`(?i)\b(?:vol|volume|v|tome|ch|chapter)\.?\s*\d+(?:\s*[-~]\s*\d+)?\b`),
		regexp.MustCompile(`\s\d+\s*[-~～]\s*\d+\s*$`),
		regexp.MustCompile(`\s0\d+\s*$`),
	}
	// Tokens naming a scanlation group, or an edition or completion state.
	noiseToken = regexp.MustCompile(`(?i)汉化|漢化|翻译|翻譯|扫图|掃圖|嵌字|scanlation|scans?$`)
	noiseWords = map[string]bool{
		"完": true, "完结": true, "完結": true, "全集": true, "单行本": true, "單行本": true, "连载中": true, "連載中": true,
		"全彩": true, "彩色版": true, "高清": true, "无修": true, "無修": true, "end": true, "complete": true, "completed": true,
	}
	edgeSeparators = " \t-_.·・~～:：,，、|/"
)

// cleanTitle strips the noise a directory or ComicInfo name carries around the title: bracketed
// tags, volume and chapter markers, scanlation groups and edition words. A name that is nothing but
// brackets falls back to the bracket contents.
func cleanTitle(name string) string {
	if cleaned := stripNoise(bracketed.ReplaceAllString(name, " ")); cleaned != "" {
		return cleaned
	}
	return stripNoise(strings.NewReplacer("[", " ", "]", " ", "【", " ", "】", " ", "(", " ", ")", " ", "（", " ", "）", " ").Replace(name))
}

// stripNoise drops noise tokens before and after removing volume markers, since a trailing group
// name hides the volume number in front of it and a removed volume can expose a trailing edition word.
func stripNoise(name string) string {
	name = dropNoiseTokens(titleQuotes.Replace(name))
	for _, marker := range volumeMarkers {
		name = marker.ReplaceAllString(name, " ")
	}
	return truncateWords(strings.Trim(dropNoiseTokens(name), edgeSeparators), maxTermRunes)
}

func dropNoiseTokens(name string) string {
	var kept []string
	for _, token := range strings.Fields(name) {
		if !noiseToken.MatchString(token) && !noiseWords[strings.ToLower(token)] {
			kept = append(kept, token)
		}
	}
	return " " + strings.Join(kept, " ") + " "
}

// truncateWords cuts text to at most limit runes, at the last space when there is one.
func truncateWords(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)[:limit]
	if cut := strings.LastIndex(string(runes), " "); cut > 0 {
		return strings.TrimSpace(string(runes)[:cut])
	}
	return string(runes)
}

// searchTerms derives the queries a series is searched with, in order and without repeats: the
// cleaned directory name, the cleaned ComicInfo series name, then the best of the two followed by
// the first ComicInfo author.
func searchTerms(directory string, comicInfo *EvidenceComicInfo) []string {
	var terms []string
	seen := map[string]bool{}
	add := func(term string) {
		term = truncateWords(strings.TrimSpace(term), maxTermRunes)
		key := database.NormalizeSearchText(term)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		terms = append(terms, term)
	}
	title := cleanTitle(directory)
	add(title)
	if comicInfo == nil {
		return terms
	}
	if series := cleanTitle(comicInfo.Series); series != "" {
		add(series)
		title = series
	}
	if len(comicInfo.Authors) > 0 && title != "" {
		add(title + " " + comicInfo.Authors[0])
	}
	return terms
}
