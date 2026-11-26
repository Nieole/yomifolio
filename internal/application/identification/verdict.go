package identification

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"

	"yomifolio/internal/application/delegated"
)

// Verdict kinds an agent answers a claimed series with.
const (
	VerdictIdentify = "identify"
	VerdictNotFound = "not_found"
	VerdictEscalate = "escalate"
)

// Escalation categories an agent may give. The identification ones hand the series to the owner;
// the note ones only leave a note and never change the status.
const (
	CategoryAmbiguousCandidates = "ambiguous_candidates"
	CategoryUncertainEdition    = "uncertain_edition"
	CategoryMissingFromSources  = "missing_from_sources"
	CategoryDirectoryGrouping   = "directory_grouping"
	CategorySuspectedDuplicate  = "suspected_duplicate"
	CategoryTagProblem          = "tag_problem"
	// CategoryProtectedValueDoubt notes a value the agent believes is wrong but may not change: a
	// person's edit, a ComicInfo value, a locked field or a protected cover.
	CategoryProtectedValueDoubt = "protected_value_doubt"
)

// AgentCategories maps each category an agent may escalate with to the class it raises, in the
// order agents are offered them.
var AgentCategories = []struct{ Category, Class string }{
	{CategoryAmbiguousCandidates, EscalationIdentification},
	{CategoryUncertainEdition, EscalationIdentification},
	{CategoryMissingFromSources, EscalationIdentification},
	{CategoryDirectoryGrouping, EscalationNote},
	{CategorySuspectedDuplicate, EscalationNote},
	{CategoryTagProblem, EscalationNote},
	{CategoryProtectedValueDoubt, EscalationNote},
}

func agentClass(category string) (string, bool) {
	for _, c := range AgentCategories {
		if c.Category == category {
			return c.Class, true
		}
	}
	return "", false
}

// Why a verdict was not taken. Every rejection leaves the series as it was.
const (
	// ProblemInvalid: the verdict is malformed; nothing about the series was read.
	ProblemInvalid = "invalid"
	// ProblemDenied: the series is unknown or outside current metadata_write authority; the two
	// read the same.
	ProblemDenied = "denied"
	// ProblemNotUnprocessed: the series was decided since the claim, by this flow or the owner.
	ProblemNotUnprocessed = "not_unprocessed"
	// ProblemNotClaimed: no lease holds the series.
	ProblemNotClaimed = "not_claimed"
	// ProblemClaimedByOther: another lease holds the series now, so the submitted one expired.
	ProblemClaimedByOther = "claimed_by_other"
	// ProblemExcludedCandidate: a cited candidate is one the owner excluded for the series.
	ProblemExcludedCandidate = "excluded_candidate"
	// ProblemNotInEvidence: a cited candidate is not in the series' evidence packet.
	ProblemNotInEvidence = "not_in_evidence"
	// ProblemRequestKeyReused: the request key already decided another verdict for the series.
	ProblemRequestKeyReused = "request_key_reused"
	// ProblemUnavailable: the verdict could not be recorded; retrying the request is safe.
	ProblemUnavailable = "unavailable"
	// ProblemNotAttempted: the request was cancelled before this verdict.
	ProblemNotAttempted = "not_attempted"
)

const maxReasonBytes = 1000

// SubmitInput answers series claimed under one lease, one verdict per series.
type SubmitInput struct {
	RequestKey string    `json:"request_key" jsonschema:"new 1-80 char ASCII key; reuse only to retry this exact call"`
	LeaseID    string    `json:"lease_id" jsonschema:"lease_id of the claim that handed out these series"`
	Verdicts   []Verdict `json:"verdicts" jsonschema:"1-20 verdicts, one per series"`
}

// Verdict is the answer for one claimed series.
type Verdict struct {
	SeriesID     int64    `json:"series_id"`
	Verdict      string   `json:"verdict" jsonschema:"identify, not_found or escalate"`
	CandidateIDs []string `json:"candidate_ids,omitempty" jsonschema:"identify only: ids of the series' evidence candidates it is, at most one per source"`
	Category     string   `json:"category,omitempty" jsonschema:"escalate only: ambiguous_candidates, uncertain_edition or missing_from_sources hand it to the owner; directory_grouping, suspected_duplicate, tag_problem or protected_value_doubt only add a note"`
	Reason       string   `json:"reason" jsonschema:"one checkable sentence, at most 1000 bytes"`
}

// VerdictResult reports one verdict. Status is the series' identification status after an
// accepted verdict, or its current status when the series was no longer unprocessed.
type VerdictResult struct {
	SeriesID int64  `json:"series_id"`
	Accepted bool   `json:"accepted"`
	Status   string `json:"status,omitempty"`
	Problem  string `json:"problem,omitempty"`
	// OperationID is the receipt of the metadata supplementation an accepted identify verdict ran,
	// read and undone with the metadata operation tools.
	OperationID string `json:"operation_id,omitempty"`
}

