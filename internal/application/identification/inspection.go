package identification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"yomifolio/internal/application/delegated"
)

// Duplicate match kinds, in the order a page lists them.
const (
	// MatchSameEntry: the series have conclusions naming the same source entry.
	MatchSameEntry = "same_entry"
	// MatchSameName: the series share a directory name or title once both are normalized like
	// catalogue search text.
	MatchSameName = "same_name"
)

// Inspection page bounds. A page stops before the item that would take its JSON past
// inspectionBudget, so a model with a small context can take it in whole; it always holds at
// least one item, and next_offset then resumes at the first one left out.
const (
	maxDuplicateGroups = 20
	maxGroupMembers    = 10
	inspectionBudget   = 30 << 10
)

// DuplicatesInput narrows the search to some of the credential's libraries and pages it.
type DuplicatesInput struct {
	LibraryIDs []int64 `json:"library_ids,omitempty" jsonschema:"libraries to search, default every library of this credential"`
	Offset     int     `json:"offset,omitempty" jsonschema:"groups to skip: next_offset of the previous page"`
	Limit      int     `json:"limit,omitempty" jsonschema:"at most this many groups, 1-20, default 10"`
}

// DuplicateMember is one series of a group of suspected duplicates.
type DuplicateMember struct {
	SeriesID  int64  `json:"series_id"`
	LibraryID int64  `json:"library_id"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	BookCount int64  `json:"book_count"`
	Status    string `json:"status"`
}

// DuplicateGroup is the series that share one entry or one name. Key is the entry as
// source:entry_id, or the normalized name. Series lists at most maxGroupMembers of them by ID;
// SeriesCount counts them all.
type DuplicateGroup struct {
	Match       string            `json:"match"`
	Key         string            `json:"key"`
	SeriesCount int64             `json:"series_count"`
	Series      []DuplicateMember `json:"series"`
}

// DuplicatePage is one page of suspected duplicates. NextOffset is set while groups remain.
type DuplicatePage struct {
	LibraryIDs []int64          `json:"library_ids"`
	Groups     []DuplicateGroup `json:"groups"`
	NextOffset int              `json:"next_offset,omitempty"`
}

// pageBounds checks an offset and a limit and returns the limit, def when it is unset.
func pageBounds(offset, limit, def, most int) (int, error) {
	if offset < 0 || limit < 0 || limit > most {
		return 0, delegated.ErrInvalid
	}
	if limit == 0 {
		limit = def
	}
	return limit, nil
}

// Duplicates groups the series of the requested libraries, or of every library the credential
// names, that look like the same work: those whose conclusions name the same source entry, then
// those sharing a normalized directory name or title. A series may sit in several groups. Groups
// count only series inside those libraries, so a group needs two of them. Each library needs
// current read authority; the search reads only and leaves every series as it is.
func (s *Service) Duplicates(ctx context.Context, subject delegated.Subject, in DuplicatesInput) (DuplicatePage, error) {
	limit, err := pageBounds(in.Offset, in.Limit, 10, maxDuplicateGroups)
	if err != nil {
		return DuplicatePage{}, err
	}
	libraries, err := s.access.Libraries(ctx, subject, in.LibraryIDs, delegated.Read)
	if err != nil {
		return DuplicatePage{}, err
	}
	out := DuplicatePage{LibraryIDs: libraries, Groups: []DuplicateGroup{}}
	if len(libraries) == 0 {
		return out, nil
	}
	scope, err := json.Marshal(libraries)
	if err != nil {
		return DuplicatePage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `WITH scope AS (SELECT value AS id FROM json_each(?1)),
		entries AS (
			SELECT c.source, c.entry_id, COUNT(*) AS n FROM identification_conclusions c JOIN series s ON s.id = c.series_id
			WHERE s.library_id IN (SELECT id FROM scope) GROUP BY c.source, c.entry_id HAVING COUNT(*) > 1),
		names AS (
			SELECT key, COUNT(*) AS n FROM (
				SELECT name AS key, series_id FROM series_search_text WHERE library_id IN (SELECT id FROM scope) AND name != ''
				UNION
				SELECT title, series_id FROM series_search_text WHERE library_id IN (SELECT id FROM scope) AND title != ''
			) GROUP BY key HAVING COUNT(*) > 1)
		SELECT 0 AS kind, source || ':' || entry_id AS key, source, entry_id, n FROM entries
		UNION ALL SELECT 1, key, '', '', n FROM names
		ORDER BY kind, key LIMIT ?2 OFFSET ?3`, string(scope), limit+1, in.Offset)
	if err != nil {
		return DuplicatePage{}, err
	}
	type found struct {
		group         DuplicateGroup
		source, entry string
	}
	var page []found
	for rows.Next() {
		var f found
		var kind int
		if err := rows.Scan(&kind, &f.group.Key, &f.source, &f.entry, &f.group.SeriesCount); err != nil {
			rows.Close()
			return DuplicatePage{}, err
		}
		f.group.Match = MatchSameName
		if kind == 0 {
			f.group.Match = MatchSameEntry
		}
		page = append(page, f)
	}
	if err := rows.Close(); err != nil {
		return DuplicatePage{}, err
	}
	if err := rows.Err(); err != nil {
		return DuplicatePage{}, err
	}
	// The empty page with the largest next_offset it can carry bounds what the groups add to.
	size := encodedSize(DuplicatePage{LibraryIDs: libraries, Groups: []DuplicateGroup{}, NextOffset: in.Offset + limit})
	for i, f := range page {
		if i == limit {
			out.NextOffset = in.Offset + i
			break
		}
		if f.group.Match == MatchSameEntry {
			f.group.Series, err = duplicateMembers(ctx, s.db, `JOIN identification_conclusions c ON c.series_id = s.id
				WHERE c.source = ? AND c.entry_id = ? AND s.library_id IN (SELECT value FROM json_each(?))`, f.source, f.entry, string(scope))
		} else {
			f.group.Series, err = duplicateMembers(ctx, s.db, `JOIN series_search_text t ON t.series_id = s.id
				WHERE (t.name = ? OR t.title = ?) AND t.library_id IN (SELECT value FROM json_each(?))`, f.group.Key, f.group.Key, string(scope))
		}
		if err != nil {
			return DuplicatePage{}, err
		}
		n := encodedSize(f.group) + 1
		if i > 0 && size+n > inspectionBudget {
			out.NextOffset = in.Offset + i
			break
		}
		size += n
		out.Groups = append(out.Groups, f.group)
	}
	return out, nil
}

// duplicateMembers lists the first members of one group by ID. match joins the group's rows to
// series s and filters them with args.
func duplicateMembers(ctx context.Context, q queryer, match string, args ...any) ([]DuplicateMember, error) {
	rows, err := q.QueryContext(ctx, `SELECT s.id, s.library_id, s.name, COALESCE(s.title, ''),
			(SELECT COUNT(*) FROM books k WHERE k.series_id = s.id), st.status
		FROM series s JOIN series_identification_status st ON st.series_id = s.id `+match+`
		ORDER BY s.id LIMIT ?`, append(args, maxGroupMembers)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DuplicateMember{}
	for rows.Next() {
		var m DuplicateMember
		if err := rows.Scan(&m.SeriesID, &m.LibraryID, &m.Name, &m.Title, &m.BookCount, &m.Status); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func encodedSize(v any) int {
	raw, _ := json.Marshal(v)
	return len(raw)
}

// Tag usage orders.
const (
	// TagsByCount lists the most used tags first, then by name.
	TagsByCount = "count"
	// TagsByName lists tags by name normalized like catalogue search text, so spelling variants
	// sit together.
	TagsByName = "name"
)

const (
	maxTagPage = 100
	tagSamples = 3
)

// TagUsageInput narrows the count to some of the credential's libraries, orders and pages it.
type TagUsageInput struct {
	LibraryIDs []int64 `json:"library_ids,omitempty" jsonschema:"libraries to count, default every library of this credential"`
	Sort       string  `json:"sort,omitempty" jsonschema:"count (most used first, default) or name (normalized, variants together)"`
	Offset     int     `json:"offset,omitempty" jsonschema:"tags to skip: next_offset of the previous page"`
	Limit      int     `json:"limit,omitempty" jsonschema:"at most this many tags, 1-100, default 50"`
}

// TagUsage is how many series of the counted libraries carry one tag, with the first few of them
// by ID.
type TagUsage struct {
	Tag             string  `json:"tag"`
	SeriesCount     int64   `json:"series_count"`
	SampleSeriesIDs []int64 `json:"sample_series_ids"`
}

// TagUsagePage is one page of tag usage. Total counts the distinct tags the libraries use;
// NextOffset is set while tags remain.
type TagUsagePage struct {
	LibraryIDs []int64    `json:"library_ids"`
	Total      int64      `json:"total"`
	Tags       []TagUsage `json:"tags"`
	NextOffset int        `json:"next_offset,omitempty"`
}

// TagUsage counts, per tag, the series of the requested libraries, or of every library the
// credential names, that carry it. Tags only the other libraries use are left out. Each library
// needs current read authority; the count reads only.
func (s *Service) TagUsage(ctx context.Context, subject delegated.Subject, in TagUsageInput) (TagUsagePage, error) {
	limit, err := pageBounds(in.Offset, in.Limit, 50, maxTagPage)
	if err != nil {
		return TagUsagePage{}, err
	}
	order := `u.n DESC, t.name`
	switch in.Sort {
	case "", TagsByCount:
	case TagsByName:
		order = `search_normalize(t.name), t.name`
	default:
		return TagUsagePage{}, delegated.ErrInvalid
	}
	libraries, err := s.access.Libraries(ctx, subject, in.LibraryIDs, delegated.Read)
	if err != nil {
		return TagUsagePage{}, err
	}
	out := TagUsagePage{LibraryIDs: libraries, Tags: []TagUsage{}}
	if len(libraries) == 0 {
		return out, nil
	}
	raw, err := json.Marshal(libraries)
	if err != nil {
		return TagUsagePage{}, err
	}
	scope := string(raw)
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT st.tag_id) FROM series_tags st JOIN series s ON s.id = st.series_id
		WHERE s.library_id IN (SELECT value FROM json_each(?))`, scope).Scan(&out.Total); err != nil {
		return TagUsagePage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `WITH usage AS (
			SELECT st.tag_id, COUNT(*) AS n FROM series_tags st JOIN series s ON s.id = st.series_id
			WHERE s.library_id IN (SELECT value FROM json_each(?1)) GROUP BY st.tag_id)
		SELECT t.id, t.name, u.n FROM usage u JOIN tags t ON t.id = u.tag_id
		ORDER BY `+order+` LIMIT ?2 OFFSET ?3`, scope, limit+1, in.Offset)
	if err != nil {
		return TagUsagePage{}, err
	}
	type found struct {
		id    int64
		usage TagUsage
	}
	var page []found
	for rows.Next() {
		var f found
		if err := rows.Scan(&f.id, &f.usage.Tag, &f.usage.SeriesCount); err != nil {
			rows.Close()
			return TagUsagePage{}, err
		}
		page = append(page, f)
	}
	if err := rows.Close(); err != nil {
		return TagUsagePage{}, err
	}
	if err := rows.Err(); err != nil {
		return TagUsagePage{}, err
	}
	size := encodedSize(TagUsagePage{LibraryIDs: libraries, Total: out.Total, Tags: []TagUsage{}, NextOffset: in.Offset + limit})
	for i, f := range page {
		if i == limit {
			out.NextOffset = in.Offset + i
			break
		}
		if f.usage.SampleSeriesIDs, err = tagSampleSeries(ctx, s.db, f.id, scope); err != nil {
			return TagUsagePage{}, err
		}
		n := encodedSize(f.usage) + 1
		if i > 0 && size+n > inspectionBudget {
			out.NextOffset = in.Offset + i
			break
		}
		size += n
		out.Tags = append(out.Tags, f.usage)
	}
	return out, nil
}

