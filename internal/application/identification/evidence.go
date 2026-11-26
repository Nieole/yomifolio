package identification

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
)

// Bounds of one evidence packet. Each field is cut to its bound; a packet still over packetBudget
// drops summary excerpts, then its lowest-ranked candidates, until it fits. A packet that custom
// searches added to may grow to researchedBudget and records its last listedSearches searches.
const (
	candidatesPerSource = 3
	searchLimit         = 5
	excerptRunes        = 60
	textRunes           = 100
	nameRunes           = 40
	listedAuthors       = 3
	listedFiles         = 3
	packetBudget        = 4 << 10
	researchedBudget    = 8 << 10
	listedSearches      = 5
)

// Source availability in a packet.
const (
	SourceAvailable   = "available"
	SourceUnavailable = "unavailable"
)

// EvidencePacket gathers the local clues of one series and the candidates its sources returned,
// as text an agent or the owner compares when identifying it. It is evidence, not a conclusion.
type EvidencePacket struct {
	FetchedAt string             `json:"fetched_at"`
	Local     EvidenceLocalClues `json:"local"`
	// Queries are the terms the server derived for the series.
	Queries []string `json:"queries"`
	// Searches are the custom-term searches merged into the packet since, oldest first.
	Searches   []EvidenceSearch    `json:"searches,omitempty"`
	Sources    []EvidenceSource    `json:"sources"`
	Candidates []EvidenceCandidate `json:"candidates"`
}

// EvidenceSearch records one search with a custom term: when it ran and which candidates it added.
type EvidenceSearch struct {
	Query      string   `json:"query"`
	SearchedAt string   `json:"searched_at"`
	Added      []string `json:"added"`
	// Dropped counts the new candidates left out because the packet had no room for them.
	Dropped int `json:"dropped,omitempty"`
}

// EvidenceLocalClues is what the library itself says about the series.
type EvidenceLocalClues struct {
	DirectoryName string   `json:"directory_name"`
	BookCount     int      `json:"book_count"`
	FileNames     []string `json:"file_names"`
	// ComicInfo is absent when none of the first books carries a readable ComicInfo document.
	ComicInfo *EvidenceComicInfo `json:"comic_info,omitempty"`
}

// EvidenceComicInfo are the identity fields of the first readable ComicInfo document.
type EvidenceComicInfo struct {
	Series  string   `json:"series,omitempty"`
	Authors []string `json:"authors,omitempty"`
	Year    int      `json:"year,omitempty"`
}

// EvidenceSource says whether one source answered any of the packet's queries.
type EvidenceSource struct {
	Source string `json:"source"`
	Status string `json:"status"`
}

// EvidenceCandidate is one source entry the series may be. ID is "<source>:<entry id>".
type EvidenceCandidate struct {
	ID            string          `json:"id"`
	Source        string          `json:"source"`
	EntryURL      string          `json:"entry_url"`
	Title         string          `json:"title"`
	OriginalTitle string          `json:"original_title,omitempty"`
	Authors       []string        `json:"authors,omitempty"`
	Year          int             `json:"year,omitempty"`
	Summary       string          `json:"summary,omitempty"`
	Signals       EvidenceSignals `json:"signals"`
}

// EvidenceSignals are checkable comparisons between a candidate and the local clues, never a
// likelihood. AuthorMatches is absent when either side names no author.
type EvidenceSignals struct {
	TitleMatches  bool  `json:"title_matches"`
	AuthorMatches *bool `json:"author_matches,omitempty"`
	SourceVolumes int   `json:"source_volumes"`
	LocalBooks    int   `json:"local_books"`
}

// candidateID names a source entry inside packets and verdicts.
func candidateID(source, entryID string) string { return source + ":" + entryID }

