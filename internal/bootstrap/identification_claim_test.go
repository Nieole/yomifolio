// These tests guard the agent's main loop of the assembled process: claiming hands each ready series
// to one session at a time under a lease that the injected identification clock expires, and each
// verdict is decided, recorded and replayed per series under current authority.

package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/task"
)

// handClock is the identification clock a test moves by hand.
type handClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *handClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *handClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// openClaimWeb opens the production process with the recording sources, the hand-moved
// identification clock and an administrator.
func openClaimWeb(t *testing.T, store database.Store, cfg *config.Manager, sources *recordingSources, clock *handClock) *identificationWeb {
	t.Helper()
	options := sources.options()
	options.Clock = clock.Now
	process, err := bootstrap.Open(context.Background(), store, cfg, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(process.Close)
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	web := &identificationWeb{t: t, store: store, process: process, router: router}
	web.setupAdmin()
	return web
}

// mainLoopAgent is one MCP session of one credential.
type mainLoopAgent struct {
	t          *testing.T
	session    *mcp.ClientSession
	credential string
}

// mainLoopAgent connects an MCP client with one new credential of the owner.
func (w *identificationWeb) mainLoopAgent(cfg *config.Manager, owner int64, libraries []int64, capabilities []string) *mainLoopAgent {
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
	issued, err := delegated.New(delegated.NewSQLStore(w.store.DB())).Issue(ctx, 1, delegated.Restrictions{UserID: owner, Name: "main loop agent", LibraryIDs: libraries, Capabilities: capabilities})
	if err != nil {
		w.t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "main-loop", Version: "1"}, nil)
	httpClient := *w.server.Client()
	httpClient.Transport = &bearerTransport{base: w.server.Client().Transport, token: issued.Token}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: w.server.URL + "/mcp", HTTPClient: &httpClient}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { _ = session.Close() })
	return &mainLoopAgent{t: w.t, session: session, credential: issued.ID}
}

// call invokes one tool and decodes its JSON text copy, checking it repeats the structured
// result. A refused call returns the result with IsError set and leaves out untouched.
func (a *mainLoopAgent) call(name string, args any, out any) *mcp.CallToolResult {
	a.t.Helper()
	result, err := a.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		a.t.Fatalf("%s: %v", name, err)
	}
	if result.IsError {
		return result
	}
	text := resultText(result)
	if err := json.Unmarshal([]byte(text), out); err != nil {
		a.t.Fatalf("%s text copy %q: %v", name, text, err)
	}
	structured, _ := json.Marshal(result.StructuredContent)
	fromStructured := reflect.New(reflect.TypeOf(out).Elem()).Interface()
	if err := json.Unmarshal(structured, fromStructured); err != nil || !reflect.DeepEqual(out, fromStructured) {
		a.t.Fatalf("%s text copy differs from structured %s", name, structured)
	}
	return result
}

// claimedBatch mirrors the claim result.
type claimedBatch struct {
	LeaseID        string `json:"lease_id"`
	LeaseExpiresAt string `json:"lease_expires_at"`
	Series         []struct {
		SeriesID  int64          `json:"series_id"`
		LibraryID int64          `json:"library_id"`
		OpenNotes []string       `json:"open_notes"`
		Evidence  evidencePacket `json:"evidence"`
	} `json:"series"`
	EmptyReason string `json:"empty_reason"`
	Next        string `json:"next"`
}

func (b claimedBatch) ids() []int64 {
	out := []int64{}
	for _, s := range b.Series {
		out = append(out, s.SeriesID)
	}
	return out
}

func (a *mainLoopAgent) claim(args map[string]any) claimedBatch {
	a.t.Helper()
	var out claimedBatch
	if result := a.call("library_claim_series", args, &out); result.IsError {
		a.t.Fatalf("claim %v refused: %s", args, resultText(result))
	}
	return out
}

