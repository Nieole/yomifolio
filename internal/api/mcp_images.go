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
	const limits = " JPEG thumbnails only, max 384 pixels per edge (no upscale), max 96 KiB per image and 256 KiB whole result including base64/JSON. No original files, cursor or archive download."
	type coverInput struct {
		SeriesID int64 `json:"series_id"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_series_cover", Description: "Read the current persisted cover of one authorized series." + limits, Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, in coverInput) (*mcp.CallToolResult, any, error) {
		ctx, cancel := imageRequestContext(ctx, transportCtx)
		defer cancel()
		out, err := service.SeriesCover(ctx, subject, in.SeriesID)
		return imageEvidenceResult(out, err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_page_thumbnails", Description: "Read 1..3 distinct user-selected one-based pages of one book belonging to series_id. Invalid pages fail the whole request; no pagination." + limits, Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, in delegated.PageThumbnailsInput) (*mcp.CallToolResult, any, error) {
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
	content := []mcp.Content{&mcp.TextContent{Text: "Image order matches structuredContent.images; JPEG identification evidence only."}}
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
