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
	mcp.AddTool(server, &mcp.Tool{Name: "library_start_scan", Description: `Start a scan of one library and return its run_id at once; follow it with library_wait_run. Disconnecting does not cancel it. Not idempotent: a queued scan may absorb the call (coalesced=true), otherwise each call starts another run. Example: {"library_id": 1}`, Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(bool)}}, func(ctx context.Context, _ *mcp.CallToolRequest, in libraryInput) (*mcp.CallToolResult, delegated.Run, error) {
		out, err := service.StartScan(ctx, subject, in.LibraryID)
		return nil, out, runToolError(err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_get_run", Description: `Read the status and progress of a run this credential started. Example: {"run_id": 42}`, Annotations: readonly}, func(ctx context.Context, _ *mcp.CallToolRequest, in runInput) (*mcp.CallToolResult, delegated.Run, error) {
		out, err := service.GetRun(ctx, subject, in.RunID)
		return nil, out, runToolError(err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_list_runs", Description: `List this credential's runs in one library, newest first. limit 1-100, default 20; pass the last run_id as before_id for the next page. Example: {"library_id": 1, "limit": 20}`, Annotations: readonly}, func(ctx context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, []delegated.Run, error) {
		if in.Limit == 0 {
			in.Limit = 20
		}
		out, err := service.ListRuns(ctx, subject, in.LibraryID, in.BeforeID, in.Limit)
		return nil, out, runToolError(err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_wait_run", Description: `Wait up to timeout_ms (0-30000) for a run this credential started, then return its status. A timeout or disconnect does not cancel the run. Example: {"run_id": 42, "timeout_ms": 30000}`, Annotations: readonly}, func(ctx context.Context, _ *mcp.CallToolRequest, in waitInput) (*mcp.CallToolResult, delegated.Run, error) {
		out, err := service.WaitRun(ctx, subject, in.RunID, in.TimeoutMillis)
		return nil, out, runToolError(err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "library_cancel_run", Description: `Ask a run this credential started to stop at its next safe point. Changes it already committed stay. Example: {"run_id": 42}`, Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(bool)}}, func(ctx context.Context, _ *mcp.CallToolRequest, in runInput) (*mcp.CallToolResult, delegated.Run, error) {
		out, err := service.CancelRun(ctx, subject, in.RunID)
		return nil, out, runToolError(err)
	})
}

func runToolError(err error) error {
	return publicMCPToolError(err, "invalid run parameters; use limit 1..100, nonnegative before_id and timeout_ms 0..30000", "run operation unavailable; retry later")
}
