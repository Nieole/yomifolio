# Final whole-branch two-axis remediation

## Scope / provenance

Only `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-final-fixes`, branch
`mcp-run-discovery-fix`; starting HEAD `a10e5fc17cc25beca9322156f8139f3cb9f9c74e`,
clean. `git merge-base --is-ancestor a10e5fc1 HEAD` exit0. No stage, commit,
merge, reset, parent TODO or other-tree changes. Read AGENTS, worker guide,
TDD/MCP-builder skills, effort spec, acceptance04/05/06/12/13/14 and relevant
owning contracts. The already approved individual tickets are not reopened or
claimed to be the historical root cause. Parent owns final integration,
canonical review and both review axes.

Agreed seams retained: production bootstrap, real SQLite/archives/images,
public HTTP sockets, compiled stdio proxy, generic SDK, existing external
source/AI factories only. No second runtime, new feature, paid call or frontend
change. ADR0016 remains draft.

## Finding 1 — both axes: admission recovery misses suffixed variants

Citation: `delegated.ListRuns` constructed the credential namespace but passed
`RunFilter.Variant`; `taskstore.identityClause` correctly interpreted Variant
as exact equality. `PrepareRun` preserves operation suffixes, so search/model/
cleanup were not discoverable after losing the admission response.

Fix:
- `task.RunFilter.VariantPrefix` is independent of exact Variant.
- `taskstore.identityClause` binds both arguments to a literal, case-sensitive
  `substr(...)=?` predicate before SQL LIMIT; `%`, `_` and quotes are data.
  Credential prefix includes its trailing colon, preventing prefix collisions.
- `delegated.ListRuns` retains scope, ownership and current declared-capability
  checks, advancing the internal before-ID cursor through invisible capability
  pages until the public page is filled or history ends. Storage/auth errors
  are not silently interpreted as an empty successful page.

Real oracle: `TestMCPIntegratedAcceptance` now runs HTTP and compiled stdio
with independent final read/scan_control/cleanup narrowing fixtures. It records
actual scan, source-search, model and async-cleanup admissions, reconnects and
asserts limit1 and limit2 complete descending pages, exact IDs, no duplicates
or omissions. A genuine same-user/same-library sibling search is invisible;
other-library list is rejected. Current narrowing hides/rejects each run whose
original capability is no longer held, while older authorized runs remain
reachable (including cleanup without read). The cleanup final fixture retains
metadata_write so the original no-read adjudication replay guard is preserved.
`TestRunVariantPrefixIsLiteralAndExactRemainsExact` tests real SQLite exact
semantics, quoted/wildcard prefix binding and credential-prefix collision
rejection with before-ID pagination.

TDD: `list-red.log` has the genuine public failure `search/model admission
recovery: []`; `list-green.log` passes after prefix repair. The initial missing
Go import was corrected before that behavioral red, not claimed as its oracle.
`matrix-first.log` exposed a test-authoring attempt to widen a narrowed grant;
fixtures now narrow only monotonically and never restore permissions.

## Finding 2 — spec axis: ComicInfo RAR work ignores request lifecycle/budgets

Citations: `parser.ReadMetadataLimited` previously took the ordinary mutex,
opened the dependency with default options and skipped predecessors by calling
Next; `bookcontent.readComicInfo` did not accept context. Additionally, public
socket/EOF tests proved discovery tools had not merged the stateless SDK tool
context with actual transport cancellation (`raw-cancel-green.log` is a failed
trial, despite its original filename). This was fixed at the owning adapter,
not by treating a disconnected client as business-run cancellation.

Fix:
- `registerDiscoveryTools` supplies transport context to ComicInfo via the same
  cancellation bridge used for synchronous image/cover evidence.
- `ComicInfo` carries context through reading and returns cancellation as an
  error after releasing IO and library admission (not a false-success clue).
- `ReadMetadataLimited(ctx, ...)` uses cancellable RAR session acquisition,
  `boundedRar` (16 MiB dictionary and context-bound volumes), explicit predecessor
  draining, declared/actual XML limits and existing finite error mapping.
- One `boundedRarDrain` accounts cumulative predecessor bytes (16 MiB) for
  ComicInfo, page directory and selected-page readers. It preserves individual
  selected/page limits and does not alter normal scanning/reading readers.
  Evidence can reject a book whose predecessors exhaust the budget.
- XML selection/ZIP limits and public statuses remain unchanged. No raw archive,
  absolute path or context/provider error is returned.

