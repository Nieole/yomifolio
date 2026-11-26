package enrichment

import (
	"context"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/config"
	"yomifolio/internal/covermaterial"
	"yomifolio/internal/database"
	"yomifolio/internal/seriesmeta"
)

// Get rechecks current authority on the actual target, including for historical receipts.
func (s *Service) Get(ctx context.Context, actor delegated.Subject, id string) (Operation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Operation{}, err
	}
	defer tx.Rollback()
	j := database.MetadataJournal{Tx: tx}
	r, err := j.Get(ctx, id)
	if err != nil {
		return Operation{}, delegated.ErrDenied
	}
	current, err := database.New(tx).GetSeries(ctx, r.SeriesID)
	if err != nil {
		if actor.CredentialID != "" || database.RequireMetadataAdmin(ctx, tx, actor.UserID) != nil {
			return Operation{}, delegated.ErrDenied
		}
	} else if err = authorize(ctx, tx, actor, current.LibraryID, false); err != nil {
		return Operation{}, err
	}
	out, err := decode(r)
	if err != nil {
		return out, err
	}
	out.Applied = false
	out.UndoneBy, err = j.UndoneBy(ctx, id)
	return out, err
}

// List uses descending persistent sequence pages. Only web administrators may omit
// the series; delegated callers must select a currently authorized actual series.
func (s *Service) List(ctx context.Context, actor delegated.Subject, series, before int64, limit int) (Page, error) {
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 50 || before < 0 || series < 0 {
		return Page{}, delegated.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Page{}, err
	}
	defer tx.Rollback()
	if series == 0 {
		if actor.CredentialID != "" || database.RequireMetadataAdmin(ctx, tx, actor.UserID) != nil {
			return Page{}, delegated.ErrDenied
		}
	} else {
		current, e := database.New(tx).GetSeries(ctx, series)
		if e != nil {
			return Page{}, delegated.ErrDenied
		}
		if err = authorize(ctx, tx, actor, current.LibraryID, false); err != nil {
			return Page{}, err
		}
	}
	j := database.MetadataJournal{Tx: tx}
	rows, err := j.List(ctx, series, before, limit+1)
	if err != nil {
		return Page{}, err
	}
	out := Page{Operations: []Operation{}}
	if len(rows) > limit {
		out.NextBefore = rows[limit-1].Sequence
		rows = rows[:limit]
	}
	size := 0
	for _, r := range rows {
		// Bounded pages may stop earlier than the row limit, without truncating a receipt.
		if size+len(r.Payload) > 220<<10 {
			if len(out.Operations) == 0 {
				return Page{}, delegated.ErrInvalid
			}
			out.NextBefore = rows[len(out.Operations)-1].Sequence
			break
		}
		op, e := decode(r)
		if e != nil {
			return Page{}, e
		}
		op.Applied = false
		op.UndoneBy, e = j.UndoneBy(ctx, op.ID)
		if e != nil {
			return Page{}, e
		}
		out.Operations = append(out.Operations, op)
		size += len(r.Payload)
	}
	return out, nil
}

