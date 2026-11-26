package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"yomifolio/internal/application/identification"
)

// mountIdentification serves the owner's identification routes. The whole prefix is
// administrator-only, reads included, because conclusions name credentials and accounts.
func (c *administrationHTTP) mountIdentification(r chi.Router) {
	r.Get("/identification/series/{seriesId}", c.getSeriesIdentification)
	r.Put("/identification/series/{seriesId}/owner-review", c.setSeriesOwnerReview)
	r.Delete("/identification/series/{seriesId}/owner-review", c.withdrawSeriesOwnerReview)
	r.Post("/identification/prefetch", c.startIdentificationPrefetch)
	r.Post("/identification/series/{seriesId}/research", c.researchSeries)
	r.Get("/identification/pending", c.listPendingIdentification)
	r.Post("/identification/series/{seriesId}/identify", c.pickSeriesCandidates)
	r.Post("/identification/series/{seriesId}/exclusions", c.excludeSeriesCandidate)
	r.Post("/identification/series/{seriesId}/not-found", c.concludeSeriesNotFound)
	r.Post("/identification/series/{seriesId}/notes/{noteId}/close", c.closeSeriesNote)
}

// closeSeriesNote records that the owner has seen one note of the series and answers with its
// identification after it.
func (c *administrationHTTP) closeSeriesNote(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	seriesID, err := parseID(r, "seriesId")
	noteID, noteErr := parseID(r, "noteId")
	if err != nil || noteErr != nil || seriesID <= 0 || noteID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid series or note ID")
		return
	}
	user, _ := userFromContext(r.Context())
	out, err := c.identification.CloseNote(r.Context(), user.ID, seriesID, noteID)
	if err != nil {
		writePendingDecisionError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, out)
}

// concludeSeriesNotFound records the owner's verdict that the sources have no entry for a waiting
// series and answers with its identification after it.
func (c *administrationHTTP) concludeSeriesNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	seriesID, err := parseID(r, "seriesId")
	if err != nil || seriesID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	user, _ := userFromContext(r.Context())
	out, err := c.identification.ConcludeNotFound(r.Context(), user.ID, seriesID)
	if err != nil {
		writePendingDecisionError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, out)
}

// excludeSeriesCandidate records the owner ruling out one candidate of a waiting series'
// evidence ({"candidate_id": "bangumi:2"}) and answers with the series' identification after it.
func (c *administrationHTTP) excludeSeriesCandidate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	seriesID, err := parseID(r, "seriesId")
	if err != nil || seriesID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	var in struct {
		CandidateID string `json:"candidate_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid exclusion request")
		return
	}
	user, _ := userFromContext(r.Context())
	out, err := c.identification.ExcludeCandidate(r.Context(), user.ID, seriesID, in.CandidateID)
	if err != nil {
		writePendingDecisionError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, out)
}

// pickSeriesCandidates records the owner's identification of a waiting series with candidates of
// its evidence ({"candidate_ids": ["bangumi:1"]}) and answers with the result and its receipt.
func (c *administrationHTTP) pickSeriesCandidates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	seriesID, err := parseID(r, "seriesId")
	if err != nil || seriesID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	var in struct {
		CandidateIDs []string `json:"candidate_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid identification request")
		return
	}
	user, _ := userFromContext(r.Context())
	out, err := c.identification.PickCandidates(r.Context(), user.ID, seriesID, in.CandidateIDs)
	if err != nil {
		writePendingDecisionError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, out)
}

// writePendingDecisionError answers a refused owner decision on the pending list. A conflict names
// what changed under the page, so it can read the series again.
func writePendingDecisionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identification.ErrInvalidPending):
		jsonError(w, http.StatusBadRequest, "Invalid decision: cite 1-5 evidence candidates of known sources, at most one per source")
	case errors.Is(err, identification.ErrSeriesNotFound):
		jsonError(w, http.StatusNotFound, "Series not found")
	case errors.Is(err, identification.ErrNoteNotFound):
		jsonError(w, http.StatusNotFound, "Note not found")
	case errors.Is(err, identification.ErrDenied):
		jsonError(w, http.StatusForbidden, apiText(requestLocale(r), "auth.admin_required"))
	case errors.Is(err, identification.ErrNotANote):
		jsonResponse(w, http.StatusConflict, map[string]string{"code": "not_a_note", "error": "Only a note closes; an escalation ends with a decision about the series"})
	case errors.Is(err, identification.ErrNotPending):
		jsonResponse(w, http.StatusConflict, map[string]string{"code": "not_pending", "error": "The series no longer waits for this decision"})
	case errors.Is(err, identification.ErrCandidateUnavailable):
		jsonResponse(w, http.StatusConflict, map[string]string{"code": "candidate_unavailable", "error": "The candidate is no longer offered for this series"})
	default:
		jsonError(w, http.StatusInternalServerError, "Identification unavailable")
	}
}

