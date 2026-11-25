package enrichment

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/organize"
)

// relationKind marks receipts whose reversible change is one franchise relation, not series fields.
const relationKind = "relation"

// RelationInput adds one franchise relation: the target series is the series' RelationType.
type RelationInput struct {
	RequestKey     string `json:"request_key"`
	SeriesID       int64  `json:"series_id"`
	TargetSeriesID int64  `json:"target_series_id"`
	RelationType   string `json:"relation_type"`
	Reason         string `json:"reason"`
}

// RelationChange is the relation a receipt added, found already present, or removed. Its direction is
// the stored one, which for an already present relation may point back at the receipt's series.
type RelationChange struct {
	ID             int64  `json:"id"`
	SourceSeriesID int64  `json:"source_series_id"`
	TargetSeriesID int64  `json:"target_series_id"`
	RelationType   string `json:"relation_type"`
}

// WithRelations schedules the franchise rebuild after a relation receipt commits. The pending mark
// written in the same transaction keeps the repair responsibility without it (ADR 0012).
func (s *Service) WithRelations(changed func()) *Service { s.relationsChanged = changed; return s }

// AddRelation adds a relation between two series the actor may write, with its reason and receipt in
// the same transaction. A relation already present between them in either direction is never edited:
// the receipt reports it as no-change and leaves nothing to undo.
func (s *Service) AddRelation(ctx context.Context, actor delegated.Subject, in RelationInput) (Operation, error) {
	if !validKey(in.RequestKey) || in.SeriesID <= 0 || in.TargetSeriesID <= 0 || in.SeriesID == in.TargetSeriesID || !slices.Contains(organize.RelationTypes, in.RelationType) || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 1000 {
		return Operation{}, delegated.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Operation{}, err
	}
	defer tx.Rollback()
	q := database.New(tx)
	j := database.MetadataJournal{Tx: tx}
	current, before, err := readState(ctx, q, in.SeriesID)
	if err != nil {
		return Operation{}, delegated.ErrDenied
	}
	if err = authorize(ctx, tx, actor, current.LibraryID, true); err != nil {
		return Operation{}, err
	}
	target, err := q.GetSeries(ctx, in.TargetSeriesID)
	if err != nil {
		return Operation{}, delegated.ErrDenied
	}
	if err = authorize(ctx, tx, actor, target.LibraryID, true); err != nil {
		return Operation{}, err
	}
	digest := relationDigest(in)
	if out, found, e := retry(ctx, j, actor, in.RequestKey, digest); e != nil || found {
		return out, e
	}
	out := operation(actor, in.RequestKey, current.ID, current.LibraryID, relationKind)
	out.Reason = in.Reason
	out.Before = before
	out.After = before
	var relation database.GetSeriesRelationRow
	created := false
	failed, err := j.TryChange(ctx, func() error {
		added, e := organize.AddRelationTx(ctx, q, in.SeriesID, in.TargetSeriesID, in.RelationType)
		if e != nil {
			return e
		}
		created = !added.Existing
		relation, e = q.GetSeriesRelation(ctx, added.ID)
		return e
	})
	if err != nil {
		return Operation{}, err
	}
	switch {
	case failed:
		out.Outcome = "failed"
		out.Reason = "relation write failed; nothing changed"
	case created:
		out.Outcome = "changed"
		out.Applied = true
	}
	if !failed {
		out.Relation = &RelationChange{ID: relation.ID, SourceSeriesID: relation.SourceSeriesID, TargetSeriesID: relation.TargetSeriesID, RelationType: relation.RelationType}
	}
	if err = save(ctx, j, out, digest); err != nil {
		return Operation{}, err
	}
	if err = tx.Commit(); err != nil {
		return Operation{}, err
	}
	if out.Applied {
		s.relationChanged()
	}
	return out, nil
}

// undoRelation removes only the relation the original receipt added, and only while it is unchanged.
// Both ends must still be writable; an end deleted since took the relation with it.
func (s *Service) undoRelation(ctx context.Context, tx *sql.Tx, actor delegated.Subject, in UndoInput, original Operation) (Operation, error) {
	q := database.New(tx)
	j := database.MetadataJournal{Tx: tx}
	current, before, err := readState(ctx, q, original.SeriesID)
	if err != nil {
		return Operation{}, delegated.ErrDenied
	}
	if err = authorize(ctx, tx, actor, current.LibraryID, true); err != nil {
		return Operation{}, err
	}
	added := original.Relation
	if added != nil {
		other := added.TargetSeriesID
		if other == current.ID {
			other = added.SourceSeriesID
		}
		end, e := q.GetSeries(ctx, other)
		switch {
		case e == nil:
			if err = authorize(ctx, tx, actor, end.LibraryID, true); err != nil {
				return Operation{}, err
			}
		case !errors.Is(e, sql.ErrNoRows):
			return Operation{}, e
		}
	}
	digest := undoDigest(in)
	if out, found, e := retry(ctx, j, actor, in.RequestKey, digest); e != nil || found {
		return out, e
	}
	out := operation(actor, in.RequestKey, current.ID, current.LibraryID, "undo")
	out.ParentID = original.ID
	out.Before = before
	out.After = before
	out.Relation = added
	undone, err := j.UndoneBy(ctx, original.ID)
	if err != nil {
		return Operation{}, err
	}
	switch {
	case original.UndoUnavailable:
		out.Outcome = "unavailable"
		out.Reason = "rollback material explicitly cleaned; recovery unavailable"
	case original.Outcome != "changed" || undone != "" || added == nil:
		out.Reason = "operation has no remaining reversible change"
	default:
		removed := false
		failed, e := j.TryChange(ctx, func() error {
			var err error
			removed, err = organize.RemoveUnchangedRelationTx(ctx, q, database.GetSeriesRelationRow{ID: added.ID, SourceSeriesID: added.SourceSeriesID, TargetSeriesID: added.TargetSeriesID, RelationType: added.RelationType})
			return err
		})
		if e != nil {
			return Operation{}, e
		}
		switch {
		case failed:
			out.Outcome = "failed"
			out.Reason = "relation undo write failed; nothing changed"
		case removed:
			out.Outcome = "changed"
			out.Applied = true
		default:
			out.Outcome = "conflict"
			out.Reason = "relation was edited or removed after this operation"
		}
	}
	if err = save(ctx, j, out, digest); err != nil {
		return Operation{}, err
	}
	if err = tx.Commit(); err != nil {
		return Operation{}, err
	}
	if out.Applied {
		s.relationChanged()
	}
	return out, nil
}

func (s *Service) relationChanged() {
	if s.relationsChanged != nil {
		s.relationsChanged()
	}
	if s.publish != nil {
		s.publish()
	}
}
