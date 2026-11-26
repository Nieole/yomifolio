package enrichment

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/proposal"
	"yomifolio/internal/seriesmeta"
)

// IdentifiedSource is one source a series is identified with: its entry and the source snapshot the
// identification kept. A source without a snapshot supplies only its entry link.
type IdentifiedSource struct {
	CandidateID string
	Source      string
	EntryURL    string
	Facts       *metadata.SourceFacts
}

// IdentifiedInput asks for the supplementation an identification calls for.
type IdentifiedInput struct {
	// RequestKey names the receipt and must be new for the actor.
	RequestKey string
	SeriesID   int64
	// Reason is the identification's reason, kept on the receipt.
	Reason string
	// Sources are all the sources the series is identified with, highest priority first.
	Sources []IdentifiedSource
	// Tags is the configured tag policy with its defaults filled in; the zero policy appends no tags.
	Tags config.TagPolicy
}

// IdentificationBasis records on a receipt the identification its supplementation followed.
type IdentificationBasis struct {
	Entries []IdentifiedEntry `json:"entries"`
	// PlaceholderTitle reports that the title replaced the locked directory-name placeholder, the one
	// change a lock does not stop; undoing the receipt restores the placeholder under the same lock.
	PlaceholderTitle bool `json:"placeholder_title,omitempty"`
	// SupersededProposals are the pending proposals of the series this identification closed.
	SupersededProposals []int64 `json:"superseded_proposals"`
}

// IdentifiedEntry is one identified source entry a receipt followed.
type IdentifiedEntry struct {
	CandidateID string `json:"candidate_id"`
	EntryURL    string `json:"entry_url"`
}

// sourceLinkNames are the link names of the catalogue sources, as proposals have always named them.
var sourceLinkNames = map[string]string{"bangumi": "Bangumi", "anilist": "AniList", "mangadex": "MangaDex", "myanimelist": "MyAnimeList", "comicvine": "Comic Vine"}

// EnrichIdentified supplements one series from the sources it is identified with, inside the
// caller's transaction that confirmed the identification, so the two commit or roll back together.
// It rereads the series' values and locks there and rechecks current metadata_write authority.
//
// Text fields take the first value in source priority and fill only empty, unlocked fields; a title
// still equal to the directory name is a placeholder that is replaced under its lock. The rating is
// computed by formula when missing, and recomputed while it is still an untouched formula result.
// Source tags append what the tag policy admits of their union (see planTags).
// Source links append every identified entry. Authors fill only the roles the series lacks: each
// missing role takes the authors of the first source in priority that names it. Every written
// field records its source, and the series' pending proposals close as superseded in the same
// transaction. The receipt is an ordinary enrichment receipt that the operation tools read, undo
// and clean up.
//
// An error means the caller must roll back; after its commit the caller calls Publish.
func (s *Service) EnrichIdentified(ctx context.Context, tx *sql.Tx, actor delegated.Subject, in IdentifiedInput) (Operation, error) {
	if !validKey(in.RequestKey) || in.SeriesID <= 0 || len(in.Sources) == 0 {
		return Operation{}, delegated.ErrInvalid
	}
	q := database.New(tx)
	j := database.MetadataJournal{Tx: tx}
	current, before, err := readState(ctx, q, in.SeriesID)
	if err != nil {
		return Operation{}, delegated.ErrDenied
	}
	if err = authorize(ctx, tx, actor, current.LibraryID, true); err != nil {
		return Operation{}, err
	}
	formula, err := formulaRating(ctx, q, j, current)
	if err != nil {
		return Operation{}, err
	}
	out := operation(actor, in.RequestKey, current.ID, current.LibraryID, "enrich")
	out.Before, out.After, out.Reason = before, before, in.Reason
	out.Identification = &IdentificationBasis{Entries: []IdentifiedEntry{}, SupersededProposals: []int64{}}
	for _, source := range in.Sources {
		out.Identification.Entries = append(out.Identification.Entries, IdentifiedEntry{CandidateID: source.CandidateID, EntryURL: source.EntryURL})
	}
	planIdentified(current, in.Sources, in.Tags, formula, &out)
	if out.Applied {
		if err = writeState(ctx, tx, q, current, out.After, changedFields(out.Fields)); err != nil {
			return Operation{}, err
		}
		if err = recordProvenance(ctx, q, out, 0); err != nil {
			return Operation{}, err
		}
	}
	if out.Identification.SupersededProposals, err = proposal.SupersedePending(ctx, q, current.ID); err != nil {
		return Operation{}, err
	}
	if err = save(ctx, j, out, identifiedDigest(in)); err != nil {
		return Operation{}, err
	}
	return out, nil
}

