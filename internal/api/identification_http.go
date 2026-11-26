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
	r.Get("/identification/pending", c.listAwaitingIdentification)
	r.Post("/identification/series/{seriesId}/identify", c.pickSeriesCandidates)
	r.Post("/identification/series/{seriesId}/exclusions", c.excludeSeriesCandidate)
	r.Post("/identification/series/{seriesId}/not-found", c.concludeSeriesNotFound)
	r.Post("/identification/series/{seriesId}/notes/{noteId}/close", c.closeSeriesNote)
	r.Post("/identification/series/{seriesId}/keep-identification", c.keepSeriesIdentification)
	r.Put("/identification/series/{seriesId}/conclusions/{source}", c.identifySeriesAsOwner)
	r.Delete("/identification/series/{seriesId}/conclusions/{source}", c.withdrawSeriesIdentification)
}

// identificationRefusals maps each identification refusal to its answer, in the order they are
// tried. A conflict carries a code the page reads to say what changed under it; the denial reads
// in the request's language.
var identificationRefusals = []struct {
	err     error
	status  int
	code    string
	message string
}{
	{identification.ErrDenied, http.StatusForbidden, "", ""},
	{identification.ErrSeriesNotFound, http.StatusNotFound, "", "Series not found"},
	{identification.ErrLibraryNotFound, http.StatusNotFound, "", "Library not found"},
	{identification.ErrNoteNotFound, http.StatusNotFound, "", "Note not found"},
	{identification.ErrNoConclusion, http.StatusNotFound, "", "The series is not identified with this source"},
	{identification.ErrInvalidAwaiting, http.StatusBadRequest, "", "Invalid request: a decision cites 1-5 evidence candidates of known sources, at most one per source; the list takes offset at least 0 and limit 0-50"},
	{identification.ErrInvalidResearch, http.StatusBadRequest, "", "Invalid research: the term needs 1-100 characters with a letter or digit and no control characters; wait_ms is 0-30000"},
	{identification.ErrNotIdentifiable, http.StatusBadRequest, "", "Not an identification source entry: the source must be a catalogue source and the result must name its entry"},
	{identification.ErrNotANote, http.StatusConflict, "not_a_note", "Only a note closes; an escalation ends with a decision about the series"},
	{identification.ErrNoLongerAwaiting, http.StatusConflict, "not_pending", "The series no longer waits for this decision"},
	{identification.ErrCandidateUnavailable, http.StatusConflict, "candidate_unavailable", "The candidate is no longer offered for this series"},
}

// writeIdentificationRefusal answers err when it is an identification refusal and reports whether
// it did; any other error is the caller's to answer.
func writeIdentificationRefusal(w http.ResponseWriter, r *http.Request, err error) bool {
	for _, refusal := range identificationRefusals {
		if !errors.Is(err, refusal.err) {
			continue
		}
		switch {
		case refusal.err == identification.ErrDenied:
			jsonError(w, refusal.status, apiText(requestLocale(r), "auth.admin_required"))
		case refusal.code != "":
			jsonResponse(w, refusal.status, map[string]string{"code": refusal.code, "error": refusal.message})
		default:
			jsonError(w, refusal.status, refusal.message)
		}
		return true
	}
	return false
}

// answerIdentification answers an identification route with its result, which is the
// authoritative state after the call so the page renders what the server holds, or with the
// refusal its error maps to.
func answerIdentification(w http.ResponseWriter, r *http.Request, out any, err error) {
	switch {
	case err == nil:
		jsonResponse(w, http.StatusOK, out)
	case !writeIdentificationRefusal(w, r, err):
		jsonError(w, http.StatusInternalServerError, "Identification unavailable")
	}
}

// identificationSeries reads the route's series ID, answering a malformed one itself.
func identificationSeries(w http.ResponseWriter, r *http.Request) (int64, bool) {
	w.Header().Set("Cache-Control", "no-store")
	seriesID, err := parseID(r, "seriesId")
	if err != nil || seriesID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return 0, false
	}
	return seriesID, true
}

// withdrawSeriesIdentification withdraws the owner's or an agent's identification of the series
// with one source and answers with the series' identification after it.
func (c *administrationHTTP) withdrawSeriesIdentification(w http.ResponseWriter, r *http.Request) {
	seriesID, ok := identificationSeries(w, r)
	if !ok {
		return
	}
	user, _ := userFromContext(r.Context())
	out, err := c.identification.WithdrawAsOwner(r.Context(), user.ID, seriesID, chi.URLParam(r, "source"))
	answerIdentification(w, r, out, err)
}

