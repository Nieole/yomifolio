package bootstrap_test

import (
	"archive/zip"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"yomifolio/internal/database"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func assertDiscoveryPage(t *testing.T, session *mcp.ClientSession, library, series int64) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "library_find_series", Arguments: map[string]any{"library_ids": []int64{library}, "series_ids": []int64{series}, "missing_fields": []string{"summary", "publisher"}, "added_from": "2000-01-01T00:00:00Z", "limit": 1}})
	if err != nil || result.IsError {
		t.Fatalf("discovery: %+v %v", result, err)
	}
	envelope, _ := json.Marshal(result)
	if len(envelope) > 256<<10 {
		t.Fatalf("oversized tool result: %d", len(envelope))
	}
	raw, _ := json.Marshal(result.StructuredContent)
	var page struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &page); err != nil || len(page.Items) != 1 || page.Items[0].ID != series {
		t.Fatalf("discovery page: %s %v", raw, err)
	}
}

func assertLocalClues(t *testing.T, session *mcp.ClientSession, store database.Store, library, series, hiddenLibrary, hiddenSeries int64, root string) {
	t.Helper()
	ctx := context.Background()
	call := func(name string, args map[string]any, wantError bool) json.RawMessage {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			// A capability the credential no longer holds also withdraws the tool itself.
			if wantError {
				return nil
			}
			t.Fatalf("%s: %+v %v", name, result, err)
		}
		if result.IsError != wantError {
			t.Fatalf("%s: %+v %v", name, result, err)
		}
		envelope, _ := json.Marshal(result)
		if len(envelope) > 256<<10 {
			t.Fatalf("oversized tool result: %d", len(envelope))
		}
		raw, _ := json.Marshal(result.StructuredContent)
		if strings.Contains(string(raw), filepath.ToSlash(root)) || strings.Contains(string(raw), `C:\`) {
			t.Fatalf("path leak: %s", raw)
		}
		return raw
	}
	// The public bootstrap/client boundary observes real local archives, not parser substitutes.
	var ids []int64
	for _, tc := range []struct{ name, xml, status string }{
		{"valid.cbz", "<ComicInfo><Series>Identity</Series><Title>Volume One</Title><Writer>Writer A</Writer></ComicInfo>", "available"},
		{"broken-tail.cbz", "<ComicInfo><Title>Valid</Title></ComicInfo><broken", "corrupt"},
		{"second-root.cbz", "<ComicInfo/><ComicInfo/>", "corrupt"},
		{"text-tail.cbz", "<ComicInfo/>extra", "corrupt"},
		{"misc.cbz", `<?xml version="1.0"?><!--before--><ComicInfo><Series>Identity</Series></ComicInfo>
<!--after--><?tool done?>`, "available"},
		{"missing.cbz", "", "missing"},
		{"corrupt.cbz", "<ComicInfo><Title>", "corrupt"},
		{"oversized.cbz", strings.Repeat("x", 65537), "too_large"},
		{"gone.cbz", "", "unavailable"},
	} {
		path := filepath.Join(root, tc.name)
		if tc.name != "gone.cbz" {
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			zw := zip.NewWriter(file)
			page, err := zw.Create("001.png")
			if err != nil {
				t.Fatal(err)
			}
			if err = png.Encode(page, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
				t.Fatal(err)
			}
			if tc.xml != "" {
				w, e := zw.Create("Scans/comicinfo.xml")
				if e != nil {
					t.Fatal(e)
				}
				if _, e = w.Write([]byte(tc.xml)); e != nil {
					t.Fatal(e)
				}
			}
			if err = zw.Close(); err != nil {
				t.Fatal(err)
			}
			if err = file.Close(); err != nil {
				t.Fatal(err)
			}
		}
		result, err := store.DB().ExecContext(ctx, `INSERT INTO books(series_id,library_id,name,path,size,file_modified_at) VALUES(?,?,?,?,0,CURRENT_TIMESTAMP)`, series, library, tc.name, path)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		ids = append(ids, id)
		raw := call("library_get_book_clues", map[string]any{"book_id": id}, false)
		var out struct {
			Book struct {
				ID       int64  `json:"id"`
				Filename string `json:"filename"`
			} `json:"book"`
			ComicInfo struct {
				Status string `json:"status"`
				Series string `json:"series"`
			} `json:"comic_info"`
		}
		if err = json.Unmarshal(raw, &out); err != nil || out.ComicInfo.Status != tc.status || out.Book.ID != id || out.Book.Filename != tc.name {
			t.Fatalf("clue %s: %s %v", tc.name, raw, err)
		}
		if tc.status == "available" && out.ComicInfo.Series != "Identity" {
			t.Fatalf("lost identity: %s", raw)
		}
	}
	cursor := int64(0)
	for i := 0; i*2 < len(ids); i++ {
		raw := call("library_list_books", map[string]any{"series_id": series, "limit": 2, "after_id": cursor}, false)
		var p struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
			Next int64 `json:"next_after_id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		expected := 2
		if len(ids)-i*2 < expected {
			expected = len(ids) - i*2
		}
		if len(p.Items) != expected || p.Items[0].ID != ids[i*2] {
			t.Fatalf("book page: %s", raw)
		}
		cursor = p.Next
		if (i+1)*2 < len(ids) && cursor == 0 || (i+1)*2 >= len(ids) && cursor != 0 {
			t.Fatalf("book cursor: %s", raw)
		}
	}
	for _, name := range []string{"library_find_series", "library_list_books"} {
		call(name, map[string]any{"series_id": series, "limit": 51}, true)
	}
	call("library_find_series", map[string]any{"library_ids": []int64{hiddenLibrary}}, true)
	call("library_find_series", map[string]any{"series_ids": []int64{hiddenSeries}, "added_before": "2000-01-01T00:00:00Z"}, true)
	call("library_list_books", map[string]any{"series_id": hiddenSeries}, true)
	result, err := store.DB().ExecContext(ctx, `INSERT INTO books(series_id,library_id,name,path,size,file_modified_at) VALUES(?,?, 'hidden',?,0,CURRENT_TIMESTAMP)`, hiddenSeries, hiddenLibrary, filepath.Join(root, "hidden.cbz"))
	if err != nil {
		t.Fatal(err)
	}
	hiddenBook, _ := result.LastInsertId()
	call("library_get_book_clues", map[string]any{"book_id": hiddenBook}, true)
	mismatch, err := store.DB().ExecContext(ctx, `INSERT INTO books(series_id,library_id,name,path,size,file_modified_at) VALUES(?,?,'mismatched',?,0,CURRENT_TIMESTAMP)`, hiddenSeries, library, filepath.Join(root, "mismatch.cbz"))
	if err != nil {
		t.Fatal(err)
	}
	mismatchID, _ := mismatch.LastInsertId()
	call("library_get_book_clues", map[string]any{"book_id": mismatchID}, true)
	call("library_get_book_clues", map[string]any{"book_id": 99999}, true)
	call("library_find_series", map[string]any{"missing_fields": []string{"unsupported"}}, true)
	call("library_find_series", map[string]any{"added_from": "not-a-date"}, true)
	raw := call("library_find_series", map[string]any{"series_ids": []int64{series}, "added_before": "2000-01-01T00:00:00Z"}, false)
	if !strings.Contains(string(raw), `"items":[]`) {
		t.Fatalf("time intersection: %s", raw)
	}
	raw = call("library_find_series", map[string]any{"missing_fields": []string{"authors", "tags", "source_links", "rating"}}, false)
	if !strings.Contains(string(raw), `"id":`) {
		t.Fatalf("missing collections: %s", raw)
	}
	// Two scoped series permit deterministic keyset traversal; hidden series never appears.
	second, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: library, Name: "Second", Path: filepath.Join(root, "second")})
	if err != nil {
		t.Fatal(err)
	}
	raw = call("library_find_series", map[string]any{"limit": 1}, false)
	var p struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
		Next int64 `json:"next_after_id"`
	}
	_ = json.Unmarshal(raw, &p)
	if len(p.Items) != 1 || p.Items[0].ID != series || p.Next != series {
		t.Fatalf("first series page: %s", raw)
	}
	raw = call("library_find_series", map[string]any{"limit": 1, "after_id": p.Next}, false)
	_ = json.Unmarshal(raw, &p)
	if len(p.Items) != 1 || p.Items[0].ID != second.ID {
		t.Fatalf("second series page: %s", raw)
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE series SET publisher='Known' WHERE id=?`, second.ID); err != nil {
		t.Fatal(err)
	}
	// A series lacking any one listed field matches; only one holding every listed field drops out.
	missing := func() []int64 {
		t.Helper()
		raw := call("library_find_series", map[string]any{"missing_fields": []string{"summary", "publisher"}}, false)
		var filtered struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &filtered); err != nil {
			t.Fatal(err)
		}
		ids := make([]int64, 0, len(filtered.Items))
		for _, item := range filtered.Items {
			ids = append(ids, item.ID)
		}
		return ids
	}
	if got := missing(); !slices.Equal(got, []int64{series, second.ID}) {
		t.Fatalf("ANY missing filter with publisher known: %v", got)
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE series SET summary='Known' WHERE id=?`, second.ID); err != nil {
		t.Fatal(err)
	}
	if got := missing(); !slices.Equal(got, []int64{series}) {
		t.Fatalf("ANY missing filter with both known: %v", got)
	}
	// Payload bounds fail rather than silently truncating local evidence.
	if _, err = store.DB().ExecContext(ctx, `UPDATE series SET path=? WHERE id=?`, filepath.Join(root, strings.Repeat("x", 250<<10)), second.ID); err != nil {
		t.Fatal(err)
	}
	call("library_find_series", map[string]any{"series_ids": []int64{second.ID}}, true)
	call("library_get_series", map[string]any{"series_id": second.ID}, true)
	if _, err = store.DB().ExecContext(ctx, `UPDATE series SET path=?,created_at='2025-01-01 00:00:00' WHERE id=?`, filepath.Join(root, "second"), second.ID); err != nil {
		t.Fatal(err)
	}
	raw = call("library_find_series", map[string]any{"series_ids": []int64{second.ID}, "added_from": "2025-01-01T00:00:00Z", "added_before": "2025-01-01T00:00:01Z"}, false)
	if !strings.Contains(string(raw), `"name":"Second"`) {
		t.Fatalf("inclusive lower boundary: %s", raw)
	}
	raw = call("library_find_series", map[string]any{"series_ids": []int64{second.ID}, "added_before": "2025-01-01T00:00:00Z"}, false)
	if !strings.Contains(string(raw), `"items":[]`) {
		t.Fatalf("exclusive upper boundary: %s", raw)
	}

}