// Publish announces committed metadata changes, for callers that committed EnrichIdentified.
func (s *Service) Publish() {
	if s.publish != nil {
		s.publish()
	}
}

func identifiedDigest(in IdentifiedInput) string {
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// formulaRating reports whether the current rating is still the result of an enrichment formula: its
// source record holds the current value and points at an operation whose receipt computed it.
func formulaRating(ctx context.Context, q *database.Queries, j database.MetadataJournal, current database.Series) (bool, error) {
	if !current.Rating.Valid {
		return false, nil
	}
	records, err := q.GetSeriesMetadataProvenance(ctx, current.ID)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if record.FieldName != "rating" || !record.OperationID.Valid {
			continue
		}
		if recorded, err := strconv.ParseFloat(record.Value, 64); err != nil || recorded != current.Rating.Float64 {
			return false, nil
		}
		r, err := j.Get(ctx, record.OperationID.String)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		original, err := decode(r)
		if err != nil {
			return false, err
		}
		return slices.ContainsFunc(original.Fields, func(f FieldAudit) bool { return f.Field == "rating" && f.Changed && f.Formula != "" }), nil
	}
	return false, nil
}

// Field audit reasons of supplementation after identification.
const (
	reasonFilled      = "filled empty value from the first identified source in priority order"
	reasonPlaceholder = "replaced the directory-name placeholder title; the lock stays"
	reasonRated       = "equal-weight ten-point mean of the identified sources' ratings"
	reasonLinked      = "appended identified source entries"
	reasonRoles       = "added the author roles the series lacked, each from the first identified source in priority order naming it"
	reasonLocked      = "field is locked"
	reasonKept        = "kept existing value"
	reasonNoValue     = "no identified source has a value"
)

