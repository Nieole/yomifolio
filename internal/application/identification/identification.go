// Package identification owns series identification: the durable per-source conclusions, the
// entries a person excluded, escalations, not-found verdicts and the owner-reviewed decision, and
// the identification status derived from them. The status is never stored or edited on its own;
// the series_identification_status view is its single definition. It also owns the evidence
// packets that prefetch and research runs prepare, the agents' main loop of claim leases and
// verdicts over them, and the read-only inspection behind the notes agents leave the owner.
//
// Agent-facing use cases re-check delegated authority through Access on every call; owner-facing
// ones take the acting account and re-check it is an administrator in the change's transaction.
package identification

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/task"
)

var (
	// ErrSeriesNotFound means the series does not exist.
	ErrSeriesNotFound = errors.New("series not found")
	// ErrDenied means the acting account may not perform an owner decision.
	ErrDenied = errors.New("identification decision denied")
)

// Identification status values, in the precedence the status view applies them.
const (
	StatusOwnerReviewed = "owner_reviewed"
	StatusNeedsRecheck  = "needs_recheck"
	StatusAwaitingOwner = "awaiting_owner"
	StatusIdentified    = "identified"
	StatusNotFound      = "not_found"
	StatusUnprocessed   = "unprocessed"
)

// Confirmer kinds of a conclusion.
const (
	ConfirmedByOwner = "owner"
	ConfirmedByAgent = "agent"
)

// Escalation classes. An open escalation's class, not its category, decides the status: an
// identification escalation makes the series awaiting_owner, a recheck escalation makes it
// needs_recheck, and a note never changes the status.
const (
	EscalationIdentification = "identification"
	EscalationRecheck        = "recheck"
	EscalationNote           = "note"
)

// Access is the delegated authority the agent-facing use cases rely on.
type Access interface {
	Libraries(context.Context, delegated.Subject, []int64, string) ([]int64, error)
	Authorize(ctx context.Context, subject delegated.Subject, library int64, capability string) error
	PrepareRun(ctx context.Context, subject delegated.Subject, library int64, capability string, spec task.RunSpec, body task.Body) (task.RunSpec, task.Body, error)
}

// Service holds no process state. Each call is one transaction or one read, except a research,
// which launches a run and waits for it a bounded time.
type Service struct {
	db       *sql.DB
	access   Access
	prefetch *Prefetcher
	images   *imageSources
	now      func() time.Time
}

// New binds the service to the process database and delegated authority.
func New(db *sql.DB, access Access) *Service {
	return &Service{db: db, access: access, now: time.Now}
}

// WithPrefetch lets the owner launch the evidence prefetch, and the owner and agents research one
// series, through this service.
func (s *Service) WithPrefetch(p *Prefetcher) *Service { s.prefetch = p; return s }

// WithClock replaces the current time every identification record and lease is stamped and
// compared with. It must be set before the service is used.
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

// Confirmer says who made an identification decision: the owner, an agent and its credential,
// or, for an escalation only, the system. An owner row from history has no user.
type Confirmer struct {
	Kind           string `json:"kind"`
	UserID         int64  `json:"user_id"`
	UserName       string `json:"user_name"`
	CredentialID   string `json:"credential_id"`
	CredentialName string `json:"credential_name"`
}

// Conclusion is one source's identification of a series.
type Conclusion struct {
	Source        string    `json:"source"`
	EntryID       string    `json:"entry_id"`
	EntryURL      string    `json:"entry_url"`
	Title         string    `json:"title"`
	OriginalTitle string    `json:"original_title"`
	Aliases       []string  `json:"aliases"`
	Confirmer     Confirmer `json:"confirmer"`
	ConfirmedAt   string    `json:"confirmed_at"`
	Reason        string    `json:"reason"`
	// ProposalID is the applied proposal a conclusion derived from history came from, else 0.
	ProposalID int64 `json:"proposal_id"`
}

// ExcludedCandidate is a source entry a person ruled out for the series.
type ExcludedCandidate struct {
	Source     string `json:"source"`
	EntryID    string `json:"entry_id"`
	EntryURL   string `json:"entry_url"`
	Title      string `json:"title"`
	UserID     int64  `json:"user_id"`
	ExcludedAt string `json:"excluded_at"`
	// ProposalID is the rejected proposal an exclusion derived from history came from, else 0.
	ProposalID int64 `json:"proposal_id"`
}