// claimLibrary prepares series with evidence packets: each series is named after its directory and
// every source answers with one candidate per series.
func claimLibrary(t *testing.T, store database.Store, name string, directories ...string) (*prefetchLibrary, []int64) {
	t.Helper()
	library := newPrefetchLibrary(t, store, name)
	ids := make([]int64, len(directories))
	for i, directory := range directories {
		ids[i] = library.series(directory, "", "01.cbz")
	}
	return library, ids
}

// candidatesBySeries answers every catalogue source with one entry per query, numbered after the
// query so each series gets its own candidates: bangumi:<n> and anilist:<n> for series "Title <n>".
func candidatesBySeries(source, query string) ([]*metadata.SeriesMetadata, error) {
	var n int
	if _, err := fmt.Sscanf(query, "Title %d", &n); err != nil {
		return nil, nil
	}
	switch source {
	case "bangumi":
		return []*metadata.SeriesMetadata{{Title: query, SourceID: n, SourceURL: fmt.Sprintf("https://bgm.tv/subject/%d", n)}}, nil
	case "anilist":
		return []*metadata.SeriesMetadata{{Title: query, OriginalTitle: query + " Original", SourceID: n, SourceURL: fmt.Sprintf("https://anilist.co/manga/%d", n)}}, nil
	}
	return nil, fmt.Errorf("%s is down", source)
}

func TestAgentClaimsReadySeriesWithEvidenceUnderLease(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library, ids := claimLibrary(t, store, "Claims", "Title 1", "Title 2")
	sources := &recordingSources{answer: candidatesBySeries}
	clock := &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	web := openClaimWeb(t, store, cfg, sources, clock)
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.MetadataWrite})

	batch := agent.claim(map[string]any{"library_ids": []int64{library.lib.ID}})
	if batch.LeaseID == "" || batch.LeaseExpiresAt != "2026-03-01T09:30:00Z" || batch.EmptyReason != "" {
		t.Fatalf("claim lease %+v", batch)
	}
	if !reflect.DeepEqual(batch.ids(), ids) {
		t.Fatalf("claimed %v want %v", batch.ids(), ids)
	}
	first := batch.Series[0]
	if first.LibraryID != library.lib.ID || first.Evidence.Local.DirectoryName != "Title 1" || len(first.Evidence.Candidates) != 2 ||
		first.Evidence.Candidates[0].ID != "bangumi:1" || first.Evidence.Candidates[1].ID != "anilist:1" {
		t.Fatalf("claimed evidence %+v", first)
	}
}

func TestClaimSaysWhyNoSeriesIsHandedOut(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library, ids := claimLibrary(t, store, "Ready", "Title 1", "Title 2")
	done, finished := claimLibrary(t, store, "Done", "Title 3")
	historicalProposal(t, store, finished[0], "bangumi", 3, "https://bgm.tv/subject/3", `{}`, "applied")
	sources := &recordingSources{answer: candidatesBySeries}
	clock := &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	web := openClaimWeb(t, store, cfg, sources, clock)
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	web.call("PUT", fmt.Sprintf("/api/identification/series/%d/owner-review", ids[1]), 200, nil)
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID, done.lib.ID}, []string{delegated.MetadataWrite})

	// The owner keeps the reviewed series, so only the other one is handed out.
	if batch := agent.claim(map[string]any{"library_ids": []int64{library.lib.ID}}); !reflect.DeepEqual(batch.ids(), ids[:1]) {
		t.Fatalf("claimed %v want %v", batch.ids(), ids[:1])
	}
	empty := agent.claim(map[string]any{"library_ids": []int64{library.lib.ID}})
	if len(empty.Series) != 0 || empty.EmptyReason != "all_claimed" || empty.LeaseID != "" || empty.Next == "" {
		t.Fatalf("claim of a leased library %+v", empty)
	}
	if _, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: library.lib.ID, Name: "Title 4", Path: library.lib.Path + "/Title 4"}); err != nil {
		t.Fatal(err)
	}
	if pending := agent.claim(map[string]any{"library_ids": []int64{library.lib.ID}}); len(pending.Series) != 0 || pending.EmptyReason != "prefetch_pending" {
		t.Fatalf("claim while a packet is missing %+v", pending)
	}
	if processed := agent.claim(map[string]any{"library_ids": []int64{done.lib.ID}}); len(processed.Series) != 0 || processed.EmptyReason != "all_processed" {
		t.Fatalf("claim of an identified library %+v", processed)
	}
}

