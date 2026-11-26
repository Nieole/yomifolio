// These tests guard the author correction primitive: an agent replaces series-local author
// associations only with credits the series' identified sources list, against the whole current
// author list, with a receipt the existing operation tools read and undo.

package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/task"
)

// credit is one author association as the tools and the series page show it.
type credit struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// creditedSources answer every query "Title <n>" with one entry per source: Bangumi credits the
// canonical pen name and the artist, AniList credits a writer nobody identified the series to.
func creditedSources(source, query string) ([]*metadata.SeriesMetadata, error) {
	var n int
	if _, err := fmt.Sscanf(query, "Title %d", &n); err != nil {
		return nil, nil
	}
	switch source {
	case "bangumi":
		return []*metadata.SeriesMetadata{{Title: query, SourceID: n, SourceURL: fmt.Sprintf("https://bgm.tv/subject/%d", n),
			Authors: []metadata.SeriesAuthor{{Name: "Canonical", Role: "Writer"}, {Name: "Artist", Role: "Artist"}}}}, nil
	case "anilist":
		return []*metadata.SeriesMetadata{{Title: query, SourceID: n, SourceURL: fmt.Sprintf("https://anilist.co/manga/%d", n),
			Authors: []metadata.SeriesAuthor{{Name: "Unconfirmed", Role: "Writer"}}}}, nil
	}
	return nil, fmt.Errorf("%s is down", source)
}

// authorWorld is a library whose first series an agent identified to Bangumi, while every series
// still carries the alias credit they share.
type authorWorld struct {
	t       *testing.T
	store   database.Store
	web     *identificationWeb
	agent   *mainLoopAgent
	library *prefetchLibrary
	series  []int64
}

func openAuthorWorld(t *testing.T) *authorWorld {
	t.Helper()
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library, ids := claimLibrary(t, store, "Authors", "Title 1", "Title 2")
	if _, err := store.DB().Exec(`INSERT INTO authors(name, role) VALUES ('Alias', 'Artist')`); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if _, err := store.DB().Exec(`INSERT INTO series_authors SELECT ?, id FROM authors WHERE name = 'Alias'`, id); err != nil {
			t.Fatal(err)
		}
	}
	web := openClaimWeb(t, store, cfg, &recordingSources{answer: creditedSources}, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	batch := agent.claim(map[string]any{})
	results := agent.submit("identify-first", batch.LeaseID, identifyVerdict(ids[0], "directory and title agree", "bangumi:1"))
	wantResult(t, results[0], ids[0], true, "identified")
	return &authorWorld{t: t, store: store, web: web, agent: agent, library: library, series: ids}
}

// authors reads the series' current credits from the owner's series page, in a stable order.
func (w *authorWorld) authors(series int64) []credit {
	w.t.Helper()
	var page struct {
		Authors []credit `json:"authors"`
	}
	w.web.call("GET", "/api/series/"+strconv.FormatInt(series, 10)+"/context", http.StatusOK, &page)
	slices.SortFunc(page.Authors, func(a, b credit) int {
		if a.Name+"\x00"+a.Role < b.Name+"\x00"+b.Role {
			return -1
		}
		return 1
	})
	return page.Authors
}

// receipt mirrors the operation receipt fields these tests read.
type receipt struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Outcome  string `json:"outcome"`
	Reason   string `json:"reason"`
	Applied  bool   `json:"applied"`
	Replayed bool   `json:"replayed"`
	ParentID string `json:"parent_id"`
	UndoneBy string `json:"undone_by"`
	Before   struct {
		Authors []credit `json:"authors"`
	} `json:"before"`
	After struct {
		Authors []credit `json:"authors"`
	} `json:"after"`
	Fields []struct {
		Field   string `json:"field"`
		Changed bool   `json:"changed"`
		Reason  string `json:"reason"`
		Sources []struct {
			CandidateID string `json:"candidate_id"`
			Source      string `json:"source"`
		} `json:"sources"`
	} `json:"fields"`
	CoverSelection *struct {
		BookID int64  `json:"book_id"`
		Page   int64  `json:"page"`
		Reason string `json:"reason"`
	} `json:"cover_selection"`
	OldCoverMaterial string `json:"old_cover_material"`
	NewCoverMaterial string `json:"new_cover_material"`
}

