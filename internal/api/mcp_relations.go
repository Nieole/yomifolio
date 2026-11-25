package api

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
	"yomifolio/internal/organize"
)

type mcpRelationInput struct {
	RequestKey     string `json:"request_key" jsonschema:"new 1-80 char ASCII key; reuse only to retry this exact call"`
	SeriesID       int64  `json:"series_id" jsonschema:"series the relation is added to"`
	TargetSeriesID int64  `json:"target_series_id" jsonschema:"related series"`
	RelationType   string `json:"relation_type" jsonschema:"what target_series_id is to series_id"`
	Reason         string `json:"reason" jsonschema:"short checkable evidence, at most 1000 bytes"`
}

// relationInputSchema lists the existing relation kinds as an enum, so a client can choose one without a
// description long enough to crowd its context. Without it the SDK infers the plain schema.
func relationInputSchema() any {
	schema, err := jsonschema.For[mcpRelationInput](nil)
	if err != nil {
		return nil
	}
	kinds := make([]any, 0, len(organize.RelationTypes))
	for _, kind := range organize.RelationTypes {
		kinds = append(kinds, kind)
	}
	schema.Properties["relation_type"].Enum = kinds
	return schema
}

func registerMCPRelations(server *mcp.Server, service *enrichment.Service, subject delegated.Subject) {
	mcp.AddTool(server, &mcp.Tool{Name: "library_add_series_relation", InputSchema: relationInputSchema(), Description: `Add a franchise relation: target_series_id is series_id's relation_type. Both series must be in your libraries. An existing relation between them is never changed (no-change). Returns a receipt; undo it with library_undo_metadata_operation. Example: {"request_key":"rel-12-34","series_id":12,"target_series_id":34,"relation_type":"sequel","reason":"Vol.1 afterword names it the sequel"}`, Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(bool)}}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpRelationInput) (*mcp.CallToolResult, enrichment.Operation, error) {
		out, err := service.AddRelation(ctx, subject, enrichment.RelationInput(in))
		return nil, out, publicMCPToolError(err, "invalid relation: use two different series ids from your results, a listed relation_type, a reason, and a new request_key", "relation operation unavailable")
	})
}