func TestParallelClaimsNeverOverlapAndExpiredLeasesReturn(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library, ids := claimLibrary(t, store, "Parallel", "Title 1", "Title 2", "Title 3", "Title 4", "Title 5", "Title 6")
	sources := &recordingSources{answer: candidatesBySeries}
	clock := &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	web := openClaimWeb(t, store, cfg, sources, clock)
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agents := make([]*mainLoopAgent, 3)
	for i := range agents {
		agents[i] = web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.MetadataWrite})
	}

	batches := make([]claimedBatch, len(agents))
	var wg sync.WaitGroup
	for i, agent := range agents {
		wg.Go(func() { batches[i] = agent.claim(map[string]any{"limit": 2}) })
	}
	wg.Wait()
	handed := map[int64]int{}
	leases := map[string]bool{}
	for _, batch := range batches {
		if len(batch.Series) != 2 || leases[batch.LeaseID] {
			t.Fatalf("batches %+v", batches)
		}
		leases[batch.LeaseID] = true
		for _, id := range batch.ids() {
			handed[id]++
		}
	}
	for _, id := range ids {
		if handed[id] != 1 {
			t.Fatalf("series handed out %v, want each of %v once", handed, ids)
		}
	}

	clock.advance(29 * time.Minute)
	if held := agents[0].claim(map[string]any{}); held.EmptyReason != "all_claimed" {
		t.Fatalf("claim before the leases expire %+v", held)
	}
	clock.advance(2 * time.Minute)
	if returned := agents[0].claim(map[string]any{}); !reflect.DeepEqual(returned.ids(), ids) || returned.LeaseExpiresAt != "2026-03-01T10:01:00Z" {
		t.Fatalf("claim after the leases expired %v at %s, want %v", returned.ids(), returned.LeaseExpiresAt, ids)
	}
}

// verdictResult mirrors one result of a submission.
type verdictResult struct {
	SeriesID int64  `json:"series_id"`
	Accepted bool   `json:"accepted"`
	Status   string `json:"status"`
	Problem  string `json:"problem"`
	Message  string `json:"message"`
}

type submission struct {
	Results []verdictResult `json:"results"`
}

func (a *mainLoopAgent) submit(key, lease string, verdicts ...map[string]any) []verdictResult {
	a.t.Helper()
	var out submission
	if result := a.call("library_submit_verdicts", map[string]any{"request_key": key, "lease_id": lease, "verdicts": verdicts}, &out); result.IsError {
		a.t.Fatalf("submission %s refused: %s", key, resultText(result))
	}
	return out.Results
}

func identifyVerdict(series int64, reason string, candidates ...string) map[string]any {
	return map[string]any{"series_id": series, "verdict": "identify", "candidate_ids": candidates, "reason": reason}
}

func notFoundVerdict(series int64, reason string) map[string]any {
	return map[string]any{"series_id": series, "verdict": "not_found", "reason": reason}
}

func escalateVerdict(series int64, category, reason string) map[string]any {
	return map[string]any{"series_id": series, "verdict": "escalate", "category": category, "reason": reason}
}

