// A rating computed from identified sources is the server's own result, so a later identification
// recomputes it over every source while nobody has changed it, and never once a person has. The rule
// is pinned at the enrichment entry every identification, an agent's or the owner's, goes through.

package enrichment_test

import (
	"context"
	"path/filepath"
	"testing"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
)

func rated(source string, value, scale float64) enrichment.IdentifiedSource {
	return enrichment.IdentifiedSource{CandidateID: source + ":1", Source: source, EntryURL: "https://" + source + ".example/1",
		Facts: &metadata.SourceFacts{SiteRating: &metadata.SiteRating{Value: value, Scale: scale}}}
}

func TestIdentifiedRatingIsRecomputedOnlyWhileItIsAnUntouchedFormulaResult(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "identified.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.DB().Exec(`INSERT INTO users(id,username,role) VALUES(1,'owner','admin')`); err != nil {
		t.Fatal(err)
	}
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "L", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "S", Path: lib.Path + "/s"})
	if err != nil {
		t.Fatal(err)
	}
	svc := enrichment.New(store.DB(), nil)
	enrich := func(key string, sources ...enrichment.IdentifiedSource) enrichment.Operation {
		t.Helper()
		tx, err := store.DB().BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		out, err := svc.EnrichIdentified(ctx, tx, delegated.Subject{UserID: 1}, enrichment.IdentifiedInput{RequestKey: key, SeriesID: series.ID, Reason: "identified", Sources: sources})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	rating := func() float64 {
		t.Helper()
		got, err := store.GetSeries(ctx, series.ID)
		if err != nil || !got.Rating.Valid {
			t.Fatalf("rating %+v %v", got.Rating, err)
		}
		return got.Rating.Float64
	}

	enrich("first", rated("bangumi", 9, 10))
	if got := rating(); got != 9 {
		t.Fatalf("first rating %v", got)
	}
	// A new source arrives while the rating is still the formula's: every source counts again.
	second := enrich("second", rated("bangumi", 9, 10), rated("anilist", 80, 100))
	if got := rating(); got != 8.5 {
		t.Fatalf("recomputed rating %v", got)
	}
	for _, f := range second.Fields {
		if f.Field == "rating" && (!f.Changed || f.Formula != "(9/10*10 + 80/100*10)/2 = 8.5") {
			t.Fatalf("recomputed rating audit %+v", f)
		}
	}
	// Once a person sets the rating it is theirs.
	if _, err := store.DB().Exec(`UPDATE series SET rating = 6 WHERE id = ?`, series.ID); err != nil {
		t.Fatal(err)
	}
	enrich("third", rated("bangumi", 9, 10), rated("anilist", 80, 100), rated("myanimelist", 7, 10))
	if got := rating(); got != 6 {
		t.Fatalf("a person's rating became %v", got)
	}
}
