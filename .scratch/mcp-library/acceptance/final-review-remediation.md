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

## Fresh spec-recheck remediation — review pending

This section supersedes the earlier bounded-solid claim and the two historical
coverage gaps above; it does not reopen the approved run-list change. Sole tree
`C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-final-fixes`, branch
`mcp-run-discovery-fix`, fresh starting HEAD `1da89130`, initially clean. No
index/history operation, parent TODO edit or other-tree change. Existing
canonical/StandardsApprove run-discovery behavior is untouched. This is an
uncommitted implementation report, **not** an independent closure/approval.

### Confirmed dependency mechanism and policy

rardecode v2.4.6 `packedFileReader.newArchiveFileFrom` wraps the decoder in a
reader limited to declared UnPackedSize; `decodeReader.init` sets its solid
state from **archive-wide** arcSolid, not current FileHeader.Solid.
`decodeReader.nextFile` drains its own decoder directly with io.Copy when that
state is true. Caller-visible draining, context, dictionary limits, wall-clock
or compressed-input counting do not impose an output-work limit on that drain.
There is no public decoder-output budget hook in the installed dependency.

`TestRarDependencyHiddenSolidDrain` now experimentally verifies the mechanism:
real compressed RAR5, first file **not** file-solid, global solid, original
24 MiB output; hostile copy declares 1 byte, removes its file checksum and
recomputes header CRC. Dependency Read returns 1 byte; Next successfully
reaches ComicInfo and consumes 1264 additional compressed bytes. The original
counterpart proves the stream's output size. This is an input/call trace tied
to the dependency source, not a claim that 1264 input bytes are an output cap.
Fixture provenance/mutation recipe is in
`internal/parser/testdata/rar-evidence-fixtures.md` and `make-undersized.py`.

All three evidence paths share `boundedRar` -> `pageContextFS.Open` ->
`preflightRarEvidence`, before the opened file is handed to the SDK. The strict
classifier checks raw RAR4/5 headers/CRCs and file boundaries, skipping packed
payloads with seek, never decompressing. Same open file is rewound and used by
SDK (no reopen-after-approval). Limits: 65536 blocks, 16 MiB cumulative headers,
RAR5 1 MiB single header. Global/file solid, multi-volume/split, unknown output
sizes, header/file encryption, SFX, service/comment/unknown blocks and malformed
layouts fail closed with `ErrRarEvidenceUnsupported`. Unsupported ComicInfo is
truthfully `unavailable`, not missing/corrupt/too_large. Thumbnail errors remain
finite unavailable/unsafe with no original-file fallback. Ordinary RAR reading
and scanning retain full existing solid support; standard single-volume
non-solid RAR4/5 evidence, ZIP behavior and existing dictionary/visible-byte
limits remain. No dependency replacement/fork, permanent format ban or change
to ordinary reader policy. Owning parser/bookcontent specs and both READMEs
publish the intentionally narrower evidence boundary.

### Oracles and honest red/green provenance

- `red.log`: before classification, all three bounded APIs incorrectly succeed
  on the hostile compressed stream; the honest large stream reaches ordinary
  declared-byte rejection instead of strict classification. Its first trace
  assertion incorrectly expected >32 KiB further input: corrected to >1024
  after observing actual 1264. This instrumentation mistake is not claimed as
  a vulnerability regression. The hostile three-path success is the real red.
- `header-green.log`: original/hostile compressed archives rejected as the
  specific unsupported class by all three paths; real compressed non-solid
  metadata/directory/page succeed; ordinary compressed-solid metadata/page
  remain readable. CRC-valid generated RAR4/5 headers cover plain, global-solid,
  multi-volume, encrypted headers/files, split files, unknown sizes, file-solid,
  unknown blocks, truncation and bad CRC. Earlier stored-wire tests now expect
  strict refusal for solid, retain non-solid limits; not weakened to arbitrary
  error matching. Compressed fixtures do not claim all LZ/PPM coverage.
- `public-red.log`: with only the classifier temporarily bypassed, real HTTP
  production integrated acceptance sees hostile ComicInfo `available` and
  fails. The bypass was restored, never retained. Final combined HTTP and real
  compiled stdio acceptance requires hostile clue `unavailable` and thumbnail
  error with no ImageContent; normal ZIP and ordinary RAR clues still succeed.
