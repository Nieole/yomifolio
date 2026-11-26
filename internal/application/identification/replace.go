package identification

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
)

// Why a correction was not taken, beyond the problems a verdict shares. Every rejection leaves the
// series as it was.
const (
	// ProblemOwnerHolds: the series is the owner's turn or keeps the owner's decision
	// (awaiting_owner, needs_recheck or owner_reviewed), so the agent leaves it alone.
	ProblemOwnerHolds = "owner_holds"
	// ProblemNotConcluded: a withdrawn ID is not one of the series' identifications.
	ProblemNotConcluded = "not_concluded"
	// ProblemOwnerConfirmed: a withdrawn identification was confirmed by a person.
	ProblemOwnerConfirmed = "owner_confirmed"
	// ProblemSourceTaken: a cited candidate's source keeps an identification this request does not
	// withdraw.
	ProblemSourceTaken = "source_taken"
)

// ReplaceInput takes back some of an agent's identifications of a series and identifies it with
// evidence candidates instead.
type ReplaceInput struct {
	RequestKey   string   `json:"request_key" jsonschema:"new 1-80 char ASCII key; reuse only to retry this exact call"`
	SeriesID     int64    `json:"series_id"`
	Withdraw     []string `json:"withdraw" jsonschema:"1-5 agent identifications of the series to take back, as source:entry_id"`
	CandidateIDs []string `json:"candidate_ids" jsonschema:"1-5 ids of the series' evidence candidates it is instead, at most one per source"`
	Reason       string   `json:"reason" jsonschema:"one checkable sentence, at most 1000 bytes"`
}

// Replacement reports a replacement. Status is the series' identification status after an
// accepted replacement, or its current status when the owner holds it.
type Replacement struct {
	SeriesID int64  `json:"series_id"`
	Accepted bool   `json:"accepted"`
	Status   string `json:"status,omitempty"`
	Problem  string `json:"problem,omitempty"`
	// WithdrawalIDs are the receipts that took back what the withdrawn identifications supplemented.
	WithdrawalIDs []string `json:"withdrawal_ids,omitempty"`
	// OperationID is the receipt of the supplementation by the new identification.
	OperationID string `json:"operation_id,omitempty"`
}

// Replace corrects identifications an agent made: it takes back the withdrawn ones and identifies
// the series with the cited evidence candidates instead, under current metadata_write authority
// re-checked in the one transaction that records it. Only an identified series is corrected, and
// only identifications an agent confirmed are withdrawn; a person's identification is the owner's
// to change. The result is kept under the request key, so a retry returns it again.
func (s *Service) Replace(ctx context.Context, subject delegated.Subject, in ReplaceInput) (Replacement, error) {
	in, ok := replaceShape(in)
	if !ok {
		return Replacement{}, delegated.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Replacement{}, err
	}
	defer tx.Rollback()
	if err := authorizeSeries(ctx, tx, subject, in.SeriesID, delegated.MetadataWrite); err != nil {
		return Replacement{}, err
	}
	digest := decisionDigest("replace", in)
	var result Replacement
	if found, err := replayed(ctx, tx, subject, in.RequestKey, in.SeriesID, digest, &result); err != nil || found {
		return result, err
	}
	if result, err = s.judgeReplacement(ctx, tx, subject, in); err != nil {
		return Replacement{}, err
	}
	if err := recordDecision(ctx, tx, subject, in.RequestKey, in.SeriesID, digest, result, s.now().UnixMilli()); err != nil {
		return Replacement{}, err
	}
	if err := tx.Commit(); err != nil {
		return Replacement{}, err
	}
	if result.Accepted && s.enricher != nil {
		s.enricher.Publish()
	}
	return result, nil
}

func replaceShape(in ReplaceInput) (ReplaceInput, bool) {
	in.Reason = strings.TrimSpace(in.Reason)
	if !validRequestKey(in.RequestKey) || in.SeriesID <= 0 || in.Reason == "" || len(in.Reason) > maxReasonBytes {
		return in, false
	}
	return in, wellFormedIDs(in.Withdraw) && wellFormedIDs(in.CandidateIDs) &&
		!slices.ContainsFunc(in.CandidateIDs, func(id string) bool { return slices.Contains(in.Withdraw, id) })
}