// NotFoundVerdict records that the searched sources have no entry for the series.
type NotFoundVerdict struct {
	SearchedSources []string  `json:"searched_sources"`
	Confirmer       Confirmer `json:"confirmer"`
	ConcludedAt     string    `json:"concluded_at"`
	Reason          string    `json:"reason"`
}

// Escalation is one open escalation of the series. Class decides what it means for the status.
type Escalation struct {
	ID       int64     `json:"id"`
	Category string    `json:"category"`
	Class    string    `json:"class"`
	Reason   string    `json:"reason"`
	RaisedBy Confirmer `json:"raised_by"`
	RaisedAt string    `json:"raised_at"`
}

// OwnerReview records who set the series owner-reviewed and when.
type OwnerReview struct {
	UserID     int64  `json:"user_id"`
	UserName   string `json:"user_name"`
	ReviewedAt string `json:"reviewed_at"`
}

// SeriesIdentification is the owner's view of one series: its status and what it rests on.
type SeriesIdentification struct {
	SeriesID           int64               `json:"series_id"`
	LibraryID          int64               `json:"library_id"`
	Status             string              `json:"status"`
	Conclusions        []Conclusion        `json:"conclusions"`
	ExcludedCandidates []ExcludedCandidate `json:"excluded_candidates"`
	NotFound           *NotFoundVerdict    `json:"not_found,omitempty"`
	Escalations        []Escalation        `json:"escalations,omitempty"`
	OwnerReview        *OwnerReview        `json:"owner_review,omitempty"`
	// Evidence is the series' evidence packet without the candidates excluded since its fetch;
	// absent until a prefetch stored one.
	Evidence *EvidencePacket `json:"evidence,omitempty"`
}

func stamp(ms int64) string { return time.UnixMilli(ms).UTC().Format(time.RFC3339) }

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Series reads one series' identification for the owner.
func (s *Service) Series(ctx context.Context, actor, seriesID int64) (SeriesIdentification, error) {
	if err := requireAdmin(ctx, s.db, actor); err != nil {
		return SeriesIdentification{}, err
	}
	return read(ctx, s.db, seriesID)
}

func requireAdmin(ctx context.Context, q queryer, actor int64) error {
	var ok bool
	err := q.QueryRowContext(ctx, `SELECT role = 'admin' AND NOT must_change_password FROM users WHERE id = ?`, actor).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !ok) {
		return ErrDenied
	}
	return err
}

func read(ctx context.Context, q queryer, seriesID int64) (SeriesIdentification, error) {
	out := SeriesIdentification{SeriesID: seriesID, Conclusions: []Conclusion{}, ExcludedCandidates: []ExcludedCandidate{}}
	err := q.QueryRowContext(ctx, `SELECT library_id, status FROM series_identification_status WHERE series_id = ?`, seriesID).Scan(&out.LibraryID, &out.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return SeriesIdentification{}, ErrSeriesNotFound
	}
	if err != nil {
		return SeriesIdentification{}, err
	}
	if out.Conclusions, err = conclusions(ctx, q, seriesID); err != nil {
		return SeriesIdentification{}, err
	}
	if out.ExcludedCandidates, err = exclusions(ctx, q, seriesID); err != nil {
		return SeriesIdentification{}, err
	}
	if out.Evidence, err = readEvidence(ctx, q, seriesID, out.ExcludedCandidates); err != nil {
		return SeriesIdentification{}, err
	}
	if out.NotFound, err = notFound(ctx, q, seriesID); err != nil {
		return SeriesIdentification{}, err
	}
	if out.Escalations, err = openEscalations(ctx, q, seriesID); err != nil {
		return SeriesIdentification{}, err
	}
	var review OwnerReview
	var reviewedAt int64
	err = q.QueryRowContext(ctx, `SELECT o.user_id, COALESCE(NULLIF(u.display_name, ''), u.username, ''), o.reviewed_at_ms
		FROM identification_owner_reviews o LEFT JOIN users u ON u.id = o.user_id WHERE o.series_id = ?`, seriesID).Scan(&review.UserID, &review.UserName, &reviewedAt)
	switch {
	case err == nil:
		review.ReviewedAt = stamp(reviewedAt)
		out.OwnerReview = &review
	case !errors.Is(err, sql.ErrNoRows):
		return SeriesIdentification{}, err
	}
	return out, nil
}

