package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"yomifolio/internal/application/identification"
	"yomifolio/internal/metadata"
)

// mountIdentification serves the owner's identification routes. The whole prefix is
// administrator-only, reads included, because conclusions name credentials and accounts.
func (c *administrationHTTP) mountIdentification(r chi.Router) {
	r.Get("/identification/series/{seriesId}", c.getSeriesIdentification)
	r.Put("/identification/series/{seriesId}/owner-review", c.setSeriesOwnerReview)
	r.Delete("/identification/series/{seriesId}/owner-review", c.withdrawSeriesOwnerReview)
	r.Post("/identification/prefetch", c.startIdentificationPrefetch)
	r.Post("/identification/series/{seriesId}/research", c.researchSeries)
	r.Put("/identification/series/{seriesId}/conclusions/{source}", c.identifySeriesAsOwner)
}

// identifySeriesAsOwner records the owner's pick from a manual search of one source
// ({"result": <search result>}) as the owner's identification of the series with that entry.
func (c *administrationHTTP) identifySeriesAsOwner(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	seriesID, err := parseID(r, "seriesId")
	if err != nil || seriesID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	var body struct {
		Result *metadata.SeriesMetadata `json:"result"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil || body.Result == nil {
		jsonError(w, http.StatusBadRequest, "Invalid identification: send the picked search result as result")
		return
	}
	user, _ := userFromContext(r.Context())
	out, err := c.identification.IdentifyAsOwner(r.Context(), user.ID, identification.OwnerIdentifyInput{SeriesID: seriesID, Source: chi.URLParam(r, "source"), Result: *body.Result})
	switch {
	case errors.Is(err, identification.ErrNotIdentifiable):
		jsonError(w, http.StatusBadRequest, "Not an identification source entry: the source must be a catalogue source and the result must name its entry")
	case errors.Is(err, identification.ErrExcludedEntry):
		jsonError(w, http.StatusConflict, "This entry is an excluded candidate of the series")
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