// wantResult checks one verdict result: an accepted one by its status, a rejected one by its
// problem and a next step.
func wantResult(t *testing.T, got verdictResult, series int64, accepted bool, statusOrProblem string) {
	t.Helper()
	if got.SeriesID != series || got.Accepted != accepted {
		t.Fatalf("result %+v want series %d accepted=%v %s", got, series, accepted, statusOrProblem)
	}
	if accepted && (got.Status != statusOrProblem || got.Problem != "" || got.Message != "") {
		t.Fatalf("accepted result %+v want status %s", got, statusOrProblem)
	}
	if !accepted && (got.Problem != statusOrProblem || got.Message == "") {
		t.Fatalf("rejected result %+v want problem %s with a next step", got, statusOrProblem)
	}
}

// ownerVerdicts mirrors what the owner's identification route shows of not-found verdicts and
// open escalations.
type ownerVerdicts struct {
	NotFound *struct {
		SearchedSources []string `json:"searched_sources"`
		Reason          string   `json:"reason"`
		Confirmer       struct {
			Kind         string `json:"kind"`
			CredentialID string `json:"credential_id"`
		} `json:"confirmer"`
	} `json:"not_found"`
	Escalations []struct {
		Category string `json:"category"`
		Class    string `json:"class"`
		Reason   string `json:"reason"`
		RaisedBy struct {
			Kind         string `json:"kind"`
			CredentialID string `json:"credential_id"`
		} `json:"raised_by"`
	} `json:"escalations"`
}

func (w *identificationWeb) ownerVerdicts(series int64) ownerVerdicts {
	w.t.Helper()
	var out ownerVerdicts
	w.call("GET", fmt.Sprintf("/api/identification/series/%d", series), 200, &out)
	return out
}

