// These tests guard that identification conclusions and the derived status are durable facts of
// the assembled process: history becomes conclusions once at upgrade, the owner's review decision
// is reversible, and agents read the counts only inside their credential's libraries.

package bootstrap_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/proposal"
)

// identificationWeb is an administrator browser session against one opened production process.
type identificationWeb struct {
	t       *testing.T
	store   database.Store
	process *bootstrap.Process
	router  http.Handler
	server  *httptest.Server
	cookie  *http.Cookie
	csrf    string
}

func openIdentificationWeb(t *testing.T, store database.Store, cfg *config.Manager) *identificationWeb {
	t.Helper()
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	return &identificationWeb{t: t, store: store, process: process, router: router}
}

func (w *identificationWeb) setupAdmin() {
	w.t.Helper()
	rec := w.raw("POST", "/api/auth/setup", `{"username":"admin","password":"password123"}`)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("setup %d %s", rec.Code, rec.Body)
	}
	w.login(rec)
}

func (w *identificationWeb) loginAs(username, password string) {
	w.t.Helper()
	w.cookie, w.csrf = nil, ""
	rec := w.raw("POST", "/api/auth/login", `{"username":"`+username+`","password":"`+password+`"}`)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("login %s %d %s", username, rec.Code, rec.Body)
	}
	w.login(rec)
}

