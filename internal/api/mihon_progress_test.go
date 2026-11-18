package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"yomifolio/internal/database"
	"yomifolio/internal/reading"

	"github.com/go-chi/chi/v5"
)

func TestMihonSnapshotDoesNotInferLegacyOccurrence(t *testing.T) {
	state := database.ReadingPositionState{
		Page: 4, Present: true,
		LastReadAt: sql.NullTime{Time: time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC), Valid: true},
	}
	snapshot := mihonSnapshot(state)
	if snapshot.Page != 4 || snapshot.State != "position" || snapshot.OccurredAt != nil {
		t.Fatalf("legacy snapshot invents an occurrence: %+v", snapshot)
	}
}

func TestMihonSnapshotReadStateWithoutPage(t *testing.T) {
	for _, read := range []bool{false, true} {
		t.Run(strconv.FormatBool(read), func(t *testing.T) {
			snapshot := mihonSnapshot(database.ReadingPositionState{Present: true, IsRead: read})
			if snapshot.State != "position" || snapshot.Page != 0 || snapshot.IsRead != read || snapshot.OccurredAt != nil {
				t.Fatalf("read-only snapshot invents a position or loses its flag: %+v", snapshot)
			}
		})
	}
}

type mihonTestACK struct {
	Status        string                `json:"status"`
	Outcome       string                `json:"outcome"`
	FactID        string                `json:"fact_id"`
	Authoritative mihonProgressSnapshot `json:"authoritative"`
	ServerTime    time.Time             `json:"server_time"`
}

