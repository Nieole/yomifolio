package api

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
)

func registerDiscoveryTools(server *mcp.Server, service *delegated.Service, subject delegated.Subject, transportCtx context.Context) {
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(bool)}
	mcp.AddTool(server, &mcp.Tool{Name: "library_find_series", Description: `Find readable series. Every given filter applies: library_ids, series_ids (max 50), added_from/added_before (RFC3339, from inclusive), missing_fields (matches if ANY listed field is empty: title, summary, publisher, status, language, rating, authors, tags, source_links). Ascending ID; pass next_after_id as after_id; limit 1-50, default 20. Example: {"missing_fields": ["summary", "tags"]}`, Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, in delegated.FindSeriesInput) (*mcp.CallToolResult, delegated.SeriesPage, error) {
		out, err := service.FindSeries(ctx, subject, in)
		return nil, out, discoveryError(err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_list_books", Description: `List the books of one series, ascending ID, names only (no paths). Pass next_after_id as after_id for the next page; limit 1-50, default 20. Read a book's ComicInfo with library_get_book_clues. Example: {"series_id": 12, "limit": 20}`, Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, in delegated.BooksInput) (*mcp.CallToolResult, delegated.BooksPage, error) {
		out, err := service.ListBooks(ctx, subject, in)
		return nil, out, discoveryError(err)
	})
	type input struct {
		BookID int64 `json:"book_id"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_book_clues", Description: `Read the ComicInfo identity fields of one book (ID from library_list_books). comic_info.status is available, missing, corrupt, unreadable, unavailable or too_large (XML over 64 KiB). No raw XML, paths or downloads. Example: {"book_id": 345}`, Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, delegated.BookClues, error) {
		ctx, cancel := imageRequestContext(ctx, transportCtx)
		defer cancel()
		out, err := service.GetBookClues(ctx, subject, in.BookID)
		return nil, out, discoveryError(err)
	})
}

func discoveryError(err error) error {
	return publicMCPToolError(err, "invalid discovery filters, pagination or oversized result; use supported fields and limit 1..50, or reduce the target/page size", "local catalogue evidence unavailable; retry after current library work completes")
}
