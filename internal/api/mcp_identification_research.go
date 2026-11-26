package api

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/identification"
)

func registerMCPResearch(server *mcp.Server, service *identification.Service, subject delegated.Subject) {
	mcp.AddTool(server, &mcp.Tool{Name: "library_research_series", Description: `Search the sources again for one series with your own term when its evidence packet has no fitting candidate. New candidates join the packet; earlier ones stay. Waits up to wait_ms (default 20000) and returns the packet and the added IDs. If status is running, call library_wait_run with run_id, then repeat this call. Example: {"series_id":12,"query":"進撃の巨人 諫山創"}`, Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(bool)}}, func(ctx context.Context, _ *mcp.CallToolRequest, in identification.ResearchInput) (*mcp.CallToolResult, identification.ResearchResult, error) {
		out, err := service.Research(ctx, subject, in)
		return nil, out, publicMCPToolError(err, "invalid research: query needs 1-100 characters with a letter or digit and no control characters; wait_ms is 0-30000", "research unavailable; retry later")
	})
}