func TestMihonProgressSyncRouter(t *testing.T) {
	c, store, _, root := newTestController(t)
	_, _, book := seedBookFixture(t, store, root, "Lib", "Series", "01.cbz", 20)
	now := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)
	c.reader.mihonClock = func() time.Time { return now }
	hash, err := hashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := store.CreateUser(context.Background(), database.CreateUserParams{Username: "owner", PasswordHash: hash, Role: database.RoleRegular})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(context.Background(), database.CreateUserParams{Username: "other", PasswordHash: hash, Role: database.RoleRegular}); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	c.SetupRoutes(router)
	base := "/api/mihon/v1"
	progress := base + "/books/" + strconv.FormatInt(book.ID, 10) + "/progress"
	request := func(method, path, body, user string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if user != "" {
			req.SetBasicAuth(user, "password1")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	for _, path := range []string{base + "/capabilities", base + "/time", progress} {
		if rec := request("GET", path, "", "owner"); rec.Code != 404 {
			t.Fatalf("disabled=%d", rec.Code)
		}
	}
	cfg := c.currentConfig()
	cfg.Protocols.Mihon.Enabled = true
	c.config.Replace(&cfg)
	for _, path := range []string{base + "/capabilities", base + "/time", progress} {
		if rec := request("GET", path, "", ""); rec.Code != 401 {
			t.Fatalf("auth %s=%d", path, rec.Code)
		}
		rec := request("GET", path, "", "owner")
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("GET %s=%d %s", path, rec.Code, rec.Body.String())
		}
	}
	clock := request("GET", base+"/time", "", "owner")
	var times map[string]time.Time
	if err := json.Unmarshal(clock.Body.Bytes(), &times); err != nil || !times["received_at"].Equal(now) || !times["sent_at"].Equal(now) {
		t.Fatalf("time=%s err=%v", clock.Body.String(), err)
	}
	caps := request("GET", base+"/capabilities", "", "owner")
	var capabilities map[string]any
	if err := json.Unmarshal(caps.Body.Bytes(), &capabilities); err != nil || capabilities["sync_revision"] != float64(1) || capabilities["page_base"] != float64(1) || capabilities["future_tolerance_seconds"] != float64(300) || capabilities["bulk_progress"] != false {
		t.Fatalf("caps=%s err=%v", caps.Body.String(), err)
	}
	fact := func(id string, at time.Time, page int) string {
		return fmt.Sprintf(`{"sync_revision":1,"fact_id":%q,"updated_at":%q,"page":%d}`, id, at.Format(time.RFC3339Nano), page)
	}
	for _, test := range []struct {
		body, code string
		status     int
	}{
		{`{"sync_revision":2}`, "unsupported_revision", 400},
		{`{"sync_revision":1}`, "invalid_sync", 400},
		{`{"sync_revision":1,"fact_id":"x","updated_at":"bad"}`, "invalid_sync", 400},
		{fact(" ", now, 1), "invalid_sync", 400},
		{fact(strings.Repeat("x", 129), now, 1), "invalid_sync", 400},
		{fact("zero", time.Time{}, 1), "invalid_sync", 400},
		{fact("future", now.Add(5*time.Minute+time.Nanosecond), 1), "clock_error", 422},
	} {
		rec := request("POST", progress, test.body, "owner")
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != test.status || body["code"] != test.code || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("body=%s response=%d %s", test.body, rec.Code, rec.Body.String())
		}
	}
	read := func(user string) mihonProgressSnapshot {
		rec := request("GET", progress, "", user)
		var snap mihonProgressSnapshot
		if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil || rec.Code != 200 {
			t.Fatalf("read=%d %s err=%v", rec.Code, rec.Body.String(), err)
		}
		return snap
	}
	if snap := read("owner"); snap.State != "absent" || snap.OccurredAt != nil {
		t.Fatalf("absent=%+v", snap)
	}
	submit := func(body, outcome, status, winner string, page int64) mihonTestACK {
		rec := request("POST", progress, body, "owner")
		var ack mihonTestACK
		if err := json.Unmarshal(rec.Body.Bytes(), &ack); err != nil || rec.Code != 200 || ack.Outcome != outcome || ack.Status != status || ack.Authoritative.FactID != winner || ack.Authoritative.Page != page || rec.Header().Get("Cache-Control") != "no-store" || !ack.ServerTime.Equal(now) {
			t.Fatalf("ACK=%d %s err=%v", rec.Code, rec.Body.String(), err)
		}
		return ack
	}
	first := submit(fact("a", now, 0), "applied", "Progress updated", "a", 1)
	if first.FactID != "a" || first.Authoritative.OccurredAt == nil || !first.Authoritative.OccurredAt.Equal(now) {
		t.Fatalf("ACK=%+v", first)
	}
	boundary := now.Add(5 * time.Minute)
	submit(fact("b", boundary, 100), "applied", "Progress updated", "b", 20)
	submit(fact("a", now, 0), "duplicate", "Progress unchanged", "b", 20)
	submit(fact("stale", now, 3), "stale", "Progress unchanged", "b", 20)
	submit(fact("equal", boundary, 3), "stale", "Progress unchanged", "b", 20)
	submit(fact("equal-page", boundary, 20), "unchanged", "Progress unchanged", "b", 20)
	if snap := read("other"); snap.State != "absent" {
		t.Fatalf("isolation=%+v", snap)
	}
	clearAt := boundary.Add(time.Second)
	if _, err := c.reader.positions.MarkBooks(context.Background(), reading.MarkBooksInput{UserID: owner.ID, BookIDs: []int64{book.ID}, OperationID: "clear", OccurredAt: &clearAt}); err != nil {
		t.Fatal(err)
	}
	if snap := read("owner"); snap.State != "cleared" || snap.FactID != "mark:clear" || snap.Page != 0 || snap.OccurredAt == nil || !snap.OccurredAt.Equal(clearAt) {
		t.Fatalf("cleared=%+v", snap)
	}
	submit(fact("a", now, 0), "duplicate", "Progress unchanged", "mark:clear", 0)
	// Legacy mode still accepts future timestamps and preserves its exact status strings.
	legacyAt := now.Add(time.Hour)
	submit(fmt.Sprintf(`{"page":4,"updated_at":%q}`, legacyAt.Format(time.RFC3339Nano)), "applied", "Progress updated", "", 4)
	submit(fmt.Sprintf(`{"sync_revision":0,"page":4,"updated_at":%q}`, legacyAt.Format(time.RFC3339Nano)), "unchanged", "Progress unchanged", "", 4)
	rec := request(http.MethodGet, base+"/books/999999/progress", "", "owner")
	if rec.Code != 404 {
		t.Fatalf("missing=%d %s", rec.Code, rec.Body.String())
	}
	// No per-protocol batch endpoint is advertised or installed.
	if rec := request("POST", base+"/books/bulk-progress/sync", `{}`, "owner"); rec.Code != 404 {
		t.Fatalf("bulk=%d", rec.Code)
	}
}
