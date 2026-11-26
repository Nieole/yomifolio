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

// reportInputSchema lists the report categories the credential can use as an enum: the dispute
// only with metadata_write.
func reportInputSchema(write bool) any {
	schema, err := jsonschema.For[identification.ReportInput](nil)
	if err != nil {
		return nil
	}
	offered := identification.NoteCategories
	if write {
		offered = identification.ReportCategories
	}
	categories := []any{}
	for _, c := range offered {
		categories = append(categories, c)
	}
	schema.Properties["category"].Enum = categories
	return schema
}

var (
	// errNothingToDispute tells an agent a dispute names a series no source identifies.
	errNothingToDispute = errors.New("no source identifies this series, so there is no identification to dispute; an unprocessed series is answered with library_claim_series and library_submit_verdicts")
	// errDisputeNeedsWrite tells an agent that may only read why its dispute was refused.
	errDisputeNeedsWrite = errors.New(identification.CategoryDisputedIdentification + " sends the series to the owner's recheck, which needs metadata_write; this credential may leave notes only, such as " + identification.CategoryProtectedValueDoubt)
)

// registerMCPInspection binds the read-only inspection primitives and the report. They need only
// read, so a regular account's credential can use them too; a dispute in a report needs
// metadata_write as well, and write says whether the credential holds it.
func registerMCPInspection(server *mcp.Server, service *identification.Service, subject delegated.Subject, write bool) {
	read := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(bool)}
	mcp.AddTool(server, &mcp.Tool{Name: "library_find_duplicate_series", Description: `Find suspected duplicate series in the libraries this credential may read. Groups of match same_entry (identified as the same source entry) come first, then same_name (equal normalized directory name or title); each lists up to 10 series. Pass next_offset as offset for the next page; limit 1-20, default 10. Report one with library_report_series. Example: {"limit":10}`, Annotations: read}, func(ctx context.Context, _ *mcp.CallToolRequest, in identification.DuplicatesInput) (*mcp.CallToolResult, identification.DuplicatePage, error) {
		out, err := service.Duplicates(ctx, subject, in)
		return nil, out, publicMCPToolError(err, "invalid duplicate search; limit is 1-20, offset at least 0 and library_ids at most 256 positive library IDs", "duplicate search unavailable; retry later")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_count_tags", InputSchema: tagUsageInputSchema(), Description: `Count how many series carry each tag in the libraries this credential may read, with up to 3 sample_series_ids per tag to report on. sort is count (most used first, default) or name (normalized, so spelling variants sit together). Pass next_offset as offset for the next page; limit 1-100, default 50. Example: {"sort":"name","limit":50}`, Annotations: read}, func(ctx context.Context, _ *mcp.CallToolRequest, in identification.TagUsageInput) (*mcp.CallToolResult, identification.TagUsagePage, error) {
		out, err := service.TagUsage(ctx, subject, in)
		return nil, out, publicMCPToolError(err, "invalid tag count; sort is count or name, limit 1-100, offset at least 0 and library_ids at most 256 positive library IDs", "tag count unavailable; retry later")
	})
	description := `Report a series to the owner. Notes change nothing: directory_grouping (mixes or splits works), suspected_duplicate (name the other series_id), tag_problem, protected_value_doubt (a value you may not change is wrong); each once per series, even if closed. Example: {"series_id":12,"category":"tag_problem","reason":"bad tags"}`
	categories := "directory_grouping, suspected_duplicate, tag_problem or protected_value_doubt"
	if write {
		description = `Report a series to the owner. Notes change nothing: directory_grouping (mixes or splits works), suspected_duplicate (name the other series_id), tag_problem, protected_value_doubt (a value you may not change is wrong); each once per series, even if closed. disputed_identification sends any identification, yours too, to recheck. Example: {"series_id":12,"category":"tag_problem","reason":"bad tags"}`
		categories = "directory_grouping, suspected_duplicate, tag_problem, protected_value_doubt or disputed_identification"
	}
	mcp.AddTool(server, &mcp.Tool{Name: "library_report_series", InputSchema: reportInputSchema(write), Description: description, Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(bool), OpenWorldHint: new(bool)}}, func(ctx context.Context, _ *mcp.CallToolRequest, in identification.ReportInput) (*mcp.CallToolResult, identification.Report, error) {
		out, err := service.Report(ctx, subject, in)
		switch {
		case errors.Is(err, identification.ErrNothingToDispute):
			return nil, out, errNothingToDispute
		case errors.Is(err, identification.ErrDisputeNeedsWrite):
			return nil, out, errDisputeNeedsWrite
		}
		return nil, out, publicMCPToolError(err, "invalid report; category is "+categories+" and reason one sentence of at most 1000 bytes; escalating for identification is a verdict of library_submit_verdicts, which needs metadata_write", "report unavailable; retry later")
	})
}