- `TestPublicClueDisconnectWhileRarSessionOwned` adds the formerly missing
  combination: real production bootstrap/SQLite/CBR, raw HTTP socket close or
  real compiled proxy stdin EOF while the shared RAR mutex owner remains held
  by the existing **test-only ordinary-session barrier**. It observes a second
  real borrow and active volume IO before disconnect; cancellation returns to
  one ordinary-owner borrow, zero IO active/waiting and no library read conflict
  **before unlocking the owner**. Each transport repeats, then races Close,
  proves Close cannot bypass the owner, releases owner, observes Close finish
  and verifies no reopen; first iteration verifies ordinary page reuse. Normal
  stdio exit is required; process kill is failure cleanup only. This is a real
  mutex/transport combination, not an assertion that an OS archive read was
  itself physically blocked or every close schedule was enumerated.
- `public-lock-sensitivity-red.log`: mutation check replacing only ComicInfo's
  cancellable lock with ordinary Lock makes the public HTTP combination fail
  to release borrow/IO while owner remains active. Restored immediately; final
  parser/bookcontent pass twice. This is sensitivity validation, not a claimed
  new production lock fix or red-before-first-test history.

### Actual fresh checks (logs under `.tmp/solidfix/`)

Same Go1.26.10/shared GOCACHE as `.tmp/finalfix/env.sh`; own tree GOTMPDIR and
independent `.tmp/solidfix/lintcache`. No frontend changes or frontend gate rerun.

| Command | Result |
| --- | --- |
| Focused real decoder/strict classification/ordinary compatibility tests (`header-green.log`) | PASS |
| `go test -p 1 ./internal/parser ./internal/bookcontent ./internal/images ./internal/taskstore ./internal/task ./internal/application/delegated ./internal/bootstrap -count=2 -timeout=10m` (`related-twice.log/.exit`) | PASS exit0; bootstrap248.870s; includes both real transports and hostile compressed book |
| **Exactly one fresh** `go test -p 1 ./... -count=1 -timeout=10m`, outer1800s (`full-backend.log/.exit`) | PASS exit0; API122.352s/bootstrap127.849s |
| New combined held-lock public HTTP/stdio test twice (`public-lock.log/.exit`) | PASS exit0 |
| Final `go test -p 1 ./internal/parser ./internal/bookcontent -count=2 -timeout=3m` (`final-parser-bookcontent-twice.log/.exit`) | PASS exit0, after adding combined test and sensitivity restore |
| Independent-cache lint (`lint-final.log/.exit`) | `0 issues.`, exit0 |
| `gofmt -l ./cmd ./internal` (`gofmt-final.log`) | Empty |
| `go run ./cmd/tsgen` (`tsgen.log/.exit`) | 48 types, exit0; before/after SHA256 identical |
| `git diff --check` | PASS |

The sole fresh full pass was on the final production Go; afterward only the
combined held-lock test and documentation were added. That test and final
parser/bookcontent passed twice, lint passed. No second full suite, speculative
retry, frontend gate, SQL regeneration (no SQL changes), paid model or browser
claim. Initial unsupported `Rar.exe -ma4` fixture-generation attempt produced no
RAR4 compressed fixture; RAR4 classification cases are generated raw headers.
Spec tools index the parent's tree; correct-tree owning files were read/updated
directly rather than validating another tree.

Remaining limits/parent handoff: strict preflight is deliberately conservative
(service/comment/unknown blocks and SFX can make an otherwise readable archive
ineligible for evidence), and is not a general RAR validator. Ordinary reader
compatibility is tested, not every codec variant or multi-volume reader case.
Concurrent hostile external in-place file mutation is not a new supported
publication guarantee. Existing per-process archive publication rules remain;
no arbitrary external-writer locking is claimed. Parent owns canonical and spec
independent recheck, especially classifier/dependency semantic alignment and
this documented evidence-only compatibility reduction. No finding is marked
review-closed here; all delta remains uncommitted.

Precision for spec recheck: the retained 16 MiB drain counter measures
SDK-visible predecessor bytes. The classifier removes the confirmed solid
hidden-Next drain; it does not introduce an SDK-internal output/instruction
counter. Decoder window/filter buffering is still dependency-owned. No proof
is claimed that every codec's internal generated output equals caller-visible
bytes or stays at exactly that number; review should distinguish the confirmed
solid advancement fix from a universal codec-work guarantee.
