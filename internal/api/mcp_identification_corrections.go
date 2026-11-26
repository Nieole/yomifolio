package api

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
	"yomifolio/internal/application/identification"
)

// mcpReplacement is a replacement result with the next step a rejection calls for.
type mcpReplacement struct {
	identification.AgentReplacement
	Message string `json:"message,omitempty"`
}

// protectedValueNext is the next step after a refusal to change a protected value.
var protectedValueNext = "if you are sure it is wrong, leave the owner a note with library_report_series category " + identification.CategoryProtectedValueDoubt + " instead"

// mcpCorrection is a field correction result with the next step a rejection calls for.
type mcpCorrection struct {
	identification.Correction
	Message string `json:"message,omitempty"`
}

// correctionProblems tells an agent what to do about each rejected correction.
var correctionProblems = map[string]string{
	identification.ProblemOwnerHolds:        "the owner holds this series now (status shows why); leave it",
	identification.ProblemNotConcluded:      "the series is not identified with a cited candidate's source, so there is nothing to replace; the identified entries are in identification.entries of its receipts (library_list_metadata_operations)",
	identification.ProblemAlreadyIdentified: "the series is already identified with a cited candidate; cite only entries that replace another",
	identification.ProblemOwnerConfirmed:    "a person confirmed this identification, so you may not change it; if you are sure it is wrong, report it with library_report_series category " + identification.CategoryDisputedOwnerIdentification,
	identification.ProblemExcludedCandidate: verdictProblems[identification.ProblemExcludedCandidate],
	identification.ProblemNotInEvidence:     "a cited candidate is not in this series' evidence; copy ids from its candidates, or add some with library_research_series",
	identification.ProblemRequestKeyReused:  "this request_key already decided something else for the series; use a new request_key",
	identification.ProblemLocked:            "the field is locked, so you may not change it; " + protectedValueNext,
	identification.ProblemNotFromSource:     "the value is not a source value nobody changed: a person edited it, ComicInfo brought it or no source supplied it, so you may not change it; " + protectedValueNext,
	identification.ProblemUnchanged:         "the field already holds this value; nothing to correct",
}

// correctionInputSchema lists the correctable fields as an enum.
func correctionInputSchema() any {
	schema, err := jsonschema.For[identification.CorrectionInput](nil)
	if err != nil {
		return nil
	}
	fields := make([]any, 0, len(enrichment.CorrectableFields))
	for _, f := range enrichment.CorrectableFields {
		fields = append(fields, f)
	}
	schema.Properties["field"].Enum = fields
	return schema
}

// registerMCPIdentificationCorrections binds the agent's corrections of its identifications and of
// source values. They need metadata_write.
func registerMCPIdentificationCorrections(server *mcp.Server, service *identification.Service, subject delegated.Subject) {
	write := &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(bool), OpenWorldHint: new(bool)}
	mcp.AddTool(server, &mcp.Tool{Name: "library_replace_identification", Description: `Fix an agent's wrong identification: each of candidate_ids, from the series' evidence, replaces its source's identification. What the old entry filled, unchanged since, is taken back, then filled from the new one. A person's identification is refused. Undo: library_undo_metadata_operation. Example: {"request_key":"r1","series_id":12,"candidate_ids":["bangumi:202"],"reason":"101 is a sequel"}`, Annotations: write}, func(ctx context.Context, _ *mcp.CallToolRequest, in identification.ReplaceInput) (*mcp.CallToolResult, mcpReplacement, error) {
		out, err := service.Replace(ctx, subject, in)
		if err != nil {
			return nil, mcpReplacement{}, publicMCPToolError(err, "invalid replacement: pass a new request_key, 1-5 candidate_ids as source:entry_id (one per source) and a reason of at most 1000 bytes", "replacement unavailable; retry with the same request_key")
		}
		return nil, mcpReplacement{AgentReplacement: out, Message: correctionProblems[out.Problem]}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_correct_series_field", InputSchema: correctionInputSchema(), Description: `Correct a field a source got wrong: title, summary, publisher, status or rating, if its value came from a source and nobody changed it since. A person's edit, a ComicInfo value or a locked field is refused. Undo the receipt with library_undo_metadata_operation. Example: {"request_key":"c1","series_id":12,"field":"publisher","value":"Kodansha","reason":"the colophon names Kodansha"}`, Annotations: write}, func(ctx context.Context, _ *mcp.CallToolRequest, in identification.CorrectionInput) (*mcp.CallToolResult, mcpCorrection, error) {
		out, err := service.CorrectField(ctx, subject, in)
		if err != nil {
			return nil, mcpCorrection{}, publicMCPToolError(err, "invalid correction: pass a new request_key, a field of title, summary, publisher, status or rating, a value it takes (status ongoing, completed, hiatus or cancelled; rating 0-10 like 8.5; empty clears any field but title) and a reason of at most 1000 bytes", "correction unavailable; retry with the same request_key")
		}
		return nil, mcpCorrection{Correction: out, Message: correctionProblems[out.Problem]}, nil
	})
}
