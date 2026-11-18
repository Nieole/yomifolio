package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"yomifolio/internal/catalogsearch"
)

func (c *ReaderHTTP) searchBooks(w http.ResponseWriter, r *http.Request) {
	params, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil || len(params["cursor"]) > 1 {
		writeInvalidSearchCursor(w)
		return
	}
	limit := 20
	if values, ok := params["limit"]; ok {
		var err error
		if len(values) != 1 || values[0] == "" || strings.IndexFunc(values[0], func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			jsonError(w, http.StatusBadRequest, "Invalid search limit")
			return
		}
		limit, err = strconv.Atoi(values[0])
		if err != nil || limit < 1 || limit > 100 {
			jsonError(w, http.StatusBadRequest, "Invalid search limit")
			return
		}
	}
	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	// authGate revalidates the current account on every request. All current
	// readers can access all catalogue libraries; role changes invalidate cursors.
	authority := "anonymous"
	if user, ok := userFromContext(r.Context()); ok {
		authority = user.Role
	}
	result, err := c.search.Query(r.Context(), subject, params.Get("q"), params.Get("target"), int32(limit), params.Get("cursor"), authority)
	if errors.Is(err, catalogsearch.ErrCursor) {
		writeInvalidSearchCursor(w)
		return
	}
	if errors.Is(err, catalogsearch.ErrTarget) || errors.Is(err, catalogsearch.ErrLimit) {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Search failed")
		return
	}
	out := SearchResult{Hits: make([]*SearchHit, 0, len(result.Hits)), Total: result.Total, MaxScore: result.MaxScore, HasMore: result.HasMore, NextCursor: result.NextCursor}
	for _, hit := range result.Hits {
		out.Hits = append(out.Hits, &SearchHit{ID: hit.ID, Score: hit.Score, Fields: SearchHitFields{ID: hit.ID, Title: hit.Title, SeriesName: hit.SeriesName, Type: hit.Kind, CoverPath: hit.CoverPath}})
	}
	jsonResponse(w, http.StatusOK, out)
}

func writeInvalidSearchCursor(w http.ResponseWriter) {
	jsonResponse(w, http.StatusBadRequest, map[string]string{"error": "Search cursor is invalid; search again", "code": "invalid_search_cursor"})
}