// listPendingIdentification pages the owner's pending list (?category=&offset=&limit=); limit=0
// answers the counts only.
func (c *administrationHTTP) listPendingIdentification(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	query := r.URL.Query()
	offset, offsetErr := parseOptionalInt(query.Get("offset"), 0)
	limit, limitErr := parseOptionalInt(query.Get("limit"), identification.DefaultPendingPage)
	if offsetErr != nil || limitErr != nil {
		jsonError(w, http.StatusBadRequest, "Invalid pending query")
		return
	}
	user, _ := userFromContext(r.Context())
	out, err := c.identification.Pending(r.Context(), user.ID, identification.PendingQuery{Category: query.Get("category"), Offset: offset, Limit: limit})
	switch {
	case errors.Is(err, identification.ErrInvalidPending):
		jsonError(w, http.StatusBadRequest, "Invalid pending query: offset is at least 0 and limit 0-50")
	case errors.Is(err, identification.ErrDenied):
		jsonError(w, http.StatusForbidden, apiText(requestLocale(r), "auth.admin_required"))
	case err != nil:
		jsonError(w, http.StatusInternalServerError, "Pending list unavailable")
	default:
		jsonResponse(w, http.StatusOK, out)
	}
}

// researchSeries searches one series again with the owner's term ({"query": "...", "wait_ms": n})
// and answers with the outcome; a search still running after the wait goes on as its run.
func (c *administrationHTTP) researchSeries(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	seriesID, err := parseID(r, "seriesId")
	if err != nil || seriesID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	var in identification.ResearchInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid research request")
		return
	}
	in.SeriesID = seriesID
	user, _ := userFromContext(r.Context())
	out, err := c.identification.ResearchAsOwner(r.Context(), user.ID, in)
	switch {
	case errors.Is(err, identification.ErrInvalidResearch):
		jsonError(w, http.StatusBadRequest, "Invalid research: the term needs 1-100 characters with a letter or digit and no control characters; wait_ms is 0-30000")
	case errors.Is(err, identification.ErrSeriesNotFound):
		jsonError(w, http.StatusNotFound, "Series not found")
	case errors.Is(err, identification.ErrDenied):
		jsonError(w, http.StatusForbidden, apiText(requestLocale(r), "auth.admin_required"))
	case err != nil:
		writeTaskLaunchError(w, err, "A research of this series is already running", "Research unavailable")
	default:
		jsonResponse(w, http.StatusOK, out)
	}
}

// startIdentificationPrefetch launches the evidence prefetch over one library ({"library_id": n})
// or over every library (an empty body).
func (c *administrationHTTP) startIdentificationPrefetch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LibraryID int64 `json:"library_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); (err != nil && !errors.Is(err, io.EOF)) || in.LibraryID < 0 {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}
	user, _ := userFromContext(r.Context())
	out, err := c.identification.StartPrefetch(r.Context(), user.ID, in.LibraryID)
	switch {
	case errors.Is(err, identification.ErrLibraryNotFound):
		jsonError(w, http.StatusNotFound, "Library not found")
	case errors.Is(err, identification.ErrDenied):
		jsonError(w, http.StatusForbidden, apiText(requestLocale(r), "auth.admin_required"))
	case err != nil:
		writeTaskLaunchError(w, err, "An evidence prefetch is already running", "Failed to start evidence prefetch")
	default:
		jsonResponse(w, http.StatusOK, out)
	}
}

func (c *administrationHTTP) getSeriesIdentification(w http.ResponseWriter, r *http.Request) {
	c.serveIdentification(w, r, c.identification.Series)
}
func (c *administrationHTTP) setSeriesOwnerReview(w http.ResponseWriter, r *http.Request) {
	c.serveIdentification(w, r, c.identification.SetOwnerReviewed)
}
func (c *administrationHTTP) withdrawSeriesOwnerReview(w http.ResponseWriter, r *http.Request) {
	c.serveIdentification(w, r, c.identification.WithdrawOwnerReviewed)
}

// serveIdentification answers every identification route with the authoritative state after the
// call, so the page renders what the server holds rather than what it asked for.
func (c *administrationHTTP) serveIdentification(w http.ResponseWriter, r *http.Request, call func(context.Context, int64, int64) (identification.SeriesIdentification, error)) {
	w.Header().Set("Cache-Control", "no-store")
	seriesID, err := parseID(r, "seriesId")
	if err != nil || seriesID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	user, _ := userFromContext(r.Context())
	out, err := call(r.Context(), user.ID, seriesID)
	switch {
	case errors.Is(err, identification.ErrSeriesNotFound):
		jsonError(w, http.StatusNotFound, "Series not found")
	case errors.Is(err, identification.ErrDenied):
		jsonError(w, http.StatusForbidden, apiText(requestLocale(r), "auth.admin_required"))
	case err != nil:
		jsonError(w, http.StatusInternalServerError, "Identification unavailable")
	default:
		jsonResponse(w, http.StatusOK, out)
	}
}