func (w *identificationWeb) login(rec *httptest.ResponseRecorder) {
	for _, c := range rec.Result().Cookies() {
		if c.Value != "" {
			w.cookie = c
		}
	}
	var auth struct {
		CSRF string `json:"csrf_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &auth)
	w.csrf = auth.CSRF
}

func (w *identificationWeb) raw(method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if w.cookie != nil {
		r.AddCookie(w.cookie)
		r.Header.Set("X-CSRF-Token", w.csrf)
	}
	rec := httptest.NewRecorder()
	w.router.ServeHTTP(rec, r)
	return rec
}

// call performs one request and decodes a successful JSON body into out.
func (w *identificationWeb) call(method, path string, want int, out any) {
	w.t.Helper()
	rec := w.raw(method, path, "")
	if rec.Code != want {
		w.t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body)
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			w.t.Fatalf("%s %s: %v %s", method, path, err, rec.Body)
		}
	}
}

// seriesIdentification mirrors the public administration projection the tests read.
type seriesIdentification struct {
	SeriesID    int64  `json:"series_id"`
	Status      string `json:"status"`
	Conclusions []struct {
		Source        string   `json:"source"`
		EntryID       string   `json:"entry_id"`
		EntryURL      string   `json:"entry_url"`
		Title         string   `json:"title"`
		OriginalTitle string   `json:"original_title"`
		Aliases       []string `json:"aliases"`
		Reason        string   `json:"reason"`
		ProposalID    int64    `json:"proposal_id"`
		ConfirmedAt   string   `json:"confirmed_at"`
		Confirmer     struct {
			Kind         string `json:"kind"`
			UserID       int64  `json:"user_id"`
			CredentialID string `json:"credential_id"`
		} `json:"confirmer"`
	} `json:"conclusions"`
	Excluded []struct {
		Source     string `json:"source"`
		EntryID    string `json:"entry_id"`
		EntryURL   string `json:"entry_url"`
		Title      string `json:"title"`
		ProposalID int64  `json:"proposal_id"`
		ExcludedAt string `json:"excluded_at"`
	} `json:"excluded_candidates"`
	OwnerReview *struct {
		UserID     int64  `json:"user_id"`
		UserName   string `json:"user_name"`
		ReviewedAt string `json:"reviewed_at"`
	} `json:"owner_review"`
}

func (w *identificationWeb) identification(series int64) seriesIdentification {
	w.t.Helper()
	var out seriesIdentification
	w.call("GET", "/api/identification/series/"+strconv.FormatInt(series, 10), http.StatusOK, &out)
	return out
}

func TestIdentificationOfUntouchedSeriesIsUnprocessed(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Fresh", Path: lib.Path + "/fresh"})
	if err != nil {
		t.Fatal(err)
	}
	web := openIdentificationWeb(t, store, cfg)
	defer web.process.Close()
	web.setupAdmin()

	got := web.identification(series.ID)
	if got.SeriesID != series.ID || got.Status != "unprocessed" || len(got.Conclusions) != 0 || len(got.Excluded) != 0 || got.OwnerReview != nil {
		t.Fatalf("fresh series identification %+v", got)
	}
	web.call("GET", "/api/identification/series/999999", http.StatusNotFound, nil)
}

// historicalProposal writes one proposal the way the old review flow left it, before any upgrade.
func historicalProposal(t *testing.T, store database.Store, series int64, provider string, sourceID int64, sourceURL, payload, status string) int64 {
	t.Helper()
	ctx := context.Background()
	row, err := store.CreateMetadataReview(ctx, database.CreateMetadataReviewParams{SeriesID: series, Provider: provider, SourceUrl: sourceURL, SourceID: sourceID, Summary: "history", Status: "pending", RawPayload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		if n, err := store.ResolvePendingMetadataReview(ctx, database.ResolvePendingMetadataReviewParams{ID: row.ID, Status: status}); err != nil || n != 1 {
			t.Fatalf("resolve %d as %s: %d %v", row.ID, status, n, err)
		}
	}
	return row.ID
}

func TestUpgradeDerivesIdentificationFromProposalHistory(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	makeSeries := func(name string) int64 {
		t.Helper()
		s, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: name, Path: lib.Path + "/" + name})
		if err != nil {
			t.Fatal(err)
		}
		return s.ID
	}
	const mangadexEntry = "0b4b1b7c-1111-2222-3333-444455556666"
	settled := makeSeries("settled")
	applied := historicalProposal(t, store, settled, "Bangumi", 42, "https://bgm.tv/subject/42", `{"Title":"中文名","OriginalTitle":"原名","Provider":"Bangumi","SourceID":42}`, "applied")
	rejected := historicalProposal(t, store, settled, "mangadex", 0, "https://mangadex.org/title/"+mangadexEntry, `{"Title":"Wrong work","Provider":"MangaDex"}`, "rejected")
	pending := historicalProposal(t, store, settled, "anilist", 7, "https://anilist.co/manga/7", `{"Title":"Pending","Provider":"AniList","SourceID":7}`, "pending")
	waiting := makeSeries("waiting")
	waitingProposal := historicalProposal(t, store, waiting, "bangumi", 9, "https://bgm.tv/subject/9", `{"Title":"Waiting"}`, "pending")
	guessed := makeSeries("guessed")
	historicalProposal(t, store, guessed, "llm", 0, "", `{"Title":"Model guess","Provider":"OpenAI"}`, "applied")
	reconsidered := makeSeries("reconsidered")
	historicalProposal(t, store, reconsidered, "bangumi", 5, "https://bgm.tv/subject/5", `{"Title":"Fields declined"}`, "rejected")
	historicalProposal(t, store, reconsidered, "bangumi", 5, "https://bgm.tv/subject/5", `{"Title":"Same entry"}`, "applied")
	historicalProposal(t, store, reconsidered, "bangumi", 6, "https://bgm.tv/subject/6", `{"Title":"Older choice"}`, "applied")
	latest := historicalProposal(t, store, reconsidered, "bangumi", 8, "", `{"Title":"Latest choice"}`, "applied")

	web := openIdentificationWeb(t, store, cfg)
	web.setupAdmin()

	got := web.identification(settled)
	if got.Status != "identified" || len(got.Conclusions) != 1 || len(got.Excluded) != 1 || got.OwnerReview != nil {
		t.Fatalf("settled series %+v", got)
	}
	c := got.Conclusions[0]
	if c.Source != "bangumi" || c.EntryID != "42" || c.EntryURL != "https://bgm.tv/subject/42" || c.Title != "中文名" || c.OriginalTitle != "原名" ||
		c.Aliases == nil || len(c.Aliases) != 0 || c.ProposalID != applied || c.Confirmer.Kind != "owner" || c.Confirmer.UserID != 0 || c.Confirmer.CredentialID != "" {
		t.Fatalf("applied proposal conclusion %+v", c)
	}
	if _, err := time.Parse(time.RFC3339, c.ConfirmedAt); err != nil {
		t.Fatalf("confirmed_at %q: %v", c.ConfirmedAt, err)
	}
	x := got.Excluded[0]
	if x.Source != "mangadex" || x.EntryID != mangadexEntry || x.EntryURL != "https://mangadex.org/title/"+mangadexEntry || x.Title != "Wrong work" || x.ProposalID != rejected {
		t.Fatalf("rejected proposal exclusion %+v", x)
	}
	if _, err := time.Parse(time.RFC3339, x.ExcludedAt); err != nil {
		t.Fatalf("excluded_at %q: %v", x.ExcludedAt, err)
	}
	// The derived identification supersedes the identified series' backlog; a series that stays
	// unprocessed keeps its pending proposal until its own identification.
	pendingReviews := func(series int64) []int64 {
		var reviews struct {
			Reviews []struct {
				ID int64 `json:"id"`
			} `json:"reviews"`
		}
		web.call("GET", "/api/series/"+strconv.FormatInt(series, 10)+"/metadata-review", http.StatusOK, &reviews)
		ids := []int64{}
		for _, r := range reviews.Reviews {
			ids = append(ids, r.ID)
		}
		return ids
	}
	if ids := pendingReviews(settled); len(ids) != 0 {
		t.Fatalf("identified series still has pending proposals %v", ids)
	}
	if row, err := store.GetMetadataReview(ctx, pending); err != nil || row.Status != proposal.StatusSuperseded || row.RejectedAt.Valid {
		t.Fatalf("backlog proposal of an identified series %+v %v", row, err)
	}
	if ids := pendingReviews(waiting); len(ids) != 1 || ids[0] != waitingProposal {
		t.Fatalf("pending proposal of an unprocessed series %v", ids)
	}

	for name, id := range map[string]int64{"pending only": waiting, "no source entry": guessed} {
		if got := web.identification(id); got.Status != "unprocessed" || len(got.Conclusions) != 0 || len(got.Excluded) != 0 {
			t.Fatalf("%s: %+v", name, got)
		}
	}
	// The latest applied entry per source wins, and an entry the owner applied is never also excluded.
	again := web.identification(reconsidered)
	if again.Status != "identified" || len(again.Conclusions) != 1 || again.Conclusions[0].EntryID != "8" ||
		again.Conclusions[0].EntryURL != "https://bgm.tv/subject/8" || again.Conclusions[0].ProposalID != latest || len(again.Excluded) != 0 {
		t.Fatalf("reconsidered series %+v", again)
	}

	before := map[int64]string{}
	for _, id := range []int64{settled, waiting, guessed, reconsidered} {
		before[id] = web.raw("GET", "/api/identification/series/"+strconv.FormatInt(id, 10), "").Body.String()
	}
	web.process.Close()
	reopened := openIdentificationWeb(t, store, cfg)
	defer reopened.process.Close()
	reopened.loginAs("admin", "password123")
	for id, want := range before {
		if got := reopened.raw("GET", "/api/identification/series/"+strconv.FormatInt(id, 10), "").Body.String(); got != want {
			t.Fatalf("series %d changed on reopen:\nbefore %s\nafter  %s", id, want, got)
		}
	}
}

func TestOwnerSetsAndWithdrawsOwnerReviewed(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	identified, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "identified", Path: lib.Path + "/identified"})
	if err != nil {
		t.Fatal(err)
	}
	historicalProposal(t, store, identified.ID, "bangumi", 3, "https://bgm.tv/subject/3", `{"Title":"Known"}`, "applied")
	fresh, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "fresh", Path: lib.Path + "/fresh"})
	if err != nil {
		t.Fatal(err)
	}
	web := openIdentificationWeb(t, store, cfg)
	defer web.process.Close()
	web.setupAdmin()
	path := func(id int64) string {
		return "/api/identification/series/" + strconv.FormatInt(id, 10) + "/owner-review"
	}

	var set seriesIdentification
	web.call("PUT", path(identified.ID), http.StatusOK, &set)
	if set.Status != "owner_reviewed" || set.OwnerReview == nil || set.OwnerReview.UserID != 1 || set.OwnerReview.UserName != "admin" || len(set.Conclusions) != 1 {
		t.Fatalf("set owner-reviewed %+v", set)
	}
	if _, err := time.Parse(time.RFC3339, set.OwnerReview.ReviewedAt); err != nil {
		t.Fatalf("reviewed_at %q: %v", set.OwnerReview.ReviewedAt, err)
	}
	// Setting it again keeps the original decision.
	var repeated seriesIdentification
	web.call("PUT", path(identified.ID), http.StatusOK, &repeated)
	if repeated.Status != "owner_reviewed" || *repeated.OwnerReview != *set.OwnerReview {
		t.Fatalf("repeated set %+v want %+v", repeated.OwnerReview, set.OwnerReview)
	}
	if got := web.identification(identified.ID); got.Status != "owner_reviewed" {
		t.Fatalf("read after set %+v", got)
	}
	var withdrawn seriesIdentification
	web.call("DELETE", path(identified.ID), http.StatusOK, &withdrawn)
	if withdrawn.Status != "identified" || withdrawn.OwnerReview != nil || len(withdrawn.Conclusions) != 1 {
		t.Fatalf("withdrawn %+v", withdrawn)
	}
	web.call("DELETE", path(identified.ID), http.StatusOK, &withdrawn)
	if withdrawn.Status != "identified" {
		t.Fatalf("repeated withdraw %+v", withdrawn)
	}
	web.call("PUT", path(fresh.ID), http.StatusOK, &set)
	if set.Status != "owner_reviewed" {
		t.Fatalf("fresh series set %+v", set)
	}
	web.call("DELETE", path(fresh.ID), http.StatusOK, &withdrawn)
	if withdrawn.Status != "unprocessed" {
		t.Fatalf("fresh series withdrawn %+v", withdrawn)
	}
	web.call("PUT", path(999999), http.StatusNotFound, nil)

	// The owner's decision and the conclusions behind it stay out of regular accounts' reach.
	if rec := web.raw("POST", "/api/users", `{"username":"reader","password":"initialpass"}`); rec.Code != http.StatusOK {
		t.Fatalf("create reader %d %s", rec.Code, rec.Body)
	}
	web.loginAs("reader", "initialpass")
	if rec := web.raw("POST", "/api/auth/change-password", `{"current_password":"initialpass","new_password":"readerpass"}`); rec.Code != http.StatusOK {
		t.Fatalf("change password %d %s", rec.Code, rec.Body)
	}
	web.loginAs("reader", "readerpass")
	for _, method := range []string{"PUT", "DELETE"} {
		web.call(method, path(identified.ID), http.StatusForbidden, nil)
	}
	web.call("GET", "/api/identification/series/"+strconv.FormatInt(identified.ID, 10), http.StatusForbidden, nil)
	web.loginAs("admin", "password123")
	if got := web.identification(identified.ID); got.Status != "identified" || got.OwnerReview != nil {
		t.Fatalf("regular account changed the decision: %+v", got)
	}
}

// identificationAgent connects an MCP client to the web session's process with one new credential.
func (w *identificationWeb) identificationAgent(cfg *config.Manager, owner int64, libraries []int64, capabilities []string) *mcp.ClientSession {
	w.t.Helper()
	ctx := context.Background()
	if w.server == nil {
		w.server = httptest.NewServer(w.router)
		w.t.Cleanup(w.server.Close)
		snapshot := cfg.Snapshot()
		snapshot.MCP.PublicURL = w.server.URL
		snapshot.MCP.AllowLANHTTP = true
		cfg.Replace(&snapshot)
	}
	issued, err := delegated.New(delegated.NewSQLStore(w.store.DB())).Issue(ctx, 1, delegated.Restrictions{UserID: owner, Name: "identification agent", LibraryIDs: libraries, Capabilities: capabilities})
	if err != nil {
		w.t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "identification", Version: "1"}, nil)
	httpClient := *w.server.Client()
	httpClient.Transport = &bearerTransport{base: w.server.Client().Transport, token: issued.Token}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: w.server.URL + "/mcp", HTTPClient: &httpClient}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { _ = session.Close() })
	return session
}

type identificationProgress struct {
	LibraryIDs []int64          `json:"library_ids"`
	Series     int64            `json:"series"`
	Statuses   map[string]int64 `json:"statuses"`
}

// progress calls the progress tool and checks that the text copy carries the same result.
func progress(t *testing.T, session *mcp.ClientSession, args map[string]any) (identificationProgress, *mcp.CallToolResult) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "library_identification_progress", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	var out identificationProgress
	if result.IsError {
		return out, result
	}
	text := ""
	for _, block := range result.Content {
		if content, ok := block.(*mcp.TextContent); ok {
			text = content.Text
		}
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("progress text copy %q: %v", text, err)
	}
	structured, _ := json.Marshal(result.StructuredContent)
	var fromStructured identificationProgress
	if err := json.Unmarshal(structured, &fromStructured); err != nil || !reflect.DeepEqual(out, fromStructured) {
		t.Fatalf("text copy %+v differs from structured %s", out, structured)
	}
	return out, result
}

func TestAgentReadsIdentificationProgressWithinCredentialLibraries(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	library := func(name string) int64 {
		t.Helper()
		lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: name, Path: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		return lib.ID
	}
	granted, hidden := library("Granted"), library("Hidden")
	series := func(lib int64, name string) int64 {
		t.Helper()
		s, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib, Name: name, Path: t.TempDir() + "/" + name})
		if err != nil {
			t.Fatal(err)
		}
		return s.ID
	}
	historicalProposal(t, store, series(granted, "identified"), "bangumi", 1, "", `{}`, "applied")
	reviewed := series(granted, "reviewed")
	series(granted, "untouched one")
	series(granted, "untouched two")
	historicalProposal(t, store, series(hidden, "elsewhere"), "bangumi", 2, "", `{}`, "applied")
	series(hidden, "elsewhere untouched")

	web := openIdentificationWeb(t, store, cfg)
	defer web.process.Close()
	web.setupAdmin()
	web.call("PUT", "/api/identification/series/"+strconv.FormatInt(reviewed, 10)+"/owner-review", http.StatusOK, nil)

	agent := web.identificationAgent(cfg, 1, []int64{granted}, []string{delegated.Read})
	listed, err := agent.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var tool *mcp.Tool
	for _, candidate := range listed.Tools {
		if candidate.Name == "library_identification_progress" {
			tool = candidate
		}
	}
	if tool == nil || len(tool.Description) > 400 || !strings.Contains(tool.Description, `{"library_ids":[`) || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Fatalf("progress tool listing %+v", tool)
	}
	want := identificationProgress{LibraryIDs: []int64{granted}, Series: 4, Statuses: map[string]int64{
		"unprocessed": 2, "identified": 1, "awaiting_owner": 0, "needs_recheck": 0, "not_found": 0, "owner_reviewed": 1,
	}}
	for _, args := range []map[string]any{{}, {"library_ids": []int64{granted}}} {
		if got, _ := progress(t, agent, args); !reflect.DeepEqual(got, want) {
			t.Fatalf("progress %v = %+v want %+v", args, got, want)
		}
	}
	if _, denied := progress(t, agent, map[string]any{"library_ids": []int64{granted, hidden}}); !denied.IsError {
		t.Fatalf("a library outside the credential was counted: %+v", denied)
	}

	// Read is the one capability a regular account's credential can use, and progress needs no more.
	reader, err := store.CreateUser(ctx, database.CreateUserParams{Username: "reader", PasswordHash: "unused", Role: "regular"})
	if err != nil {
		t.Fatal(err)
	}
	regular := web.identificationAgent(cfg, reader.ID, []int64{granted, hidden}, []string{delegated.Read})
	got, _ := progress(t, regular, map[string]any{})
	if got.Series != 6 || got.Statuses["identified"] != 2 || got.Statuses["unprocessed"] != 3 || got.Statuses["owner_reviewed"] != 1 {
		t.Fatalf("regular credential progress %+v", got)
	}
	writer := web.identificationAgent(cfg, 1, []int64{granted}, []string{delegated.MetadataWrite})
	if _, denied := progress(t, writer, map[string]any{}); !denied.IsError {
		t.Fatalf("progress without read %+v", denied)
	}
}

// awaitJSON polls one administration read until done accepts it.
func (w *identificationWeb) awaitJSON(path string, out any, done func() bool) {
	w.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		w.call("GET", path, http.StatusOK, out)
		if done() {
			return
		}
		if time.Now().After(deadline) {
			w.t.Fatalf("%s did not settle: %+v", path, out)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRelocationKeepsIdentification(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(source, "series"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(source, "series", "book.cbz")
	writeStdioRunArchive(t, archive)
	info, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Moving", Path: source})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "series", Path: filepath.Join(source, "series")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateBook(ctx, database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID, Name: "book.cbz", Path: archive, Size: info.Size(), FileModifiedAt: info.ModTime(), PageCount: 1}); err != nil {
		t.Fatal(err)
	}
	historicalProposal(t, store, series.ID, "bangumi", 11, "https://bgm.tv/subject/11", `{"Title":"Kept"}`, "applied")
	historicalProposal(t, store, series.ID, "bangumi", 12, "https://bgm.tv/subject/12", `{"Title":"Ruled out"}`, "rejected")

	web := openIdentificationWeb(t, store, cfg)
	defer web.process.Close()
	web.setupAdmin()
	web.call("PUT", "/api/identification/series/"+strconv.FormatInt(series.ID, 10)+"/owner-review", http.StatusOK, nil)
	before := web.raw("GET", "/api/identification/series/"+strconv.FormatInt(series.ID, 10), "").Body.String()

	target := filepath.Join(root, "target")
	if err := os.Rename(source, target); err != nil {
		t.Fatal(err)
	}
	rec := web.raw("POST", "/api/libraries/"+strconv.FormatInt(lib.ID, 10)+"/relocation-plans", `{"target_root":"`+target+`"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("relocation preview %d %s", rec.Code, rec.Body)
	}
	var plan struct {
		ID           int64  `json:"id"`
		Status       string `json:"status"`
		BlockerCount int    `json:"blocker_count"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &plan)
	web.awaitJSON("/api/relocation-plans/"+strconv.FormatInt(plan.ID, 10), &plan, func() bool { return plan.Status != "pending" && plan.Status != "checking" })
	if plan.Status != "checked" || plan.BlockerCount != 0 {
		t.Fatalf("relocation preview %+v", plan)
	}
	rec = web.raw("POST", "/api/relocation-plans/"+strconv.FormatInt(plan.ID, 10)+"/commit", `{}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("relocation commit %d %s", rec.Code, rec.Body)
	}
	var operation struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &operation)
	web.awaitJSON("/api/relocation-operations/"+strconv.FormatInt(operation.ID, 10), &operation, func() bool { return operation.Status == "completed" || operation.Status == "failed" })
	if operation.Status != "completed" {
		t.Fatalf("relocation %+v", operation)
	}
	moved, err := store.GetSeries(ctx, series.ID)
	if err != nil || moved.Path != filepath.Join(target, "series") {
		t.Fatalf("series after relocation %+v %v", moved, err)
	}
	if after := web.raw("GET", "/api/identification/series/"+strconv.FormatInt(series.ID, 10), "").Body.String(); after != before {
		t.Fatalf("relocation changed identification:\nbefore %s\nafter  %s", before, after)
	}
}

// historyProposals is the proposal module over the raw store, used to leave review-page history
// behind before the first open.
type historyProposals struct{ database.Store }

func (d historyProposals) ExecTx(ctx context.Context, fn func(proposal.Queries) error) error {
	return d.Store.ExecTx(ctx, func(q *database.Queries) error { return fn(q) })
}

func TestUpgradeTreatsPartlyAppliedProposalAsOwnerConfirmed(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "partly", Path: lib.Path + "/partly", Title: sql.NullString{String: "Own title", Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	// The owner filled only the empty summary on the review page; the differing title stays pending.
	proposals := proposal.NewService(historyProposals{store})
	queued, err := proposals.Queue(ctx, series, &metadata.SeriesMetadata{Provider: "Bangumi", SourceID: 77, SourceURL: "https://bgm.tv/subject/77", Title: "来源标题", OriginalTitle: "原题", Summary: "来源简介"}, "bangumi", series.Name, proposal.QueueOptions{})
	if err != nil || queued.Status != proposal.QueueQueued {
		t.Fatalf("queue %+v %v", queued, err)
	}
	applied, err := proposals.Apply(ctx, queued.Proposal.ID, proposal.ApplyModeFillEmpty)
	if err != nil || applied.Status != proposal.ApplyPartial {
		t.Fatalf("partial apply %+v %v", applied, err)
	}
	untouched, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "untouched", Path: lib.Path + "/untouched"})
	if err != nil {
		t.Fatal(err)
	}
	if queued, err := proposals.Queue(ctx, untouched, &metadata.SeriesMetadata{Provider: "Bangumi", SourceID: 78, Summary: "Never applied"}, "bangumi", untouched.Name, proposal.QueueOptions{}); err != nil || queued.Status != proposal.QueueQueued {
		t.Fatalf("queue untouched %+v %v", queued, err)
	}

	web := openIdentificationWeb(t, store, cfg)
	web.setupAdmin()
	got := web.identification(series.ID)
	if got.Status != "identified" || len(got.Conclusions) != 1 || len(got.Excluded) != 0 {
		t.Fatalf("partly applied series %+v", got)
	}
	c := got.Conclusions[0]
	if c.Source != "bangumi" || c.EntryID != "77" || c.EntryURL != "https://bgm.tv/subject/77" || c.Title != "来源标题" || c.OriginalTitle != "原题" ||
		c.ProposalID != queued.Proposal.ID || c.Confirmer.Kind != "owner" || c.Confirmer.UserID != 0 || c.Confirmer.CredentialID != "" {
		t.Fatalf("partly applied conclusion %+v", c)
	}
	if _, err := time.Parse(time.RFC3339, c.ConfirmedAt); err != nil {
		t.Fatalf("confirmed_at %q: %v", c.ConfirmedAt, err)
	}
	// The identification supersedes what is left of the proposal; the proposal of the series that
	// stays unprocessed waits for that series' identification.
	pendingReviews := func(series int64) int {
		var reviews struct {
			Reviews []struct {
				ID int64 `json:"id"`
			} `json:"reviews"`
		}
		web.call("GET", "/api/series/"+strconv.FormatInt(series, 10)+"/metadata-review", http.StatusOK, &reviews)
		return len(reviews.Reviews)
	}
	if n := pendingReviews(series.ID); n != 0 {
		t.Fatalf("partly applied series still has %d pending proposals", n)
	}
	if row, err := store.GetMetadataReview(ctx, queued.Proposal.ID); err != nil || row.Status != proposal.StatusSuperseded {
		t.Fatalf("partly applied proposal %+v %v", row, err)
	}
	if got := web.identification(untouched.ID); got.Status != "unprocessed" || len(got.Conclusions) != 0 || pendingReviews(untouched.ID) != 1 {
		t.Fatalf("untouched pending proposal %+v", got)
	}

	before := map[int64]string{}
	for _, id := range []int64{series.ID, untouched.ID} {
		before[id] = web.raw("GET", "/api/identification/series/"+strconv.FormatInt(id, 10), "").Body.String()
	}
	web.process.Close()
	reopened := openIdentificationWeb(t, store, cfg)
	defer reopened.process.Close()
	reopened.loginAs("admin", "password123")
	for id, want := range before {
		if got := reopened.raw("GET", "/api/identification/series/"+strconv.FormatInt(id, 10), "").Body.String(); got != want {
			t.Fatalf("series %d changed on reopen:\nbefore %s\nafter  %s", id, want, got)
		}
	}
}
