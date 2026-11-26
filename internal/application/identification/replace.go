package identification

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
)

// replacement is what replace did to a series.
type replacement struct {
	// Withdrawn are the conclusions of the cited sources that named other entries.
	Withdrawn []Conclusion
	// Retractions are the receipts that took back what each withdrawn entry supplemented, in the
	// order of Withdrawn; Supplement is the supplementation from the series' identifications after
	// the change. All are zero on an owner-reviewed series.
	Retractions []enrichment.Operation
	Supplement  enrichment.Operation
}

// replace identifies the series with the cited candidates in place of what it is identified with
// for their sources, all in the caller's transaction. A cited source identified with another entry
// is withdrawn first: its conclusion goes, and so does every field that entry supplemented and
// nobody changed since, under a receipt of its own. A cited entry the series is already identified
// with is confirmed again, by this confirmer. confirm then records the cited candidates and
// supplements the series from all its identifications, so values the remaining sources supply come
// back. It is the one replacement use case of agents and the owner: the caller has decided this
// confirmer may take the place of those conclusions, and decides what becomes of the withdrawn
// entries. On an owner-reviewed series only the conclusions change. receiptKey names the
// supplementation receipt and must be new for actor; each retraction's key derives from it.
func (s *Service) replace(ctx context.Context, tx *sql.Tx, actor delegated.Subject, confirmer, receiptKey string, seriesID int64, cited []citedCandidate, reason string) (replacement, error) {
	var out replacement
	current, err := conclusions(ctx, tx, seriesID)
	if err != nil {
		return out, err
	}
	status, err := seriesStatus(ctx, tx, seriesID)
	if err != nil {
		return out, err
	}
	for _, c := range cited {
		_, entry, _ := strings.Cut(c.ID, ":")
		for _, old := range current {
			if old.Source != c.Source {
				continue
			}
			if old.EntryID == entry {
				if _, err := tx.ExecContext(ctx, `DELETE FROM identification_conclusions WHERE series_id = ? AND source = ?`, seriesID, c.Source); err != nil {
					return out, err
				}
				continue
			}
			op, err := s.withdraw(ctx, tx, actor, retractionKey(receiptKey, old.Source), seriesID, status, old, reason)
			if err != nil {
				return out, err
			}
			out.Withdrawn = append(out.Withdrawn, old)
			if op.ID != "" {
				out.Retractions = append(out.Retractions, op)
			}
		}
	}
	out.Supplement, err = s.confirm(ctx, tx, actor, confirmer, receiptKey, seriesID, cited, reason)
	return out, err
}

// withdraw removes the series' conclusion old and takes back, under the receipt receiptKey, what
// its entry supplemented and nobody changed since, in the caller's transaction. Values other
// identified sources supply come back only from a supplementation the caller runs next. On an
// owner-reviewed series, or without an enricher, only the conclusion goes and the receipt is zero.
func (s *Service) withdraw(ctx context.Context, tx *sql.Tx, actor delegated.Subject, receiptKey string, seriesID int64, status string, old Conclusion, reason string) (enrichment.Operation, error) {
	if _, err := tx.ExecContext(ctx, `DELETE FROM identification_conclusions WHERE series_id = ? AND source = ?`, seriesID, old.Source); err != nil {
		return enrichment.Operation{}, err
	}
	if s.enricher == nil || status == StatusOwnerReviewed {
		return enrichment.Operation{}, nil
	}
	return s.enricher.RetractIdentified(ctx, tx, actor, enrichment.RetractInput{RequestKey: receiptKey, SeriesID: seriesID,
		Entry: enrichment.IdentifiedEntry{CandidateID: candidateID(old.Source, old.EntryID), EntryURL: old.EntryURL}, Reason: reason})
}

// retractionKey names the retraction receipt of one withdrawn source inside the replacement whose
// supplementation receipt is receiptKey.
func retractionKey(receiptKey, source string) string {
	sum := sha256.Sum256([]byte(receiptKey + "\x00" + source))
	return "retract-" + hex.EncodeToString(sum[:16])
}