// wellFormedIDs checks 1-5 source:entry IDs of known sources, at most one per source.
func wellFormedIDs(ids []string) bool {
	if len(ids) == 0 || len(ids) > len(Sources) {
		return false
	}
	var sources []string
	for _, id := range ids {
		source, entry, ok := strings.Cut(id, ":")
		if !ok || entry == "" || !slices.Contains(Sources, source) || slices.Contains(sources, source) {
			return false
		}
		sources = append(sources, source)
	}
	return true
}

func (s *Service) judgeReplacement(ctx context.Context, tx *sql.Tx, subject delegated.Subject, in ReplaceInput) (Replacement, error) {
	result := Replacement{SeriesID: in.SeriesID}
	status, err := seriesStatus(ctx, tx, in.SeriesID)
	if err != nil {
		return Replacement{}, err
	}
	switch {
	case ownerHolds(status):
		result.Problem, result.Status = ProblemOwnerHolds, status
		return result, nil
	case status != StatusIdentified:
		result.Problem = ProblemNotConcluded
		return result, nil
	}
	held, err := conclusions(ctx, tx, in.SeriesID)
	if err != nil {
		return Replacement{}, err
	}
	remaining := map[string]bool{}
	for _, c := range held {
		remaining[c.Source] = true
	}
	for _, id := range in.Withdraw {
		index := slices.IndexFunc(held, func(c Conclusion) bool { return candidateID(c.Source, c.EntryID) == id })
		switch {
		case index < 0:
			result.Problem = ProblemNotConcluded
		case held[index].Confirmer.Kind != ConfirmedByAgent && result.Problem == "":
			result.Problem = ProblemOwnerConfirmed
		}
		if index >= 0 {
			delete(remaining, held[index].Source)
		}
	}
	if result.Problem != "" {
		return result, nil
	}
	cited, problem, err := citeEvidence(ctx, tx, in.SeriesID, in.CandidateIDs)
	if err != nil || problem != "" {
		result.Problem = problem
		return result, err
	}
	for _, c := range cited {
		if remaining[c.Source] {
			result.Problem = ProblemSourceTaken
			return result, nil
		}
	}
	withdrawals, op, err := s.replace(ctx, tx, subject, ConfirmedByAgent, correctionReceiptKey(subject, "replace", in.RequestKey, in.SeriesID), in.SeriesID, in.Withdraw, cited, in.Reason)
	if err != nil {
		return Replacement{}, err
	}
	for _, w := range withdrawals {
		result.WithdrawalIDs = append(result.WithdrawalIDs, w.ID)
	}
	result.Accepted, result.OperationID = true, op.ID
	result.Status, err = seriesStatus(ctx, tx, in.SeriesID)
	return result, err
}

// citeEvidence looks the candidate IDs up in the series' evidence packet. problem is
// ProblemExcludedCandidate or ProblemNotInEvidence when one cannot be cited.
func citeEvidence(ctx context.Context, tx *sql.Tx, seriesID int64, ids []string) ([]citedCandidate, string, error) {
	excluded, err := excludedIDs(ctx, tx, seriesID)
	if err != nil {
		return nil, "", err
	}
	packet, facts, _, err := storedEvidence(ctx, tx, seriesID)
	if err != nil {
		return nil, "", err
	}
	var cited []citedCandidate
	problem := ""
	for _, id := range ids {
		index := slices.IndexFunc(packet.Candidates, func(c EvidenceCandidate) bool { return c.ID == id })
		switch {
		case excluded[id]:
			problem = ProblemExcludedCandidate
		case index < 0 && problem == "":
			problem = ProblemNotInEvidence
		case index >= 0:
			cited = append(cited, citedCandidate{EvidenceCandidate: packet.Candidates[index], facts: facts[id]})
		}
	}
	return cited, problem, nil
}