func tagSampleSeries(ctx context.Context, q queryer, tag int64, scope string) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT st.series_id FROM series_tags st JOIN series s ON s.id = st.series_id
		WHERE st.tag_id = ? AND s.library_id IN (SELECT value FROM json_each(?)) ORDER BY st.series_id LIMIT ?`, tag, scope, tagSamples)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CategoryDisputedOwnerIdentification is the category of the recheck escalation an agent raises
// when it believes a person's identification of the series is wrong. An agent may not replace a
// person's identification, so this hands the question to the owner.
const CategoryDisputedOwnerIdentification = "disputed_owner_identification"

// ErrNoOwnerIdentification means a dispute names a series no person identified.
var ErrNoOwnerIdentification = errors.New("series has no identification a person confirmed")

// ReportCategories are the categories Report takes, in the order agents are offered them.
var ReportCategories = []string{CategoryDirectoryGrouping, CategorySuspectedDuplicate, CategoryTagProblem, CategoryProtectedValueDoubt, CategoryDisputedOwnerIdentification}

// ReportInput is one report an agent leaves the owner on a series.
type ReportInput struct {
	SeriesID int64  `json:"series_id"`
	Category string `json:"category" jsonschema:"directory_grouping, suspected_duplicate, tag_problem or protected_value_doubt leave a note; disputed_owner_identification sends a series a person identified to the owner's recheck"`
	Reason   string `json:"reason" jsonschema:"one checkable sentence, at most 1000 bytes"`
}

// Report says what a report left. Recorded is false when the series already has a report of the
// category that keeps it from being raised again, which then keeps its reason: an open one, or for
// a note one the owner closed. Status is the series' identification status: a note never changes
// it, a dispute makes it needs_recheck unless the owner reviewed it.
type Report struct {
	SeriesID int64  `json:"series_id"`
	Category string `json:"category"`
	Recorded bool   `json:"recorded"`
	Status   string `json:"status"`
}

// Report leaves the owner a report on a series, under current read authority re-checked in the
// recording transaction; a series that does not exist reads as one outside it. A note of one of the
// note categories changes neither the series' identification status nor its metadata. A dispute
// questions an identification a person confirmed, which no agent may replace: it raises a recheck
// escalation, so the series waits for the owner's recheck with its identification and metadata as
// they are; a series no person identified is refused with ErrNoOwnerIdentification. A report needs
// no claim and stays open until the owner closes it. Like a note given to Submit, it is not repeated
// while one of the same category is open, nor, for a note, after the owner closed one.
func (s *Service) Report(ctx context.Context, subject delegated.Subject, in ReportInput) (Report, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if !slices.Contains(ReportCategories, in.Category) || in.SeriesID <= 0 || in.Reason == "" || len(in.Reason) > maxReasonBytes {
		return Report{}, delegated.ErrInvalid
	}
	class := EscalationNote
	if in.Category == CategoryDisputedOwnerIdentification {
		class = EscalationRecheck
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Report{}, err
	}
	defer tx.Rollback()
	var library int64
	err = tx.QueryRowContext(ctx, `SELECT library_id FROM series WHERE id = ?`, in.SeriesID).Scan(&library)
	if errors.Is(err, sql.ErrNoRows) {
		return Report{}, delegated.ErrDenied
	}
	if err != nil {
		return Report{}, err
	}
	if err := delegated.AuthorizeTx(ctx, tx, subject, library, delegated.Read); err != nil {
		return Report{}, delegated.ErrDenied
	}
	if class == EscalationRecheck {
		var disputable bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identification_conclusions WHERE series_id = ? AND confirmer = ?)`, in.SeriesID, ConfirmedByOwner).Scan(&disputable); err != nil {
			return Report{}, err
		}
		if !disputable {
			return Report{}, ErrNoOwnerIdentification
		}
	}
	out := Report{SeriesID: in.SeriesID, Category: in.Category}
	if out.Recorded, err = raiseOnce(ctx, tx, subject, in.SeriesID, in.Category, class, in.Reason, s.now().UnixMilli()); err != nil {
		return Report{}, err
	}
	if out.Status, err = seriesStatus(ctx, tx, in.SeriesID); err != nil {
		return Report{}, err
	}
	return out, tx.Commit()
}

// raiseOnce records an agent's escalation on the series unless one of the same category is still
// open, and reports whether it recorded one. A note is not raised again either once the owner has
// closed one of its category on the series: closing settles that category for the series. Other
// escalations may be raised again after their resolution returns the series to the agents.
func raiseOnce(ctx context.Context, tx *sql.Tx, subject delegated.Subject, series int64, category, class, reason string, at int64) (bool, error) {
	var raised bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM identification_escalations WHERE series_id = ? AND category = ?
		AND (resolved_at_ms IS NULL OR ? = ?))`, series, category, class, EscalationNote).Scan(&raised); err != nil || raised {
		return false, err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO identification_escalations (series_id, category, class, reason, raised_by, user_id, credential_id, raised_at_ms)
		VALUES (?, ?, ?, ?, 'agent', ?, ?, ?)`, series, category, class, reason, subject.UserID, subject.CredentialID, at)
	return err == nil, err
}
