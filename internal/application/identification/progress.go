package identification

import (
	"context"
	"strings"

	"yomifolio/internal/application/delegated"
)

// ProgressInput narrows the count to some of the credential's libraries.
type ProgressInput struct {
	LibraryIDs []int64 `json:"library_ids,omitempty" jsonschema:"libraries to count, default every library of this credential"`
}

// StatusCounts holds one count per identification status; every status is always present.
type StatusCounts struct {
	Unprocessed   int64 `json:"unprocessed"`
	Identified    int64 `json:"identified"`
	AwaitingOwner int64 `json:"awaiting_owner"`
	NeedsRecheck  int64 `json:"needs_recheck"`
	NotFound      int64 `json:"not_found"`
	OwnerReviewed int64 `json:"owner_reviewed"`
}

// Progress counts series per identification status across the covered libraries.
type Progress struct {
	LibraryIDs []int64      `json:"library_ids"`
	Series     int64        `json:"series"`
	Statuses   StatusCounts `json:"statuses"`
}

// Progress counts the series of the requested libraries, or of every library the credential
// names, per identification status. Each library needs current read authority.
func (s *Service) Progress(ctx context.Context, subject delegated.Subject, in ProgressInput) (Progress, error) {
	libraries, err := s.access.Libraries(ctx, subject, in.LibraryIDs, delegated.Read)
	if err != nil {
		return Progress{}, err
	}
	out := Progress{LibraryIDs: libraries}
	if len(libraries) == 0 {
		return out, nil
	}
	args := make([]any, len(libraries))
	for i, id := range libraries {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM series_identification_status
		WHERE library_id IN (?`+strings.Repeat(", ?", len(libraries)-1)+`) GROUP BY status`, args...)
	if err != nil {
		return Progress{}, err
	}
	defer rows.Close()
	counts := map[string]*int64{
		StatusUnprocessed: &out.Statuses.Unprocessed, StatusIdentified: &out.Statuses.Identified, StatusAwaitingOwner: &out.Statuses.AwaitingOwner,
		StatusNeedsRecheck: &out.Statuses.NeedsRecheck, StatusNotFound: &out.Statuses.NotFound, StatusOwnerReviewed: &out.Statuses.OwnerReviewed,
	}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return Progress{}, err
		}
		if target, ok := counts[status]; ok {
			*target = n
		}
		out.Series += n
	}
	return out, rows.Err()
}