func conclusions(ctx context.Context, q queryer, seriesID int64) ([]Conclusion, error) {
	rows, err := q.QueryContext(ctx, `SELECT c.source, c.entry_id, c.entry_url, c.title, c.original_title, c.aliases,
			c.confirmer, c.user_id, COALESCE(NULLIF(u.display_name, ''), u.username, ''), c.credential_id, COALESCE(m.name, ''),
			c.confirmed_at_ms, c.reason, c.proposal_id
		FROM identification_conclusions c
		LEFT JOIN users u ON u.id = c.user_id
		LEFT JOIN mcp_credentials m ON m.id = c.credential_id
		WHERE c.series_id = ?`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conclusion{}
	for rows.Next() {
		var c Conclusion
		var aliases string
		var confirmedAt int64
		if err := rows.Scan(&c.Source, &c.EntryID, &c.EntryURL, &c.Title, &c.OriginalTitle, &aliases,
			&c.Confirmer.Kind, &c.Confirmer.UserID, &c.Confirmer.UserName, &c.Confirmer.CredentialID, &c.Confirmer.CredentialName,
			&confirmedAt, &c.Reason, &c.ProposalID); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(aliases), &c.Aliases); err != nil || c.Aliases == nil {
			c.Aliases = []string{}
		}
		c.ConfirmedAt = stamp(confirmedAt)
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Conclusion) int { return cmp.Compare(sourceRank(a.Source), sourceRank(b.Source)) })
	return out, rows.Err()
}

func notFound(ctx context.Context, q queryer, seriesID int64) (*NotFoundVerdict, error) {
	var out NotFoundVerdict
	var searched string
	var concludedAt int64
	err := q.QueryRowContext(ctx, `SELECT n.searched_sources, n.confirmer, n.user_id, COALESCE(NULLIF(u.display_name, ''), u.username, ''), n.credential_id,
			COALESCE(m.name, ''), n.concluded_at_ms, n.reason
		FROM identification_not_found n
		LEFT JOIN users u ON u.id = n.user_id
		LEFT JOIN mcp_credentials m ON m.id = n.credential_id
		WHERE n.series_id = ?`, seriesID).Scan(&searched, &out.Confirmer.Kind, &out.Confirmer.UserID, &out.Confirmer.UserName, &out.Confirmer.CredentialID,
		&out.Confirmer.CredentialName, &concludedAt, &out.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(searched), &out.SearchedSources); err != nil || out.SearchedSources == nil {
		out.SearchedSources = []string{}
	}
	out.ConcludedAt = stamp(concludedAt)
	return &out, nil
}

func openEscalations(ctx context.Context, q queryer, seriesID int64) ([]Escalation, error) {
	rows, err := q.QueryContext(ctx, `SELECT e.id, e.category, e.class, e.reason, e.raised_by, e.user_id, COALESCE(NULLIF(u.display_name, ''), u.username, ''),
			e.credential_id, COALESCE(m.name, ''), e.raised_at_ms
		FROM identification_escalations e
		LEFT JOIN users u ON u.id = e.user_id
		LEFT JOIN mcp_credentials m ON m.id = e.credential_id
		WHERE e.series_id = ? AND e.resolved_at_ms IS NULL ORDER BY e.id`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Escalation
	for rows.Next() {
		var e Escalation
		var raisedAt int64
		if err := rows.Scan(&e.ID, &e.Category, &e.Class, &e.Reason, &e.RaisedBy.Kind, &e.RaisedBy.UserID, &e.RaisedBy.UserName,
			&e.RaisedBy.CredentialID, &e.RaisedBy.CredentialName, &raisedAt); err != nil {
			return nil, err
		}
		e.RaisedAt = stamp(raisedAt)
		out = append(out, e)
	}
	return out, rows.Err()
}

func exclusions(ctx context.Context, q queryer, seriesID int64) ([]ExcludedCandidate, error) {
	rows, err := q.QueryContext(ctx, `SELECT source, entry_id, entry_url, title, user_id, excluded_at_ms, proposal_id
		FROM identification_exclusions WHERE series_id = ? ORDER BY excluded_at_ms DESC, source, entry_id`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExcludedCandidate{}
	for rows.Next() {
		var x ExcludedCandidate
		var excludedAt int64
		if err := rows.Scan(&x.Source, &x.EntryID, &x.EntryURL, &x.Title, &x.UserID, &excludedAt, &x.ProposalID); err != nil {
			return nil, err
		}
		x.ExcludedAt = stamp(excludedAt)
		out = append(out, x)
	}
	return out, rows.Err()
}

// sourceRank orders sources by the default priority; unknown keys sort last.
func sourceRank(source string) int {
	if i := slices.Index(Sources, source); i >= 0 {
		return i
	}
	return len(Sources)
}
