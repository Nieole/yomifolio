package enrichment

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/config"
	"yomifolio/internal/covermaterial"
	"yomifolio/internal/database"
	"yomifolio/internal/images"
	"yomifolio/internal/seriesmeta"
)

// CoverSelection explicitly selects an archive page after comparison, never a URL/path.
type CoverSelection struct {
	BookID          int64  `json:"book_id"`
	Page            int64  `json:"page"`
	QualityCompared bool   `json:"quality_compared"`
	Reason          string `json:"reason"`
}

// CoverAccess reuses delegated content admission, archive budgets and current authority.
type CoverAccess struct {
	Images *delegated.Service
	Config func() config.Config
}

func (s *Service) WithCovers(access CoverAccess) *Service { s.covers = &access; return s }

type preparedCover struct{ old, new string }

var coverSlots = make(chan struct{}, 2)

func eligibleCover(c *seriesmeta.CoverState) bool { return c != nil && c.Automatic && !c.Locked }

func readCover(ctx context.Context, q *database.Queries, series int64) (*seriesmeta.CoverState, error) {
	book, err := q.GetEnrichmentCoverBook(ctx, series)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &seriesmeta.CoverState{BookID: book.ID, Path: book.CoverPath.String, Locked: book.CoverLocked, Automatic: seriesmeta.ArchiveCover(book)}, nil
}

func (s *Service) prepareCover(ctx context.Context, actor delegated.Subject, in Input) (*preparedCover, error) {
	c := in.Cover
	if s.covers == nil || s.covers.Images == nil || c.BookID <= 0 || c.Page < 1 || !c.QualityCompared || strings.TrimSpace(c.Reason) == "" || len(c.Reason) > 1000 || !eligibleCover(in.Expected.Cover) {
		return nil, delegated.ErrInvalid
	}
	select {
	case coverSlots <- struct{}{}:
		defer func() { <-coverSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// Authorize target and source through the existing content use case, then fence again
	// in the write transaction. No path, arbitrary archive or remote URL is accepted.
	thumb, err := s.covers.Images.PageThumbnails(ctx, actor, delegated.PageThumbnailsInput{SeriesID: in.SeriesID, BookID: c.BookID, Pages: []int64{c.Page}})
	if err != nil {
		return nil, err
	}
	state, err := s.State(ctx, actor, in.SeriesID)
	if err != nil {
		return nil, err
	}
	if !same(state, in.Expected.state(), "cover") || !eligibleCover(state.Cover) {
		return nil, delegated.ErrInvalid
	}
	dir := config.ThumbnailDir(s.covers.Config())
	old, err := covermaterial.Read(ctx, dir, state.Cover.Path)
	if err != nil {
		return nil, err
	}
	if _, err = images.EvidenceThumbnail(ctx, old); err != nil {
		return nil, err
	}
	oldKey, err := covermaterial.Prepare(ctx, dir, old, filepath.Ext(state.Cover.Path))
	if err != nil {
		return nil, err
	}
	newKey, err := covermaterial.Prepare(ctx, dir, thumb.Images[0].Data, ".jpg")
	if err != nil {
		return nil, err
	}
	return &preparedCover{oldKey, newKey}, nil
}

func writeState(ctx context.Context, tx *sql.Tx, q *database.Queries, current database.Series, state State, fields []string) error {
	for _, field := range fields {
		if field == "cover" {
			if err := seriesmeta.WriteEnrichmentCover(ctx, tx, current, state.Cover); err != nil {
				return err
			}
		}
	}
	return seriesmeta.WriteEnrichmentState(ctx, q, current, state, fields)
}
