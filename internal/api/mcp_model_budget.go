package api

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
)

func registerMCPModelBudgets(server *mcp.Server, service *delegated.Service, subject delegated.Subject) {
	mcp.AddTool(server, &mcp.Tool{Name: "library_assess_match", Description: "Start optional internal-model identity/edition assistance for a completed source search. Requires current management permission AND independent internal_llm capability AND an explicit user-relayed allowance (1-50). This is delegated instruction, NOT independently verified human approval. One immutable operation per search: repeat returns it, changed allowance is rejected, no retries/refunds/restart replay. Each admitted request consumes one even on failure. Results are advisory labels, never ratings, confirmation, proposals or metadata writes. Unprocessed candidates remain available for non-model work.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false}}, func(ctx context.Context, _ *mcp.CallToolRequest, in delegated.AssessMatchInput) (*mcp.CallToolResult, delegated.ModelOperation, error) {
		out, err := service.AssessMatch(ctx, subject, in)
		return discoveryResult(), out, publicMCPToolError(err, "explicit immutable allowance 1-50 and completed search required", "model operation unavailable")
	})
	type input struct {
		OperationID string `json:"operation_id"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_model_operation", Description: "Read owning credential's durable model-budget operation after reconnect/restart. used counts conservative admissions, NOT money or proof of approval. unknown means an admitted call has no safely stored outcome. remaining does not permit retries. No automatic recovery reissues. status=history_unavailable means run history is absent, not a known run outcome; durable allowance, usage and item outcomes remain available. Query/cancel run_id using run tools only while history exists.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, delegated.ModelOperation, error) {
		out, err := service.GetModelOperation(ctx, subject, in.OperationID)
		return discoveryResult(), out, publicMCPToolError(err, "invalid model operation", "model operation unavailable")
	})
}
