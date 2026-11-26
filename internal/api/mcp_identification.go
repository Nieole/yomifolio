package api

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/identification"
)

// mcpClaim is a claim with the next step its agent should take.
type mcpClaim struct {
	identification.Claim
	Next string `json:"next"`
}

// claimNext names the next step for each kind of claim.
var claimNext = map[string]string{
	"":                                  "Decide every series from its evidence, then answer them all with library_submit_verdicts and this lease_id before lease_expires_at.",
	identification.EmptyAllProcessed:    "Nothing is left to identify in these libraries; stop.",
	identification.EmptyPrefetchPending: "Evidence is still being prepared; retry later, or ask the owner to start the evidence prefetch.",
	identification.EmptyAllClaimed:      "Every ready series is claimed, maybe by you: answer your own batch first, or retry after the leases expire.",
}

// mcpVerdictResult is one verdict result with the next step a rejection calls for.
type mcpVerdictResult struct {
	identification.VerdictResult
	Message string `json:"message,omitempty"`
}

type mcpSubmission struct {
	Results []mcpVerdictResult `json:"results"`
}

// verdictProblems tells an agent what to do about each rejected verdict.
var verdictProblems = map[string]string{
	identification.ProblemInvalid:           "malformed verdict: identify takes 1-5 candidate_ids (one per source) and no category, not_found takes neither, escalate takes one listed category; every verdict needs a reason of at most 1000 bytes",
	identification.ProblemDenied:            errMCPDenied.Error(),
	identification.ProblemNotUnprocessed:    "already decided since your claim (status shows how); skip it",
	identification.ProblemNotClaimed:        "no claim holds this series; answer only series library_claim_series handed you",
	identification.ProblemClaimedByOther:    "this lease_id does not hold the series: another claim does, yours or another session's after this lease expired; use that claim's lease_id or skip it",
	identification.ProblemExcludedCandidate: "the owner ruled out a cited candidate; cite another, answer not_found, or escalate",
	identification.ProblemNotInEvidence:     "a cited candidate is not in this series' evidence; copy ids from its candidates",
	identification.ProblemRequestKeyReused:  "this request_key already answered the series differently; use a new request_key",
	identification.ProblemUnavailable:       "not recorded; retry the same request with the same request_key",
	identification.ProblemNotAttempted:      "not attempted because the request was cancelled; retry with the same request_key",
}

// submitInputSchema lists the verdict kinds and escalation categories as enums.
func submitInputSchema() any {
	schema, err := jsonschema.For[identification.SubmitInput](nil)
	if err != nil {
		return nil
	}
	verdict := schema.Properties["verdicts"].Items
	verdict.Properties["verdict"].Enum = []any{identification.VerdictIdentify, identification.VerdictNotFound, identification.VerdictEscalate}
	categories := make([]any, 0, len(identification.AgentCategories))
	for _, c := range identification.AgentCategories {
		categories = append(categories, c.Category)
	}
	verdict.Properties["category"].Enum = categories
	return schema
}

func registerMCPIdentification(server *mcp.Server, service *identification.Service, subject delegated.Subject) {
	read := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(bool)}
	write := &mcp.ToolAnnotations{DestructiveHint: new(bool), OpenWorldHint: new(bool)}
	mcp.AddTool(server, &mcp.Tool{Name: "library_identification_progress", Description: "Count series per identification status in the libraries this credential may read: unprocessed, identified, awaiting_owner (escalated to the owner), needs_recheck, not_found, owner_reviewed (the owner keeps it). prefetch splits unprocessed into ready (evidence prepared) and pending. library_ids is optional, default every library of the credential. Example: {\"library_ids\":[1]}", Annotations: read}, func(ctx context.Context, _ *mcp.CallToolRequest, in identification.ProgressInput) (*mcp.CallToolResult, identification.Progress, error) {
		out, err := service.Progress(ctx, subject, in)
		return nil, out, publicMCPToolError(err, "invalid library_ids; pass at most 256 positive library IDs or omit it", "identification progress unavailable; retry later")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_claim_series", Description: `Claim up to 20 unprocessed series to identify, each with its evidence packet: local clues and source candidates with match signals. You hold them for 30 minutes; answer each with library_submit_verdicts. An empty batch has empty_reason: all_processed (stop), prefetch_pending or all_claimed (retry later). library_ids is optional. Example: {"limit":20}`, Annotations: write}, func(ctx context.Context, _ *mcp.CallToolRequest, in identification.ClaimInput) (*mcp.CallToolResult, mcpClaim, error) {
		out, err := service.Claim(ctx, subject, in)
		if err != nil {
			return nil, mcpClaim{}, publicMCPToolError(err, "invalid claim; limit is 1-20 and library_ids at most 256 positive library IDs", "claim unavailable; retry later")
		}
		return nil, mcpClaim{Claim: out, Next: claimNext[out.EmptyReason]}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_submit_verdicts", InputSchema: submitInputSchema(), Description: `Answer claimed series, one verdict each with a reason: identify (candidate_ids from its evidence, one per source), not_found, or escalate (category). Results are per series; rejections say what to do. Retrying a request_key replays it. Example: {"request_key":"v1","lease_id":"5e1f0c9a","verdicts":[{"series_id":12,"verdict":"identify","candidate_ids":["bangumi:101"],"reason":"title matches"}]}`, Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(bool), OpenWorldHint: new(bool)}}, func(ctx context.Context, _ *mcp.CallToolRequest, in identification.SubmitInput) (*mcp.CallToolResult, mcpSubmission, error) {
		out, err := service.Submit(ctx, subject, in)
		if err != nil {
			return nil, mcpSubmission{}, publicMCPToolError(err, "invalid submission: pass a new request_key, the lease_id of your claim and 1-20 verdicts with distinct series_id", "verdicts unavailable; retry with the same request_key")
		}
		results := make([]mcpVerdictResult, len(out.Results))
		for i, r := range out.Results {
			results[i] = mcpVerdictResult{VerdictResult: r, Message: verdictProblems[r.Problem]}
		}
		return nil, mcpSubmission{Results: results}, nil
	})
}
