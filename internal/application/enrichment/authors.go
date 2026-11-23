package enrichment

import (
	"slices"
	"strings"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/metadata"
	"yomifolio/internal/seriesmeta"
)

// AuthorCorrection is the explicit series-local exception to member preservation.
// From must be in the expected author group; To must occur in confirmed source evidence.
// Multiple aliases may point at one identity. No global identity is renamed or removed.
type AuthorCorrection struct {
	From   seriesmeta.Author `json:"from"`
	To     seriesmeta.Author `json:"to"`
	Reason string            `json:"reason"`
}

func correctAuthors(before []seriesmeta.Author, sources []metadata.FieldEvidence, corrections []AuthorCorrection) ([]seriesmeta.Author, error) {
	supplied := []seriesmeta.Author{}
	for _, source := range sources {
		for _, a := range source.Metadata.Authors {
			if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.Role) == "" || len(a.Name) > 500 || len(a.Role) > 100 {
				return nil, delegated.ErrInvalid
			}
			supplied = append(supplied, seriesmeta.Author{Name: a.Name, Role: a.Role})
		}
	}
	if len(supplied) > 100 {
		return nil, delegated.ErrInvalid
	}
	removed := map[seriesmeta.Author]bool{}
	for _, c := range corrections {
		if removed[c.From] || c.From == c.To || !slices.Contains(before, c.From) || !slices.Contains(supplied, c.To) || strings.TrimSpace(c.Reason) == "" || len(c.Reason) > 1000 {
			return nil, delegated.ErrInvalid
		}
		removed[c.From] = true
	}
	for _, c := range corrections {
		if removed[c.To] {
			return nil, delegated.ErrInvalid
		}
	}
	out := []seriesmeta.Author{}
	for _, a := range before {
		if !removed[a] {
			out = append(out, a)
		}
	}
	for _, a := range supplied {
		if !removed[a] {
			out = append(out, a)
		}
	}
	return seriesmeta.AuthorSet(out), nil
}
