package enrichment

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"slices"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/seriesmeta"
)

// RetractInput asks to take back what one withdrawn identification entry supplemented a series with.
type RetractInput struct {
	// RequestKey names the receipt and must be new for the actor.
	RequestKey string
	SeriesID   int64
	// Entry is the withdrawn entry. The caller has already removed it from the series' conclusions.
	Entry IdentifiedEntry
	// Reason is the withdrawal's reason, kept on the receipt.
	Reason string
}

// Field audit reasons of a retraction.
const (
	reasonRetracted = "took back the value the withdrawn entry supplemented; nobody changed it since"
	reasonChanged   = "changed since the withdrawn entry supplemented it; kept"
)

// retractable are the fields supplementation after identification writes.
var retractable = []string{"title", "summary", "publisher", "status", "rating", "tags", "source_links", "authors"}

// RetractIdentified takes back, field by field, what the supplementation after identification wrote
// from a withdrawn entry, inside the caller's transaction that withdrew it. A field is taken back
// only while its source record still points at a standing supplementation and its value is still
// what that supplementation left, the expected-value check undo makes; it then returns to the value
// it had before the run of untouched supplementations that led to it, when any of them cited the
// entry. Values other entries supplied come back from the supplementation the caller runs next.
// Locks stop it, except on a directory-name placeholder title that a supplementation replaced.
// Fields changed since, by a person or by anything else, are kept. The receipt is an ordinary
// enrichment receipt naming the entry it retracted; undoing it restores what it took back.
//
// An error means the caller must roll back; after its commit the caller calls Publish.
func (s *Service) RetractIdentified(ctx context.Context, tx *sql.Tx, actor delegated.Subject, in RetractInput) (Operation, error) {
	if !validKey(in.RequestKey) || in.SeriesID <= 0 || in.Entry.CandidateID == "" {
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
	history, err := seriesHistory(ctx, j, current.ID)
	if err != nil {
		return Operation{}, err
	}
	records, err := q.GetSeriesMetadataProvenance(ctx, current.ID)
	if err != nil {
		return Operation{}, err
	}
	origins := map[string]string{}
	for _, r := range records {
		if r.OperationID.Valid {
			origins[r.FieldName] = r.OperationID.String
		}
	}
	out := operation(actor, in.RequestKey, current.ID, current.LibraryID, "enrich")
	out.Before, out.After, out.Reason = before, before, in.Reason
	entry := in.Entry
	out.Identification = &IdentificationBasis{Entries: []IdentifiedEntry{}, Retracted: &entry, SupersededProposals: []int64{}}
	forget := map[string]string{}
	for _, field := range retractable {
		contributed := history.cited(field, entry.CandidateID)
		if len(contributed) == 0 {
			continue
		}
		chain := history.chain(origins[field], before, field)
		if !slices.ContainsFunc(chain, func(op Operation) bool { return cites(op, field, entry.CandidateID) }) {
			out.Fields = append(out.Fields, FieldAudit{Field: field, Reason: reasonChanged, Sources: contributed})
			continue
		}
		earliest := chain[len(chain)-1]
		placeholder := field == "title" && earliest.Identification.PlaceholderTitle
		if seriesmeta.EnrichmentLocked(current, field) && !placeholder {
			out.Fields = append(out.Fields, FieldAudit{Field: field, Reason: reasonLocked, Sources: contributed})
			continue
		}
		copyField(&out.After, earliest.Before, field)
		out.Fields = append(out.Fields, FieldAudit{Field: field, Changed: !same(before, out.After, field), Reason: reasonRetracted, Sources: contributed})
		out.Identification.PlaceholderTitle = out.Identification.PlaceholderTitle || placeholder
		forget[field] = chain[0].ID
	}
	for _, f := range out.Fields {
		if f.Changed {
			out.Outcome, out.Applied = "changed", true
		}
	}
	if out.Applied {
		if err = writeState(ctx, tx, q, current, out.After, changedFields(out.Fields)); err != nil {
			return Operation{}, err
		}
		for field, op := range forget {
			if err = seriesmeta.ForgetEnrichmentProvenance(ctx, tx, current.ID, seriesmeta.FieldOrigin{Operation: op}, []string{field}); err != nil {
				return Operation{}, err
			}
		}
	}
	if err = save(ctx, j, out, retractDigest(in)); err != nil {
		return Operation{}, err
	}
	return out, nil
}

func retractDigest(in RetractInput) string {
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// history is a series' receipts, newest first, with the ones an undo reversed.
type history struct {
	ops    []Operation
	undone map[string]bool
}

func seriesHistory(ctx context.Context, j database.MetadataJournal, series int64) (history, error) {
	out := history{undone: map[string]bool{}}
	const page = 200
	before := int64(0)
	for {
		rows, err := j.List(ctx, series, before, page)
		if err != nil {
			return history{}, err
		}
		for _, r := range rows {
			op, err := decode(r)
			if err != nil {
				return history{}, err
			}
			if op.Kind == "undo" && op.Outcome == "changed" {
				out.undone[op.ParentID] = true
			}
			out.ops = append(out.ops, op)
		}
		if len(rows) < page {
			return out, nil
		}
		before = rows[len(rows)-1].Sequence
	}
}

// standing reports whether op is a supplementation after identification whose changes still stand:
// not a retraction, not undone and not stripped of its reversal snapshots.
func (h history) standing(op Operation) bool {
	return op.Kind == "enrich" && op.Outcome == "changed" && op.Identification != nil && op.Identification.Retracted == nil &&
		!op.UndoUnavailable && !h.undone[op.ID]
}

func changes(op Operation, field string) bool {
	return op.Outcome == "changed" && slices.ContainsFunc(op.Fields, func(f FieldAudit) bool { return f.Field == field && f.Changed })
}

func cites(op Operation, field, candidate string) bool {
	return slices.ContainsFunc(op.Fields, func(f FieldAudit) bool {
		return f.Field == field && f.Changed && slices.ContainsFunc(f.Sources, func(e metadata.FieldEvidence) bool { return e.CandidateID == candidate })
	})
}

// cited lists the evidence of candidate in every standing supplementation of field.
func (h history) cited(field, candidate string) []metadata.FieldEvidence {
	out := []metadata.FieldEvidence{}
	for _, op := range h.ops {
		if !h.standing(op) || !cites(op, field, candidate) {
			continue
		}
		for _, f := range op.Fields {
			if f.Field != field {
				continue
			}
			for _, e := range f.Sources {
				if e.CandidateID == candidate && !slices.ContainsFunc(out, func(x metadata.FieldEvidence) bool { return x.CandidateID == e.CandidateID }) {
					out = append(out, e)
				}
			}
		}
	}
	return out
}

// chain is the run of standing supplementations that led to the field's current value, newest
// first: it starts at the receipt the field's source record points at, while the current value is
// still what that receipt left, and goes back while each older change of the field is a standing
// supplementation that left exactly what the next one started from. It is empty when the field
// changed since its last supplementation.
func (h history) chain(origin string, current State, field string) []Operation {
	start := slices.IndexFunc(h.ops, func(op Operation) bool { return op.ID == origin })
	if origin == "" || start < 0 || !h.standing(h.ops[start]) || !changes(h.ops[start], field) || !same(current, h.ops[start].After, field) {
		return nil
	}
	chain := []Operation{h.ops[start]}
	for _, older := range h.ops[start+1:] {
		if !changes(older, field) {
			continue
		}
		if !h.standing(older) || !same(older.After, chain[len(chain)-1].Before, field) {
			break
		}
		chain = append(chain, older)
	}
	return chain
}