// Undo restores only fields actually changed by the original operation. Current locks,
// authorization and exact after-values (whole tag/link sets) are checked in this same
// transaction; unrelated changes survive. The reversal is itself an immutable receipt.
func (s *Service) Undo(ctx context.Context, actor delegated.Subject, in UndoInput) (Operation, error) {
	if !validKey(in.RequestKey) || in.OperationID == "" {
		return Operation{}, delegated.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Operation{}, err
	}
	defer tx.Rollback()
	q := database.New(tx)
	j := database.MetadataJournal{Tx: tx}
	receipt, err := j.Get(ctx, in.OperationID)
	if err != nil {
		return Operation{}, delegated.ErrDenied
	}
	original, err := decode(receipt)
	if err != nil {
		return Operation{}, err
	}
	if original.Kind == relationKind {
		return s.undoRelation(ctx, tx, actor, in, original)
	}
	current, before, err := readState(ctx, q, original.SeriesID)
	if err != nil {
		return Operation{}, delegated.ErrDenied
	}
	if err = authorize(ctx, tx, actor, current.LibraryID, true); err != nil {
		return Operation{}, err
	}
	digest := undoDigest(in)
	if out, found, e := retry(ctx, j, actor, in.RequestKey, digest); e != nil || found {
		return out, e
	}
	out := operation(actor, in.RequestKey, current.ID, current.LibraryID, "undo")
	out.ParentID = original.ID
	out.Before = before
	out.After = before
	undone, err := j.UndoneBy(ctx, original.ID)
	if err != nil {
		return Operation{}, err
	}
	if original.UndoUnavailable {
		out.Outcome = "unavailable"
		out.Reason = "rollback material explicitly cleaned; recovery unavailable"
	} else if original.Kind != "enrich" || original.Outcome != "changed" || undone != "" {
		out.Reason = "operation has no remaining reversible change"
	} else {
		for _, field := range original.Fields {
			if !field.Changed {
				continue
			}
			out.Fields = append(out.Fields, field)
			// A placeholder title was replaced under its lock, so restoring it is not stopped by that lock.
			placeholder := field.Field == "title" && original.Identification != nil && original.Identification.PlaceholderTitle
			if (seriesmeta.EnrichmentLocked(current, field.Field) && !placeholder) || !same(before, original.After, field.Field) || (field.Field == "cover" && (before.Cover == nil || before.Cover.Locked)) {
				out.Outcome = "conflict"
				out.Reason = "current field or lock differs from operation after-state"
			}
			copyField(&out.After, original.Before, field.Field)
			if field.Field == "cover" {
				if original.OldCoverMaterial == "" {
					return Operation{}, delegated.ErrInvalid
				}
				restored := *original.Before.Cover
				restored.Path = original.OldCoverMaterial
				restored.Automatic = true
				out.After.Cover = &restored
				out.OldCoverMaterial = original.OldCoverMaterial
				out.NewCoverMaterial = original.NewCoverMaterial
				if s.covers == nil {
					return Operation{}, delegated.ErrInvalid
				}
				if _, e := covermaterial.Read(ctx, config.ThumbnailDir(s.covers.Config()), restored.Path); e != nil {
					return Operation{}, e
				}
			}
		}
		if out.Outcome != "conflict" {
			out.Outcome = "changed"
			out.Applied = true
		}
	}
	if !out.Applied {
		out.After = before
		for i := range out.Fields {
			out.Fields[i].Changed = false
		}
	}
	if out.Applied {
		// Keep the receipt in the same authorized transaction. A second transaction
		// could race a successful undo or observe different authority/state.
		failed, e := j.TryChange(ctx, func() error {
			if err := writeState(ctx, tx, q, current, out.After, changedFields(out.Fields)); err != nil {
				return err
			}
			origin := seriesmeta.FieldOrigin{Operation: original.ID}
			if original.Adjudication != nil {
				origin.Proposal = original.Adjudication.ProposalID
			}
			if err := seriesmeta.ForgetEnrichmentProvenance(ctx, tx, current.ID, origin, changedFields(out.Fields)); err != nil {
				return err
			}
			if c := original.Correction; c != nil {
				// The corrected value replaced a source value, so the restored value is one again.
				r := c.Replaced
				return seriesmeta.RestoreFieldProvenance(ctx, tx, current.ID, c.Field, seriesmeta.FieldRecord{Value: r.Value, Source: r.Source, SourceURL: r.SourceURL,
					Confidence: r.Confidence, Origin: seriesmeta.FieldOrigin{Operation: r.OperationID, Proposal: r.ProposalID}})
			}
			return nil
		})
		if e != nil {
			return Operation{}, e
		}
		if failed {
			out.Applied = false
			out.Outcome = "failed"
			out.Reason = "metadata undo write failed; nothing changed"
			out.After = out.Before
			for i := range out.Fields {
				out.Fields[i].Changed = false
			}
		}
	}
	if err = save(ctx, j, out, digest); err != nil {
		return Operation{}, err
	}
	if err = tx.Commit(); err != nil {
		return Operation{}, err
	}
	if out.Applied && s.publish != nil {
		s.publish()
	}
	return out, nil
}
