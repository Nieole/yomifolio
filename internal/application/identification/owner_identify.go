package identification

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/metadata"
)

var (
	// ErrNotIdentifiable means the pick names no entry of an identification source.
	ErrNotIdentifiable = errors.New("not an identification source entry")
	// ErrExcludedEntry means the picked entry is an excluded candidate of the series.
	ErrExcludedEntry = errors.New("entry is an excluded candidate of the series")
)

// OwnerIdentifyInput is the owner's pick of one source entry for a series, as a manual search of
// that source listed it.
type OwnerIdentifyInput struct {
	SeriesID int64
	// Source is the searched source, by its source key or a provider name SourceKey maps.
	Source string
	// Result is the search result picked; it is the entry's source snapshot as well.
	Result metadata.SeriesMetadata
}

// OwnerIdentification reports the owner's identification of a series with one source entry.
type OwnerIdentification struct {
	// Identification is the series' identification after the change.
	Identification SeriesIdentification `json:"identification"`
	// Replaced is the conclusion of the same source that named another entry; that entry is now an
	// excluded candidate. Absent when the source had no other entry.
	Replaced *Conclusion `json:"replaced,omitempty"`
	// RetractionID is the receipt that took back what the replaced entry supplemented, and
	// OperationID the receipt of the supplementation that followed. Both are empty on an
	// owner-reviewed series, whose metadata the identification flow leaves alone.
	RetractionID string `json:"retraction_id,omitempty"`
	OperationID  string `json:"operation_id,omitempty"`
}

// IdentifyAsOwner records the owner's pick as an owner-confirmed identification of the series and
// supplements its metadata by the identification policy, in one transaction. Picking another entry
// of a source the series is identified with replaces that identification: the old entry becomes an
// excluded candidate and what it supplemented, untouched since, is taken back first. Picking the
// entry the series is already identified with confirms it again as the owner's. The owner's
// identification settles the open identification and recheck escalations of the series and ends
// any claim lease on it; notes stay open. An excluded candidate cannot be picked.
func (s *Service) IdentifyAsOwner(ctx context.Context, actor int64, in OwnerIdentifyInput) (OwnerIdentification, error) {
	source, known := SourceKey(in.Source)
	facts, valid := metadata.Snapshot(source, &in.Result)
	if !known || !valid || in.SeriesID <= 0 {
		return OwnerIdentification{}, ErrNotIdentifiable
	}
	entry, ok := EntryOf(source, int64(facts.SourceID), facts.SourceURL)
	if !ok {
		return OwnerIdentification{}, ErrNotIdentifiable
	}
	id := candidateID(source, entry.ID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OwnerIdentification{}, err
	}
	defer tx.Rollback()
	if err := requireAdmin(ctx, tx, actor); err != nil {
		return OwnerIdentification{}, err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM series WHERE id = ?)`, in.SeriesID).Scan(&exists); err != nil {
		return OwnerIdentification{}, err
	}
	if !exists {
		return OwnerIdentification{}, ErrSeriesNotFound
	}
	excluded, err := excludedIDs(ctx, tx, in.SeriesID)
	if err != nil {
		return OwnerIdentification{}, err
	}
	if excluded[id] {
		return OwnerIdentification{}, ErrExcludedEntry
	}
	cited := citedCandidate{EvidenceCandidate: EvidenceCandidate{ID: id, Source: source, EntryURL: entry.URL, Title: cut(facts.Title, textRunes), OriginalTitle: cut(facts.OriginalTitle, textRunes)}, facts: facts}
	out, err := s.identifyByOwner(ctx, tx, actor, in.SeriesID, cited)
	if err != nil {
		return OwnerIdentification{}, err
	}
	if err := tx.Commit(); err != nil {
		return OwnerIdentification{}, err
	}
	if out.OperationID != "" && s.enricher != nil {
		s.enricher.Publish()
	}
	return out, nil
}

// identifyByOwner records an owner-confirmed identification of the series with one candidate in
// the caller's transaction: it replaces what the series is identified with for the candidate's
// source, excludes a replaced entry, settles the identification and recheck escalations, drops a
// not-found verdict and any claim lease, and reads the result. The caller has checked the actor
// and that the candidate is not excluded.
func (s *Service) identifyByOwner(ctx context.Context, tx *sql.Tx, actor, seriesID int64, cited citedCandidate) (OwnerIdentification, error) {
	subject := delegated.Subject{UserID: actor}
	now := s.now().UnixMilli()
	done, err := s.replace(ctx, tx, subject, ConfirmedByOwner, ownerReceiptKey(), seriesID, []citedCandidate{cited}, "")
	if err != nil {
		return OwnerIdentification{}, err
	}
	var out OwnerIdentification
	for i, old := range done.Withdrawn {
		replaced := old
		out.Replaced = &replaced
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO identification_exclusions (series_id, source, entry_id, entry_url, title, user_id, excluded_at_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, seriesID, old.Source, old.EntryID, old.EntryURL, old.Title, actor, now); err != nil {
			return OwnerIdentification{}, err
		}
		if i < len(done.Retractions) {
			out.RetractionID = done.Retractions[i].ID
		}
	}
	out.OperationID = done.Supplement.ID
	if _, err := tx.ExecContext(ctx, `UPDATE identification_escalations SET resolved_at_ms = ?, resolved_by = ?, resolution = 'identified'
		WHERE series_id = ? AND resolved_at_ms IS NULL AND class IN (?, ?)`, now, actor, seriesID, EscalationIdentification, EscalationRecheck); err != nil {
		return OwnerIdentification{}, err
	}
	for _, table := range []string{"identification_not_found", "identification_leases"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE series_id = ?`, seriesID); err != nil {
			return OwnerIdentification{}, err
		}
	}
	out.Identification, err = read(ctx, tx, seriesID)
	return out, err
}

// ownerReceiptKey names the receipt of one owner identification. The web has no request keys, so
// every pick is a new one.
func ownerReceiptKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "owner-identify-" + hex.EncodeToString(b)
}
