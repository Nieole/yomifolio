package api

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/identification"
)

func registerMCPIdentificationImages(server *mcp.Server, service *identification.Service, subject delegated.Subject, transportCtx context.Context) {
	// Candidate covers come from the sources' own image hosts, so the tool reaches outside.
	openWorld := true
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &openWorld}
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_identification_images", Description: `Compare one series with similar candidates by eye: its cover, 1-3 pages of one of its books, and up to 3 evidence candidates' covers, which the server downloads from the source. JPEG thumbnails, max 384 px. Each requested image gets a status; only included ones follow the JSON text block, in list order. Example: {"series_id": 12, "cover": true, "candidate_ids": ["bangumi:101"]}`, Annotations: annotations}, func(ctx context.Context, _ *mcp.CallToolRequest, in identification.ImagesInput) (*mcp.CallToolResult, any, error) {
		ctx, cancel := imageRequestContext(ctx, transportCtx)
		defer cancel()
		out, err := service.Images(ctx, subject, in)
		if err != nil {
			return nil, nil, publicMCPToolError(err, "ask for at least one image: cover, 1-3 distinct positive pages with book_id, or up to 3 distinct candidate_ids", "identification images unavailable; retry later")
		}
		text, err := json.Marshal(out)
		if err != nil {
			return nil, nil, errors.New("identification images unavailable; retry later")
		}
		content := []mcp.Content{&mcp.TextContent{Text: string(text)}}
		for _, image := range out.Images {
			if image.Status == identification.ImageIncluded {
				content = append(content, &mcp.ImageContent{Data: image.Data, MIMEType: "image/jpeg"})
			}
		}
		result := &mcp.CallToolResult{Content: content, StructuredContent: out}
		raw, err := json.Marshal(result)
		if err != nil || len(raw)+1024 > delegated.MaxImageResultBytes {
			return nil, nil, errors.New("image result exceeds 256 KiB budget; ask for fewer images")
		}
		return result, nil, nil
	})
}
