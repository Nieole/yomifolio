# 14 — Integrated acceptance and deployment documentation

## Baseline and reviewed matrix (before tests)

Only `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-14`, branch
`mcp-integrated-acceptance`; HEAD `9a221ca8e737e2b84c80900fd9b4b6c8126fecbf`.
`git merge-base --is-ancestor 9a221ca8 HEAD` exit0; initial status clean.
01–13 are delivered and independently approved per parent; preflight's old 11
warning is historical. No feature reimplementation, index/history mutation or
parent TODO changes. Agreed seam: bootstrap.Open, real SQLite/CBZ/PNG, generic
SDK HTTP and compiled stdio helper, administrator HTTP and decoded public images.
Only existing external source/AI factories are substituted. No second runtime.

| Combination | Symbol / expected public oracle |
| --- | --- |
| Restricted init/list/call → combined selection → ComicInfo/pages | TestMCPIntegratedAcceptance/{http,stdio}: one scoped target, archive identity, decoded image; hidden library denied |
| Multiple sources → ambiguity → identity/edition and field origin | Same test, related series/search/proposal IDs: no inbox entry or write for ambiguity; mixed origins persisted only after confirmation |
| Allowance exhaustion → fresh connection → non-model publication | Same search/model operation: used=1/remaining=0 unchanged; uncertain does not confirm identity; explicit non-model proposal still possible |
| Shared author + text + cover + adjudication | Same target: changed receipt, public blue image/text/canonical credit, other series alias untouched; remaining differences close with accepted=true |
| Per-series reporting | Agent-side report from public success receipt, ambiguous search and failed source search; not an invented server batch API |
| Undo after closure | Same operation: public red cover/original authors/text restored; proposal remains terminal |
| Another combined operation → cleanup result-write fault → normal close/reopen → original-key resume | Same target/proposal lineage: pending/unavailable before reopen, same cleanup ID complete after; current blue image/minimal adjudication survive |
| 11 permanent key replay/read authority after 13 cropping | Exact old command returns original ID and immutable decision; narrowed Read denies replay even when caller removes adjudication |
| Run lifecycle / revocation | Source barrier + engine pause: queued/active permission/password revocation; compiled helper EOF leaves accepted run alive, fresh helper can cancel |
| Model cancellation/reopen | Existing TestMCPModelBudget{PersistsWithoutReplay,StdioReconnectWithoutReplay}: unknown consumed unit, no refund/resend. Combined test also keeps exhausted operation across cleanup/reopen |

Self-review: distinguish reconnect from production close/reopen; do not sum
feature suites as end-to-end proof or duplicate their full security matrices.
Test-first adds composition regressions before docs. Business features exist:
a first green is not fabricated red/green implementation. Actual missing/security
findings go to parent fresh repair, not production changes under ticket14.

## Actual checks and limits

### Preserved retry evidence

Retry retained all prior uncommitted files and `.tmp/14` diagnostics, at HEAD
`9a221ca8`; integration still points to this baseline. No production business
code changed. `first.log` records test-authoring enrichment failures (generic SDK
pointer diagnostics alone do not establish a production defect);
`lifecycle-first.log` records an incorrect test assumption about the admin
response's nullable summary shape. `second.log`, `lifecycle-green.log`,
`combined-final-first.log` and `combined-two.log` remain intact. Final candidate
passed two consecutive rounds before the single full backend gate. This is
composition regression coverage, not a fabricated red/green business repair.

### Executed gates

All commands ran from the assigned tree. `.tmp/14/env.sh` records Go1.26.10,
shared GOCACHE, and this tree's GOTMPDIR/lint cache. Frontend used fnm
Node26.13.1/npm11.20.0 and existing dependencies; dist was actually rebuilt.
Evidence paths below are relative to `.tmp/14/`.

| Check | Actual result / evidence |
| --- | --- |
| Combined acceptance, two rounds | HTTP + compiled stdio PASS twice; `combined-two.log`, `combined-two.exit=0` |
| `go test -p 1 ./... -count=1 -timeout=10m` (outer1800s), once | PASS, 38 tested packages; `full-backend.log`, exit0 |
| `gofmt -l ./cmd ./internal` | Empty; `gofmt-final.log` |
| `golangci-lint run` | `0 issues.`; `lint-final.log`, exit0 |
| web `npm test -- --maxWorkers=2` | 121 files/1164 tests PASS; `frontend-test.log`, exit0 |
| web `npm run build` | PASS; `frontend-build.log`, exit0 |
| web `npm run lint` | PASS; `frontend-lint.log`, exit0 |
| PowerShell `sqlc generate` | exit0; `sqlc.log` |
| `go run ./cmd/tsgen` | exit0, 48 types; `tsgen.log` |
| `git diff --exit-code -- internal/database web/src/api/generated.ts` after generators | No drift, exit0; `generated-drift.log` |
| `git diff --check` | PASS |
| `bash scripts/check-doc-style.sh` | Blocked, not passed: outer60s timeout, MSYS fork failure; `doc-style.log` |
| Spec graph tool validation | Parent pending; worker did not run it against another tree |

### Oracles and limits

The matrix maps to one `TestMCPIntegratedAcceptance` body, not a sum of feature
suites. Administrator routes issue/narrow credentials and expose proposal inbox
and operation audit. MCP observes metadata, proposal acceptance, immutable replay,
model budget and cleanup state. `GET /api/covers/{bookID}` decodes actual pixels.
SQLite triggers inject faults only, not success oracles. Source barriers and
shared-engine pause/resume control queued/active work; engine Await observes
terminal state. Real helper EOF and a fresh process query/cancel the same accepted
run. Only external source/AI factories are substituted. Per-series reporting is
test-agent aggregation of public outcomes, not an invented server batch API.

Normal production close/SQLite reopen covers interrupted model admission and
pending cleanup result-write failure together. It does not prove forced kill,
power loss, damaged disks, external file deletion or directory durability. No
historical Windows rename AccessDenied occurred in this full run; its cause
remains unlocated, not fixed or disproved. No paid model or product-specific
client compatibility is claimed.

The extra doc-style command retained exact diagnostics:
`dofork: child -1 - forked process 57336 died unexpectedly ... exit code 0xC0000142,
errno 11` and `scripts/check-doc-style.sh: fork: retry: Resource temporarily
unavailable`. It was not blindly retried or called passed. Required backend,
frontend and generation gates above completed successfully.

Long-lived product scope and stdio dependency are in root owning specs; detailed
contracts remain in mm-application/mm-api/mm-mcp-stdio. Effort spec remains
planning/history and ADR0016 remains draft. No new business/security missing
finding was identified; no business repairs were folded into ticket14.

Open: parent spec graph validation, doc-style environment diagnosis, independent
ticket review and whole-branch two-axis review.
