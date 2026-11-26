package api

import (
	"context"
	"errors"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/identification"
)

// tagUsageInputSchema lists the tag orders as an enum.
func tagUsageInputSchema() any {
	schema, err := jsonschema.For[identification.TagUsageInput](nil)
	if err != nil {
		return nil
	}
	schema.Properties["sort"].Enum = []any{identification.TagsByCount, identification.TagsByName}
	return schema
}

// reportInputSchema lists the report categories as an enum.
func reportInputSchema() any {
	schema, err := jsonschema.For[identification.ReportInput](nil)
	if err != nil {
		return nil
	}
	categories := []any{}
	for _, c := range identification.ReportCategories {
		categories = append(categories, c)
	}
	schema.Properties["category"].Enum = categories
	return schema
}

// errNoOwnerIdentification tells an agent a dispute names a series no person identified.
var errNoOwnerIdentification = errors.New("no person identified this series, so there is nothing to dispute; an agent's identification is replaced with library_replace_identification, which needs metadata_write")

// registerMCPInspection binds the read-only inspection primitives. They need only read, so a
// regular account's credential can use them too.
func registerMCPInspection(server *mcp.Server, service *identification.Service, subject delegated.Subject) {
	read := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(bool)}
	mcp.AddTool(server, &mcp.Tool{Name: "library_find_duplicate_series", Description: `Find suspected duplicate series in the libraries this credential may read. Groups of match same_entry (identified as the same source entry) come first, then same_name (equal normalized directory name or title); each lists up to 10 series. Pass next_offset as offset for the next page; limit 1-20, default 10. Report one with library_report_series. Example: {"limit":10}`, Annotations: read}, func(ctx context.Context, _ *mcp.CallToolRequest, in identification.DuplicatesInput) (*mcp.CallToolResult, identification.DuplicatePage, error) {
		out, err := service.Duplicates(ctx, subject, in)
		return nil, out, publicMCPToolError(err, "invalid duplicate search; limit is 1-20, offset at least 0 and library_ids at most 256 positive library IDs", "duplicate search unavailable; retry later")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_count_tags", InputSchema: tagUsageInputSchema(), Description: `Count how many series carry each tag in the libraries this credential may read, with up to 3 sample_series_ids per tag to report on. sort is count (most used first, default) or name (normalized, so spelling variants sit together). Pass next_offset as offset for the next page; limit 1-100, default 50. Example: {"sort":"name","limit":50}`, Annotations: read}, func(ctx context.Context, _ *mcp.CallToolRequest, in identification.TagUsageInput) (*mcp.CallToolResult, identification.TagUsagePage, error) {
		out, err := service.TagUsage(ctx, subject, in)
		return nil, out, publicMCPToolError(err, "invalid tag count; sort is count or name, limit 1-100, offset at least 0 and library_ids at most 256 positive library IDs", "tag count unavailable; retry later")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_report_series", InputSchema: reportInputSchema(), Description: `Report a series to the owner. Notes change nothing: directory_grouping (mixes or splits works), suspected_duplicate (name the other series_id), tag_problem, protected_value_doubt (a value you may not change is wrong); each once per series, even if closed. disputed_owner_identification sends a person's identification to recheck. Example: {"series_id":12,"category":"tag_problem","reason":"bad tags"}`, Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(bool), OpenWorldHint: new(bool)}}, func(ctx context.Context, _ *mcp.CallToolRequest, in identification.ReportInput) (*mcp.CallToolResult, identification.Report, error) {
		out, err := service.Report(ctx, subject, in)
		if errors.Is(err, identification.ErrNoOwnerIdentification) {
			return nil, out, errNoOwnerIdentification
		}
		return nil, out, publicMCPToolError(err, "invalid report; category is directory_grouping, suspected_duplicate, tag_problem, protected_value_doubt or disputed_owner_identification and reason one sentence of at most 1000 bytes; escalating for identification is a verdict of library_submit_verdicts, which needs metadata_write", "report unavailable; retry later")
	})
}
