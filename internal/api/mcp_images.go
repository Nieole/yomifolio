package api

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/bookcontent"
)

func registerMCPImages(server *mcp.Server, service *delegated.Service, subject delegated.Subject, transportCtx context.Context) {
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(bool)}
	type coverInput struct {
		SeriesID int64 `json:"series_id"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_series_cover", Description: `Read the current cover of one series as a JPEG thumbnail (max 384 px per edge, 96 KiB). The text block is the JSON result; the image follows it. Example: {"series_id": 12}`, Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, in coverInput) (*mcp.CallToolResult, any, error) {
		ctx, cancel := imageRequestContext(ctx, transportCtx)
		defer cancel()
		out, err := service.SeriesCover(ctx, subject, in.SeriesID)
		return imageEvidenceResult(out, err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_page_thumbnails", Description: `Read 1-3 distinct one-based pages of one book in the series as JPEG thumbnails (max 384 px per edge, 96 KiB each, 256 KiB per result). One invalid page fails the call. The text block is the JSON result; images follow in the order of its images list. Example: {"series_id": 12, "book_id": 345, "pages": [1, 2]}`, Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, in delegated.PageThumbnailsInput) (*mcp.CallToolResult, any, error) {
		ctx, cancel := imageRequestContext(ctx, transportCtx)
		defer cancel()
		out, err := service.PageThumbnails(ctx, subject, in)
		return imageEvidenceResult(out, err)
	})
}
func imageEvidenceResult(out delegated.ImageEvidence, err error) (*mcp.CallToolResult, any, error) {
	if errors.Is(err, bookcontent.ErrPageNotFound) {
		return nil, nil, errors.New("page outside current book; pages are one-based")
	}
	if errors.Is(err, bookcontent.ErrEvidenceLimit) {
		return nil, nil, errors.New("image exceeds safe input or thumbnail budget")
	}
	if err != nil {
		return nil, nil, publicMCPToolError(err, "expected 1..3 distinct positive pages; image result exceeds 256 KiB budget", "image evidence unavailable or unsafe; no original-file fallback")
	}
	// The text block repeats structuredContent as JSON for text-only clients; its images list
	// metadata in the order of the image blocks that follow.
	text, err := json.Marshal(out)
	if err != nil {
		return nil, nil, errors.New("image evidence unavailable or unsafe; no original-file fallback")
	}
	content := []mcp.Content{&mcp.TextContent{Text: string(text)}}
	for _, thumb := range out.Images {
		content = append(content, &mcp.ImageContent{Data: thumb.Data, MIMEType: "image/jpeg"})
	}
	result := &mcp.CallToolResult{Content: content, StructuredContent: out}
	raw, err := json.Marshal(result)
	if err != nil || len(raw)+1024 > delegated.MaxImageResultBytes {
		return nil, nil, errors.New("image result exceeds 256 KiB budget; select fewer pages")
	}
	return result, nil, nil
}

// SDK stateless sessions do not themselves bind tool lifetime to the HTTP body.
// Synchronous evidence and cover enrichment must release admission even while
// waiting on IO after a disconnect. Keep this context through the commit boundary.
func imageRequestContext(tool, transport context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(tool)
	stop := context.AfterFunc(transport, cancel)
	return ctx, func() { stop(); cancel() }
}