func correction(from, to credit, reason string) map[string]any {
	return map[string]any{"from": from, "to": to, "reason": reason}
}

// correct calls the primitive and fails the test when it is refused.
func (w *authorWorld) correct(key string, series int64, expected []credit, corrections ...map[string]any) receipt {
	w.t.Helper()
	var out receipt
	if result := w.agent.call("library_correct_authors", map[string]any{"request_key": key, "series_id": series, "expected_authors": expected, "corrections": corrections}, &out); result.IsError {
		w.t.Fatalf("correction %s refused: %s", key, resultText(result))
	}
	return out
}

// refused calls the primitive and fails the test unless it is refused.
func (w *authorWorld) refused(key string, series int64, expected []credit, corrections ...map[string]any) {
	w.t.Helper()
	var out receipt
	if result := w.agent.call("library_correct_authors", map[string]any{"request_key": key, "series_id": series, "expected_authors": expected, "corrections": corrections}, &out); !result.IsError {
		w.t.Fatalf("correction %s accepted: %+v", key, out)
	}
}

var (
	alias     = credit{"Alias", "Artist"}
	canonical = credit{"Canonical", "Writer"}
)

func without(list []credit, drop credit) []credit {
	return slices.DeleteFunc(slices.Clone(list), func(c credit) bool { return c == drop })
}

func TestAgentCorrectsAnAuthorWithAnIdentifiedSourceCredit(t *testing.T) {
	w := openAuthorWorld(t)
	first, other := w.series[0], w.series[1]
	current := w.authors(first)
	if !slices.Contains(current, alias) {
		t.Fatalf("identified series lost its alias credit: %v", current)
	}
	// Shared author records are never edited, only the series' associations.
	if _, err := w.store.DB().Exec(`CREATE TRIGGER identity_immutable BEFORE UPDATE ON authors BEGIN SELECT RAISE(ABORT, 'identity update forbidden'); END`); err != nil {
		t.Fatal(err)
	}

	applied := w.correct("authors-1", first, current, correction(alias, canonical, "Bangumi credits the canonical pen name"))
	if applied.Kind != "enrich" || applied.Outcome != "changed" || !applied.Applied {
		t.Fatalf("correction receipt %+v", applied)
	}
	want := append(without(current, alias), canonical)
	got := w.authors(first)
	slices.SortFunc(want, func(a, b credit) int {
		if a.Name+"\x00"+a.Role < b.Name+"\x00"+b.Role {
			return -1
		}
		return 1
	})
	if !slices.Equal(got, slices.Compact(want)) {
		t.Fatalf("corrected credits %v want %v", got, want)
	}
	if shared := w.authors(other); !slices.Equal(shared, []credit{alias}) {
		t.Fatalf("another series' credits changed: %v", shared)
	}
	var kept int
	if err := w.store.DB().QueryRow(`SELECT count(*) FROM authors WHERE name = 'Alias' AND role = 'Artist'`).Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("shared author record: %d %v", kept, err)
	}
	if len(applied.Fields) != 1 || applied.Fields[0].Field != "authors" || !applied.Fields[0].Changed || !strings.Contains(applied.Fields[0].Reason, "Bangumi credits the canonical pen name") ||
		len(applied.Fields[0].Sources) != 1 || applied.Fields[0].Sources[0].CandidateID != "bangumi:1" {
		t.Fatalf("correction audit %+v", applied.Fields)
	}
	// The owner reads the same receipt in the operation history.
	if rec := w.web.raw("GET", "/api/mcp/operations/"+applied.ID, ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Canonical") || !strings.Contains(rec.Body.String(), "Alias") {
		t.Fatalf("owner receipt %d %s", rec.Code, rec.Body)
	}

	var read receipt
	if result := w.agent.call("library_get_metadata_operation", map[string]any{"operation_id": applied.ID}, &read); result.IsError || read.ID != applied.ID || read.Outcome != "changed" {
		t.Fatalf("receipt read %+v %s", read, resultText(result))
	}
	var undone receipt
	if result := w.agent.call("library_undo_metadata_operation", map[string]any{"request_key": "authors-1-undo", "operation_id": applied.ID}, &undone); result.IsError || undone.Outcome != "changed" {
		t.Fatalf("undo %+v %s", undone, resultText(result))
	}
	if restored := w.authors(first); !slices.Equal(restored, current) {
		t.Fatalf("undo restored %v want %v", restored, current)
	}
}

