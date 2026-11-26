package identification

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"yomifolio/internal/database"
)

// CategoryNewBookContradiction is the category of the recheck escalation the system raises when a
// book added to an identified series names another work.
const CategoryNewBookContradiction = "new_book_contradiction"

// NewBook is a book a scan just added to a series that already existed, with the series name its
// ComicInfo gives, "" when it gives none.
type NewBook struct {
	SeriesID        int64
	Name            string
	ComicInfoSeries string
}

// RecheckNewBooks sends a series to needs_recheck when a book just added to it contradicts the
// series' identification. Only a series with at least one conclusion is checked, and not when it is
// owner-reviewed or already has an open recheck escalation. A book contradicts when its ComicInfo
// series name, compared after database.NormalizeSearchText, neither equals nor contains nor is
// contained in any title, original title or alias of any of the series' conclusions; a contained
// name must be at least two characters. A book without a ComicInfo series name, or a series whose
// conclusions carry no name, gives nothing to compare and changes nothing. The check reads only
// local state and leaves the conclusions and the series metadata as they are; the first
// contradicting book of a series is quoted in the escalation's reason.
func (s *Service) RecheckNewBooks(ctx context.Context, books []NewBook) error {
	clues := map[int64][]NewBook{}
	for _, book := range books {
		if book.SeriesID > 0 && database.NormalizeSearchText(book.ComicInfoSeries) != "" {
			clues[book.SeriesID] = append(clues[book.SeriesID], book)
		}
	}
	if len(clues) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raised []NewBook
	for _, series := range slices.Sorted(maps.Keys(clues)) {
		names, err := recheckNames(ctx, tx, series)
		if err != nil {
			return err
		}
		index := slices.IndexFunc(clues[series], func(book NewBook) bool { return contradicts(book.ComicInfoSeries, names) })
		if index < 0 {
			continue
		}
		book := clues[series][index]
		reason := fmt.Sprintf("new book %q has ComicInfo series %q, which matches no identified title, original title or alias", clip(book.Name, 200), clip(book.ComicInfoSeries, 200))
		if _, err := tx.ExecContext(ctx, `INSERT INTO identification_escalations (series_id, category, class, reason, raised_by, raised_at_ms)
			VALUES (?, ?, ?, ?, 'system', ?)`, series, CategoryNewBookContradiction, EscalationRecheck, reason, s.now().UnixMilli()); err != nil {
			return err
		}
		raised = append(raised, book)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, book := range raised {
		slog.InfoContext(ctx, "New book contradicts the series identification; series needs recheck", "series_id", book.SeriesID, "book", book.Name, "comicinfo_series", book.ComicInfoSeries)
	}
	return nil
}

// recheckNames returns the normalized names of the series' conclusions, or nil when the series is
// not subject to the check.
func recheckNames(ctx context.Context, tx *sql.Tx, series int64) ([]string, error) {
	var checked bool
	err := tx.QueryRowContext(ctx, `SELECT NOT EXISTS (SELECT 1 FROM identification_owner_reviews WHERE series_id = ?)
		AND NOT EXISTS (SELECT 1 FROM identification_escalations WHERE series_id = ? AND class = ? AND resolved_at_ms IS NULL)`,
		series, series, EscalationRecheck).Scan(&checked)
	if err != nil || !checked {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT title, original_title, aliases FROM identification_conclusions WHERE series_id = ?`, series)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var title, original, encoded string
		if err := rows.Scan(&title, &original, &encoded); err != nil {
			return nil, err
		}
		var aliases []string
		_ = json.Unmarshal([]byte(encoded), &aliases)
		for _, name := range append([]string{title, original}, aliases...) {
			if normalized := database.NormalizeSearchText(name); normalized != "" {
				names = append(names, normalized)
			}
		}
	}
	return names, rows.Err()
}

func contradicts(clue string, names []string) bool {
	if len(names) == 0 {
		return false
	}
	clue = database.NormalizeSearchText(clue)
	for _, name := range names {
		shorter, longer := name, clue
		if utf8.RuneCountInString(shorter) > utf8.RuneCountInString(longer) {
			shorter, longer = longer, shorter
		}
		if shorter == longer || (utf8.RuneCountInString(shorter) >= 2 && strings.Contains(longer, shorter)) {
			return false
		}
	}
	return true
}

func clip(text string, runes int) string {
	if utf8.RuneCountInString(text) <= runes {
		return text
	}
	return string([]rune(text)[:runes]) + "…"
}
