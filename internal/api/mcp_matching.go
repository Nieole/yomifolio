package api

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
)

func registerMCPMatching(server *mcp.Server, service *delegated.Service, subject delegated.Subject) {
	type searchInput struct {
		SearchID string `json:"search_id"`
	}
	type proposalInput struct {
		SeriesID   int64 `json:"series_id"`
		ProposalID int64 `json:"proposal_id"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "library_search_sources", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false}, Description: "Start a bounded multi-source catalogue search for an authorized series. No LLM calls. Results are unconfirmed evidence, never identity probabilities or automatic first-result matches. Page with offset 0-1000 (multiple of limit); limit 1-10. Search evidence expires after 30 minutes or restart."}, func(ctx context.Context, _ *mcp.CallToolRequest, in delegated.SearchSourcesInput) (*mcp.CallToolResult, delegated.StartedSearch, error) {
		out, err := service.SearchSources(ctx, subject, in)
		return nil, out, publicMCPToolError(err, "invalid matching parameters or unconfirmed identity/edition", "source operation unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_source_search", Description: "Read multiple candidate details from a search, with per-source availability and pagination. next_offset is followable with the same limit; truncated=true means the offset bound stops continuation despite more source results, not source exhaustion. Raw covers/links are evidence only, not supported proposal fields. Confidence is source quality, NOT match probability.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, delegated.SearchEvidence, error) {
		out, err := service.GetSourceSearch(ctx, subject, in.SearchID)
		return nil, out, publicMCPToolError(err, "invalid matching parameters or unconfirmed identity/edition", "source operation unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_match_proposal", Description: "Read a proposal and its persisted matching evidence without adjudicating it. Requires series identity; cross-library identifiers cannot bypass current read AND metadata_write authority. Returns current pending/terminal status, accepted flag, remaining fields and original evidence. Legacy proposals without identity/edition evidence remain readable but may only be explicitly rejected, not automatically applied.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *mcp.CallToolRequest, in proposalInput) (*mcp.CallToolResult, delegated.MatchProposal, error) {
		out, err := service.GetMatchProposal(ctx, subject, in.SeriesID, in.ProposalID)
		return nil, out, publicMCPToolError(err, "invalid matching parameters or unconfirmed identity/edition", "source operation unavailable")
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_propose_match", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false}, Description: "Persist a proposal ONLY after explicit identity AND edition confirmation. fields maps each supported field (title, summary, publisher, status, rating, tags, authors) to a returned candidate_id. Include a short verifiable summary, not internal reasoning. No metadata is applied. Uncertain matches must be reported, not proposed. Return distinguishes queued/reused/no_changes/all_fields_locked/rejected_before."}, func(ctx context.Context, _ *mcp.CallToolRequest, in delegated.ConfirmMatchInput) (*mcp.CallToolResult, delegated.MatchProposal, error) {
		out, err := service.ProposeMatch(ctx, subject, in)
		return nil, out, publicMCPToolError(err, "invalid matching parameters or unconfirmed identity/edition", "source operation unavailable")
	})
}
