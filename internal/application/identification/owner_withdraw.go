package identification

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
)

// ErrNoConclusion means the series is not identified with the named source.
var ErrNoConclusion = errors.New("series is not identified with that source")

// OwnerWithdrawal reports the owner's withdrawal of one source's identification of a series.
type OwnerWithdrawal struct {
	// Identification is the series' identification after the change.
	Identification SeriesIdentification `json:"identification"`
	// Withdrawn is the withdrawn conclusion; its entry is now an excluded candidate.
	Withdrawn Conclusion `json:"withdrawn"`
	// RetractionID is the receipt that took back what the entry supplemented, and OperationID the
	// receipt of supplementing the series again from the sources it stays identified with. Both are
	// empty on an owner-reviewed series, and OperationID also when no source remains.
	RetractionID string `json:"retraction_id,omitempty"`
	OperationID  string `json:"operation_id,omitempty"`
}

// WithdrawAsOwner withdraws the series' identification with one source, in one transaction: the
// entry becomes an excluded candidate, what it supplemented and nobody changed since is taken back,
// and the series is supplemented again from the sources it stays identified with. A series left
// with no identification is unprocessed again, so the agents take it up anew without that entry.
// The owner's decision settles the open identification and recheck escalations; notes stay open.
// An owner-reviewed series keeps its metadata as it is.
func (s *Service) WithdrawAsOwner(ctx context.Context, actor, seriesID int64, source string) (OwnerWithdrawal, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OwnerWithdrawal{}, err
	}
	defer tx.Rollback()
	if err := requireAdmin(ctx, tx, actor); err != nil {
		return OwnerWithdrawal{}, err
	}
	status, err := seriesStatus(ctx, tx, seriesID)
	if errors.Is(err, sql.ErrNoRows) {
		return OwnerWithdrawal{}, ErrSeriesNotFound
	}
	if err != nil {
		return OwnerWithdrawal{}, err
	}
	current, err := conclusions(ctx, tx, seriesID)
	if err != nil {
		return OwnerWithdrawal{}, err
	}
	key, _ := SourceKey(source)
	index := slices.IndexFunc(current, func(c Conclusion) bool { return c.Source == key })
	if index < 0 {
		return OwnerWithdrawal{}, ErrNoConclusion
	}
	out := OwnerWithdrawal{Withdrawn: current[index]}
	subject := delegated.Subject{UserID: actor}
	now := s.now().UnixMilli()
	receiptKey := ownerReceiptKey()
	retraction, err := s.withdraw(ctx, tx, subject, retractionKey(receiptKey, key), seriesID, status, out.Withdrawn, "")
	if err != nil {
		return OwnerWithdrawal{}, err
	}
	out.RetractionID = retraction.ID
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO identification_exclusions (series_id, source, entry_id, entry_url, title, user_id, excluded_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, seriesID, out.Withdrawn.Source, out.Withdrawn.EntryID, out.Withdrawn.EntryURL, out.Withdrawn.Title, actor, now); err != nil {
		return OwnerWithdrawal{}, err
	}
	if status != StatusOwnerReviewed {
		remaining, err := s.identifiedSources(ctx, tx, seriesID)
		if err != nil {
			return OwnerWithdrawal{}, err
		}
		if len(remaining) > 0 {
			op, err := s.enricher.EnrichIdentified(ctx, tx, subject, enrichment.IdentifiedInput{RequestKey: receiptKey, SeriesID: seriesID, Sources: remaining})
			if err != nil {
				return OwnerWithdrawal{}, err
			}
			out.OperationID = op.ID
		}
	}
	if err := settleEscalations(ctx, tx, actor, seriesID, now); err != nil {
		return OwnerWithdrawal{}, err
	}
	if out.Identification, err = read(ctx, tx, seriesID); err != nil {
		return OwnerWithdrawal{}, err
	}
	if err := tx.Commit(); err != nil {
		return OwnerWithdrawal{}, err
	}
	if out.RetractionID != "" || out.OperationID != "" {
		s.enricher.Publish()
	}
	return out, nil
}
