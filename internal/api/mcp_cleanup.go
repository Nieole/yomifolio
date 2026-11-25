package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
)

func registerMCPCleanup(server *mcp.Server, service *enrichment.Service, subject delegated.Subject, transportCtx context.Context) {
	mcp.AddTool(server, &mcp.Tool{Name: "library_start_metadata_cleanup", Description: "Accept 1-20 explicit operation_id/request_key pairs in ONE library for asynchronous cleanup. Independent cleanup permission, no further human confirmation. Disconnect does not cancel; use existing run query/wait/cancel. Check each item's durable cleanup result by request_key; cancelled remainder is unprocessed. Not batch atomic; retries can create a new run but never redo completed item effects.", Annotations: &mcp.ToolAnnotations{}}, func(ctx context.Context, _ *mcp.CallToolRequest, in enrichment.CleanupBatchInput) (*mcp.CallToolResult, delegated.Run, error) {
		out, err := service.StartCleanup(ctx, subject, in)
		return nil, out, publicMCPToolError(err, "invalid cleanup batch", "cleanup unavailable")
	})
	type key struct {
		RequestKey string `json:"request_key"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_metadata_cleanup", Description: "Read this credential's durable cleanup result by request_key under current cleanup permission. Unknown/unprocessed requests have no receipt. pending means undo already lost but file reclamation incomplete; same-key single cleanup can resume pending only. partial is a durable file failure, not full reclamation.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in key) (*mcp.CallToolResult, enrichment.CleanupResult, error) {
		out, err := service.GetCleanup(ctx, subject, in.RequestKey)
		return nil, out, publicMCPToolError(err, "invalid cleanup key", "cleanup unavailable")
	})

	mcp.AddTool(server, &mcp.Tool{Name: "library_cleanup_metadata_operation", Description: "Permanently discard ONE operation's rollback snapshots. Requires independent cleanup capability, NOT metadata_write. No per-batch human approval. Minimal audit remains; undo becomes unavailable even when files are protected or deletion fails. Never deletes collection files, current covers, or other operations' retained images. Exact request_key replays the durable result; partial file failure is not full reclamation.", Annotations: &mcp.ToolAnnotations{IdempotentHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in enrichment.CleanupInput) (*mcp.CallToolResult, enrichment.CleanupResult, error) {
		ctx, cancel := imageRequestContext(ctx, transportCtx)
		defer cancel()
		out, err := service.Cleanup(ctx, subject, in)
		return nil, out, publicMCPToolError(err, "invalid cleanup request or reused key", "cleanup unavailable")
	})
}
func (c *administrationHTTP) cleanupMetadataOperation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, _ := userFromContext(r.Context())
	var in enrichment.CleanupInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		jsonError(w, 400, "Invalid cleanup request")
		return
	}
	in.OperationID = chi.URLParam(r, "operationId")
	out, err := c.enrichment.Cleanup(r.Context(), delegated.Subject{UserID: user.ID}, in)
	if err != nil {
		credentialError(w, err)
		return
	}
	jsonResponse(w, 200, out)
}

func (c *administrationHTTP) inspectMetadataCleanup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, _ := userFromContext(r.Context())
	out, err := c.enrichment.InspectCleanup(r.Context(), delegated.Subject{UserID: user.ID}, chi.URLParam(r, "cleanupId"))
	if err != nil {
		credentialError(w, err)
		return
	}
	jsonResponse(w, 200, out)
}
