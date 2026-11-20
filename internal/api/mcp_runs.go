package api

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
)

func registerMCPRuns(server *mcp.Server, service *delegated.Service, subject delegated.Subject) {
	type libraryInput struct {
		LibraryID int64 `json:"library_id"`
	}
	type runInput struct {
		RunID int64 `json:"run_id"`
	}
	type waitInput struct {
		RunID         int64 `json:"run_id"`
		TimeoutMillis int   `json:"timeout_ms" jsonschema:"0 to 30000 milliseconds; timeout does not cancel work"`
	}
	type listInput struct {
		LibraryID int64 `json:"library_id"`
		BeforeID  int64 `json:"before_id,omitempty"`
		Limit     int   `json:"limit,omitempty"`
	}
	readonly := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(bool)}
	mcp.AddTool(server, &mcp.Tool{Name: "library_start_scan", Description: "Accept a library scan and return run_id immediately. Disconnect does not cancel work. Repeated requests are NOT idempotent: a queued scan may coalesce (reported explicitly); otherwise another run is accepted. No paid model calls.", Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(bool)}}, func(ctx context.Context, _ *mcp.CallToolRequest, in libraryInput) (*mcp.CallToolResult, delegated.Run, error) {
		out, err := service.StartScan(ctx, subject, in.LibraryID)
		return nil, out, runToolError(err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_run", Description: "Read a run owned by this credential under current library/capability restrictions.", Annotations: readonly}, func(ctx context.Context, _ *mcp.CallToolRequest, in runInput) (*mcp.CallToolResult, delegated.Run, error) {
		out, err := service.GetRun(ctx, subject, in.RunID)
		return nil, out, runToolError(err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_list_runs", Description: "List this credential's authorized library runs (default 20, max 100), newest run_id first. Pass the last run_id as before_id.", Annotations: readonly}, func(ctx context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, []delegated.Run, error) {
		if in.Limit == 0 {
			in.Limit = 20
		}
		out, err := service.ListRuns(ctx, subject, in.LibraryID, in.BeforeID, in.Limit)
		return nil, out, runToolError(err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_wait_run", Description: "Wait at most 30 seconds for an authorized run; return current status on timeout. Disconnect/timeout does not cancel the run.", Annotations: readonly}, func(ctx context.Context, _ *mcp.CallToolRequest, in waitInput) (*mcp.CallToolResult, delegated.Run, error) {
		out, err := service.WaitRun(ctx, subject, in.RunID, in.TimeoutMillis)
		return nil, out, runToolError(err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_cancel_run", Description: "Request cooperative cancellation of this credential's currently authorized run. Committed changes are not rolled back.", Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(bool)}}, func(ctx context.Context, _ *mcp.CallToolRequest, in runInput) (*mcp.CallToolResult, delegated.Run, error) {
		out, err := service.CancelRun(ctx, subject, in.RunID)
		return nil, out, runToolError(err)
	})
}

func runToolError(err error) error {
	return publicMCPToolError(err, "invalid run parameters; use limit 1..100, nonnegative before_id and timeout_ms 0..30000", "run operation unavailable; retry later")
}
