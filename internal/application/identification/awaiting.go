// The owner's awaiting list (the web's 待人工 page): every series an open escalation waits on the owner for, and the
// decisions the owner makes there.

package identification

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"

	"yomifolio/internal/database"
)

// Bounds of one page of the awaiting list.
const (
	DefaultAwaitingPage = 20
	maxAwaitingPage     = 50
)

var (
	// ErrInvalidAwaiting means a query of the awaiting list or a decision on it is malformed.
	ErrInvalidAwaiting = errors.New("invalid awaiting-list query or decision")
	// ErrNoLongerAwaiting means the series no longer waits for the decision: another decision got there
	// first.
	ErrNoLongerAwaiting = errors.New("series does not wait for this decision")
	// ErrNoteNotFound means the series has no escalation by that ID.
	ErrNoteNotFound = errors.New("note not found")
	// ErrNotANote means the escalation is not a note: an identification or recheck escalation ends
	// with a decision about the series, not by closing it.
	ErrNotANote = errors.New("escalation is not a note")
	// ErrCandidateUnavailable means a cited candidate is no longer offered for the series: it left
	// the evidence, was excluded, or names a source the series is already identified with.
	ErrCandidateUnavailable = errors.New("candidate no longer offered")
)

// Resolutions an owner decision settles an escalation with.
const (
	resolvedNotFound = "not_found"
	// resolvedKept settles a recheck the owner answered by keeping the identification.
	resolvedKept = "kept"
	// resolvedClosed settles a note the owner has seen. A closed note keeps agents from noting the
	// same category on the series again.
	resolvedClosed = "closed"
)

// AwaitingQuery pages the awaiting list. A Category narrows it to the series with an escalation of
// that category waiting; a zero Limit asks for the counts only.
type AwaitingQuery struct {
	Category string
	Offset   int
	Limit    int
}

// AwaitingPage is one page of the series waiting for the owner, oldest waiting escalation first.
type AwaitingPage struct {
	Items []AwaitingSeries `json:"items"`
	// Total counts the series the query matches.
	Total int `json:"total"`
	// NextOffset is where the next page starts; absent on the last page.
	NextOffset int `json:"next_offset,omitempty"`
	// Categories counts the series of each waiting category on the whole list, whatever the query
	// narrows to.
	Categories []AwaitingCategory `json:"categories"`
}

// AwaitingCategory is one escalation category on the awaiting list and how many series it holds.
type AwaitingCategory struct {
	Category string `json:"category"`
	Class    string `json:"class"`
	Series   int    `json:"series"`
}

// AwaitingSeries is one series on the awaiting list: what the library shows of it, and its
// identification, whose escalations are the ones waiting for the owner.
type AwaitingSeries struct {
	SeriesID       int64                `json:"series_id"`
	LibraryName    string               `json:"library_name"`
	DirectoryName  string               `json:"directory_name"`
	Title          string               `json:"title"`
	BookCount      int                  `json:"book_count"`
	CoverBookID    int64                `json:"cover_book_id"`
	Identification SeriesIdentification `json:"identification"`
}

// waitingEscalations selects the open escalations that wait for the owner: every note, and the
// identification and recheck escalations of a series awaiting the owner or needing a recheck. An
// owner-reviewed series keeps its escalations without waiting on them.
const waitingEscalations = `SELECT e.id, e.series_id, e.category, e.class, e.raised_at_ms
	FROM identification_escalations e JOIN series_identification_status v ON v.series_id = e.series_id
	WHERE e.resolved_at_ms IS NULL AND (e.class = 'note' OR v.status IN ('awaiting_owner', 'needs_recheck'))`

func waiting(status string, e Escalation) bool {
	return e.Class == EscalationNote || status == StatusAwaitingOwner || status == StatusNeedsRecheck
}