// identifySeriesAsOwner records the owner's pick from a manual search of one source
// ({"result": <search result>}) as the owner's identification of the series with that entry.
func (c *administrationHTTP) identifySeriesAsOwner(w http.ResponseWriter, r *http.Request) {
	seriesID, ok := identificationSeries(w, r)
	if !ok {
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
	answerIdentification(w, r, out, err)
}

// keepSeriesIdentification records the owner's answer that a questioned identification is right
// and answers with the series' identification after it.
func (c *administrationHTTP) keepSeriesIdentification(w http.ResponseWriter, r *http.Request) {
	c.serveIdentification(w, r, c.identification.KeepIdentification)
}

// closeSeriesNote records that the owner has seen one note of the series and answers with its
// identification after it.
func (c *administrationHTTP) closeSeriesNote(w http.ResponseWriter, r *http.Request) {
	seriesID, ok := identificationSeries(w, r)
	if !ok {
		return
	}
	noteID, err := parseID(r, "noteId")
	if err != nil || noteID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid series or note ID")
		return
	}
	user, _ := userFromContext(r.Context())
	out, err := c.identification.CloseNote(r.Context(), user.ID, seriesID, noteID)
	answerIdentification(w, r, out, err)
}

// concludeSeriesNotFound records the owner's verdict that the sources have no entry for a waiting
// series and answers with its identification after it.
func (c *administrationHTTP) concludeSeriesNotFound(w http.ResponseWriter, r *http.Request) {
	c.serveIdentification(w, r, c.identification.ConcludeNotFound)
}

// excludeSeriesCandidate records the owner ruling out one candidate of a waiting series'
// evidence ({"candidate_id": "bangumi:2"}) and answers with the series' identification after it.
func (c *administrationHTTP) excludeSeriesCandidate(w http.ResponseWriter, r *http.Request) {
	seriesID, ok := identificationSeries(w, r)
	if !ok {
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
	answerIdentification(w, r, out, err)
}

// pickSeriesCandidates records the owner's identification of a waiting series with candidates of
// its evidence ({"candidate_ids": ["bangumi:1"]}) and answers with the result and its receipt.
func (c *administrationHTTP) pickSeriesCandidates(w http.ResponseWriter, r *http.Request) {
	seriesID, ok := identificationSeries(w, r)
	if !ok {
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
	answerIdentification(w, r, out, err)
}

// listAwaitingIdentification pages the owner's awaiting list (?category=&offset=&limit=); limit=0
// answers the counts only.
func (c *administrationHTTP) listAwaitingIdentification(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	query := r.URL.Query()
	offset, offsetErr := parseOptionalInt(query.Get("offset"), 0)
	limit, limitErr := parseOptionalInt(query.Get("limit"), identification.DefaultAwaitingPage)
	if offsetErr != nil || limitErr != nil {
		jsonError(w, http.StatusBadRequest, "Invalid awaiting-list query")
		return
	}
	user, _ := userFromContext(r.Context())
	out, err := c.identification.Awaiting(r.Context(), user.ID, identification.AwaitingQuery{Category: query.Get("category"), Offset: offset, Limit: limit})
	answerIdentification(w, r, out, err)
}

// researchSeries searches one series again with the owner's term ({"query": "...", "wait_ms": n})
// and answers with the outcome; a search still running after the wait goes on as its run.
func (c *administrationHTTP) researchSeries(w http.ResponseWriter, r *http.Request) {
	seriesID, ok := identificationSeries(w, r)
	if !ok {
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
	if err != nil && !writeIdentificationRefusal(w, r, err) {
		writeTaskLaunchError(w, err, "A research of this series is already running", "Research unavailable")
		return
	}
	if err == nil {
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
	if err != nil && !writeIdentificationRefusal(w, r, err) {
		writeTaskLaunchError(w, err, "An evidence prefetch is already running", "Failed to start evidence prefetch")
		return
	}
	if err == nil {
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

// serveIdentification answers a route whose use case takes only the acting account and the series.
func (c *administrationHTTP) serveIdentification(w http.ResponseWriter, r *http.Request, call func(context.Context, int64, int64) (identification.SeriesIdentification, error)) {
	seriesID, ok := identificationSeries(w, r)
	if !ok {
		return
	}
	user, _ := userFromContext(r.Context())
	out, err := call(r.Context(), user.ID, seriesID)
	answerIdentification(w, r, out, err)
}