// assemble builds the packet from the local clues and every query's fan-out pages. Excluded
// entries are left out. It reports false when no source answered: such a packet would read as
// "no candidates" while nothing was searched, so it is never stored on its own.
func assemble(local EvidenceLocalClues, queries []string, pages []metadata.SourcePage, excluded map[string]bool, fetchedAt time.Time) (EvidencePacket, map[string]metadata.SeriesMetadata, bool) {
	if queries == nil {
		queries = []string{}
	}
	packet := EvidencePacket{FetchedAt: stamp(fetchedAt.UnixMilli()), Local: local, Queries: queries, Sources: []EvidenceSource{}, Candidates: []EvidenceCandidate{}}
	facts := map[string]metadata.SeriesMetadata{}
	titles, authors := localTitleKeys(local), localAuthorKeys(local)
	bySource := map[string][]EvidenceCandidate{}
	var order []string
	answered := false
	for _, page := range pages {
		if _, known := bySource[page.Source]; !known {
			order = append(order, page.Source)
			bySource[page.Source] = []EvidenceCandidate{}
		}
		if page.Status != metadata.SourceAvailable {
			continue
		}
		answered = true
		for _, result := range page.Results {
			entry, ok := EntryOf(page.Source, int64(result.SourceID), result.SourceURL)
			if !ok {
				continue
			}
			id := candidateID(page.Source, entry.ID)
			if _, dup := facts[id]; dup || excluded[id] {
				continue
			}
			facts[id] = result
			bySource[page.Source] = append(bySource[page.Source], candidate(id, entry, result, titles, authors, local.BookCount))
		}
	}
	for _, source := range order {
		status := SourceUnavailable
		for _, page := range pages {
			if page.Source == source && page.Status == metadata.SourceAvailable {
				status = SourceAvailable
			}
		}
		packet.Sources = append(packet.Sources, EvidenceSource{Source: source, Status: status})
		packet.Candidates = append(packet.Candidates, rankHits(bySource[source])...)
	}
	packet = fit(packet)
	kept := map[string]metadata.SeriesMetadata{}
	for _, c := range packet.Candidates {
		kept[c.ID] = facts[c.ID]
	}
	return packet, kept, answered
}

// rankHits puts the candidates whose title matches a local name first, keeping the sources' own
// order otherwise, and keeps at most candidatesPerSource of them.
func rankHits(hits []EvidenceCandidate) []EvidenceCandidate {
	slices.SortStableFunc(hits, func(a, b EvidenceCandidate) int {
		return cmp.Compare(boolRank(a.Signals.TitleMatches), boolRank(b.Signals.TitleMatches))
	})
	return hits[:min(len(hits), candidatesPerSource)]
}

func boolRank(b bool) int {
	if b {
		return 0
	}
	return 1
}

func candidate(id string, entry Entry, result metadata.SeriesMetadata, titles, authors map[string]bool, books int) EvidenceCandidate {
	c := EvidenceCandidate{ID: id, Source: entry.Source, EntryURL: entry.URL, Title: cut(result.Title, textRunes), OriginalTitle: cut(result.OriginalTitle, textRunes),
		Year: yearOf(result.ReleaseDate), Summary: excerpt(result.Summary), Signals: EvidenceSignals{SourceVolumes: result.VolumeCount, LocalBooks: books}}
	if c.OriginalTitle == c.Title {
		c.OriginalTitle = ""
	}
	for _, title := range []string{result.Title, result.OriginalTitle} {
		if key := database.NormalizeSearchText(title); key != "" && titles[key] {
			c.Signals.TitleMatches = true
		}
	}
	var names []string
	matched := false
	for _, author := range result.Authors {
		name := strings.TrimSpace(author.Name)
		if name == "" || slices.Contains(names, name) {
			continue
		}
		names = append(names, name)
		if authors[database.NormalizeSearchText(name)] {
			matched = true
		}
	}
	for _, name := range names[:min(len(names), listedAuthors)] {
		c.Authors = append(c.Authors, cut(name, nameRunes))
	}
	if len(names) > 0 && len(authors) > 0 {
		c.Signals.AuthorMatches = &matched
	}
	return c
}

// localTitleKeys are the normalized names the library gives the series.
func localTitleKeys(local EvidenceLocalClues) map[string]bool {
	keys := map[string]bool{}
	names := []string{cleanTitle(local.DirectoryName)}
	if local.ComicInfo != nil {
		names = append(names, local.ComicInfo.Series, cleanTitle(local.ComicInfo.Series))
	}
	for _, name := range names {
		if key := database.NormalizeSearchText(name); key != "" {
			keys[key] = true
		}
	}
	return keys
}

func localAuthorKeys(local EvidenceLocalClues) map[string]bool {
	keys := map[string]bool{}
	if local.ComicInfo != nil {
		for _, name := range local.ComicInfo.Authors {
			if key := database.NormalizeSearchText(name); key != "" {
				keys[key] = true
			}
		}
	}
	return keys
}

var yearPattern = regexp.MustCompile(`\b(1[89]\d\d|2[01]\d\d)\b`)