// Awaiting reads one page of the owner's awaiting list.
func (s *Service) Awaiting(ctx context.Context, actor int64, in AwaitingQuery) (AwaitingPage, error) {
	if in.Offset < 0 || in.Limit < 0 || in.Limit > maxAwaitingPage || len(in.Category) > 64 {
		return AwaitingPage{}, ErrInvalidAwaiting
	}
	if err := requireAdmin(ctx, s.db, actor); err != nil {
		return AwaitingPage{}, err
	}
	out := AwaitingPage{Items: []AwaitingSeries{}, Categories: []AwaitingCategory{}}
	rows, err := s.db.QueryContext(ctx, `SELECT category, class, COUNT(DISTINCT series_id) FROM (`+waitingEscalations+`)
		GROUP BY category, class ORDER BY CASE class WHEN 'identification' THEN 0 WHEN 'recheck' THEN 1 ELSE 2 END, category`)
	if err != nil {
		return AwaitingPage{}, err
	}
	for rows.Next() {
		var c AwaitingCategory
		if err := rows.Scan(&c.Category, &c.Class, &c.Series); err != nil {
			rows.Close()
			return AwaitingPage{}, err
		}
		out.Categories = append(out.Categories, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return AwaitingPage{}, err
	}
	narrowed := `SELECT * FROM (` + waitingEscalations + `) WHERE ? = '' OR category = ?`
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT series_id) FROM (`+narrowed+`)`, in.Category, in.Category).Scan(&out.Total); err != nil {
		return AwaitingPage{}, err
	}
	if in.Limit == 0 {
		return out, nil
	}
	rows, err = s.db.QueryContext(ctx, `SELECT series_id FROM (`+narrowed+`) GROUP BY series_id ORDER BY MIN(raised_at_ms), MIN(id), series_id LIMIT ? OFFSET ?`,
		in.Category, in.Category, in.Limit, in.Offset)
	if err != nil {
		return AwaitingPage{}, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return AwaitingPage{}, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return AwaitingPage{}, err
	}
	for _, id := range ids {
		item, err := readAwaiting(ctx, s.db, id)
		if errors.Is(err, ErrSeriesNotFound) {
			continue
		}
		if err != nil {
			return AwaitingPage{}, err
		}
		out.Items = append(out.Items, item)
	}
	if next := in.Offset + len(ids); next < out.Total {
		out.NextOffset = next
	}
	return out, nil
}

// readAwaiting reads one series as the awaiting list shows it.
func readAwaiting(ctx context.Context, q queryer, seriesID int64) (AwaitingSeries, error) {
	identification, err := read(ctx, q, seriesID)
	if err != nil {
		return AwaitingSeries{}, err
	}
	var open []Escalation
	for _, e := range identification.Escalations {
		if waiting(identification.Status, e) {
			open = append(open, e)
		}
	}
	identification.Escalations = open
	out := AwaitingSeries{SeriesID: seriesID, Identification: identification}
	err = q.QueryRowContext(ctx, `SELECT COALESCE(l.name, ''), s.name, COALESCE(NULLIF(s.title, ''), s.name),
			(SELECT COUNT(*) FROM books b WHERE b.series_id = s.id), COALESCE(st.cover_book_id, 0)
		FROM series s LEFT JOIN libraries l ON l.id = s.library_id LEFT JOIN series_stats st ON st.series_id = s.id
		WHERE s.id = ?`, seriesID).Scan(&out.LibraryName, &out.DirectoryName, &out.Title, &out.BookCount, &out.CoverBookID)
	return out, err
}

// OwnerIdentified is the series' identification after the owner picked candidates, with the
// receipts of the supplementations the pick ran, one per candidate in source priority order; none
// when the series was owner-reviewed.
type OwnerIdentified struct {
	Identification SeriesIdentification `json:"identification"`
	OperationIDs   []string             `json:"operation_ids"`
}

// PickCandidates records the owner's identification of a series waiting for the owner with
// candidates of its evidence, at most one per source, through the owner's identification use case
// (identifyByOwner): each candidate in the configured source priority, so the highest-priority
// source fills the fields first, all in one transaction. The pick settles what the series was
// waiting on and ends any claim lease. It identifies only sources the series has no conclusion for.
func (s *Service) PickCandidates(ctx context.Context, actor, seriesID int64, candidateIDs []string) (OwnerIdentified, error) {
	if !citable(candidateIDs) {
		return OwnerIdentified{}, ErrInvalidAwaiting
	}
	out := OwnerIdentified{OperationIDs: []string{}}
	identification, err := s.awaitingDecision(ctx, actor, seriesID, []string{StatusAwaitingOwner}, func(tx *sql.Tx) error {
		packet, facts, _, err := storedEvidence(ctx, tx, seriesID)
		if err != nil {
			return err
		}
		excluded, err := excludedIDs(ctx, tx, seriesID)
		if err != nil {
			return err
		}
		identified, err := conclusions(ctx, tx, seriesID)
		if err != nil {
			return err
		}
		var cited []citedCandidate
		for _, id := range candidateIDs {
			index := slices.IndexFunc(packet.Candidates, func(c EvidenceCandidate) bool { return c.ID == id })
			source, _, _ := parseCandidate(id)
			if index < 0 || excluded[id] || slices.ContainsFunc(identified, func(c Conclusion) bool { return c.Source == source }) {
				return ErrCandidateUnavailable
			}
			cited = append(cited, citedCandidate{EvidenceCandidate: packet.Candidates[index], facts: facts[id]})
		}
		rank := s.sourceRank()
		slices.SortStableFunc(cited, func(a, b citedCandidate) int { return cmp.Compare(rank(a.Source), rank(b.Source)) })
		for _, c := range cited {
			done, err := s.identifyByOwner(ctx, tx, actor, seriesID, c)
			if err != nil {
				return err
			}
			if done.OperationID != "" {
				out.OperationIDs = append(out.OperationIDs, done.OperationID)
			}
		}
		return nil
	})
	if err != nil {
		return OwnerIdentified{}, err
	}
	if len(out.OperationIDs) > 0 {
		s.enricher.Publish()
	}
	out.Identification = identification
	return out, nil
}

// resolve settles the series' open escalations of class as the owner's decision.
func (s *Service) resolve(ctx context.Context, tx *sql.Tx, actor, seriesID int64, class, resolution string) error {
	_, err := tx.ExecContext(ctx, `UPDATE identification_escalations SET resolved_at_ms = ?, resolved_by = ?, resolution = ?
		WHERE series_id = ? AND class = ? AND resolved_at_ms IS NULL`, s.now().UnixMilli(), actor, resolution, seriesID, class)
	return err
}

// awaitingDecision checks the actor and that the series is in one of the statuses the decision is
// for, applies change and reads the result, all in one transaction.
func (s *Service) awaitingDecision(ctx context.Context, actor, seriesID int64, statuses []string, change func(*sql.Tx) error) (SeriesIdentification, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SeriesIdentification{}, err
	}
	defer tx.Rollback()
	if err := requireAdmin(ctx, tx, actor); err != nil {
		return SeriesIdentification{}, err
	}
	status, err := seriesStatus(ctx, tx, seriesID)
	if errors.Is(err, sql.ErrNoRows) {
		return SeriesIdentification{}, ErrSeriesNotFound
	}
	if err != nil {
		return SeriesIdentification{}, err
	}
	if !slices.Contains(statuses, status) {
		return SeriesIdentification{}, ErrNoLongerAwaiting
	}
	if err := change(tx); err != nil {
		return SeriesIdentification{}, err
	}
	out, err := read(ctx, tx, seriesID)
	if err != nil {
		return SeriesIdentification{}, err
	}
	if err := tx.Commit(); err != nil {
		return SeriesIdentification{}, err
	}
	return out, nil
}

// ExcludeCandidate records the owner ruling out one candidate of a waiting series' evidence: it
// leaves the evidence and may never be cited for the series again. Excluding it again changes
// nothing; an entry the series is identified with cannot be excluded this way.
func (s *Service) ExcludeCandidate(ctx context.Context, actor, seriesID int64, candidateID string) (SeriesIdentification, error) {
	if !citable([]string{candidateID}) {
		return SeriesIdentification{}, ErrInvalidAwaiting
	}
	return s.awaitingDecision(ctx, actor, seriesID, []string{StatusAwaitingOwner, StatusNeedsRecheck}, func(tx *sql.Tx) error {
		excluded, err := excludedIDs(ctx, tx, seriesID)
		if err != nil || excluded[candidateID] {
			return err
		}
		packet, _, _, err := storedEvidence(ctx, tx, seriesID)
		if err != nil {
			return err
		}
		index := slices.IndexFunc(packet.Candidates, func(c EvidenceCandidate) bool { return c.ID == candidateID })
		source, entry, _ := parseCandidate(candidateID)
		var identified bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identification_conclusions WHERE series_id = ? AND source = ? AND entry_id = ?)`,
			seriesID, source, entry).Scan(&identified); err != nil {
			return err
		}
		if index < 0 || identified {
			return ErrCandidateUnavailable
		}
		c := packet.Candidates[index]
		_, err = tx.ExecContext(ctx, `INSERT INTO identification_exclusions (series_id, source, entry_id, entry_url, title, user_id, excluded_at_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, seriesID, source, entry, c.EntryURL, c.Title, actor, s.now().UnixMilli())
		return err
	})
}

// ConcludeNotFound records the owner's verdict that the sources have no entry for a waiting
// series. The sources searched are those that answered for its evidence; the identification
// escalations it was waiting on are settled.
func (s *Service) ConcludeNotFound(ctx context.Context, actor, seriesID int64) (SeriesIdentification, error) {
	return s.awaitingDecision(ctx, actor, seriesID, []string{StatusAwaitingOwner}, func(tx *sql.Tx) error {
		packet, _, _, err := storedEvidence(ctx, tx, seriesID)
		if err != nil {
			return err
		}
		var searched []string
		for _, source := range packet.Sources {
			if source.Status == SourceAvailable {
				searched = append(searched, source.Source)
			}
		}
		if err := recordNotFound(ctx, tx, seriesID, searched, ConfirmedByOwner, actor, "", s.now().UnixMilli(), ""); err != nil {
			return err
		}
		return s.resolve(ctx, tx, actor, seriesID, EscalationIdentification, resolvedNotFound)
	})
}

// CloseNote records that the owner has seen one note of the series. It changes neither the
// series' identification status nor its metadata; closing a closed note changes nothing.
func (s *Service) CloseNote(ctx context.Context, actor, seriesID, noteID int64) (SeriesIdentification, error) {
	return s.ownerDecision(ctx, actor, seriesID, func(tx *sql.Tx) error {
		var class string
		var open bool
		err := tx.QueryRowContext(ctx, `SELECT class, resolved_at_ms IS NULL FROM identification_escalations WHERE id = ? AND series_id = ?`,
			noteID, seriesID).Scan(&class, &open)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return ErrNoteNotFound
		case err != nil:
			return err
		case class != EscalationNote:
			return ErrNotANote
		case !open:
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE identification_escalations SET resolved_at_ms = ?, resolved_by = ?, resolution = ? WHERE id = ?`,
			s.now().UnixMilli(), actor, resolvedClosed, noteID)
		return err
	})
}