// Submission reports every verdict of a request, in request order.
type Submission struct {
	Results []VerdictResult `json:"results"`
}

// Submit decides each verdict on its own: the batch is not atomic, and one rejected or failed
// verdict leaves the others as decided. A verdict is taken only for a series that is still
// unprocessed and held by the submitted lease, expired or not; once another claim has taken the
// series over, the old lease answers nothing. Each verdict re-checks current metadata_write
// authority inside the transaction that records it, and its decided result is kept under the
// request key, so retrying the same request returns the same results without writing again.
//
// identify records an agent conclusion per cited source and, in the same transaction, supplements
// the series' metadata from its identified sources; not_found records the sources the series'
// evidence packet heard from; an identification escalation hands the series to the owner. Each of
// these ends the lease. A note leaves the status and the lease as they are, so the same lease may
// still answer the series; a note of a category the series already has, open or closed by the
// owner, is not repeated. Only an identify verdict changes series metadata.
func (s *Service) Submit(ctx context.Context, subject delegated.Subject, in SubmitInput) (Submission, error) {
	if !validRequestKey(in.RequestKey) || in.LeaseID == "" || len(in.LeaseID) > 64 || len(in.Verdicts) == 0 || len(in.Verdicts) > MaxClaim {
		return Submission{}, delegated.ErrInvalid
	}
	seen := map[int64]bool{}
	for _, v := range in.Verdicts {
		if seen[v.SeriesID] {
			return Submission{}, delegated.ErrInvalid
		}
		seen[v.SeriesID] = true
	}
	if _, err := s.access.Libraries(ctx, subject, nil, delegated.MetadataWrite); err != nil {
		return Submission{}, err
	}
	out := Submission{Results: make([]VerdictResult, 0, len(in.Verdicts))}
	for _, v := range in.Verdicts {
		if ctx.Err() != nil {
			out.Results = append(out.Results, VerdictResult{SeriesID: v.SeriesID, Problem: ProblemNotAttempted})
			continue
		}
		result, err := s.decide(ctx, subject, in.RequestKey, in.LeaseID, v)
		if err != nil {
			slog.WarnContext(ctx, "Identification verdict not recorded", "series_id", v.SeriesID, "error", err)
			result = VerdictResult{SeriesID: v.SeriesID, Problem: ProblemUnavailable}
		}
		out.Results = append(out.Results, result)
	}
	return out, nil
}

