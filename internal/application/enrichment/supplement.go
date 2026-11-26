package enrichment

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"slices"
	"sort"
	"strings"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/seriesmeta"
)

// fieldEvidence is confirmed per-field source evidence and the short reason every
// field audit shares. It does not say where the evidence was confirmed.
type fieldEvidence struct {
	sources map[string][]metadata.FieldEvidence
	reason  string
}

// supplement is one series' command to the apply stage: selected fields, the
// caller's expected values, confirmed evidence and explicit cover or author choices.
// correctOnly makes the authors field apply the corrections alone instead of also
// appending every source credit.
type supplement struct {
	key, digest string
	series      int64
	fields      []string
	expected    State
	evidence    fieldEvidence
	cover       *CoverSelection
	prepared    *preparedCover
	corrections []AuthorCorrection
	correctOnly bool
}

// decision is the confirmation behind the evidence. The apply stage consults it only
// inside the series' write transaction, so its terminal state is reread together
// with the write (ADR 0011).
type decision interface {
	// authorize checks current authority the decision needs beyond metadata_write.
	authorize(ctx context.Context, tx *sql.Tx, actor delegated.Subject, library int64) error
	// guard runs before any field is planned. False persists a conflict receipt with
	// the returned reason and changes nothing else.
	guard(ctx context.Context, tx *sql.Tx, q *database.Queries, out *Operation) (bool, string, error)
	// settle finishes the decision after planning: inside the metadata write's
	// savepoint when fields changed, so a failed write rolls both back, otherwise on
	// its own. Conflicts never reach it. It reports whether the decision itself
	// changed public state.
	settle(ctx context.Context, tx *sql.Tx, q *database.Queries, out *Operation) (bool, error)
	// abandon resets decision results in a receipt whose write failed.
	abandon(out *Operation)
}

// applyEvidence commits one series' metadata, decision and receipt in one
// transaction that rereads values, locks and current authority. An identical
// request replays its original receipt instead.
func (s *Service) applyEvidence(ctx context.Context, actor delegated.Subject, c supplement, d decision) (Operation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Operation{}, err
	}
	defer tx.Rollback()
	q := database.New(tx)
	j := database.MetadataJournal{Tx: tx}
	current, before, err := readState(ctx, q, c.series)
	if err != nil {
		return Operation{}, delegated.ErrDenied
	}
	if err = authorize(ctx, tx, actor, current.LibraryID, true); err != nil {
		return Operation{}, err
	}
	if err = d.authorize(ctx, tx, actor, current.LibraryID); err != nil {
		return Operation{}, err
	}
	if out, found, e := retry(ctx, j, actor, c.key, c.digest); e != nil || found {
		return out, e
	}
	out := operation(actor, c.key, current.ID, current.LibraryID, "enrich")
	out.Before = before
	out.After = before
	pending, reason, err := d.guard(ctx, tx, q, &out)
	if err != nil {
		return Operation{}, err
	}
	if !pending {
		out.Outcome = "conflict"
		out.Reason = reason
		if err = save(ctx, j, out, c.digest); err != nil {
			return Operation{}, err
		}
		if err = tx.Commit(); err != nil {
			return Operation{}, err
		}
		return out, nil
	}
	if err = plan(current, c, &out); err != nil {
		return Operation{}, err
	}
	if c.cover != nil {
		source, e := q.GetBook(ctx, c.cover.BookID)
		if e != nil || source.SeriesID != current.ID || source.LibraryID != current.LibraryID {
			return Operation{}, delegated.ErrDenied
		}
	}
	visible := false
	if out.Applied {
		failed, e := j.TryChange(ctx, func() error {
			if err := writeState(ctx, tx, q, current, out.After, changedFields(out.Fields)); err != nil {
				return err
			}
			var err error
			visible, err = d.settle(ctx, tx, q, &out)
			return err
		})
		if e != nil {
			return Operation{}, e
		}
		if failed {
			visible = false
			out.Applied = false
			out.Outcome = "failed"
			out.Reason = "metadata write failed; nothing changed"
			d.abandon(&out)
			out.After = out.Before
			for i := range out.Fields {
				out.Fields[i].Changed = false
			}
		}
	}
	if !out.Applied && out.Outcome != "conflict" && out.Outcome != "failed" {
		if visible, err = d.settle(ctx, tx, q, &out); err != nil {
			return Operation{}, err
		}
	}
	if err = save(ctx, j, out, c.digest); err != nil {
		return Operation{}, err
	}
	if err = tx.Commit(); err != nil {
		return Operation{}, err
	}
	if s.publish != nil && (out.Applied || visible) {
		s.publish()
	}
	return out, nil
}

