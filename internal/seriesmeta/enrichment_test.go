package seriesmeta_test

import (
	"testing"
	"yomifolio/internal/metadata"
	"yomifolio/internal/seriesmeta"
)

func TestEnrichmentUsesEqualSiteWeightsAndConservativeContentTags(t *testing.T) {
	rating, sources, formula := seriesmeta.EnrichmentRating([]metadata.FieldEvidence{
		{Source: "anilist", Metadata: metadata.SourceFacts{SiteRating: &metadata.SiteRating{Value: 80, Scale: 100}}},
		{Source: "bangumi", Metadata: metadata.SourceFacts{SiteRating: &metadata.SiteRating{Value: 9, Scale: 10}}},
		{Source: "openai", Metadata: metadata.SourceFacts{SiteRating: &metadata.SiteRating{Value: 10, Scale: 10}}},
		{Source: "anilist", Metadata: metadata.SourceFacts{SiteRating: &metadata.SiteRating{Value: 80, Scale: 100}}},
	})
	if rating == nil || *rating != 8.5 || len(sources) != 2 || formula == "" {
		t.Fatalf("rating=%v sources=%+v formula=%s", rating, sources, formula)
	}
	tags := seriesmeta.ContentTags([]string{"Science Fiction", "read", "2026", "favorite", "冒险", "完结", "Bangumi", "downloaded"})
	if len(tags) != 2 || tags[0] != "Science Fiction" || tags[1] != "冒险" {
		t.Fatal(tags)
	}
}

func TestEnrichmentRatingSingleAndMissingSources(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input []metadata.FieldEvidence
		want  *float64
	}{
		{name: "one site", input: []metadata.FieldEvidence{{Source: "myanimelist", Metadata: metadata.SourceFacts{SiteRating: &metadata.SiteRating{Value: 7.5, Scale: 10}}}}, want: ptrRating(7.5)},
		{name: "absent score", input: []metadata.FieldEvidence{{Source: "bangumi", Metadata: metadata.SourceFacts{Rating: 9}}}},
		{name: "invalid scale", input: []metadata.FieldEvidence{{Source: "bangumi", Metadata: metadata.SourceFacts{SiteRating: &metadata.SiteRating{Value: 9, Scale: 0}}}}},
		{name: "out of scale", input: []metadata.FieldEvidence{{Source: "anilist", Metadata: metadata.SourceFacts{SiteRating: &metadata.SiteRating{Value: 101, Scale: 100}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, sources, formula := seriesmeta.EnrichmentRating(tc.input)
			if tc.want == nil {
				if got != nil || len(sources) != 0 || formula != "" {
					t.Fatalf("invented score %v %v %s", got, sources, formula)
				}
				return
			}
			if got == nil || *got != *tc.want || len(sources) != 1 || formula != "(7.5/10*10)/1 = 7.5" {
				t.Fatalf("%v %v %s", got, sources, formula)
			}
		})
	}
}
func ptrRating(value float64) *float64 { return &value }