func validRequestKey(key string) bool {
	if len(key) < 1 || len(key) > 80 {
		return false
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

// shape checks a verdict without reading anything and returns it trimmed.
func shape(v Verdict) (Verdict, bool) {
	v.Reason = strings.TrimSpace(v.Reason)
	if v.SeriesID <= 0 || v.Reason == "" || len(v.Reason) > maxReasonBytes {
		return v, false
	}
	switch v.Verdict {
	case VerdictIdentify:
		if v.Category != "" || len(v.CandidateIDs) == 0 || len(v.CandidateIDs) > len(Sources) {
			return v, false
		}
		var sources []string
		for _, id := range v.CandidateIDs {
			source, entry, ok := strings.Cut(id, ":")
			if !ok || entry == "" || !slices.Contains(Sources, source) || slices.Contains(sources, source) {
				return v, false
			}
			sources = append(sources, source)
		}
	case VerdictNotFound:
		if v.Category != "" || len(v.CandidateIDs) > 0 {
			return v, false
		}
	case VerdictEscalate:
		if _, ok := agentClass(v.Category); !ok || len(v.CandidateIDs) > 0 {
			return v, false
		}
	default:
		return v, false
	}
	return v, true
}

func verdictDigest(lease string, v Verdict) string {
	raw, _ := json.Marshal(struct {
		Lease string  `json:"lease"`
		V     Verdict `json:"verdict"`
	}{lease, v})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// decide authorizes, replays or judges one verdict and records its result, in one transaction.
// An error means nothing was recorded.
func (s *Service) decide(ctx context.Context, subject delegated.Subject, key, lease string, v Verdict) (VerdictResult, error) {
	v, ok := shape(v)
	if !ok {
		return VerdictResult{SeriesID: v.SeriesID, Problem: ProblemInvalid}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return VerdictResult{}, err
	}
	defer tx.Rollback()
	var library int64
	err = tx.QueryRowContext(ctx, `SELECT library_id FROM series WHERE id = ?`, v.SeriesID).Scan(&library)
	if errors.Is(err, sql.ErrNoRows) {
		return VerdictResult{SeriesID: v.SeriesID, Problem: ProblemDenied}, nil
	}
	if err != nil {
		return VerdictResult{}, err
	}
	if delegated.AuthorizeTx(ctx, tx, subject, library, delegated.MetadataWrite) != nil {
		return VerdictResult{SeriesID: v.SeriesID, Problem: ProblemDenied}, nil
	}
	digest := verdictDigest(lease, v)
	var storedDigest, stored string
	err = tx.QueryRowContext(ctx, `SELECT digest, result FROM identification_verdicts WHERE credential_id = ? AND request_key = ? AND series_id = ?`,
		subject.CredentialID, key, v.SeriesID).Scan(&storedDigest, &stored)
	switch {
	case err == nil && storedDigest != digest:
		return VerdictResult{SeriesID: v.SeriesID, Problem: ProblemRequestKeyReused}, nil
	case err == nil:
		var replay VerdictResult
		return replay, json.Unmarshal([]byte(stored), &replay)
	case !errors.Is(err, sql.ErrNoRows):
		return VerdictResult{}, err
	}
	result, err := s.judge(ctx, tx, subject, key, lease, v)
	if err != nil {
		return VerdictResult{}, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return VerdictResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identification_verdicts (credential_id, request_key, series_id, user_id, digest, result, decided_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, subject.CredentialID, key, v.SeriesID, subject.UserID, digest, string(raw), s.now().UnixMilli()); err != nil {
		return VerdictResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return VerdictResult{}, err
	}
	if result.OperationID != "" {
		s.enricher.Publish()
	}
	return result, nil
}

// judge checks the series is still the lease's to answer and the verdict against its evidence,
// then writes what the verdict records.
func (s *Service) judge(ctx context.Context, tx *sql.Tx, subject delegated.Subject, key, lease string, v Verdict) (VerdictResult, error) {
	result := VerdictResult{SeriesID: v.SeriesID}
	status, err := seriesStatus(ctx, tx, v.SeriesID)
	if err != nil {
		return VerdictResult{}, err
	}
	if status != StatusUnprocessed {
		result.Problem, result.Status = ProblemNotUnprocessed, status
		return result, nil
	}
	var holder string
	err = tx.QueryRowContext(ctx, `SELECT lease_id FROM identification_leases WHERE series_id = ?`, v.SeriesID).Scan(&holder)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		result.Problem = ProblemNotClaimed
		return result, nil
	case err != nil:
		return VerdictResult{}, err
	case holder != lease:
		result.Problem = ProblemClaimedByOther
		return result, nil
	}
	packet, _, _, err := storedEvidence(ctx, tx, v.SeriesID)
	if err != nil {
		return VerdictResult{}, err
	}
	now := s.now().UnixMilli()
	finished := true
	switch v.Verdict {
	case VerdictIdentify:
		cited, problem, err := citeEvidence(ctx, tx, v.SeriesID, v.CandidateIDs)
		if err != nil {
			return VerdictResult{}, err
		}
		if problem != "" {
			result.Problem = problem
			return result, nil
		}
		op, err := s.confirm(ctx, tx, subject, ConfirmedByAgent, verdictReceiptKey(subject, key, v.SeriesID), v.SeriesID, cited, v.Reason)
		if err != nil {
			return VerdictResult{}, err
		}
		// The series was unprocessed, so a not-found verdict here is one a new source reopened.
		if _, err := tx.ExecContext(ctx, `DELETE FROM identification_not_found WHERE series_id = ?`, v.SeriesID); err != nil {
			return VerdictResult{}, err
		}
		result.OperationID = op.ID
	case VerdictNotFound:
		var searched []string
		for _, source := range packet.Sources {
			if source.Status == SourceAvailable {
				searched = append(searched, source.Source)
			}
		}
		if err := recordNotFound(ctx, tx, v.SeriesID, searched, ConfirmedByAgent, subject.UserID, subject.CredentialID, now, v.Reason); err != nil {
			return VerdictResult{}, err
		}
	case VerdictEscalate:
		class, _ := agentClass(v.Category)
		finished = class != EscalationNote
		if _, err := raiseOnce(ctx, tx, subject, v.SeriesID, v.Category, class, v.Reason, now); err != nil {
			return VerdictResult{}, err
		}
	}
	if finished {
		if _, err := tx.ExecContext(ctx, `DELETE FROM identification_leases WHERE series_id = ?`, v.SeriesID); err != nil {
			return VerdictResult{}, err
		}
	}
	result.Accepted = true
	result.Status, err = seriesStatus(ctx, tx, v.SeriesID)
	return result, err
}

func seriesStatus(ctx context.Context, q queryer, seriesID int64) (string, error) {
	var status string
	err := q.QueryRowContext(ctx, `SELECT status FROM series_identification_status WHERE series_id = ?`, seriesID).Scan(&status)
	return status, err
}
