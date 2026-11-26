package identification

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
)

// Why a field correction was not taken, beyond ProblemOwnerHolds and ProblemRequestKeyReused.
const (
	// ProblemLocked: the field is locked.
	ProblemLocked = "locked"
	// ProblemNotFromSource: the current value is not a source value nobody changed since: a person
	// edited it, ComicInfo brought it, or no source ever supplied it.
	ProblemNotFromSource = "not_from_source"
	// ProblemUnchanged: the corrected value is the current value.
	ProblemUnchanged = "unchanged"
)

// CorrectionInput corrects one field of a series whose current value a source supplied.
type CorrectionInput struct {
	RequestKey string `json:"request_key" jsonschema:"new 1-80 char ASCII key; reuse only to retry this exact call"`
	SeriesID   int64  `json:"series_id"`
	Field      string `json:"field" jsonschema:"title, summary, publisher, status or rating"`
	Value      string `json:"value" jsonschema:"the corrected value; status is ongoing, completed, hiatus or cancelled; rating is 0-10 as text like 8.5; empty clears any field but title"`
	Reason     string `json:"reason" jsonschema:"one checkable sentence, at most 1000 bytes"`
}

// Correction reports a field correction. Status is the series' identification status when the
// owner holds it.
type Correction struct {
	SeriesID int64  `json:"series_id"`
	Accepted bool   `json:"accepted"`
	Status   string `json:"status,omitempty"`
	Problem  string `json:"problem,omitempty"`
	// OperationID is the receipt of the correction, read and undone with the metadata operation tools.
	OperationID string `json:"operation_id,omitempty"`
}

// CorrectField corrects one field whose current value is still the value a source supplied, under
// current metadata_write authority re-checked in the one transaction that records it. A locked
// field and a value without such a source record, a person's edit or a ComicInfo value, are
// refused, and so is any field while the owner holds the series (awaiting_owner, needs_recheck or
// owner_reviewed). The result is kept under the request key, so a retry returns it again.
func (s *Service) CorrectField(ctx context.Context, subject delegated.Subject, in CorrectionInput) (Correction, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if !validRequestKey(in.RequestKey) || in.SeriesID <= 0 || in.Reason == "" || len(in.Reason) > maxReasonBytes || !slices.Contains(enrichment.CorrectableFields, in.Field) {
		return Correction{}, delegated.ErrInvalid
	}
	if s.enricher == nil {
		return Correction{}, delegated.ErrDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Correction{}, err
	}
	defer tx.Rollback()
	if err := authorizeSeries(ctx, tx, subject, in.SeriesID, delegated.MetadataWrite); err != nil {
		return Correction{}, err
	}
	digest := decisionDigest("correct", in)
	var result Correction
	if found, err := replayed(ctx, tx, subject, in.RequestKey, in.SeriesID, digest, &result); err != nil || found {
		return result, err
	}
	result = Correction{SeriesID: in.SeriesID}
	status, err := seriesStatus(ctx, tx, in.SeriesID)
	if err != nil {
		return Correction{}, err
	}
	if ownerHolds(status) {
		result.Problem, result.Status = ProblemOwnerHolds, status
	} else if result.Problem, err = s.correctField(ctx, tx, subject, in, &result); err != nil {
		return Correction{}, err
	}
	if err := recordDecision(ctx, tx, subject, in.RequestKey, in.SeriesID, digest, result, s.now().UnixMilli()); err != nil {
		return Correction{}, err
	}
	if err := tx.Commit(); err != nil {
		return Correction{}, err
	}
	if result.Accepted {
		s.enricher.Publish()
	}
	return result, nil
}

// correctField records the correction and returns the problem of a refusal. CorrectField refuses
// before it writes, so a refusal leaves the transaction as it was.
func (s *Service) correctField(ctx context.Context, tx *sql.Tx, subject delegated.Subject, in CorrectionInput, result *Correction) (string, error) {
	op, err := s.enricher.CorrectField(ctx, tx, subject, enrichment.CorrectionInput{RequestKey: correctionReceiptKey(subject, "correct", in.RequestKey, in.SeriesID),
		SeriesID: in.SeriesID, Field: in.Field, Value: in.Value, Reason: in.Reason})
	switch {
	case errors.Is(err, enrichment.ErrFieldLocked):
		return ProblemLocked, nil
	case errors.Is(err, enrichment.ErrNotSourced):
		return ProblemNotFromSource, nil
	case errors.Is(err, enrichment.ErrUnchanged):
		return ProblemUnchanged, nil
	case err != nil:
		return "", err
	}
	result.Accepted, result.OperationID = true, op.ID
	return "", nil
}

// ownerHolds reports whether a status is the owner's turn or the owner's decision.
func ownerHolds(status string) bool {
	return status == StatusAwaitingOwner || status == StatusNeedsRecheck || status == StatusOwnerReviewed
}