func TestAgentSubmitsVerdictsSeriesBySeries(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library, ids := claimLibrary(t, store, "Verdicts", "Title 1", "Title 2", "Title 3", "Title 4", "Title 5", "Title 6")
	historicalProposal(t, store, ids[2], "bangumi", 3, "https://bgm.tv/subject/3", `{}`, "rejected")
	sources := &recordingSources{answer: candidatesBySeries}
	clock := &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	web := openClaimWeb(t, store, cfg, sources, clock)
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.MetadataWrite})
	batch := agent.claim(map[string]any{})
	if !reflect.DeepEqual(batch.ids(), ids) {
		t.Fatalf("claimed %v want %v", batch.ids(), ids)
	}

	request := []map[string]any{
		identifyVerdict(ids[0], "Bangumi and AniList titles equal the directory name", "bangumi:1", "anilist:1"),
		notFoundVerdict(ids[1], "no candidate shares the title"),
		identifyVerdict(ids[2], "the Bangumi title matches", "bangumi:3"),
		identifyVerdict(ids[3], "the entry of another series", "bangumi:1"),
		escalateVerdict(ids[4], "ambiguous_candidates", "two candidates fit equally"),
		escalateVerdict(ids[5], "directory_grouping", "the directory mixes two works"),
	}
	results := agent.submit("first", batch.LeaseID, request...)
	if len(results) != len(request) {
		t.Fatalf("results %+v", results)
	}
	wantResult(t, results[0], ids[0], true, "identified")
	wantResult(t, results[1], ids[1], true, "not_found")
	wantResult(t, results[2], ids[2], false, "excluded_candidate")
	wantResult(t, results[3], ids[3], false, "not_in_evidence")
	wantResult(t, results[4], ids[4], true, "awaiting_owner")
	wantResult(t, results[5], ids[5], true, "unprocessed")

	identified := web.identification(ids[0])
	if identified.Status != "identified" || len(identified.Conclusions) != 2 {
		t.Fatalf("identified series %+v", identified)
	}
	for i, want := range []struct{ source, entry, url string }{{"bangumi", "1", "https://bgm.tv/subject/1"}, {"anilist", "1", "https://anilist.co/manga/1"}} {
		c := identified.Conclusions[i]
		if c.Source != want.source || c.EntryID != want.entry || c.EntryURL != want.url || c.Title != "Title 1" || c.Reason != "Bangumi and AniList titles equal the directory name" ||
			c.Confirmer.Kind != "agent" || c.Confirmer.UserID != 1 || c.Confirmer.CredentialID != agent.credential || c.ConfirmedAt != "2026-03-01T09:00:00Z" {
			t.Fatalf("conclusion %d %+v", i, c)
		}
	}
	if identified.Conclusions[1].OriginalTitle != "Title 1 Original" {
		t.Fatalf("AniList conclusion keeps the original title: %+v", identified.Conclusions[1])
	}
	// A verdict records identity only; enrichment is not part of it.
	if series, err := store.GetSeries(context.Background(), ids[0]); err != nil || series.Summary.Valid || series.Title.Valid || series.Publisher.Valid {
		t.Fatalf("identified series metadata %+v %v", series, err)
	}
	missing := web.ownerVerdicts(ids[1]).NotFound
	if missing == nil || !reflect.DeepEqual(missing.SearchedSources, []string{"bangumi", "anilist"}) || missing.Reason != "no candidate shares the title" ||
		missing.Confirmer.Kind != "agent" || missing.Confirmer.CredentialID != agent.credential {
		t.Fatalf("not-found verdict %+v", missing)
	}
	for _, rejected := range ids[2:4] {
		if got := web.identification(rejected); got.Status != "unprocessed" || len(got.Conclusions) != 0 {
			t.Fatalf("rejected verdict changed series %d: %+v", rejected, got)
		}
	}
	escalated := web.ownerVerdicts(ids[4]).Escalations
	if len(escalated) != 1 || escalated[0].Category != "ambiguous_candidates" || escalated[0].Class != "identification" || escalated[0].Reason != "two candidates fit equally" ||
		escalated[0].RaisedBy.Kind != "agent" || escalated[0].RaisedBy.CredentialID != agent.credential {
		t.Fatalf("identification escalation %+v", escalated)
	}
	noted := web.ownerVerdicts(ids[5]).Escalations
	if len(noted) != 1 || noted[0].Category != "directory_grouping" || noted[0].Class != "note" {
		t.Fatalf("note %+v", noted)
	}

	// Replaying the request returns the original results and writes nothing again.
	if replay := agent.submit("first", batch.LeaseID, request...); !reflect.DeepEqual(replay, results) {
		t.Fatalf("replay %+v want %+v", replay, results)
	}
	if got := web.identification(ids[0]); len(got.Conclusions) != 2 {
		t.Fatalf("replay rewrote conclusions %+v", got)
	}
	if again := web.ownerVerdicts(ids[4]).Escalations; len(again) != 1 {
		t.Fatalf("replay raised another escalation %+v", again)
	}
	reused := agent.submit("first", batch.LeaseID, notFoundVerdict(ids[2], "nothing else fits"))
	wantResult(t, reused[0], ids[2], false, "request_key_reused")

	// Rejected verdicts and notes leave the lease in place, so it answers those series again.
	second := agent.submit("second", batch.LeaseID,
		notFoundVerdict(ids[2], "the excluded entry was the only one"),
		identifyVerdict(ids[5], "the Bangumi title matches", "bangumi:6"),
		notFoundVerdict(ids[0], "changed my mind"))
	wantResult(t, second[0], ids[2], true, "not_found")
	wantResult(t, second[1], ids[5], true, "identified")
	if second[2].SeriesID != ids[0] || second[2].Accepted || second[2].Problem != "not_unprocessed" || second[2].Status != "identified" {
		t.Fatalf("verdict on a decided series %+v", second[2])
	}
	if got := web.identification(ids[0]); len(got.Conclusions) != 2 || got.Status != "identified" {
		t.Fatalf("a decided series changed %+v", got)
	}
}