Real oracles:
- `TestEvidenceCancellationDoesNotWaitForOrdinaryRarSession` and
  `TestEvidenceQueuedCancellationWithConcurrentClose` now include ComicInfo:
  cancellation before/across lock acquisition returns while the ordinary owner
  still holds the real mutex; concurrent Close still waits for that owner;
  ordinary page read remains correct; closed archive cannot reopen. Initial
  `comic-red.log` times out before the old implementation even checks the
  context preflight barrier. Parser/bookcontent then pass (`comic-green.log`).
- `assertClueTransportDisconnect`, called by the production combined test,
  uses a real RAR book and saturated real volume IO. Twice per fixture it closes
  the HTTP socket or the compiled proxy's stdin, waits for zero IO waiters and
  zero library-read conflicts while still holding the saturation lease, and
  verifies no retained archive borrow. A later successful clue proves reuse;
  stdio normal EOF exit is checked, not killed-and-called-success.
- SDK `ClientSession.Close` while an RPC is active was an invalid EOF driver:
  it can wait for the RPC before closing stdin. `cancel-green.log` records that
  timed-out test attempt. Final tests explicitly close the real stdin/socket.
- `TestComicInfoRealRarBudgets` generates actual CRC-valid RAR5 wire headers and
  stored payloads, solid-flag and non-solid variants: small ComicInfo, XML over
  64 KiB, two 9 MiB predecessors exceeding the cumulative budget, and a real
  compressed-file header asking for a 32 MiB dictionary. The dependency accepts
  the archive headers; bounded readers reject dictionary/predecessor cases.
  The same inputs cover directory and selected-page readers, not fake errors.

## Actual checks

Environment `.tmp/finalfix/env.sh`: Go1.26.10, guide's shared GOCACHE, own
GOTMPDIR and independent lint cache. Ignored web/dist copied for Go embedding;
no frontend source changed, so no frontend gates rerun. Acceptance14's prior
frontend result remains historical, not a result of this remediation.

| Command / evidence under `.tmp/finalfix/` | Result |
| --- | --- |
| parser/taskstore hostile-wire/prefix focused (`wire.log`) | PASS |
| `go test -p 1 ./internal/parser ./internal/bookcontent ./internal/images ./internal/taskstore ./internal/task ./internal/application/delegated ./internal/bootstrap -count=2 -timeout=10m` (`related-twice.log/.exit`) | PASS, exit0 |
| **One** `go test -p 1 ./... -count=1 -timeout=10m`, outer1800s (`full-backend.log/.exit`) | PASS, exit0; API114.388s/bootstrap126.183s |
| Final parser + combined public matrix twice after test-only switch/normal-EOF-exit assertions (`final-focused-twice.log/.exit`) | PASS, exit0; bootstrap71.641s |
| `gofmt -l ./cmd ./internal` (`gofmt-final.log`) | Empty |
| independent-cache `golangci-lint run --allow-parallel-runners` (`lint-final.log/.exit`) | `0 issues.`, exit0 |
| `go run ./cmd/tsgen` (`tsgen.log/.exit`) | 48 types, exit0 |
| PowerShell `sqlc generate` (`sqlc.log/.exit`) | exit0 |
| SHA256 comparison generated.ts + sqlc query/models/querier (`generated-before/after.log`) | Identical, no drift |
| `git diff --check` | PASS |

The single full backend was before the final **test-only** tagged-switch lint
cleanup and added normal-EOF process-exit assertion. Production Go was unchanged
after that full pass; those final tests passed twice and lint passed. Initial
lint's two QF1003 test-style findings are retained in `lint.log`, not called
passed. No Windows rename failure occurred; no retry, skip or claim to have
fixed its historical cause. Spec-graph tools resolve the parent working tree,
so this worker did not validate another tree; parent validation remains due.

## Explicit coverage limits / handoff

- No historical malicious compressed-solid RAR fixture/tool was available.
  Generated solid-flag **stored** predecessor payloads exercise real traversal
  and budgets, but do not prove every compressed solid LZ/PPM history case.
  The hostile dictionary case is a real minimal compressed header, deliberately
  rejected before decoding a payload, not a complete valid compressed book.
- Public HTTP/stdio cancellation is deterministic at the real IO wait; the
  shared-lock-owner cancellation/concurrent-Close oracle is at the parser seam.
  A single production HTTP/stdio experiment combining cancellation **while the
  ordinary RAR mutex owner remains blocked** has not been added. No claim that
  this exact combined scheduling case was publicly exercised, nor that all
  close/release schedules were counted exactly-once by instrumentation.
- No process kill/power-loss guarantee, browser run, paid model or new frontend
  verification. No claim that historical Windows rename root cause was fixed.
- All changes remain uncommitted. Parent must integrate and run independent
  canonical and two-axis review, including the explicit combined-lock test gap
  if required as an acceptance blocker.
