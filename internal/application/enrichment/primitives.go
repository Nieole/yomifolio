package enrichment

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/seriesmeta"
)

// IdentifiedSources reads, in the caller's transaction, the source snapshot behind each entry the
// series is currently identified to.
type IdentifiedSources func(ctx context.Context, tx *sql.Tx, series int64) ([]metadata.FieldEvidence, error)

// WithIdentifiedSources lets author corrections rest on the series' identification.
func (s *Service) WithIdentifiedSources(read IdentifiedSources) *Service {
	s.identified = read
	return s
}

// AuthorsInput corrects the authors of one series against its whole current author group.
type AuthorsInput struct {
	RequestKey  string              `json:"request_key"`
	SeriesID    int64               `json:"series_id"`
	Expected    []seriesmeta.Author `json:"expected_authors"`
	Corrections []AuthorCorrection  `json:"corrections"`
}

// CoverInput improves the series' automatic cover with one page of one of its archive books.
type CoverInput struct {
	RequestKey string `json:"request_key"`
	SeriesID   int64  `json:"series_id"`
	BookID     int64  `json:"book_id"`
	Page       int64  `json:"page"`
	Reason     string `json:"reason"`
}

const identifiedReason = "series-local author correction backed by identified sources"

// SeriesAuthors is what an author correction is prepared from: the series' current author group,
// which a correction expects whole, and the credits of each source the series is identified to,
// which are the only ones a correction may name as its To.
type SeriesAuthors struct {
	SeriesID int64               `json:"series_id"`
	Authors  []seriesmeta.Author `json:"authors"`
	Sources  []SourceCredits     `json:"sources"`
}

// SourceCredits are the author credits of one identified source entry, as its stored snapshot lists them.
type SourceCredits struct {
	Source  string              `json:"source"`
	EntryID string              `json:"entry_id"`
	Authors []seriesmeta.Author `json:"authors"`
}

// Authors reads the series' current author group and the identified source credits under current
// read authority, from the same evidence CorrectAuthors checks against.
func (s *Service) Authors(ctx context.Context, actor delegated.Subject, series int64) (SeriesAuthors, error) {
	if series <= 0 || s.identified == nil {
		return SeriesAuthors{}, delegated.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SeriesAuthors{}, err
	}
	defer tx.Rollback()
	current, state, err := readState(ctx, database.New(tx), series)
	if err != nil {
		return SeriesAuthors{}, delegated.ErrDenied
	}
	if err = authorize(ctx, tx, actor, current.LibraryID, false); err != nil {
		return SeriesAuthors{}, err
	}
	evidence, err := s.identified(ctx, tx, series)
	if err != nil {
		return SeriesAuthors{}, err
	}
	out := SeriesAuthors{SeriesID: series, Authors: state.Authors, Sources: []SourceCredits{}}
	for _, e := range evidence {
		credits := []seriesmeta.Author{}
		for _, a := range e.Metadata.Authors {
			credits = append(credits, seriesmeta.Author{Name: a.Name, Role: a.Role})
		}
		out.Sources = append(out.Sources, SourceCredits{Source: e.Source, EntryID: strings.TrimPrefix(e.CandidateID, e.Source+":"), Authors: seriesmeta.AuthorSet(credits)})
	}
	return out, nil
}

// CorrectAuthors applies only the explicit corrections: each From leaves the series and its To,
// which an identified source of the series credits, joins it. The other members stay, no shared
// author record changes, and the receipt is an ordinary enrichment one that Undo reverses.
func (s *Service) CorrectAuthors(ctx context.Context, actor delegated.Subject, in AuthorsInput) (Operation, error) {
	if !validKey(in.RequestKey) || in.SeriesID <= 0 || len(in.Corrections) == 0 || len(in.Corrections) > 50 || len(in.Expected) > 100 || s.identified == nil {
		return Operation{}, delegated.ErrInvalid
	}
	digest := authorsDigest(in)
	if out, found, err := s.existing(ctx, actor, in.SeriesID, in.RequestKey, digest); err != nil || found {
		return out, err
	}
	evidence, err := s.identifiedEvidence(ctx, actor, in.SeriesID)
	if err != nil {
		return Operation{}, err
	}
	if len(evidence) == 0 {
		return Operation{}, delegated.ErrInvalid
	}
	expected := in.Expected
	if expected == nil {
		expected = []seriesmeta.Author{}
	}
	c := supplement{key: in.RequestKey, digest: digest, series: in.SeriesID, fields: []string{"authors"}, expected: State{Authors: expected},
		evidence: fieldEvidence{sources: map[string][]metadata.FieldEvidence{"authors": evidence}, reason: identifiedReason}, corrections: in.Corrections, correctOnly: true}
	return s.applyEvidence(ctx, actor, c, &identifiedDecision{read: s.identified, series: in.SeriesID, evidence: evidence})
}