func TestExpiredLeaseAnswersUntilAnotherClaimTakesOver(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library, ids := claimLibrary(t, store, "Leases", "Title 1", "Title 2", "Title 3")
	sources := &recordingSources{answer: candidatesBySeries}
	clock := &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	web := openClaimWeb(t, store, cfg, sources, clock)
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	first := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.MetadataWrite})
	second := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.MetadataWrite})
	early := first.claim(map[string]any{"limit": 2})
	if !reflect.DeepEqual(early.ids(), ids[:2]) {
		t.Fatalf("first claim %v", early.ids())
	}
	wantResult(t, first.submit("note", early.LeaseID, escalateVerdict(ids[0], "directory_grouping", "two works in one directory"))[0], ids[0], true, "unprocessed")
	wantResult(t, first.submit("unclaimed", early.LeaseID, notFoundVerdict(ids[2], "never claimed"))[0], ids[2], false, "not_claimed")

	// Expired, but nobody else has claimed it yet: the old lease still answers.
	clock.advance(31 * time.Minute)
	wantResult(t, first.submit("late", early.LeaseID, identifyVerdict(ids[1], "the Bangumi title matches", "bangumi:2"))[0], ids[1], true, "identified")

	late := second.claim(map[string]any{})
	if !reflect.DeepEqual(late.ids(), []int64{ids[0], ids[2]}) || !reflect.DeepEqual(late.Series[0].OpenNotes, []string{"directory_grouping"}) || late.Series[1].OpenNotes != nil {
		t.Fatalf("second claim %+v", late)
	}
	wantResult(t, first.submit("taken", early.LeaseID, notFoundVerdict(ids[0], "too late"))[0], ids[0], false, "claimed_by_other")
	// A note already open on the series is not raised twice.
	wantResult(t, second.submit("again", late.LeaseID, escalateVerdict(ids[0], "directory_grouping", "still two works"))[0], ids[0], true, "unprocessed")
	if notes := web.ownerVerdicts(ids[0]).Escalations; len(notes) != 1 || notes[0].Reason != "two works in one directory" {
		t.Fatalf("notes %+v", notes)
	}
	wantResult(t, second.submit("done", late.LeaseID, identifyVerdict(ids[0], "the AniList title matches", "anilist:1"))[0], ids[0], true, "identified")
}

func TestVerdictsRecheckCurrentAuthority(t *testing.T) {
	ctx := context.Background()
	store, cfg := fixture(t)
	unpaced(t, cfg)
	kept, keptIDs := claimLibrary(t, store, "Kept", "Title 1")
	dropped, droppedIDs := claimLibrary(t, store, "Dropped", "Title 2")
	sources := &recordingSources{answer: candidatesBySeries}
	clock := &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	web := openClaimWeb(t, store, cfg, sources, clock)
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{kept.lib.ID, dropped.lib.ID}, []string{delegated.MetadataWrite})
	batch := agent.claim(map[string]any{})
	if len(batch.Series) != 2 {
		t.Fatalf("claim %+v", batch)
	}

	narrowed := web.raw("PATCH", "/api/mcp/credentials/"+agent.credential, fmt.Sprintf(`{"user_id":1,"name":"main loop agent","library_ids":[%d],"capabilities":["metadata_write"]}`, kept.lib.ID))
	if narrowed.Code != 200 {
		t.Fatalf("narrow %d %s", narrowed.Code, narrowed.Body)
	}
	results := agent.submit("after-narrowing", batch.LeaseID,
		identifyVerdict(droppedIDs[0], "the Bangumi title matches", "bangumi:2"),
		identifyVerdict(999999, "an unknown series", "bangumi:2"),
		identifyVerdict(keptIDs[0], "the Bangumi title matches", "bangumi:1"))
	wantResult(t, results[0], droppedIDs[0], false, "denied")
	wantResult(t, results[1], 999999, false, "denied")
	wantResult(t, results[2], keptIDs[0], true, "identified")
	if results[0].Message != results[1].Message {
		t.Fatalf("a series outside the credential reads differently from a missing one: %q vs %q", results[0].Message, results[1].Message)
	}
	if got := web.identification(droppedIDs[0]); got.Status != "unprocessed" {
		t.Fatalf("a series outside the credential changed %+v", got)
	}

	// Claiming and answering need metadata_write, which a regular account's credential never carries.
	reader := web.mainLoopAgent(cfg, 1, []int64{kept.lib.ID}, []string{delegated.Read})
	for _, name := range []string{"library_claim_series", "library_submit_verdicts"} {
		result, err := reader.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{"request_key": "k", "lease_id": batch.LeaseID, "verdicts": []any{notFoundVerdict(keptIDs[0], "read only")}}})
		if err == nil && !result.IsError {
			t.Fatalf("%s is callable with read only: %s", name, resultText(result))
		}
	}
	regular, err := store.CreateUser(ctx, database.CreateUserParams{Username: "regular", PasswordHash: "unused", Role: database.RoleRegular})
	if err != nil {
		t.Fatal(err)
	}
	writer := web.mainLoopAgent(cfg, regular.ID, []int64{kept.lib.ID}, []string{delegated.MetadataWrite})
	var refused claimedBatch
	if result := writer.call("library_claim_series", map[string]any{}, &refused); !result.IsError {
		t.Fatalf("a regular account's credential claimed %+v", refused)
	}
}