// replace is the one identification-replacement use case of agents and the owner, in the caller's
// transaction: it removes the series' conclusions named by withdrawn (source:entry IDs), takes back
// what supplementation brought while the series rested on them and nobody changed since, then
// confirms the cited candidates and supplements the series from every source it is now identified
// with. The caller has already decided these conclusions may be withdrawn and these candidates
// cited; exclusions are the caller's to record. An owner-reviewed series keeps its metadata.
// receiptKey names the receipts and must be new for actor. It returns the withdrawal receipts and
// the supplementation receipt, which is zero when nothing was supplemented.
func (s *Service) replace(ctx context.Context, tx *sql.Tx, actor delegated.Subject, confirmer, receiptKey string, seriesID int64, withdrawn []string, cited []citedCandidate, reason string) ([]enrichment.Operation, enrichment.Operation, error) {
	for _, id := range withdrawn {
		source, entry, _ := strings.Cut(id, ":")
		if _, err := tx.ExecContext(ctx, `DELETE FROM identification_conclusions WHERE series_id = ? AND source = ? AND entry_id = ?`, seriesID, source, entry); err != nil {
			return nil, enrichment.Operation{}, err
		}
	}
	var withdrawals []enrichment.Operation
	if s.enricher != nil {
		status, err := seriesStatus(ctx, tx, seriesID)
		if err != nil {
			return nil, enrichment.Operation{}, err
		}
		if status != StatusOwnerReviewed {
			if withdrawals, err = s.enricher.WithdrawIdentified(ctx, tx, actor, enrichment.WithdrawInput{RequestKey: receiptKey, SeriesID: seriesID, Entries: withdrawn, Reason: reason}); err != nil {
				return nil, enrichment.Operation{}, err
			}
		}
	}
	op, err := s.confirm(ctx, tx, actor, confirmer, receiptKey, seriesID, cited, reason)
	return withdrawals, op, err
}

// authorizeSeries rechecks the subject's capability on the series' library inside tx; a series that
// does not exist reads as one outside it.
func authorizeSeries(ctx context.Context, tx *sql.Tx, subject delegated.Subject, seriesID int64, capability string) error {
	var library int64
	err := tx.QueryRowContext(ctx, `SELECT library_id FROM series WHERE id = ?`, seriesID).Scan(&library)
	if errors.Is(err, sql.ErrNoRows) {
		return delegated.ErrDenied
	}
	if err != nil {
		return err
	}
	if delegated.AuthorizeTx(ctx, tx, subject, library, capability) != nil {
		return delegated.ErrDenied
	}
	return nil
}

// decisionDigest fingerprints one decision request of a kind, so a request key reused for another
// kind or other content is told apart from a retry.
func decisionDigest(kind string, in any) string {
	raw, _ := json.Marshal(struct {
		Kind  string `json:"kind"`
		Input any    `json:"input"`
	}{kind, in})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// replayed loads the result the request key already decided for the series into out. A key first
// used with other content yields a request_key_reused problem written into out.
func replayed(ctx context.Context, tx *sql.Tx, subject delegated.Subject, key string, seriesID int64, digest string, out any) (bool, error) {
	var stored, result string
	err := tx.QueryRowContext(ctx, `SELECT digest, result FROM identification_verdicts WHERE credential_id = ? AND request_key = ? AND series_id = ?`,
		subject.CredentialID, key, seriesID).Scan(&stored, &result)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, err
	case stored != digest:
		reused, _ := json.Marshal(map[string]any{"series_id": seriesID, "problem": ProblemRequestKeyReused})
		return true, json.Unmarshal(reused, out)
	}
	return true, json.Unmarshal([]byte(result), out)
}

// recordDecision keeps a decided result under the request key, in the deciding transaction.
func recordDecision(ctx context.Context, tx *sql.Tx, subject delegated.Subject, key string, seriesID int64, digest string, result any, at int64) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO identification_verdicts (credential_id, request_key, series_id, user_id, digest, result, decided_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, subject.CredentialID, key, seriesID, subject.UserID, digest, string(raw), at)
	return err
}

// correctionReceiptKey names the receipts of one correction request of a kind: request keys are
// the agent's, while receipt keys are unique per credential.
func correctionReceiptKey(subject delegated.Subject, kind, key string, seriesID int64) string {
	sum := sha256.Sum256([]byte(subject.CredentialID + "\x00" + kind + "\x00" + key + "\x00" + strconv.FormatInt(seriesID, 10)))
	return kind + "-" + hex.EncodeToString(sum[:16])
}
