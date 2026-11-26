package api

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/identification"
)

func registerMCPIdentification(server *mcp.Server, service *identification.Service, subject delegated.Subject) {
	read := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(bool)}
	mcp.AddTool(server, &mcp.Tool{Name: "library_identification_progress", Description: "Count series per identification status in the libraries this credential may read: unprocessed, identified, awaiting_owner (escalated to the owner), needs_recheck, not_found, owner_reviewed (the owner keeps it). prefetch splits unprocessed into ready (evidence prepared) and pending. library_ids is optional, default every library of the credential. Example: {\"library_ids\":[1]}", Annotations: read}, func(ctx context.Context, _ *mcp.CallToolRequest, in identification.ProgressInput) (*mcp.CallToolResult, identification.Progress, error) {
		out, err := service.Progress(ctx, subject, in)
		return nil, out, publicMCPToolError(err, "invalid library_ids; pass at most 256 positive library IDs or omit it", "identification progress unavailable; retry later")
	})
}