// plan fills field audits and the after-state from the evidence. A selected lock,
// changed expected value or ineligible cover makes the whole series a conflict that
// keeps its before-state; scalars only fill empty values and collections only append.
func plan(current database.Series, c supplement, out *Operation) error {
	before := out.Before
	if c.cover != nil {
		out.CoverSelection = c.cover
		if c.prepared == nil {
			out.Outcome = "conflict"
			out.Reason = "current field or lock differs"
		}
	}
	if c.prepared != nil {
		out.OldCoverMaterial = c.prepared.old
		out.NewCoverMaterial = c.prepared.new
	}
	for _, f := range c.fields {
		if seriesmeta.EnrichmentLocked(current, f) || !same(before, c.expected, f) || (f == "cover" && !eligibleCover(before.Cover)) {
			out.Outcome = "conflict"
			out.Reason = "current field or lock differs"
		}
		if f == "cover" {
			if before.Cover != nil && c.prepared != nil {
				out.After.Cover = &seriesmeta.CoverState{BookID: before.Cover.BookID, Path: c.prepared.new, Automatic: true}
			}
			out.Fields = append(out.Fields, FieldAudit{Field: f, Changed: true, Reason: c.cover.Reason, Sources: []metadata.FieldEvidence{}})
			continue
		}
		sources := c.evidence.sources[f]
		if len(sources) == 0 {
			return delegated.ErrInvalid
		}
		audit := FieldAudit{Field: f, Reason: c.evidence.reason, Sources: sources}
		switch f {
		case "authors":
			var e error
			out.After.Authors, e = correctAuthors(c.expected.Authors, sources, c.corrections, !c.correctOnly)
			if e != nil {
				return e
			}
			for _, correction := range c.corrections {
				audit.Reason += "; " + correction.Reason
			}
		case "rating":
			r, valid, formula := seriesmeta.EnrichmentRating(sources)
			audit.Sources = valid
			audit.Formula = formula
			if before.Rating == nil {
				out.After.Rating = r
			}
		case "tags":
			out.After.Tags = slices.Clone(before.Tags)
			for _, source := range sources {
				for _, tag := range seriesmeta.ContentTags(source.Metadata.Tags) {
					if !slices.Contains(out.After.Tags, tag) {
						out.After.Tags = append(out.After.Tags, tag)
					}
				}
			}
			sort.Strings(out.After.Tags)
		case "source_links":
			out.After.Links = slices.Clone(before.Links)
			for _, source := range sources {
				u, e := url.Parse(source.Metadata.SourceURL)
				if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
					continue
				}
				exists := false
				for _, l := range out.After.Links {
					if l.Name == source.Source || l.Url == u.String() {
						exists = true
					}
				}
				if !exists {
					out.After.Links = append(out.After.Links, seriesmeta.Link{Name: source.Source, Url: u.String()})
				}
			}
		default:
			target := scalar(&out.After, f)
			if *target == nil || strings.TrimSpace(**target) == "" {
				facts := sources[0].Metadata
				v := ""
				switch f {
				case "title":
					v = facts.Title
				case "summary":
					v = facts.Summary
				case "publisher":
					v = facts.Publisher
				case "status":
					v = facts.Status
				}
				if strings.TrimSpace(v) != "" {
					*target = &v
				}
			}
		}
		audit.Changed = !same(before, out.After, f)
		if !audit.Changed {
			audit.Reason = "preserved existing value or no eligible source change: " + c.evidence.reason
		}
		out.Fields = append(out.Fields, audit)
	}
	if out.Outcome == "conflict" {
		out.After = before
		for i := range out.Fields {
			out.Fields[i].Changed = false
		}
		return nil
	}
	for _, f := range out.Fields {
		if f.Changed {
			out.Outcome = "changed"
			out.Applied = true
		}
	}
	return nil
}

// recordProvenance points each changed field's latest origin at this operation and,
// when given, the proposal that confirmed its evidence. A cover selection is not
// source evidence and records none.
func recordProvenance(ctx context.Context, q *database.Queries, out Operation, proposal int64) error {
	for _, f := range out.Fields {
		if !f.Changed || len(f.Sources) == 0 {
			continue
		}
		stored, err := json.Marshal(value(out.After, f.Field))
		if err != nil {
			return err
		}
		text := string(stored)
		if v, ok := value(out.After, f.Field).(*string); ok && v != nil {
			text = *v
		}
		origin := seriesmeta.FieldOrigin{Operation: out.ID, Proposal: proposal}
		if err = seriesmeta.RecordEnrichmentProvenance(ctx, q, out.SeriesID, origin, f.Field, text, f.Sources[0]); err != nil {
			return err
		}
	}
	return nil
}
