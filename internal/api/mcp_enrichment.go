package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
)

func registerMCPEnrichment(server *mcp.Server, service *enrichment.Service, subject delegated.Subject) {
	type target struct {
		SeriesID int64 `json:"series_id"`
	}
	type batch struct {
		Operations []enrichment.Input `json:"operations"`
	}
	type lookup struct {
		OperationID string `json:"operation_id"`
	}
	type list struct {
		SeriesID int64 `json:"series_id"`
		Before   int64 `json:"before,omitempty"`
		Limit    int   `json:"limit,omitempty"`
	}
	read := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}
	write := &mcp.ToolAnnotations{IdempotentHint: true}
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_enrichment_state", Description: "Read current field values for expected-value checks. Supply these relevant values when supplementing; unrelated fields do not block. This is not confirmed matching evidence.", Annotations: read}, func(ctx context.Context, _ *mcp.CallToolRequest, in target) (*mcp.CallToolResult, enrichment.State, error) {
		out, err := service.State(ctx, subject, in.SeriesID)
		return discoveryResult(), out, publicMCPToolError(err, "invalid target", "metadata state unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_enrich_metadata", Description: "Atomically supplement ONE series using persisted explicitly identity/edition-confirmed proposal_ids. Select title/summary/publisher/status/rating/tags/source_links. Scalars only fill empty values; tags append conservative content genres; site scores use equal-weight ten-point normalization. Requires expected values for selected fields; current locks reject the whole series. request_key (1-80 printable ASCII) identifies this exact request forever for this credential: retry returns original outcome, replayed=true, applied=false; changed payload with same key rejected. No authors or covers.", Annotations: write}, func(ctx context.Context, _ *mcp.CallToolRequest, in enrichment.Input) (*mcp.CallToolResult, enrichment.Operation, error) {
		out, err := service.Apply(ctx, subject, in)
		return discoveryResult(), out, publicMCPToolError(err, "invalid enrichment request or reused key", "metadata operation unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_enrich_metadata_batch", Description: "Process 1-20 independent series operations in order; NOT batch atomic. Results distinguish changed/no-change/conflict/failed/unprocessed. Each input has its own unique request_key. Cancelled or response-budget-exhausted remainder is unprocessed; query uncertain receipts before retrying.", Annotations: write}, func(ctx context.Context, _ *mcp.CallToolRequest, in batch) (*mcp.CallToolResult, enrichment.BatchResult, error) {
		out, err := service.Batch(ctx, subject, in.Operations)
		return discoveryResult(), out, publicMCPToolError(err, "invalid enrichment batch", "metadata operation unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_metadata_operation", Description: "Read a complete persistent operation receipt and undo status under current target authority. Not latest field provenance. Undo material has no automatic expiry.", Annotations: read}, func(ctx context.Context, _ *mcp.CallToolRequest, in lookup) (*mcp.CallToolResult, enrichment.Operation, error) {
		out, err := service.Get(ctx, subject, in.OperationID)
		return discoveryResult(), out, publicMCPToolError(err, "invalid operation", "metadata operation unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_list_metadata_operations", Description: "List persistent receipts for ONE currently authorized series, descending sequence. limit default 20 max 50; pass next_before as before for next page. Responses bounded without truncating receipts.", Annotations: read}, func(ctx context.Context, _ *mcp.CallToolRequest, in list) (*mcp.CallToolResult, enrichment.Page, error) {
		out, err := service.List(ctx, subject, in.SeriesID, in.Before, in.Limit)
		return discoveryResult(), out, publicMCPToolError(err, "invalid operation page", "metadata operations unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_undo_metadata_operation", Description: "Atomically reverse the actual changed fields of one enrichment. Rechecks current authorization, locks and exact after-values; whole tag/link changes conflict, unrelated edits survive. request_key uses the same persistent retry namespace as enrichment. Replayed undo never reports a new change. Already reversed operations are no-change.", Annotations: write}, func(ctx context.Context, _ *mcp.CallToolRequest, in enrichment.UndoInput) (*mcp.CallToolResult, enrichment.Operation, error) {
		out, err := service.Undo(ctx, subject, in)
		return discoveryResult(), out, publicMCPToolError(err, "invalid undo request or reused key", "metadata undo unavailable")
	})
}
func (c *administrationHTTP) listMetadataOperations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, _ := userFromContext(r.Context())
	values := []int64{0, 0, 20}
	for i, key := range []string{"series_id", "before", "limit"} {
		if text := r.URL.Query().Get(key); text != "" {
			n, err := strconv.ParseInt(text, 10, 32)
			if err != nil {
				jsonError(w, 400, "Invalid operation page")
				return
			}
			values[i] = n
		}
	}
	out, err := c.enrichment.List(r.Context(), delegated.Subject{UserID: user.ID}, values[0], values[1], int(values[2]))
	if err != nil {
		credentialError(w, err)
		return
	}
	jsonResponse(w, 200, out)
}
func (c *administrationHTTP) getMetadataOperation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, _ := userFromContext(r.Context())
	out, err := c.enrichment.Get(r.Context(), delegated.Subject{UserID: user.ID}, chi.URLParam(r, "operationId"))
	if err != nil {
		credentialError(w, err)
		return
	}
	jsonResponse(w, 200, out)
}
func (c *administrationHTTP) undoMetadataOperation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, _ := userFromContext(r.Context())
	var in enrichment.UndoInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		jsonError(w, 400, "Invalid undo request")
		return
	}
	in.OperationID = chi.URLParam(r, "operationId")
	out, err := c.enrichment.Undo(r.Context(), delegated.Subject{UserID: user.ID}, in)
	if err != nil {
		credentialError(w, err)
		return
	}
	jsonResponse(w, 200, out)
}
