package enrichment

import (
	"cmp"
	"regexp"
	"slices"
	"sort"
	"strings"

	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
)

// Field audit reasons of identified tags.
const (
	reasonTagged       = "appended source tags that passed the vote threshold, the blocklist and the per-series limit"
	reasonTagsFiltered = "no source tag passed the vote threshold, the blocklist and the per-series limit"
)

// sourceTag is one tag a source applies, with its votes when the source counts them.
type sourceTag struct {
	name  string
	votes int
	// voted marks a source whose tags are votes; known is false when its snapshot kept no counts.
	voted, known bool
}

// passes reports whether the tag meets the vote threshold; min <= 0 is no threshold.
func (t sourceTag) passes(min int) bool {
	return !t.voted || min <= 0 || (t.known && t.votes >= min)
}

// rankedTags lists a source's tags, a voted source's by votes, most first.
func rankedTags(source IdentifiedSource) []sourceTag {
	facts := source.Facts
	var out []sourceTag
	if len(facts.TagVotes) > 0 {
		for _, v := range facts.TagVotes {
			out = append(out, sourceTag{name: v.Name, votes: v.Votes, voted: true, known: true})
		}
		slices.SortStableFunc(out, func(a, b sourceTag) int { return cmp.Compare(b.votes, a.votes) })
		return out
	}
	voted := metadata.TagsAreVoted(source.Source)
	for _, name := range facts.Tags {
		out = append(out, sourceTag{name: name, voted: voted})
	}
	return out
}

// planTags appends to the series' tags those of its identified sources the tag policy admits, and
// returns each source's share of them as evidence.
//
// The sources' tags are united in priority order, a voted source's by votes. A tag passes the
// threshold when one source applies it with enough votes; a source whose tags are not votes (every
// source but Bangumi) vouches for each of its tags. A voted source's snapshot without tag votes has
// unknown counts, which never meet an active threshold. Blocked tags drop out, the series takes
// the first MaxPerSeries of the rest, and only those it lacks are appended: a series keeps every tag
// it has, and spellings that fold to the same catalogue search text are one tag.
func planTags(policy config.TagPolicy, sources []IdentifiedSource, before []string) ([]string, []metadata.FieldEvidence, string) {
	literals, patterns := policy.Blocks()
	blocked := map[string]bool{}
	for _, l := range literals {
		blocked[database.NormalizeSearchText(l)] = true
	}
	held := map[string]bool{}
	for _, tag := range before {
		held[database.NormalizeSearchText(tag)] = true
	}
	after := slices.Clone(before)
	shares := make([]metadata.SourceFacts, len(sources))
	taken := map[string]bool{}
	offered := false
pick:
	for i, source := range sources {
		if source.Facts == nil {
			continue
		}
		for _, tag := range rankedTags(source) {
			name := strings.TrimSpace(tag.name)
			key := database.NormalizeSearchText(name)
			if key == "" {
				continue
			}
			offered = true
			if taken[key] || !tag.passes(policy.MinVotes) || blocked[key] || slices.ContainsFunc(patterns, func(re *regexp.Regexp) bool { return re.MatchString(key) }) {
				continue
			}
			if policy.MaxPerSeries >= 0 && len(taken) >= policy.MaxPerSeries {
				break pick
			}
			taken[key] = true
			if held[key] {
				continue
			}
			held[key] = true
			after = append(after, name)
			shares[i].Tags = append(shares[i].Tags, name)
			if tag.known {
				shares[i].TagVotes = append(shares[i].TagVotes, metadata.TagVote{Name: name, Votes: tag.votes})
			}
		}
	}
	sort.Strings(after)
	var evidence []metadata.FieldEvidence
	for i, share := range shares {
		if len(share.Tags) == 0 {
			continue
		}
		f := sources[i].Facts
		share.SourceID, share.SourceURL, share.Provider = f.SourceID, f.SourceURL, f.Provider
		evidence = append(evidence, metadata.FieldEvidence{CandidateID: sources[i].CandidateID, Source: sources[i].Source, Metadata: share})
	}
	switch {
	case len(evidence) > 0:
		return after, evidence, reasonTagged
	case !offered:
		return after, nil, reasonNoValue
	case len(taken) > 0:
		return after, nil, reasonKept
	}
	return after, nil, reasonTagsFiltered
}
