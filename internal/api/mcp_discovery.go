package api

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
)

func registerDiscoveryTools(server *mcp.Server, service *delegated.Service, subject delegated.Subject, transportCtx context.Context) {
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(bool)}
	mcp.AddTool(server, &mcp.Tool{Name: "library_find_series", Description: "Find authorized series. Filters intersect: library_ids (default credential scope), series_ids (max 50 explicit targets), added_from inclusive / added_before exclusive RFC3339 creation time, missing_fields ALL of title, summary, publisher, status, language, rating, authors, tags, source_links. Ascending stable ID keyset: after_id exclusive; limit default 20, max 50. Not an incremental watermark. Responses limited to 256 KiB.", Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, in delegated.FindSeriesInput) (*mcp.CallToolResult, delegated.SeriesPage, error) {
		out, err := service.FindSeries(ctx, subject, in)
		return nil, out, discoveryError(err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_list_books", Description: "List books in one authorized series, ascending stable ID. after_id exclusive; limit default 20, max 50. Names only, no paths. Use library_get_book_clues with a returned book ID for ComicInfo evidence. Responses limited to 256 KiB.", Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, in delegated.BooksInput) (*mcp.CallToolResult, delegated.BooksPage, error) {
		out, err := service.ListBooks(ctx, subject, in)
		return nil, out, discoveryError(err)
	})
	type input struct {
		BookID int64 `json:"book_id"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_book_clues", Description: "Read local identity clues of one authorized book by stable ID, not by path. ComicInfo status reports available, missing, corrupt, unreadable, unavailable or too_large (64 KiB XML limit). Only modeled identity fields, no raw XML or book download. Response limited to 256 KiB.", Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, delegated.BookClues, error) {
		ctx, cancel := imageRequestContext(ctx, transportCtx)
		defer cancel()
		out, err := service.GetBookClues(ctx, subject, in.BookID)
		return nil, out, discoveryError(err)
	})
}

func discoveryError(err error) error {
	return publicMCPToolError(err, "invalid discovery filters, pagination or oversized result; use supported fields and limit 1..50, or reduce the target/page size", "local catalogue evidence unavailable; retry after current library work completes")
}
