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

- Original 06 implementation proved server lifetime and HTTP reconnect only. Subsequent real stdio joint acceptance is recorded below; the parallel 03 concurrent-EOF repair remains outside this delta.
- Shared files can conflict with 03/04: api/mcp.go, delegated/service.go, bootstrap/bootstrap.go/tests, specs and CHANGELOG. Most new behavior is in separate named files.
- Future 07/08/12/13 must call the exported fences for their actual capability/library and implement their own transactional content ports. No future write/model functionality was added here.
- Suggested parent commit subject: `feat: add delegated scans and revocation-aware run control`.

## Integrated joint gate

On integrated `a0245ff5`, `GOTOOLCHAIN=go1.26.10 go test -p 1 ./... -count=1 -timeout=10m` failed in `internal/api.TestRelocationDuplicateConfirmationAndDatabaseFailureAreDurable`: `rename ... source ... target: Access is denied.` Bootstrap (including the real stdio joint lifecycle) and all other packages passed. This is not a successful full gate and is not claimed to be a baseline failure. Retained log: ticket-06 worktree `.tmp/stdio-joint-integrated-full.log`. The forthcoming combined 04 integration gate must include this joint test before closing tickets 03/06.

## 03/06 fresh-context joint acceptance

### Scope and observable result

- Worktree `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-06`, branch `mcp-delegated-runs`, initially clean HEAD/merge-base `02ec9f453a6fb28535b30abb52e37d6fedf366c4`, containing 03 and 06. Only tests/evidence changed; no stage/commit/merge. Parent integration advanced during verification to `0ab7dbb810e344fc0efd7632d12ecc11c432590c`; parent must integrate/review against that tip, not assume it was tested here.
- New symbol: `bootstrap_test.TestMCPStdioAcceptedScanSurvivesEOFAndExplicitCancel` in `internal/bootstrap/mcp_stdio_runs_test.go`; archive helper `writeStdioRunArchive`. Existing 03 test files were not edited.
- Actual compiled `cmd/mcp-proxy` subprocess, official Go SDK v1.8.0 generic `CommandTransport`, protocol 2025-11-25, production `bootstrap.Open`, real temporary SQLite and PNG-in-CBZ. Helper cwd is an unrelated empty directory. Server HTTP is the helper's real upstream, not a replacement for stdio. No external metadata/model source is contacted.
- First scan is accepted via stdio with nonzero run ID while dispatch is held at `Options.Tasks.RunBackground`. SDK session close sends EOF and reaps a successful helper. Only then is dispatch released. With no helper/client connected the server completes ingestion; public Engine.Await synchronizes completion, the real book exists, and a new helper queries the identical run/library ID with completed status.
- Second scan is controlled by public Engine.PauseAll, receives a distinct ID via stdio, and stays queued through denied get/wait/cancel attempts from another-library and same-library/different-credential helpers. Authorized `library_cancel_run` is sent via stdio; resume/Await observes cancelled. A further fresh helper queries both original IDs: completed versus cancelled. EOF is not business cancellation.
- All waits context-bounded (90s scenario, outer Go test timeouts); channel/public-engine barriers and Await, no sleep synchronization. SDK close checks successful process exit/reap and secret-free diagnostics.

### Actual verification

Environment: `GOTOOLCHAIN=go1.26.10`; shared `GOCACHE=C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-01/.tmp/merger01/gocache126`; own `GOTMPDIR=.../mcp-library-06/.tmp/joint`, `GOLANGCI_LINT_CACHE=.../mcp-library-06/.tmp/lint-joint`. Ignored web/dist was present. Logs are ignored local `.tmp/joint/` files.

- Acceptance supplementation, not fabricated production red/green. First harness attempt timed out: one channel permit released an unrelated background worker rather than the scanner. Corrected harness broadcasts dispatch release after EOF and uses public PauseAll for the second controlled run. Production code unchanged. `first.log`: FAIL (90s); `second.log`: PASS (12.839s).
- `go test -p 1 ./internal/bootstrap ./internal/application/delegated ./cmd/mcp-proxy -run 'MCP|Delegated|Proxy|Origin|Authorize' -count=3 -timeout=8m`: PASS; bootstrap 118.724s, including existing real helper/process, transport safety, account lifecycle, delegated checkpoints/recovery and new joint test. Delegated package had no matching tests in this first filter; cmd has no test files. Log `targeted.log`.
- `go test -p 1 ./internal/application/delegated ./internal/config ./internal/api -run 'TestCurrentAuthority|MCP|Delegated' -count=3 -timeout=8m`: PASS; delegated capability-intersection and config unsafe-plaintext tests ran. API had no matching tests (its full suite ran below). Log `security.log`.
- `go test -p 1 ./internal/bootstrap -run '^TestMCPStdioAcceptedScanSurvivesEOFAndExplicitCancel$' -count=5 -timeout=5m -v`: PASS all five (18.108s), each reporting completed offline scan and explicit-cancel terminal status across fresh helpers. Log `joint-repeat.log`.
- `go test -p 1 ./... -count=1 -timeout=10m`: FAIL solely in `internal/application/diskjobs.TestWriteSeriesComicInfoUsesPersistedMetadata`, Windows archive rename `Access is denied`. Other packages passed, including API (483.659s), bootstrap (11.509s), delegated, runtime, scanning, task and taskstore. Log `backend.log`. No controlled baseline comparison, so not labeled baseline failure.
- Exact failure rerun: `go test -p 1 ./internal/application/diskjobs -run '^TestWriteSeriesComicInfoUsesPersistedMetadata$' -count=3 -timeout=3m`: PASS all three (1.570s), log `diskjobs-rerun.log`. Complete suite not rerun; no all-green full backend gate claimed for this delta.
- `gofmt -l ./cmd ./internal`: empty (`format.log`). `golangci-lint run`: `0 issues.` (`lint.log`). `git diff --check`: PASS.
- No frontend, SQL, generated contracts, dependencies or public behavior changed; corresponding build/generation gates not applicable.

### Remaining risks / integration ownership

Both joint lifecycle checkboxes checked; statuses remain ready-for-agent until parent commit/review. This delta does not repair or establish safety for 03's separately reported concurrent transport EOF race; dedicated 03 repair agent owns it. It proves accepted-before-EOF lifecycle, not in-flight admission/response races, forcible kill, server restart auto-resume or cancellation of already-issued external calls. Explicit cancellation here is queued cancellation; running safe-checkpoint revocation remains covered by existing delegated tests. No substantive product defect found or repaired here. Parent must reconcile its advanced integration tip and independent 03 repair, and may rerun the complete backend gate after integration.