// KeepIdentification records the owner's answer to a questioned identification: it is right. The
// series' open recheck escalations are settled and the series is identified again with its
// conclusions and metadata as they are. Its agent conclusions become the owner's, keeping their
// reasons, so agents can no longer replace them. The ComicInfo series names the system rechecks
// questioned it with are accepted for the series: a later book carrying one of them, however
// written, does not question it again.
func (s *Service) KeepIdentification(ctx context.Context, actor, seriesID int64) (SeriesIdentification, error) {
	return s.awaitingDecision(ctx, actor, seriesID, []string{StatusNeedsRecheck}, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		rows, err := tx.QueryContext(ctx, `SELECT clue FROM identification_escalations WHERE series_id = ? AND class = ? AND resolved_at_ms IS NULL AND clue != ''`,
			seriesID, EscalationRecheck)
		if err != nil {
			return err
		}
		var clues []string
		for rows.Next() {
			var clue string
			if err := rows.Scan(&clue); err != nil {
				rows.Close()
				return err
			}
			clues = append(clues, clue)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, clue := range clues {
			if key := database.NormalizeSearchText(clue); key != "" {
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO identification_accepted_names (series_id, name_key, name, user_id, accepted_at_ms)
					VALUES (?, ?, ?, ?, ?)`, seriesID, key, clue, actor, now); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE identification_conclusions SET confirmer = ?, user_id = ?, credential_id = '', confirmed_at_ms = ?
			WHERE series_id = ? AND confirmer = ?`, ConfirmedByOwner, actor, now, seriesID, ConfirmedByAgent); err != nil {
			return err
		}
		return s.resolve(ctx, tx, actor, seriesID, EscalationRecheck, resolvedKept)
	})
}
