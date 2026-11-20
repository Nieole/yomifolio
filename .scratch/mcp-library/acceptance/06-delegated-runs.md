# 06 — delegated runs and continuous revocation

Worktree: `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-06`; branch `mcp-delegated-runs`; starting HEAD and merge-base both `1c96d2fe08616e4d1ebe52a46ee7478fb0e8c1d7`. All changes remain uncommitted. Integration advanced during verification to `34eb50dd836b49844b1b4578c4437ed0b43212d4`; parent must integrate safely, not assume this tree includes that tip.

## Implemented boundaries

- Five MCP tools: library_start_scan/get_run/list_runs/wait_run/cancel_run, using delegated application use cases and the existing Runtime Engine/Scans.
- Required user/credential/library/capability attribution is committed atomically with a run, using existing run_args storage. Credential-specific task variants prevent coalescing with web jobs or another credential. Subject contains IDs only.
- Admission rechecks authority; queued dispatch checks authority before StartedAt/body; execution checks before body and through taskcontrol.Wait. Revoke/narrow/password invalidation uses current durable authority, without a new polling agent or scheduler.
- Reusable PrepareRun, Checkpoint, and AuthorizeTx boundaries. AuthorizeTx reads authority inside the actual write transaction; callers must abort on SQLite conflicts and reauthorize in a new transaction. External calls remain non-transactional; already issued requests/costs cannot be recalled.
- Bounded projections exclude paths, raw args and technical errors. Direct access and list membership are private to the credential and its current library/capability. Waits are capped at 30s and do not cancel work on timeout/disconnect.
- run_id identifies execution, not a series operation_id or request-idempotency token. Repeated scan requests may create another run; only an existing queued same-credential task coalesces, explicitly reported. Delegated variants have no retry/resume registry entry, so restart marks history interrupted without autonomous replay.
- Delegated preparation scans ingest real archives without starting independent cover/hash follow-ups; otherwise those jobs would lose delegated attribution/authority. Ordinary scans retain their current follow-ups. Candidate writes, paid models, budgets and undo remain future tickets.

## Test evidence

Agreed seams: production bootstrap, real SQLite/files/archives, generic official MCP Go client, actual shared run engine, controllable scheduling/execution barriers. No second application runtime or fake business service.

- RED: `go test ./internal/bootstrap -run TestDelegatedRunRechecksAtExecution -count=1` failed with missing Service.PrepareRun before implementation; then passed after implementation.
- `TestMCPScanDisconnectQueueRevocationAndScope`: real PNG-in-CBZ ingestion; accepted run queried after MCP disconnect/reconnect; cross-library get/wait/cancel/list denied; successful authorized cancellation; 0ms bounded wait; stable paginated list; explicit queued coalescing; capability removal cancels queued scan without StartedAt; durable attribution observed through engine projection; no unowned follow-up jobs.
- `TestDelegatedRunRechecksAtExecution/{revoke,narrow,password}`: real production engine and controlled in-body barrier; committed content remains committed, next checkpoint cancels; the same transaction-bound authority API rejects later writes. Password case uses real authenticated HTTP password change. This is a reusable application-run checkpoint test, not a claim that an external network request was recalled.
- `TestOpenDoesNotReplayDelegatedRun`: simulated lost scheduler after durable admission, real database close/reopen and production bootstrap; credential still authenticates, same run is queryable as interrupted, history lists exactly one original run, no retry/resume registration.
- Existing MCP lifecycle/account tests now assert six discovered tools and explicitly deny regular-user scan elevation rather than assuming discovery itself is authorization.

## Gates

Environment: GOTOOLCHAIN=go1.26.10; shared GOCACHE at mcp-library-01/.tmp/merger01/gocache126; this tree's own GOTMPDIR=.tmp/go and GOLANGCI_LINT_CACHE=.tmp/lint. Ignored web/dist copied from integration tree, no frontend changes.

- Focused bootstrap MCP/delegated/recovery suite: PASS.
- First `go test -p 1 ./... -count=1 -timeout=10m`: FAIL in API TestRelocationCombinedLegacyRepairProtectionAndPolicyRecovery/runtime_failure=false (503 library_unavailable) and TestRestorationConsentExpiresWithRelevantFacts/interval (409 plan_not_ready). All other packages, including bootstrap, passed. No baseline attribution is claimed.
- Exact two failing relocation tests rerun with `-count=1`: PASS (5.207s).
- Full `go test -p 1 ./... -count=1 -timeout=10m` rerun: PASS across all packages (API 508.763s; bootstrap 28.709s). Logs: ignored `.tmp/backend-gate.log`, `.tmp/focused-relocation.log`, `.tmp/backend-gate-rerun.log`.
- `gofmt -l ./cmd ./internal`: empty.
- `golangci-lint run`: `0 issues.` (also rerun after final code changes).
- `git diff --check`: PASS.
- Frontend not touched; frontend gates and sqlc/tsgen regeneration not applicable. No dependency changes.

## Remaining integration / scope

- 03 owns real stdio helper/process smoke tests. This ticket proves shared server-side lifetime and HTTP reconnect; it does not claim to have run the parallel helper.
- Shared files can conflict with 03/04: api/mcp.go, delegated/service.go, bootstrap/bootstrap.go/tests, specs and CHANGELOG. Most new behavior is in separate named files.
- Future 07/08/12/13 must call the exported fences for their actual capability/library and implement their own transactional content ports. No future write/model functionality was added here.
- Suggested parent commit subject: `feat: add delegated scans and revocation-aware run control`.
