package seriesmeta

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
	"strings"
	"yomifolio/internal/database"

	"yomifolio/internal/metadata"
)

// ContentTags accepts a conservative vocabulary of content genres, not ownership,
// reading markers, publication years, source names or unknown free-form labels.
func ContentTags(input []string) []string {
	vocabulary := strings.Split("action|adventure|comedy|drama|fantasy|horror|mystery|romance|sci-fi|science fiction|slice of life|sports|supernatural|thriller|historical|psychological|mecha|music|school|动作|冒险|喜剧|搞笑|剧情|奇幻|恐怖|悬疑|恋爱|爱情|科幻|日常|运动|超自然|惊悚|历史|心理|机甲|音乐|校园|战斗|武侠|魔法|推理", "|")
	out := []string{}
	for _, tag := range input {
		tag = strings.TrimSpace(tag)
		if slices.Contains(vocabulary, strings.ToLower(tag)) && !slices.Contains(out, tag) {
			out = append(out, tag)
		}
	}
	return out
}

// EnrichmentRating deduplicates public sources, validates original scales and
// returns the equal-weight ten-point mean and its reproducible formula.
func EnrichmentRating(input []metadata.FieldEvidence) (*float64, []metadata.FieldEvidence, string) {
	sources := []metadata.FieldEvidence{}
	terms := []string{}
	seen := map[string]bool{}
	sum := 0.0
	for _, e := range input {
		if !slices.Contains([]string{"anilist", "bangumi", "myanimelist"}, e.Source) || seen[e.Source] {
			continue
		}
		r := e.Metadata.SiteRating
		if r == nil || math.IsNaN(r.Value) || math.IsInf(r.Value, 0) || math.IsNaN(r.Scale) || math.IsInf(r.Scale, 0) || r.Scale <= 0 || r.Value < 0 || r.Value > r.Scale {
			continue
		}
		seen[e.Source] = true
		sources = append(sources, e)
		sum += r.Value / r.Scale * 10
		terms = append(terms, fmt.Sprintf("%g/%g*10", r.Value, r.Scale))
	}
	if len(sources) == 0 {
		return nil, sources, ""
	}
	result := sum / float64(len(sources))
	return &result, sources, fmt.Sprintf("(%s)/%d = %g", strings.Join(terms, " + "), len(sources), result)
}

// State holds the metadata involved in safe enrichment and reversal.
type State struct {
	Title     *string  `json:"title"`
	Summary   *string  `json:"summary"`
	Publisher *string  `json:"publisher"`
	Status    *string  `json:"status"`
	Rating    *float64 `json:"rating"`
	Tags      []string `json:"tags"`
	Links     []Link   `json:"links"`
}

// EnrichmentLocked checks current source-specific field protections.
func EnrichmentLocked(current database.Series, field string) bool {
	locks := strings.Split(current.LockedFields.String, ",")
	for _, l := range locks {
		l = strings.TrimSpace(l)
		if l == field || (field == "source_links" && l == "links") {
			return true
		}
	}
	return false
}

// EnrichmentQueries is the transaction-bound persistence for prepared enrichment states.
type EnrichmentQueries interface {
	UpdateSeriesMetadata(context.Context, database.UpdateSeriesMetadataParams) (database.Series, error)
	ClearSeriesTags(context.Context, int64) error
	UpsertTag(context.Context, string) (database.Tag, error)
	LinkSeriesTag(context.Context, database.LinkSeriesTagParams) error
	ClearSeriesLinks(context.Context, int64) error
	LinkSeriesLink(context.Context, database.LinkSeriesLinkParams) (database.SeriesLink, error)
	RefreshSeriesStats(context.Context, int64) error
}

func enrichmentNullString(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}

// WriteEnrichmentState writes a prepared state in the caller's transaction.
// Callers must check current locks and relevant expected values before this call,
// and roll back all its writes on any error (transaction or encompassing savepoint).
// It also refreshes derived statistics.
func WriteEnrichmentState(ctx context.Context, q EnrichmentQueries, current database.Series, state State, fields []string) error {
	rating := sql.NullFloat64{}
	if state.Rating != nil {
		rating = sql.NullFloat64{Float64: *state.Rating, Valid: true}
	}
	title := ""
	if state.Title != nil {
		title = *state.Title
	}
	_, err := q.UpdateSeriesMetadata(ctx, database.UpdateSeriesMetadataParams{ID: current.ID, Title: enrichmentNullString(state.Title), Summary: enrichmentNullString(state.Summary), Publisher: enrichmentNullString(state.Publisher), Status: enrichmentNullString(state.Status), Rating: rating, Language: current.Language, LockedFields: current.LockedFields, NameInitial: database.SeriesInitial(title, current.Name)})
	if err != nil {
		return err
	}
	for _, f := range fields {
		switch f {
		case "tags":
			if err = q.ClearSeriesTags(ctx, current.ID); err != nil {
				return err
			}
			for _, tag := range state.Tags {
				t, e := q.UpsertTag(ctx, tag)
				if e != nil {
					return e
				}
				if e = q.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: current.ID, TagID: t.ID}); e != nil {
					return e
				}
			}
		case "source_links":
			if err = q.ClearSeriesLinks(ctx, current.ID); err != nil {
				return err
			}
			for _, link := range state.Links {
				if _, err = q.LinkSeriesLink(ctx, database.LinkSeriesLinkParams{SeriesID: current.ID, Name: link.Name, Url: link.Url}); err != nil {
					return err
				}
			}
		}
	}
	return q.RefreshSeriesStats(ctx, current.ID)
}
