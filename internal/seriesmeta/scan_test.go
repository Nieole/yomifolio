package seriesmeta_test

import (
	"database/sql"
	"testing"

	"yomifolio/internal/database"
	"yomifolio/internal/parser"
	"yomifolio/internal/seriesmeta"
)

func TestScanInitialMetadataAndExistingLocks(t *testing.T) {
	info := &parser.ComicInfo{Summary: "Archive summary", Publisher: "Publisher", LanguageISO: "ja", CommunityRating: 4.5}
	initial := seriesmeta.PrepareScanInitial("Directory", info)
	if initial.Title.String != "Directory" || initial.LockedFields.String != "title" || initial.NameInitial != "D" || initial.Summary.String != "Archive summary" || initial.Status.Valid || initial.Rating.Float64 != 4.5 {
		t.Fatalf("initial metadata: %+v", initial)
	}
	store, current := fixture(t)
	current.Title = sql.NullString{String: "Custom", Valid: true}
	current.Summary = sql.NullString{String: "Manual summary", Valid: true}
	current.Publisher = sql.NullString{String: "Old publisher", Valid: true}
	current.Status = sql.NullString{String: "ongoing", Valid: true}
	current.Rating = sql.NullFloat64{Float64: 9, Valid: true}
	current.LockedFields = sql.NullString{String: " summary , rating ,tags,authors ", Valid: true}
	update := seriesmeta.PrepareScanUpdate(current, nil, "Directory", &parser.ComicInfo{})
	if update.Title != current.Title || update.NameInitial != "C" || update.Summary != current.Summary || update.Rating != current.Rating || update.Publisher.Valid || update.Language.Valid || update.Status.Valid || update.LockedFields != current.LockedFields {
		t.Fatalf("updated metadata: %+v", update)
	}
	// The caller composes the metadata with identity and counts in its existing upsert.
	err := store.ExecTx(t.Context(), func(q *database.Queries) error {
		_, err := q.UpsertSeriesByPath(t.Context(), database.UpsertSeriesByPathParams{LibraryID: current.LibraryID, Name: current.Name, Path: current.Path, Title: update.Title, Summary: update.Summary, Publisher: update.Publisher, Status: update.Status, Rating: update.Rating, Language: update.Language, LockedFields: update.LockedFields, NameInitial: update.NameInitial, BookCount: 7, TotalPages: 70})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSeries(t.Context(), current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != current.ID || got.Path != current.Path || got.Name != "Alpha" || got.BookCount != 7 || got.TotalPages != 70 || got.Summary.String != "Manual summary" || got.Title.String != "Custom" {
		t.Fatalf("composed series: %+v", got)
	}
}

func TestScanAssociationsAppendDespiteCollectionLocksAndErrors(t *testing.T) {
	store, current := fixture(t)
	if err := store.ExecTx(t.Context(), func(q *database.Queries) error {
		_, err := seriesmeta.SaveManual(t.Context(), q, current, seriesmeta.ManualInput{LockedFields: "tags,authors", Tags: []string{"Old"}, Authors: []seriesmeta.Author{{Name: "Existing", Role: "writer"}}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_tag BEFORE INSERT ON tags WHEN NEW.name = 'Rejected' BEGIN SELECT RAISE(FAIL, 'rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.ExecTx(t.Context(), func(q *database.Queries) error {
		seriesmeta.AppendScanAssociations(t.Context(), q, current.ID, &parser.ComicInfo{Genre: "Rejected, New, Old", Writer: "Writer", Penciller: "Artist"})
		seriesmeta.AppendScanAssociations(t.Context(), q, current.ID, nil)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tags, err := store.GetTagsForSeries(t.Context(), current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[0].Name != "New" || tags[1].Name != "Old" {
		t.Fatalf("tags: %+v", tags)
	}
	authors, err := store.GetAuthorsForSeries(t.Context(), current.ID)
	if err != nil {
		t.Fatal(err)
	}
	roles := make(map[string]string)
	for _, author := range authors {
		roles[author.Name] = author.Role
	}
	if len(authors) != 3 || roles["Existing"] != "writer" || roles["Writer"] != "writer" || roles["Artist"] != "penciller" {
		t.Fatalf("authors: %+v", authors)
	}
}

func TestScanTitleFallbackAndLockedScalarMatrix(t *testing.T) {
	for _, tc := range []struct {
		name      string
		title     sql.NullString
		lock      string
		wantTitle sql.NullString
		initial   string
	}{
		{"missing unlocked title", sql.NullString{}, "", sql.NullString{String: "Directory", Valid: true}, "D"},
		{"blank unlocked title", sql.NullString{String: "  ", Valid: true}, "", sql.NullString{String: "Directory", Valid: true}, "D"},
		{"missing locked title", sql.NullString{}, "title", sql.NullString{}, "D"},
		{"existing custom title", sql.NullString{String: "Zulu", Valid: true}, "", sql.NullString{String: "Zulu", Valid: true}, "Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := database.Series{Title: tc.title, Summary: sql.NullString{String: "Summary", Valid: true}, Publisher: sql.NullString{String: "Publisher", Valid: true}, Status: sql.NullString{String: "ongoing", Valid: true}, Language: sql.NullString{String: "ja", Valid: true}, Rating: sql.NullFloat64{Float64: 8, Valid: true}, LockedFields: sql.NullString{String: tc.lock + ",summary,publisher,status,language,rating", Valid: true}}
			got := seriesmeta.PrepareScanUpdate(current, nil, "Directory", &parser.ComicInfo{Summary: "Other", CommunityRating: -1})
			if got.Title != tc.wantTitle || got.NameInitial != tc.initial || got.Summary != current.Summary || got.Publisher != current.Publisher || got.Status != current.Status || got.Language != current.Language || got.Rating != current.Rating {
				t.Fatalf("locked update: %+v", got)
			}
		})
	}
	initial := seriesmeta.PrepareScanInitial("Directory", nil)
	if initial.Summary.Valid || initial.Publisher.Valid || initial.Status.Valid || initial.Language.Valid || initial.Rating.Valid {
		t.Fatalf("nil ComicInfo: %+v", initial)
	}
}

func TestScanKeepsOnlyValuesTheirSourceRecordStillHolds(t *testing.T) {
	record := func(field, value, source string) database.SeriesMetadataProvenance {
		return database.SeriesMetadataProvenance{FieldName: field, Value: value, Source: source}
	}
	text := func(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
	rating := func(f float64) sql.NullFloat64 { return sql.NullFloat64{Float64: f, Valid: true} }
	info := &parser.ComicInfo{Summary: "ComicInfo summary", Publisher: "ComicInfo publisher", LanguageISO: "ja", CommunityRating: 3.5}
	for _, tc := range []struct {
		name    string
		current database.Series
		records []database.SeriesMetadataProvenance
		want    database.Series
	}{
		{
			name:    "proposal values as recorded",
			current: database.Series{Summary: text("Source summary"), Publisher: text("Source publisher"), Status: text("completed"), Rating: rating(7.85)},
			records: []database.SeriesMetadataProvenance{record("summary", "Source summary", "Bangumi"), record("publisher", "Source publisher", "AniList"), record("status", "completed", "Bangumi"), record("rating", "7.8", "Bangumi")},
			want:    database.Series{Summary: text("Source summary"), Publisher: text("Source publisher"), Status: text("completed"), Rating: rating(7.85), Language: text("ja")},
		},
		{
			name:    "enrichment formula rating as recorded",
			current: database.Series{Rating: rating(25.0 / 3)},
			records: []database.SeriesMetadataProvenance{record("rating", "8.333333333333334", "bangumi")},
			want:    database.Series{Summary: text("ComicInfo summary"), Publisher: text("ComicInfo publisher"), Rating: rating(25.0 / 3), Language: text("ja")},
		},
		{
			name:    "changed since the record",
			current: database.Series{Summary: text("Owner summary"), Rating: rating(9)},
			records: []database.SeriesMetadataProvenance{record("summary", "Source summary", "Bangumi"), record("rating", "8.6", "Bangumi")},
			want:    database.Series{Summary: text("ComicInfo summary"), Publisher: text("ComicInfo publisher"), Rating: rating(3.5), Language: text("ja")},
		},
		{
			name:    "upgrade backfill names no source",
			current: database.Series{Summary: text("Old summary"), Language: text("zh")},
			records: []database.SeriesMetadataProvenance{record("summary", "Old summary", "manual"), record("language", "zh", "manual")},
			want:    database.Series{Summary: text("ComicInfo summary"), Publisher: text("ComicInfo publisher"), Rating: rating(3.5), Language: text("ja")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := seriesmeta.PrepareScanUpdate(tc.current, tc.records, "Directory", info)
			if got.Summary != tc.want.Summary || got.Publisher != tc.want.Publisher || got.Status != tc.want.Status || got.Rating != tc.want.Rating || got.Language != tc.want.Language || got.LockedFields.Valid {
				t.Fatalf("scan update %+v want %+v", got, tc.want)
			}
		})
	}
}