func TestClaimKeepsTheBatchWithinItsByteBudget(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	directories := make([]string, 12)
	for i := range directories {
		directories[i] = fmt.Sprintf("Title %d", i+1)
	}
	library, ids := claimLibrary(t, store, "Bulky", directories...)
	long := func(prefix string, runes int) string {
		out := []rune(prefix)
		for len(out) < runes {
			out = append(out, '漫')
		}
		return string(out)
	}
	// Every source fills its packet places with long text, so each packet reaches its own bound.
	sources := &recordingSources{answer: func(source, query string) ([]*metadata.SeriesMetadata, error) {
		var n int
		if _, err := fmt.Sscanf(query, "Title %d", &n); err != nil || (source != "bangumi" && source != "anilist") {
			return nil, nil
		}
		var out []*metadata.SeriesMetadata
		for i := range 5 {
			id := n*10 + i
			entry := &metadata.SeriesMetadata{Title: long(query, 120), OriginalTitle: long("Original", 120), Summary: long("Summary", 200), SourceID: id,
				Authors: []metadata.SeriesAuthor{{Name: long("A", 50)}, {Name: long("B", 50)}, {Name: long("C", 50)}}}
			if source == "bangumi" {
				entry.SourceURL = fmt.Sprintf("https://bgm.tv/subject/%d", id)
			} else {
				entry.SourceURL = fmt.Sprintf("https://anilist.co/manga/%d", id)
			}
			out = append(out, entry)
		}
		return out, nil
	}}
	clock := &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	web := openClaimWeb(t, store, cfg, sources, clock)
	if _, run := web.prefetch("{}"); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.MetadataWrite})

	result, err := agent.session.CallTool(context.Background(), &mcp.CallToolParams{Name: "library_claim_series", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("claim %+v %v", result, err)
	}
	text := resultText(result)
	var first claimedBatch
	if err := json.Unmarshal([]byte(text), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Series) == 0 || len(first.Series) >= len(ids) || len(text) > 31<<10 {
		t.Fatalf("claimed %d series in %d bytes", len(first.Series), len(text))
	}
	for _, s := range first.Series {
		if len(s.Evidence.Candidates) == 0 {
			t.Fatalf("a claimed packet lost its candidates %+v", s)
		}
	}
	// The series the budget left out stay unclaimed and come with the next claim.
	rest := agent.claim(map[string]any{})
	got := append(first.ids(), rest.ids()...)
	slices.Sort(got)
	if !reflect.DeepEqual(got, ids) {
		t.Fatalf("two claims handed out %v, want %v", got, ids)
	}
}