func TestAuthorCorrectionKeepsToCurrentAuthorsAndIdentifiedCredits(t *testing.T) {
	w := openAuthorWorld(t)
	first, unidentified := w.series[0], w.series[1]
	current := w.authors(first)

	// to must be credited by a source the series is identified to: not by another candidate of
	// its evidence, and not invented.
	w.refused("to-unidentified-candidate", first, current, correction(alias, credit{"Unconfirmed", "Writer"}, "AniList credit"))
	w.refused("to-invented", first, current, correction(alias, credit{"Invented", "Writer"}, "no source"))
	w.refused("to-other-role", first, current, correction(alias, credit{"Canonical", "Artist"}, "role not credited"))
	// from must be in the expected author list; a correction needs a reason and a real change.
	w.refused("from-absent", first, current, correction(credit{"Nobody", "Writer"}, canonical, "not a current author"))
	w.refused("no-reason", first, current, correction(alias, canonical, " "))
	w.refused("same", first, current, correction(canonical, canonical, "nothing to change"))
	w.refused("twice", first, current, correction(alias, canonical, "first"), correction(alias, credit{"Artist", "Artist"}, "second"))
	// A series nobody identified has no source to back any credit, even one its evidence lists.
	w.refused("unidentified-series", unidentified, w.authors(unidentified), correction(alias, canonical, "Bangumi candidate credit"))
	if got := w.authors(first); !slices.Equal(got, current) {
		t.Fatalf("refused corrections changed credits: %v", got)
	}

	// The whole author list is the expectation: another member added since is a conflict.
	if _, err := w.store.DB().Exec(`INSERT INTO authors(name, role) VALUES ('Later', 'Writer');
		INSERT INTO series_authors SELECT ?, id FROM authors WHERE name = 'Later'`, first); err != nil {
		t.Fatal(err)
	}
	stale := w.correct("stale-list", first, current, correction(alias, canonical, "Bangumi credits the canonical pen name"))
	if stale.Outcome != "conflict" || stale.Applied {
		t.Fatalf("stale expected list %+v", stale)
	}
	current = w.authors(first)
	// A locked author field is a conflict too.
	if _, err := w.store.DB().Exec(`UPDATE series SET locked_fields = 'authors' WHERE id = ?`, first); err != nil {
		t.Fatal(err)
	}
	if locked := w.correct("locked", first, current, correction(alias, canonical, "Bangumi credits the canonical pen name")); locked.Outcome != "conflict" {
		t.Fatalf("locked authors %+v", locked)
	}
	if got := w.authors(first); !slices.Equal(got, current) {
		t.Fatalf("conflicts changed credits: %v", got)
	}
	if _, err := w.store.DB().Exec(`UPDATE series SET locked_fields = '' WHERE id = ?`, first); err != nil {
		t.Fatal(err)
	}

	// Order and duplicates in the expected list are not edits; a retry replays the receipt and the
	// same key cannot carry another intent.
	reordered := append(slices.Clone(current), current[0])
	slices.Reverse(reordered)
	applied := w.correct("reordered", first, reordered, correction(alias, canonical, "Bangumi credits the canonical pen name"))
	if applied.Outcome != "changed" {
		t.Fatalf("reordered expected list %+v", applied)
	}
	replay := w.correct("reordered", first, reordered, correction(alias, canonical, "Bangumi credits the canonical pen name"))
	if replay.ID != applied.ID || !replay.Replayed || replay.Applied {
		t.Fatalf("replay %+v of %+v", replay, applied)
	}
	w.refused("reordered", first, reordered, correction(alias, canonical, "another reason"))
	w.refused("reordered", first, reordered, correction(alias, credit{"Artist", "Artist"}, "Bangumi credits the canonical pen name"))
	w.refused("reordered", first, append(slices.Clone(reordered), credit{"Alias", "Writer"}), correction(alias, canonical, "Bangumi credits the canonical pen name"))

	// Two aliases may merge into one credited identity; another role of the same name stays its own.
	w.exec(`INSERT INTO authors(name, role) VALUES ('Other alias', 'Writer'), ('Canonical', 'Artist');
		INSERT OR IGNORE INTO series_authors SELECT ?, id FROM authors WHERE name IN ('Other alias', 'Canonical')`, first)
	w.exec(`INSERT INTO series_authors SELECT ?, id FROM authors WHERE name = 'Alias'`, first)
	current = w.authors(first)
	merged := w.correct("merge", first, current, correction(alias, canonical, "pen name"), correction(credit{"Other alias", "Writer"}, canonical, "same pen name"))
	if merged.Outcome != "changed" {
		t.Fatalf("merge %+v", merged)
	}
	want := slices.DeleteFunc(slices.Clone(current), func(c credit) bool { return c == alias || c == credit{"Other alias", "Writer"} })
	if got := w.authors(first); !slices.Equal(got, want) || !slices.Contains(got, credit{"Canonical", "Artist"}) {
		t.Fatalf("merged credits %v want %v", got, want)
	}
}

