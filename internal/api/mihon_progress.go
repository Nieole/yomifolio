package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

const mihonFutureTolerance = 5 * time.Minute

func (c *ReaderHTTP) mihonNow() time.Time {
	if c.mihonClock != nil {
		return c.mihonClock().UTC()
	}
	return time.Now().UTC()
}

func (c *ReaderHTTP) mihonCapabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, map[string]any{
		"sync_revision": 1, "page_base": 1, "page_sync_backend": true,
		"occurrence_merge": "calibrated_occurrence", "equal_time_policy": "preserve_existing",
		"future_tolerance_seconds": int64(mihonFutureTolerance / time.Second), "bulk_progress": false,
	})
}

func (c *ReaderHTTP) mihonTime(w http.ResponseWriter, r *http.Request) {
	received := c.mihonNow()
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, map[string]time.Time{"received_at": received, "sent_at": c.mihonNow()})
}

type mihonProgressSnapshot struct {
	Page       int64      `json:"page"`
	IsRead     bool       `json:"is_read"`
	State      string     `json:"state"`
	OccurredAt *time.Time `json:"occurred_at"`
	FactID     string     `json:"fact_id"`
}

func mihonSnapshot(state database.ReadingPositionState) mihonProgressSnapshot {
	snapshot := mihonProgressSnapshot{Page: state.Page, IsRead: state.IsRead, State: "absent", FactID: state.FactID}
	if state.Present {
		snapshot.State = "position"
	}
	if state.Cleared {
		snapshot.State = "cleared"
	}
	if state.Present && state.BasisPresent {
		at := time.Unix(0, state.ChangedAtNS).UTC()
		snapshot.OccurredAt = &at
	}
	return snapshot
}

func (c *ReaderHTTP) mihonProgressError(w http.ResponseWriter, status int, code, message string) {
	jsonResponse(w, status, map[string]any{"error": message, "code": code, "server_time": c.mihonNow()})
}

func (c *ReaderHTTP) mihonGetProgress(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	bookID, err := parseID(r, "bookId")
	if err != nil {
		c.mihonProgressError(w, 400, "invalid_sync", "Invalid book ID")
		return
	}
	state, err := c.positions.GetPosition(r.Context(), c.currentUserID(r), bookID)
	if err != nil {
		c.mihonStorageError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, struct {
		mihonProgressSnapshot
		ServerTime time.Time `json:"server_time"`
	}{mihonSnapshot(state), c.mihonNow()})
}

func (c *ReaderHTTP) mihonStorageError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		c.mihonProgressError(w, 404, "not_found", "Book not found")
		return
	}
	c.mihonProgressError(w, 500, "storage_error", "Failed to access progress")
}

func validMihonFactID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		if ch < 33 || ch > 126 {
			return false
		}
	}
	return true
}

func (c *ReaderHTTP) mihonUpdateProgress(w http.ResponseWriter, r *http.Request) {
	received := c.mihonNow()
	w.Header().Set("Cache-Control", "no-store")
	bookID, err := parseID(r, "bookId")
	if err != nil {
		c.mihonProgressError(w, 400, "invalid_sync", "Invalid book ID")
		return
	}
	var req struct {
		Page         int64      `json:"page"`
		UpdatedAt    *time.Time `json:"updated_at"`
		FactID       string     `json:"fact_id"`
		SyncRevision int        `json:"sync_revision"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		c.mihonProgressError(w, 400, "invalid_sync", "Invalid request payload")
		return
	}
	if req.SyncRevision != 0 && req.SyncRevision != 1 {
		c.mihonProgressError(w, 400, "unsupported_revision", "Unsupported sync revision")
		return
	}
	if req.SyncRevision == 1 {
		var extra any
		if decoder.Decode(&extra) != io.EOF || !validMihonFactID(req.FactID) || req.UpdatedAt == nil || req.UpdatedAt.IsZero() {
			c.mihonProgressError(w, 400, "invalid_sync", "Sync requires a fact_id and RFC3339 occurrence timestamp")
			return
		}
		// The durable basis stores nanoseconds; reject instants outside that range.
		if !time.Unix(0, req.UpdatedAt.UnixNano()).Equal(*req.UpdatedAt) {
			c.mihonProgressError(w, 400, "invalid_sync", "Occurrence timestamp is out of range")
			return
		}
		if req.UpdatedAt.After(received.Add(mihonFutureTolerance)) {
			c.mihonProgressError(w, 422, "clock_error", "Occurrence exceeds server time plus five minutes; preserve the pending fact")
			return
		}
	}
	result, err := c.positions.RecordPosition(r.Context(), reading.Position{UserID: c.currentUserID(r), BookID: bookID, Page: req.Page, OccurredAt: req.UpdatedAt, FactID: req.FactID})
	if err != nil {
		c.mihonStorageError(w, err)
		return
	}
	status, outcome := "Progress unchanged", "unchanged"
	switch {
	case result.Applied:
		status, outcome = "Progress updated", "applied"
	case result.Duplicate:
		outcome = "duplicate"
	case result.Stale:
		outcome = "stale"
	}
	jsonResponse(w, http.StatusOK, struct {
		Status        string                `json:"status"`
		Outcome       string                `json:"outcome"`
		FactID        string                `json:"fact_id"`
		Authoritative mihonProgressSnapshot `json:"authoritative"`
		ServerTime    time.Time             `json:"server_time"`
	}{status, outcome, req.FactID, mihonSnapshot(result.Snapshot), c.mihonNow()})
}