func yearOf(date string) int {
	year, _ := strconv.Atoi(yearPattern.FindString(date))
	return year
}

// cut trims text to at most limit runes.
func cut(text string, limit int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit])
}

// excerpt is the start of a summary on one line, marked when cut.
func excerpt(summary string) string {
	line := strings.Join(strings.Fields(summary), " ")
	if utf8.RuneCountInString(line) <= excerptRunes {
		return line
	}
	return string([]rune(line)[:excerptRunes]) + "…"
}

// fit sheds the least useful text until the packet's JSON is within packetBudget: summary excerpts
// from the last candidate back, then candidates from the lowest rank of the last source back.
func fit(packet EvidencePacket) EvidencePacket {
	over := func() bool {
		raw, _ := json.Marshal(packet)
		return len(raw) > packetBudget
	}
	for i := len(packet.Candidates) - 1; i >= 0 && over(); i-- {
		packet.Candidates[i].Summary = ""
	}
	for len(packet.Candidates) > 0 && over() {
		rank := map[string]int{}
		worst, worstRank := 0, -1
		for i, c := range packet.Candidates {
			r := rank[c.Source]
			rank[c.Source]++
			if r >= worstRank {
				worst, worstRank = i, r
			}
		}
		packet.Candidates = slices.Delete(packet.Candidates, worst, worst+1)
	}
	return packet
}

// readEvidence loads the series' packet without the candidates excluded since it was fetched.
func readEvidence(ctx context.Context, q queryer, seriesID int64, excluded []ExcludedCandidate) (*EvidencePacket, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT packet FROM identification_evidence WHERE series_id = ?`, seriesID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var packet EvidencePacket
	if err := json.Unmarshal([]byte(raw), &packet); err != nil {
		return nil, err
	}
	packet.Candidates = slices.DeleteFunc(packet.Candidates, func(c EvidenceCandidate) bool {
		return slices.ContainsFunc(excluded, func(x ExcludedCandidate) bool { return candidateID(x.Source, x.EntryID) == c.ID })
	})
	return &packet, nil
}

// storedEvidence loads the series' packet as stored, excluded candidates included, with the source
// facts of its candidates; the packet is nil when the series has none.
func storedEvidence(ctx context.Context, q queryer, seriesID int64) (*EvidencePacket, map[string]metadata.SeriesMetadata, error) {
	var rawPacket, rawFacts string
	err := q.QueryRowContext(ctx, `SELECT packet, source_facts FROM identification_evidence WHERE series_id = ?`, seriesID).Scan(&rawPacket, &rawFacts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var packet EvidencePacket
	facts := map[string]metadata.SeriesMetadata{}
	if err := json.Unmarshal([]byte(rawPacket), &packet); err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal([]byte(rawFacts), &facts); err != nil {
		return nil, nil, err
	}
	return &packet, facts, nil
}

// writeEvidence stores the series' packet with its source facts. A fetch time replaces the whole row;
// without one the packet keeps the fetch time it has.
func writeEvidence(ctx context.Context, tx *sql.Tx, seriesID int64, fetchedAt *time.Time, packet EvidencePacket, facts map[string]metadata.SeriesMetadata) error {
	rawPacket, err := json.Marshal(packet)
	if err != nil {
		return err
	}
	rawFacts, err := json.Marshal(facts)
	if err != nil {
		return err
	}
	if fetchedAt == nil {
		_, err = tx.ExecContext(ctx, `UPDATE identification_evidence SET packet = ?, source_facts = ? WHERE series_id = ?`, string(rawPacket), string(rawFacts), seriesID)
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO identification_evidence (series_id, fetched_at_ms, packet, source_facts) VALUES (?, ?, ?, ?)
		ON CONFLICT(series_id) DO UPDATE SET fetched_at_ms = excluded.fetched_at_ms, packet = excluded.packet, source_facts = excluded.source_facts`,
		seriesID, fetchedAt.UnixMilli(), string(rawPacket), string(rawFacts))
	return err
}

// excludedIDs lists the candidate ids a person ruled out for the series.
func excludedIDs(ctx context.Context, q queryer, seriesID int64) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT source, entry_id FROM identification_exclusions WHERE series_id = ?`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var source, entry string
		if err := rows.Scan(&source, &entry); err != nil {
			return nil, err
		}
		out[candidateID(source, entry)] = true
	}
	return out, rows.Err()
}