// planIdentified fills field audits and the after-state by the identification policy. Unlike plan,
// a locked or filled field is kept on its own and never turns the series into a conflict.
func planIdentified(current database.Series, sources []IdentifiedSource, tags config.TagPolicy, formula bool, out *Operation) {
	before := out.Before
	record := func(field, reason string, evidence []metadata.FieldEvidence) {
		changed := !same(before, out.After, field)
		if !changed && len(evidence) > 0 {
			reason, evidence = reasonKept, nil
		}
		if evidence == nil {
			evidence = []metadata.FieldEvidence{}
		}
		out.Fields = append(out.Fields, FieldAudit{Field: field, Changed: changed, Reason: reason, Sources: evidence})
	}
	text := func(field string, valueOf func(metadata.SourceFacts) string) {
		target := scalar(&out.After, field)
		placeholder := field == "title" && seriesmeta.TitlePlaceholder(current)
		switch {
		case seriesmeta.EnrichmentLocked(current, field) && !placeholder:
			record(field, reasonLocked, nil)
			return
		case *target != nil && strings.TrimSpace(**target) != "" && !placeholder:
			record(field, reasonKept, nil)
			return
		}
		for _, source := range sources {
			if source.Facts == nil {
				continue
			}
			if v := strings.TrimSpace(valueOf(*source.Facts)); v != "" {
				*target = &v
				reason := reasonFilled
				if placeholder {
					reason = reasonPlaceholder
					out.Identification.PlaceholderTitle = seriesmeta.EnrichmentLocked(current, field) && v != current.Name
				}
				record(field, reason, []metadata.FieldEvidence{evidenceOf(source, field)})
				return
			}
		}
		record(field, reasonNoValue, nil)
	}
	text("title", func(f metadata.SourceFacts) string { return firstText(f.Title, f.OriginalTitle) })
	text("summary", func(f metadata.SourceFacts) string { return f.Summary })
	text("publisher", func(f metadata.SourceFacts) string { return f.Publisher })
	text("status", func(f metadata.SourceFacts) string {
		if code, ok := metadata.NormalizeStatusCodeOptional(f.Status); ok && code != "unknown" {
			return code
		}
		return ""
	})

	switch {
	case seriesmeta.EnrichmentLocked(current, "rating"):
		record("rating", reasonLocked, nil)
	case before.Rating != nil && !formula:
		record("rating", reasonKept, nil)
	default:
		var evidence []metadata.FieldEvidence
		for _, source := range sources {
			if source.Facts != nil {
				evidence = append(evidence, evidenceOf(source, "rating"))
			}
		}
		rating, valid, formulaText := seriesmeta.EnrichmentRating(evidence)
		if rating == nil {
			record("rating", reasonNoValue, nil)
			break
		}
		out.After.Rating = rating
		record("rating", reasonRated, valid)
		if last := &out.Fields[len(out.Fields)-1]; last.Changed {
			last.Formula = formulaText
		}
	}

	if seriesmeta.EnrichmentLocked(current, "tags") {
		record("tags", reasonLocked, nil)
	} else {
		var evidence []metadata.FieldEvidence
		var reason string
		out.After.Tags, evidence, reason = planTags(tags, sources, before.Tags)
		record("tags", reason, evidence)
	}

	if seriesmeta.EnrichmentLocked(current, "source_links") {
		record("source_links", reasonLocked, nil)
	} else {
		out.After.Links = slices.Clone(before.Links)
		var linked []metadata.FieldEvidence
		for _, source := range sources {
			u, err := url.Parse(source.EntryURL)
			if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
				continue
			}
			name := firstText(sourceLinkNames[source.Source], source.Source)
			if slices.ContainsFunc(out.After.Links, func(l seriesmeta.Link) bool { return l.Url == u.String() || strings.EqualFold(l.Name, name) }) {
				continue
			}
			out.After.Links = append(out.After.Links, seriesmeta.Link{Name: name, Url: u.String()})
			linked = append(linked, metadata.FieldEvidence{CandidateID: source.CandidateID, Source: source.Source, Metadata: metadata.SourceFacts{SourceURL: u.String()}})
		}
		record("source_links", reasonLinked, linked)
	}

	if seriesmeta.EnrichmentLocked(current, "authors") {
		record("authors", reasonLocked, nil)
	} else {
		out.After.Authors = slices.Clone(before.Authors)
		covered := map[string]bool{}
		for _, a := range before.Authors {
			covered[roleKey(a.Role)] = roleKey(a.Role) != ""
		}
		var used []metadata.FieldEvidence
		for _, source := range sources {
			if source.Facts == nil {
				continue
			}
			added := map[string]bool{}
			for _, a := range source.Facts.Authors {
				name, role := strings.TrimSpace(a.Name), strings.TrimSpace(a.Role)
				if name == "" || role == "" || len(name) > 500 || len(role) > 100 || covered[roleKey(role)] {
					continue
				}
				out.After.Authors = append(out.After.Authors, seriesmeta.Author{Name: name, Role: role})
				added[roleKey(role)] = true
			}
			if len(added) > 0 {
				maps.Copy(covered, added)
				used = append(used, evidenceOf(source, "authors"))
			}
		}
		out.After.Authors = seriesmeta.AuthorSet(out.After.Authors)
		switch {
		case len(used) > 0:
			record("authors", reasonRoles, used)
		case len(before.Authors) > 0:
			record("authors", reasonKept, nil)
		default:
			record("authors", reasonNoValue, nil)
		}
	}

	for _, f := range out.Fields {
		if f.Changed {
			out.Outcome = "changed"
			out.Applied = true
		}
	}
}

// evidenceOf is the part of a source snapshot one field used, with the entry's identity. Receipts
// keep only that much: every identified series leaves one, so whole snapshots would add up.
func evidenceOf(source IdentifiedSource, field string) metadata.FieldEvidence {
	f := source.Facts
	kept := metadata.SourceFacts{SourceID: f.SourceID, SourceURL: f.SourceURL, Provider: f.Provider}
	switch field {
	case "title":
		kept.Title, kept.OriginalTitle = f.Title, f.OriginalTitle
	case "summary":
		kept.Summary = f.Summary
	case "publisher":
		kept.Publisher = f.Publisher
	case "status":
		kept.Status = f.Status
	case "rating":
		kept.SiteRating, kept.Rating = f.SiteRating, f.Rating
	case "authors":
		kept.Authors = f.Authors
	}
	return metadata.FieldEvidence{CandidateID: source.CandidateID, Source: source.Source, Metadata: kept}
}

// roleKey compares author roles the way sources and ComicInfo spell the same role.
func roleKey(role string) string { return strings.ToLower(strings.TrimSpace(role)) }

// firstText is the first value that is not blank.
func firstText(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
