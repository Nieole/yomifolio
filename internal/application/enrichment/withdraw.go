package enrichment

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"slices"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/seriesmeta"
)

// WithdrawInput asks to take back what supplementation after identification brought while the
// series was identified with some entries.
type WithdrawInput struct {
	// RequestKey derives the key of every withdrawal receipt and must be new for the actor.
	RequestKey string
	SeriesID   int64
	// Entries are the candidate IDs of the conclusions taken back.
	Entries []string
	// Reason is the withdrawal's reason, kept on its receipts.
	Reason string
}

// Field audit reasons of a withdrawal.
const (
	reasonWithdrawn = "withdrawn with the identification it came from"
	reasonChanged   = "changed or locked since the identification; kept"
)

// WithdrawIdentified takes back, inside the caller's transaction, what supplementation after
// identification brought while the series rested on any of the withdrawn entries. Every standing
// receipt of such a supplementation, one not undone, is undone field by field, newest first, under
// the expected-value check of Undo: a field returns to its value before that receipt only while it
// still holds the receipt's after-value and is not locked, the replaced placeholder title excepted;
// a field someone changed since keeps its value. Withdrawn fields lose the source records the
// receipt wrote. Each undone receipt gets its own undo receipt, returned newest first; one whose
// every field was changed since is left standing.
//
// It rechecks current metadata_write authority. An error means the caller must roll back; after
// its commit the caller calls Publish.
func (s *Service) WithdrawIdentified(ctx context.Context, tx *sql.Tx, actor delegated.Subject, in WithdrawInput) ([]Operation, error) {
	if !validKey(in.RequestKey) || in.SeriesID <= 0 || len(in.Entries) == 0 {
		return nil, delegated.ErrInvalid
	}
	q := database.New(tx)
	j := database.MetadataJournal{Tx: tx}
	current, _, err := readState(ctx, q, in.SeriesID)
	if err != nil {
		return nil, delegated.ErrDenied
	}
	if err = authorize(ctx, tx, actor, current.LibraryID, true); err != nil {
		return nil, err
	}
	standing, err := identifiedReceipts(ctx, j, in.SeriesID, in.Entries)
	if err != nil {
		return nil, err
	}
	out := []Operation{}
	for _, original := range standing {
		current, before, err := readState(ctx, q, in.SeriesID)
		if err != nil {
			return nil, err
		}
		key, digest := withdrawalKey(in.RequestKey, original.ID)
		op := operation(actor, key, current.ID, current.LibraryID, "undo")
		op.ParentID, op.Before, op.After, op.Reason = original.ID, before, before, in.Reason
		for _, field := range original.Fields {
			if !field.Changed || field.Field == "cover" {
				continue
			}
			audit := FieldAudit{Field: field.Field, Reason: reasonChanged, Sources: field.Sources}
			placeholder := field.Field == "title" && original.Identification.PlaceholderTitle
			if (!seriesmeta.EnrichmentLocked(current, field.Field) || placeholder) && same(before, original.After, field.Field) {
				copyField(&op.After, original.Before, field.Field)
				audit.Reason, audit.Changed = reasonWithdrawn, !same(before, op.After, field.Field)
			}
			op.Fields = append(op.Fields, audit)
		}
		withdrawn := changedFields(op.Fields)
		if len(withdrawn) > 0 {
			op.Outcome, op.Applied = "changed", true
			if err := writeState(ctx, tx, q, current, op.After, withdrawn); err != nil {
				return nil, err
			}
			if err := seriesmeta.ForgetEnrichmentProvenance(ctx, tx, current.ID, seriesmeta.FieldOrigin{Operation: original.ID}, withdrawn); err != nil {
				return nil, err
			}
		}
		if err := save(ctx, j, op, digest); err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, nil
}

// identifiedReceipts lists, newest first, the standing receipts of supplementation after
// identification on the series that were computed with any of the entries.
func identifiedReceipts(ctx context.Context, j database.MetadataJournal, series int64, entries []string) ([]Operation, error) {
	var out []Operation
	for before := int64(0); ; {
		rows, err := j.List(ctx, series, before, 100)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			op, err := decode(r)
			if err != nil {
				return nil, err
			}
			if op.Kind != "enrich" || op.Identification == nil || op.Outcome != "changed" || op.UndoUnavailable ||
				!slices.ContainsFunc(op.Identification.Entries, func(e IdentifiedEntry) bool { return slices.Contains(entries, e.CandidateID) }) {
				continue
			}
			if undone, err := j.UndoneBy(ctx, op.ID); err != nil || undone != "" {
				if err != nil {
					return nil, err
				}
				continue
			}
			out = append(out, op)
		}
		if len(rows) < 100 {
			return out, nil
		}
		before = rows[len(rows)-1].Sequence
	}
}

// withdrawalKey names the receipt that withdraws one original receipt under one request, and
// fingerprints that request.
func withdrawalKey(key, original string) (string, string) {
	sum := sha256.Sum256([]byte(key + "\x00" + original))
	return "withdraw-" + hex.EncodeToString(sum[:16]), hex.EncodeToString(sum[:])
}