func (s *Service) identifiedEvidence(ctx context.Context, actor delegated.Subject, series int64) ([]metadata.FieldEvidence, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, err := database.New(tx).GetSeries(ctx, series)
	if err != nil {
		return nil, delegated.ErrDenied
	}
	if err = authorize(ctx, tx, actor, current.LibraryID, true); err != nil {
		return nil, err
	}
	return s.identified(ctx, tx, series)
}

// ImproveCover replaces the series' unlocked archive-generated cover with the selected page. The
// expected cover is the one read when the request is admitted; a cover that is manual, locked or
// changed by the commit is a conflict, so the cover and its receipt only ever commit together.
func (s *Service) ImproveCover(ctx context.Context, actor delegated.Subject, in CoverInput) (Operation, error) {
	if !validKey(in.RequestKey) || in.SeriesID <= 0 || in.BookID <= 0 || in.Page < 1 || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 1000 || s.covers == nil || s.covers.Images == nil {
		return Operation{}, delegated.ErrInvalid
	}
	digest := coverDigest(in)
	if out, found, err := s.existing(ctx, actor, in.SeriesID, in.RequestKey, digest); err != nil || found {
		return out, err
	}
	selection := &CoverSelection{BookID: in.BookID, Page: in.Page, QualityCompared: true, Reason: in.Reason}
	admitted, _, err := s.coverAdmission(ctx, actor, coverCommand{series: in.SeriesID, cover: selection})
	if err != nil {
		return Operation{}, err
	}
	var prepared *preparedCover
	if eligibleCover(admitted.Cover) {
		if prepared, err = s.prepareCover(ctx, actor, coverCommand{series: in.SeriesID, cover: selection, expected: admitted.Cover}); err != nil {
			return Operation{}, err
		}
	}
	c := supplement{key: in.RequestKey, digest: digest, series: in.SeriesID, fields: []string{"cover"}, expected: State{Cover: admitted.Cover}, cover: selection, prepared: prepared}
	return s.applyEvidence(ctx, actor, c, coverDecision{})
}

// identifiedDecision rests author evidence on the series' identification: the write commits only
// while the identified sources still yield the evidence the corrections were checked against.
type identifiedDecision struct {
	read     IdentifiedSources
	series   int64
	evidence []metadata.FieldEvidence
}

func (*identifiedDecision) authorize(context.Context, *sql.Tx, delegated.Subject, int64) error {
	return nil
}

func (d *identifiedDecision) guard(ctx context.Context, tx *sql.Tx, _ *database.Queries, _ *Operation) (bool, string, error) {
	current, err := d.read(ctx, tx, d.series)
	if err != nil {
		return false, "", err
	}
	a, _ := json.Marshal(current)
	b, _ := json.Marshal(d.evidence)
	return string(a) == string(b), "identified source evidence changed", nil
}

func (*identifiedDecision) settle(context.Context, *sql.Tx, *database.Queries, *Operation) (bool, error) {
	return false, nil
}

func (*identifiedDecision) abandon(*Operation) {}

// coverDecision selects an archive page, not source evidence; the cover's content was read, so
// the write needs current read authority too.
type coverDecision struct{}

func (coverDecision) authorize(ctx context.Context, tx *sql.Tx, actor delegated.Subject, library int64) error {
	return authorize(ctx, tx, actor, library, false)
}

func (coverDecision) guard(context.Context, *sql.Tx, *database.Queries, *Operation) (bool, string, error) {
	return true, "", nil
}

func (coverDecision) settle(context.Context, *sql.Tx, *database.Queries, *Operation) (bool, error) {
	return false, nil
}

func (coverDecision) abandon(*Operation) {}
