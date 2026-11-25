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

func registerMCPEnrichment(server *mcp.Server, service *enrichment.Service, subject delegated.Subject, transportCtx context.Context) {
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
		return nil, out, publicMCPToolError(err, "invalid target", "metadata state unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_enrich_metadata", Description: "Atomically supplement ONE series using persisted explicitly identity/edition-confirmed proposal_ids. Select title/summary/publisher/status/rating/tags/source_links/authors. Scalars only fill empty values; tags append conservative content genres; site scores use equal-weight ten-point normalization. Requires expected values for selected fields; current locks reject the whole series. request_key (1-80 printable ASCII) identifies this exact request forever for this credential: retry returns original outcome, replayed=true, applied=false; changed payload with same key rejected. Cover selection accepts only an authorized same-series archive book_id/page, quality_compared=true and a short comparison reason. Select cover with expected.cover to improve only the unlocked archive-generated catalogue cover. Cover and text commit together; manual/upload covers are protected. No arbitrary URL or path. Authors append confirmed source identities; optional author_corrections map exact from/to name+role with a short reason: from belongs to expected authors, to belongs to confirmed evidence. Series-local corrections never edit shared identities; whole-set expected checks apply. Optional adjudication={mode,reason} processes exactly ONE existing proposal: partial consumes only actual changed pending fields; supplement_and_close also closes remaining differences (fields may be empty after prior acceptance); reject requires empty fields and never reverses prior changes. Status distinguishes pending/accepted/applied, supplemented_closed and rejected. A short reason and current read+metadata_write authority are required. Only the seven existing proposal fields are adjudicable; no cover/source_links. Legacy proposals lacking identity/edition evidence may only be rejected; never invent confirmation. Terminal or already-consumed field races return persistent conflict; identical retries retain their original immutable result. Ordinary supplementation does not consume fields but also honors proposal terminal decisions.", Annotations: write}, func(ctx context.Context, _ *mcp.CallToolRequest, in enrichment.Input) (*mcp.CallToolResult, enrichment.Operation, error) {
		ctx, cancel := imageRequestContext(ctx, transportCtx)
		defer cancel()
		out, err := service.Apply(ctx, subject, in)
		return nil, out, publicMCPToolError(err, "invalid enrichment request or reused key", "metadata operation unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_enrich_metadata_batch", Description: "Process 1-20 independent series operations in order; NOT batch atomic. Results distinguish changed/no-change/conflict/failed/unprocessed. Each input has its own unique request_key. Cancelled or response-budget-exhausted remainder is unprocessed; query uncertain receipts before retrying.", Annotations: write}, func(ctx context.Context, _ *mcp.CallToolRequest, in batch) (*mcp.CallToolResult, enrichment.BatchResult, error) {
		ctx, cancel := imageRequestContext(ctx, transportCtx)
		defer cancel()
		out, err := service.Batch(ctx, subject, in.Operations)
		return nil, out, publicMCPToolError(err, "invalid enrichment batch", "metadata operation unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_metadata_operation", Description: `Read one metadata operation receipt: the fields it changed, before and after values, and whether undo is still possible (undo_unavailable=true once cleaned up). Example: {"operation_id": "0d4f6c2a9b1e47c3a5d8e2f1b6c9a370"}`, Annotations: read}, func(ctx context.Context, _ *mcp.CallToolRequest, in lookup) (*mcp.CallToolResult, enrichment.Operation, error) {
		out, err := service.Get(ctx, subject, in.OperationID)
		return nil, out, publicMCPToolError(err, "invalid operation", "metadata operation unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_list_metadata_operations", Description: `List the metadata operation receipts of one series, newest first. limit 1-50, default 20; pass next_before as before for the next page. Example: {"series_id": 12, "limit": 20}`, Annotations: read}, func(ctx context.Context, _ *mcp.CallToolRequest, in list) (*mcp.CallToolResult, enrichment.Page, error) {
		out, err := service.List(ctx, subject, in.SeriesID, in.Before, in.Limit)
		return nil, out, publicMCPToolError(err, "invalid operation page", "metadata operations unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_undo_metadata_operation", Description: `Reverse the fields one metadata operation actually changed. A field edited since then, a locked field or a changed author/tag/link set is a conflict; unrelated edits stay. request_key (1-80 printable ASCII) makes a retry return the original outcome. Example: {"request_key": "undo-1", "operation_id": "0d4f6c2a9b1e47c3a5d8e2f1b6c9a370"}`, Annotations: write}, func(ctx context.Context, _ *mcp.CallToolRequest, in enrichment.UndoInput) (*mcp.CallToolResult, enrichment.Operation, error) {
		out, err := service.Undo(ctx, subject, in)
		return nil, out, publicMCPToolError(err, "invalid undo request or reused key", "metadata undo unavailable")
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
