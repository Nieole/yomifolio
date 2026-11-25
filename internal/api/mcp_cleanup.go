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
	mcp.AddTool(server, &mcp.Tool{Name: "library_start_metadata_cleanup", Description: `Start one run that permanently discards the undo snapshots of 1-20 operations in one library, each with its own request_key. Not atomic: read each item with library_get_metadata_cleanup. Disconnecting does not cancel it. Example: {"library_id": 1, "operations": [{"operation_id": "0d4f6c2a9b1e47c3a5d8e2f1b6c9a370", "request_key": "clean-1"}]}`, Annotations: &mcp.ToolAnnotations{}}, func(ctx context.Context, _ *mcp.CallToolRequest, in enrichment.CleanupBatchInput) (*mcp.CallToolResult, delegated.Run, error) {
		out, err := service.StartCleanup(ctx, subject, in)
		return nil, out, publicMCPToolError(err, "invalid cleanup batch", "cleanup unavailable")
	})
	type key struct {
		RequestKey string `json:"request_key"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_metadata_cleanup", Description: `Read this credential's cleanup result by request_key. pending: undo is already gone but file removal is unfinished; repeat the same cleanup to resume. partial: some files could not be removed. Example: {"request_key": "clean-1"}`, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in key) (*mcp.CallToolResult, enrichment.CleanupResult, error) {
		out, err := service.GetCleanup(ctx, subject, in.RequestKey)
		return nil, out, publicMCPToolError(err, "invalid cleanup key", "cleanup unavailable")
	})

	mcp.AddTool(server, &mcp.Tool{Name: "library_cleanup_metadata_operation", Description: `Permanently discard one operation's undo snapshots; undo becomes unavailable and a minimal audit stays. Never deletes library files or current covers. The same request_key replays the stored result. Example: {"request_key": "clean-1", "operation_id": "0d4f6c2a9b1e47c3a5d8e2f1b6c9a370"}`, Annotations: &mcp.ToolAnnotations{IdempotentHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in enrichment.CleanupInput) (*mcp.CallToolResult, enrichment.CleanupResult, error) {
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
