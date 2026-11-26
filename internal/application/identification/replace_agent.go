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
)

// Why a correction was not taken, beyond the problems a verdict shares. Every rejection leaves the
// series as it was.
const (
	// ProblemOwnerHolds: the series is the owner's turn or keeps the owner's decision
	// (awaiting_owner, needs_recheck or owner_reviewed), so the agent leaves it alone.
	ProblemOwnerHolds = "owner_holds"
	// ProblemNotConcluded: the series has no identification of a cited candidate's source to
	// replace.
	ProblemNotConcluded = "not_concluded"
	// ProblemAlreadyIdentified: the series is already identified with a cited candidate.
	ProblemAlreadyIdentified = "already_identified"
	// ProblemOwnerConfirmed: the identification a cited candidate would replace was confirmed by a
	// person.
	ProblemOwnerConfirmed = "owner_confirmed"
)

// ReplaceInput replaces identifications an agent made of a series with evidence candidates of the
// same sources.
type ReplaceInput struct {
	RequestKey   string   `json:"request_key" jsonschema:"new 1-80 char ASCII key; reuse only to retry this exact call"`
	SeriesID     int64    `json:"series_id"`
	CandidateIDs []string `json:"candidate_ids" jsonschema:"1-5 ids of the series' evidence candidates, at most one per source; each replaces the agent's identification of its source"`
	Reason       string   `json:"reason" jsonschema:"one checkable sentence, at most 1000 bytes"`
}

// AgentReplacement reports a replacement. Status is the series' identification status after an
// accepted replacement, or its current status when the owner holds it.
type AgentReplacement struct {
	SeriesID int64  `json:"series_id"`
	Accepted bool   `json:"accepted"`
	Status   string `json:"status,omitempty"`
	Problem  string `json:"problem,omitempty"`
	// RetractionIDs are the receipts that took back what each replaced entry supplemented.
	RetractionIDs []string `json:"retraction_ids,omitempty"`
	// OperationID is the receipt of the supplementation by the new identification.
	OperationID string `json:"operation_id,omitempty"`
}

// Replace corrects identifications an agent made: each cited evidence candidate replaces the
// series' identification of its source, under current metadata_write authority re-checked in the
// one transaction that records it. Only an identified series is corrected, and only an
// identification an agent confirmed is replaced; a person's identification is the owner's to
// change. The replaced entries do not become excluded candidates, which only a person's decision
// makes. The result is kept under the request key, so a retry returns it again.
func (s *Service) Replace(ctx context.Context, subject delegated.Subject, in ReplaceInput) (AgentReplacement, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if !validRequestKey(in.RequestKey) || in.SeriesID <= 0 || in.Reason == "" || len(in.Reason) > maxReasonBytes || !citable(in.CandidateIDs) {
		return AgentReplacement{}, delegated.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AgentReplacement{}, err
	}
	defer tx.Rollback()
	if err := authorizeSeries(ctx, tx, subject, in.SeriesID, delegated.MetadataWrite); err != nil {
		return AgentReplacement{}, err
	}
	digest := decisionDigest("replace", in)
	var result AgentReplacement
	if found, err := replayed(ctx, tx, subject, in.RequestKey, in.SeriesID, digest, &result); err != nil || found {
		return result, err
	}
	if result, err = s.judgeReplacement(ctx, tx, subject, in); err != nil {
		return AgentReplacement{}, err
	}
	if err := recordDecision(ctx, tx, subject, in.RequestKey, in.SeriesID, digest, result, s.now().UnixMilli()); err != nil {
		return AgentReplacement{}, err
	}
	if err := tx.Commit(); err != nil {
		return AgentReplacement{}, err
	}
	if result.Accepted {
		s.enricher.Publish()
	}
	return result, nil
}

func (s *Service) judgeReplacement(ctx context.Context, tx *sql.Tx, subject delegated.Subject, in ReplaceInput) (AgentReplacement, error) {
	result := AgentReplacement{SeriesID: in.SeriesID}
	status, err := seriesStatus(ctx, tx, in.SeriesID)
	if err != nil {
		return AgentReplacement{}, err
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
		return AgentReplacement{}, err
	}
	for _, id := range in.CandidateIDs {
		source, entry, _ := parseCandidate(id)
		index := slices.IndexFunc(held, func(c Conclusion) bool { return c.Source == source })
		switch {
		case index < 0:
			result.Problem = ProblemNotConcluded
		case held[index].Confirmer.Kind != ConfirmedByAgent:
			result.Problem = ProblemOwnerConfirmed
		case held[index].EntryID == entry:
			result.Problem = ProblemAlreadyIdentified
		}
		if result.Problem != "" {
			return result, nil
		}
	}
	cited, problem, err := citeEvidence(ctx, tx, in.SeriesID, in.CandidateIDs)
	if err != nil || problem != "" {
		result.Problem = problem
		return result, err
	}
	done, err := s.replace(ctx, tx, subject, ConfirmedByAgent, correctionReceiptKey(subject, "replace", in.RequestKey, in.SeriesID), in.SeriesID, cited, in.Reason)
	if err != nil {
		return AgentReplacement{}, err
	}
	for _, r := range done.Retractions {
		result.RetractionIDs = append(result.RetractionIDs, r.ID)
	}
	result.Accepted, result.OperationID = true, done.Supplement.ID
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

// ownerHolds reports whether a status is the owner's turn or the owner's decision.
func ownerHolds(status string) bool {
	return status == StatusAwaitingOwner || status == StatusNeedsRecheck || status == StatusOwnerReviewed
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
