package api

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
	"yomifolio/internal/seriesmeta"
)

type mcpAuthorsInput struct {
	RequestKey  string                        `json:"request_key" jsonschema:"new 1-80 char ASCII key; reuse only to retry this exact call"`
	SeriesID    int64                         `json:"series_id"`
	Expected    []seriesmeta.Author           `json:"expected_authors" jsonschema:"the series' whole current author list, name and role each"`
	Corrections []enrichment.AuthorCorrection `json:"corrections" jsonschema:"1-50 replacements: from is a current author, to a credit of an identified source, reason at most 1000 bytes"`
}

type mcpCoverInput struct {
	RequestKey string `json:"request_key" jsonschema:"new 1-80 char ASCII key; reuse only to retry this exact call"`
	SeriesID   int64  `json:"series_id"`
	BookID     int64  `json:"book_id" jsonschema:"a book of this series"`
	Page       int64  `json:"page" jsonschema:"one-based page of that book"`
	Reason     string `json:"reason" jsonschema:"how the page compares with the current cover, at most 1000 bytes"`
}

// registerMCPCorrections adds the two judgment primitives an agent applies on its own: correcting
// author credits and improving an automatic cover. Both return ordinary operation receipts.
func registerMCPCorrections(server *mcp.Server, service *enrichment.Service, subject delegated.Subject, transportCtx context.Context) {
	write := &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(bool), OpenWorldHint: new(bool)}
	mcp.AddTool(server, &mcp.Tool{Name: "library_correct_authors", Description: `Fix one series' authors: from is a current author, to a credit of an identified source; other authors stay. A changed list or lock is a conflict; undo with library_undo_metadata_operation. Example: {"request_key":"au-12","series_id":12,"expected_authors":[{"name":"A","role":"Artist"}],"corrections":[{"from":{"name":"A","role":"Artist"},"to":{"name":"B","role":"Writer"},"reason":"source"}]}`, Annotations: write}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpAuthorsInput) (*mcp.CallToolResult, enrichment.Operation, error) {
		out, err := service.CorrectAuthors(ctx, subject, enrichment.AuthorsInput(in))
		return nil, out, publicMCPToolError(err, "invalid correction: from must be in expected_authors, to must be credited by an identified source of the series, each with a reason; use a new request_key", "author correction unavailable; retry with the same request_key")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_improve_cover", Description: `Replace a series' automatic cover with a page of one of its books, after comparing them (library_get_series_cover, library_get_page_thumbnails). Manual or locked covers are kept: conflict. Undo with library_undo_metadata_operation. Example: {"request_key":"cover-12","series_id":12,"book_id":345,"page":2,"reason":"page 2 is the full cover art; page 1 is an ad"}`, Annotations: write}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpCoverInput) (*mcp.CallToolResult, enrichment.Operation, error) {
		ctx, cancel := imageRequestContext(ctx, transportCtx)
		defer cancel()
		out, err := service.ImproveCover(ctx, subject, enrichment.CoverInput(in))
		return nil, out, publicMCPToolError(err, "invalid cover: book_id must be a book of this series, page within it, and a reason; use a new request_key", "cover operation unavailable; retry with the same request_key")
	})
}
