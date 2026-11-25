package api

import (
	"context"
	"errors"
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
