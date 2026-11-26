package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
	"yomifolio/internal/application/identification"
)

// errOwnerOperation tells an agent the receipt it asked to undo records the owner's own change.
var errOwnerOperation = errors.New("this receipt records a change the owner made, which an agent never undoes. Next: if you think it is wrong, tell the owner with library_report_series: " +
	identification.CategoryProtectedValueDoubt + " for a value, " + identification.CategoryDisputedIdentification + " for an identification")

func registerMCPEnrichment(server *mcp.Server, service *enrichment.Service, subject delegated.Subject) {
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
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_metadata_operation", Description: `Read one metadata operation receipt: the fields it changed, before and after values, and whether undo is still possible (undo_unavailable=true once cleaned up). Example: {"operation_id": "0d4f6c2a9b1e47c3a5d8e2f1b6c9a370"}`, Annotations: read}, func(ctx context.Context, _ *mcp.CallToolRequest, in lookup) (*mcp.CallToolResult, enrichment.Operation, error) {
		out, err := service.Get(ctx, subject, in.OperationID)
		return nil, out, publicMCPToolError(err, "invalid operation", "metadata operation unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_list_metadata_operations", Description: `List the metadata operation receipts of one series, newest first. limit 1-50, default 20; pass next_before as before for the next page. Example: {"series_id": 12, "limit": 20}`, Annotations: read}, func(ctx context.Context, _ *mcp.CallToolRequest, in list) (*mcp.CallToolResult, enrichment.Page, error) {
		out, err := service.List(ctx, subject, in.SeriesID, in.Before, in.Limit)
		return nil, out, publicMCPToolError(err, "invalid operation page", "metadata operations unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_undo_metadata_operation", Description: `Reverse the fields one agent's metadata operation actually changed; the owner's own operations are refused. A field edited since, a locked field or a changed author/tag/link set is a conflict; unrelated edits stay. request_key (1-80 printable ASCII) makes a retry return the original outcome. Example: {"request_key": "undo-1", "operation_id": "0d4f6c2a9b1e47c3a5d8e2f1b6c9a370"}`, Annotations: write}, func(ctx context.Context, _ *mcp.CallToolRequest, in enrichment.UndoInput) (*mcp.CallToolResult, enrichment.Operation, error) {
		out, err := service.Undo(ctx, subject, in)
		if errors.Is(err, enrichment.ErrOwnerOperation) {
			return nil, out, errOwnerOperation
		}
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