func (w *authorWorld) exec(query string, args ...any) {
	w.t.Helper()
	if _, err := w.store.DB().Exec(query, args...); err != nil {
		w.t.Fatal(err)
	}
}

func TestAuthorCorrectionRestsOnHistoricalIdentification(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	library := newPrefetchLibrary(t, store, "History")
	series := library.series("Kept", "", "01.cbz")
	historicalProposal(t, store, series, "bangumi", 7, "https://bgm.tv/subject/7",
		`{"Title":"Kept","Authors":[{"Name":"Canonical","Role":"Writer"}]}`, "applied")
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO authors(name, role) VALUES ('Alias', 'Artist');
		INSERT INTO series_authors SELECT ?, id FROM authors WHERE name = 'Alias'`, series); err != nil {
		t.Fatal(err)
	}
	web := openClaimWeb(t, store, cfg, &recordingSources{answer: creditedSources}, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	w := &authorWorld{t: t, store: store, web: web, library: library, series: []int64{series},
		agent: web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.MetadataWrite})}

	applied := w.correct("history", series, []credit{alias}, correction(alias, canonical, "the applied Bangumi entry credits it"))
	if applied.Outcome != "changed" || len(applied.Fields) != 1 || applied.Fields[0].Sources[0].CandidateID != "bangumi:7" {
		t.Fatalf("correction on a historical identification %+v", applied)
	}
	if got := w.authors(series); !slices.Equal(got, []credit{canonical}) {
		t.Fatalf("credits %v", got)
	}
}

func TestAuthorCorrectionCommitsWholeOrNothingAndUndoesUnderCurrentAuthority(t *testing.T) {
	w := openAuthorWorld(t)
	first := w.series[0]
	current := w.authors(first)
	fix := correction(alias, canonical, "Bangumi credits the canonical pen name")

	// A failing association write leaves the series as it was and a failed receipt behind.
	if _, err := w.store.DB().Exec(fmt.Sprintf(`CREATE TRIGGER authors_fail BEFORE INSERT ON series_authors WHEN NEW.series_id = %d BEGIN SELECT RAISE(ABORT, 'private diagnosis'); END`, first)); err != nil {
		t.Fatal(err)
	}
	failed := w.correct("failing-write", first, current, fix)
	if failed.Outcome != "failed" || failed.Applied {
		t.Fatalf("failed write %+v", failed)
	}
	if got := w.authors(first); !slices.Equal(got, current) {
		t.Fatalf("failed write changed credits: %v", got)
	}
	if _, err := w.store.DB().Exec(`DROP TRIGGER authors_fail`); err != nil {
		t.Fatal(err)
	}

	applied := w.correct("applied", first, current, fix)
	if applied.Outcome != "changed" {
		t.Fatalf("correction %+v", applied)
	}
	// Undo needs the whole corrected list unchanged: a member added since blocks it.
	if _, err := w.store.DB().Exec(`INSERT INTO authors(name, role) VALUES ('Later', 'Writer');
		INSERT INTO series_authors SELECT ?, id FROM authors WHERE name = 'Later'`, first); err != nil {
		t.Fatal(err)
	}
	var blocked receipt
	if result := w.agent.call("library_undo_metadata_operation", map[string]any{"request_key": "undo-blocked", "operation_id": applied.ID}, &blocked); result.IsError || blocked.Outcome != "conflict" {
		t.Fatalf("undo over a changed list %+v %s", blocked, resultText(result))
	}
	if _, err := w.store.DB().Exec(`DELETE FROM series_authors WHERE series_id = ? AND author_id IN (SELECT id FROM authors WHERE name = 'Later')`, first); err != nil {
		t.Fatal(err)
	}
	// A lock blocks undo as well; an edit of another field does not.
	w.exec(`UPDATE series SET locked_fields = 'authors' WHERE id = ?`, first)
	var locked receipt
	if result := w.agent.call("library_undo_metadata_operation", map[string]any{"request_key": "undo-locked", "operation_id": applied.ID}, &locked); result.IsError || locked.Outcome != "conflict" {
		t.Fatalf("undo over a lock %+v %s", locked, resultText(result))
	}
	w.exec(`UPDATE series SET locked_fields = '', publisher = 'Unrelated human edit' WHERE id = ?`, first)
	// A failing undo write keeps the corrected list and its failed receipt.
	w.exec(fmt.Sprintf(`CREATE TRIGGER authors_undo_fail BEFORE DELETE ON series_authors WHEN OLD.series_id = %d BEGIN SELECT RAISE(ABORT, 'private diagnosis'); END`, first))
	var failing receipt
	if result := w.agent.call("library_undo_metadata_operation", map[string]any{"request_key": "undo-failing", "operation_id": applied.ID}, &failing); result.IsError || failing.Outcome != "failed" || failing.ParentID != applied.ID {
		t.Fatalf("failing undo %+v %s", failing, resultText(result))
	}
	w.exec(`DROP TRIGGER authors_undo_fail`)
	corrected := w.authors(first)
	if !slices.Contains(corrected, canonical) || slices.Contains(corrected, alias) {
		t.Fatalf("failed undo changed credits: %v", corrected)
	}

	// Current authority decides: a credential narrowed to read may neither correct nor undo.
	if rec := w.web.raw("PATCH", "/api/mcp/credentials/"+w.agent.credential, fmt.Sprintf(`{"user_id":1,"name":"main loop agent","library_ids":[%d],"capabilities":["read"]}`, w.library.lib.ID)); rec.Code != http.StatusOK {
		t.Fatalf("narrow credential %d %s", rec.Code, rec.Body)
	}
	w.refused("narrowed", first, w.authors(first), correction(credit{"Artist", "Artist"}, canonical, "merge"))
	var denied receipt
	if result := w.agent.call("library_undo_metadata_operation", map[string]any{"request_key": "undo-narrowed", "operation_id": applied.ID}, &denied); !result.IsError {
		t.Fatalf("undo without metadata_write %+v", denied)
	}

	// The owner undoes the agent's receipt over the administration API.
	rec := w.web.raw("POST", "/api/mcp/operations/"+applied.ID+"/undo", `{"request_key":"owner-undo"}`)
	var undone receipt
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &undone) != nil || undone.Outcome != "changed" {
		t.Fatalf("owner undo %d %s", rec.Code, rec.Body)
	}
	if got := w.authors(first); !slices.Equal(got, current) {
		t.Fatalf("undo restored %v want %v", got, current)
	}
	var publisher string
	if err := w.store.DB().QueryRow(`SELECT publisher FROM series WHERE id = ?`, first).Scan(&publisher); err != nil || publisher != "Unrelated human edit" {
		t.Fatalf("undo touched another field: %q %v", publisher, err)
	}
	if rec := w.web.raw("GET", "/api/mcp/operations", ""); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "private diagnosis") {
		t.Fatalf("operation history %d leaks a storage diagnostic", rec.Code)
	}
}
