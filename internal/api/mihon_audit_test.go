package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"yomifolio/internal/database"
)

func TestMihonSearchExplicitSortPaging(t *testing.T) {
	c, store, _, root := newTestController(t)
	ctx := context.Background()
	var libraryID int64
	for i, name := range []string{"Z hero 漫", "A hero 漫", "M hero 漫", "B hero 漫", "Y hero 漫"} {
		lib, series, _ := seedBookFixture(t, store, root, fmt.Sprintf("Library %d", i), name, "01.cbz", 12)
		if i == 0 {
			libraryID = lib.ID
		}
		// Interleave libraries so library filtering must happen before pagination.
		if i%2 == 0 {
			lib.ID = libraryID
			if _, err := store.DB().Exec("UPDATE series SET library_id=? WHERE id=?", libraryID, series.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := store.DB().Exec("UPDATE books SET library_id=? WHERE series_id=?", libraryID, series.ID); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.DB().Exec("UPDATE series SET updated_at=? WHERE id=?", fmt.Sprintf("2026-01-%02d 00:00:00", 10-i), series.ID); err != nil {
			t.Fatal(err)
		}
		for b := 0; b < i%3; b++ {
			if _, err := store.CreateBook(ctx, database.CreateBookParams{SeriesID: series.ID, LibraryID: lib.ID, Name: fmt.Sprintf("extra%d.cbz", b), Path: fmt.Sprintf("%s/extra%d.cbz", series.Path, b)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, q := range []string{"hero", "漫"} {
		for _, lib := range []int64{0, libraryID} {
			for _, sort := range []struct{ api, db string }{{"books_desc", "books_DESC"}, {"updated_desc", "updated_DESC"}, {"name_asc", "name_ASC"}, {"unknown", "name_ASC"}} {
				t.Run(fmt.Sprintf("%s/%d/%s", q, lib, sort.api), func(t *testing.T) {
					expected, total, err := store.SearchSeriesPaged(ctx, lib, database.SeriesListFilters{Keyword: q}, 100, 0, sort.db)
					if err != nil {
						t.Fatal(err)
					}
					if total < 3 {
						t.Fatalf("fixture did not exercise multiple pages: %d", total)
					}
					var got []int64
					for page := 1; page <= (total+1)/2+1; page++ {
						rec := httptest.NewRecorder()
						c.reader.mihonSeries(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/mihon/v1/series?q=%s&libraryId=%d&sort=%s&limit=2&page=%d", url.QueryEscape(q), lib, sort.api, page), nil))
						if rec.Code != http.StatusOK {
							t.Fatalf("%d: %s", rec.Code, rec.Body.String())
						}
						var resp MihonSeriesPageResponse
						if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
							t.Fatal(err)
						}
						if resp.Total != int64(total) || resp.Page != page || resp.Limit != 2 || resp.HasNext != (page*2 < total) {
							t.Fatalf("invalid pagination: %+v", resp)
						}
						for _, item := range resp.Items {
							got = append(got, item.ID)
						}
					}
					var want []int64
					for _, item := range expected {
						want = append(want, item.ID)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("IDs = %v, want %v", got, want)
					}
				})
			}
		}
		t.Run(q+"/relevance", func(t *testing.T) {
			rows, total, err := store.SearchProtocolSeries(ctx, q, 2, 0)
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			c.reader.mihonSeries(rec, httptest.NewRequest(http.MethodGet, "/api/mihon/v1/series?q="+url.QueryEscape(q)+"&limit=2", nil))
			var resp MihonSeriesPageResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if resp.Total != int64(total) || len(resp.Items) != len(rows) {
				t.Fatalf("unexpected relevance page: %+v", resp)
			}
			for i, row := range rows {
				if resp.Items[i].ID != row.ID {
					t.Fatalf("relevance order: %+v", resp.Items)
				}
			}
		})
	}
}

type mihonMetadataStore struct {
	ReaderQueryStore
	authorsErr, tagsErr   error
	authorCalls, tagCalls int
}

func (s *mihonMetadataStore) GetAuthorsForSeries(ctx context.Context, id int64) ([]database.Author, error) {
	s.authorCalls++
	if s.authorsErr != nil {
		return nil, s.authorsErr
	}
	return s.ReaderQueryStore.GetAuthorsForSeries(ctx, id)
}
func (s *mihonMetadataStore) GetTagsForSeries(ctx context.Context, id int64) ([]database.Tag, error) {
	s.tagCalls++
	if s.tagsErr != nil {
		return nil, s.tagsErr
	}
	return s.ReaderQueryStore.GetTagsForSeries(ctx, id)
}

func TestMihonSeriesDetailMetadata(t *testing.T) {
	c, store, _, root := newTestController(t)
	_, series, _ := seedBookFixture(t, store, root, "Library", "Series", "01.cbz", 12)
	ctx := context.Background()
	for _, role := range []string{"story", "art"} {
		author, err := store.UpsertAuthor(ctx, database.UpsertAuthorParams{Name: "Same Author", Role: role})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.LinkSeriesAuthor(ctx, database.LinkSeriesAuthorParams{SeriesID: series.ID, AuthorID: author.ID}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Zebra", "Action"} {
		tag, err := store.UpsertTag(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: series.ID, TagID: tag.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.UpsertTag(ctx, "Unlinked"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertAuthor(ctx, database.UpsertAuthorParams{Name: "Unlinked", Role: "art"}); err != nil {
		t.Fatal(err)
	}
	wrapped := &mihonMetadataStore{ReaderQueryStore: c.reader.store}
	c.reader.store = wrapped
	detail := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c.reader.mihonSeriesDetail(rec, requestWithRouteParam(http.MethodGet, "/api/mihon/v1/series/1", nil, "seriesId", fmt.Sprint(series.ID)))
		return rec
	}
	rec := detail()
	if rec.Code != http.StatusOK {
		t.Fatalf("%d: %s", rec.Code, rec.Body.String())
	}
	var resp MihonSeriesDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != series.ID || !reflect.DeepEqual(resp.Authors, []MihonSeriesAuthorResponse{{Name: "Same Author", Role: "art"}, {Name: "Same Author", Role: "story"}}) || !reflect.DeepEqual(resp.Tags, []string{"Action", "Zebra"}) {
		t.Fatalf("unexpected detail: %+v", resp)
	}
	wrapped.authorCalls, wrapped.tagCalls = 0, 0
	rec = httptest.NewRecorder()
	c.reader.mihonSeries(rec, httptest.NewRequest(http.MethodGet, "/api/mihon/v1/series", nil))
	if rec.Code != http.StatusOK || wrapped.authorCalls != 0 || wrapped.tagCalls != 0 {
		t.Fatalf("catalogue fetched metadata: %s", rec.Body.String())
	}
	var page map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	item := page["items"].([]any)[0].(map[string]any)
	if _, ok := item["authors"]; ok {
		t.Fatal("catalogue includes detail authors")
	}
	if _, ok := item["tags"]; ok {
		t.Fatal("catalogue includes detail tags")
	}
	if err := store.ClearSeriesAuthors(ctx, series.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearSeriesTags(ctx, series.ID); err != nil {
		t.Fatal(err)
	}
	rec = detail()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["authors"]) != "[]" || string(raw["tags"]) != "[]" {
		t.Fatalf("empty metadata: %s", rec.Body.String())
	}
	for _, field := range []string{"authors", "tags"} {
		wrapped.authorsErr, wrapped.tagsErr = nil, nil
		if field == "authors" {
			wrapped.authorsErr = errors.New("unavailable")
		} else {
			wrapped.tagsErr = errors.New("unavailable")
		}
		if rec := detail(); rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s failure: %d %s", field, rec.Code, rec.Body.String())
		}
	}
}
