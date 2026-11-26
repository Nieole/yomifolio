package api

import (
	"context"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"yomifolio/internal/application/delegated"
)

// listingCapability names the grant a tool needs to appear in a listing. Run reads follow the
// credential's own work, so they need some granted capability rather than one particular grant.
var listingCapability = map[string]string{
	"library_get_series":               delegated.Read,
	"library_find_series":              delegated.Read,
	"library_list_books":               delegated.Read,
	"library_get_book_clues":           delegated.Read,
	"library_get_enrichment_state":     delegated.Read,
	"library_get_metadata_operation":   delegated.Read,
	"library_list_metadata_operations": delegated.Read,
	"library_get_series_cover":         delegated.Read,
	"library_get_page_thumbnails":      delegated.Read,
	"library_search_sources":           delegated.Read,
	"library_get_source_search":        delegated.Read,
	"library_get_match_proposal":       delegated.Read,
	"library_identification_progress":  delegated.Read,

	"library_propose_match":           delegated.MetadataWrite,
	"library_enrich_metadata":         delegated.MetadataWrite,
	"library_enrich_metadata_batch":   delegated.MetadataWrite,
	"library_undo_metadata_operation": delegated.MetadataWrite,
	"library_add_series_relation":     delegated.MetadataWrite,
	"library_correct_authors":         delegated.MetadataWrite,
	"library_improve_cover":           delegated.MetadataWrite,
	"library_claim_series":            delegated.MetadataWrite,
	"library_submit_verdicts":         delegated.MetadataWrite,

	"library_start_scan": delegated.ScanControl,

	"library_assess_match":        delegated.InternalLLM,
	"library_get_model_operation": delegated.InternalLLM,

	"library_start_metadata_cleanup":     delegated.Cleanup,
	"library_get_metadata_cleanup":       delegated.Cleanup,
	"library_cleanup_metadata_operation": delegated.Cleanup,

	"library_get_run":    "",
	"library_wait_run":   "",
	"library_cancel_run": "",
	"library_list_runs":  "",
}

// grantedTools is the capability set a credential currently holds, used only to decide which
// tools a listing may show.
type grantedTools map[string]bool

func newGrantedTools(capabilities []string) grantedTools {
	tools := make(grantedTools, len(capabilities))
	for _, capability := range capabilities {
		tools[capability] = true
	}
	return tools
}

// allows reports whether a listing for these grants may name the tool.
func (t grantedTools) allows(name string) bool {
	capability, known := listingCapability[name]
	if !known {
		return true
	}
	if capability == "" {
		return len(t) > 0
	}
	return t[capability]
}

// listOnlyGranted hides the tools a credential does not currently hold. Every handler still
// authorizes its own operation, so the listing discloses grants without becoming the boundary.
func listOnlyGranted(server *mcp.Server, tools grantedTools) {
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, request)
			if method != "tools/list" || err != nil {
				return result, err
			}
			listed, ok := result.(*mcp.ListToolsResult)
			if !ok {
				return result, err
			}
			filtered := *listed
			filtered.Tools = slices.DeleteFunc(slices.Clone(listed.Tools), func(tool *mcp.Tool) bool { return !tools.allows(tool.Name) })
			return &filtered, nil
		}
	})
}
