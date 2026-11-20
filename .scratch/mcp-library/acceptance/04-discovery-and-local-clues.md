# Ticket 04 verification

Worktree: `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-04`; branch `mcp-discovery-clues`; starting HEAD/merge-base both `1c96d2fe08616e4d1ebe52a46ee7478fb0e8c1d7`. Changes are uncommitted. Integration advanced during execution (last observed `0ab7dbb810e344fc0efd7632d12ecc11c432590c`); parent must safely integrate/reconcile shared files before completion.

## Behavior and seam

Production `bootstrap.Open`, real temporary SQLite/libraries/CBZ archives (PNG pages and nested ComicInfo), official generic Go MCP client, negotiated protocol 2025-11-25, both configured LAN HTTP and TLS. No external source calls or substitutes for application services.

- Added `library_find_series`, `library_list_books`, `library_get_book_clues`; kept `library_get_series`. Read-only annotations and complete application use cases; no adapter database/file access.
- Verified combined library + explicit target + added-time + ALL missing-field filters; inclusive/exclusive time boundaries, missing scalar/collection fields, stable-ID keyset series/book pagination, oversize page/payload rejection and invalid inputs.
- Verified hidden-library filters, explicit hidden targets even when time filters would exclude them, hidden-series book lists, hidden/unknown book IDs and inconsistent book/series ownership all reject. Returned names do not expose host paths.
- Verified valid, absent, corrupt XML, oversized XML and missing-archive clues. Raw metadata decompression is capped at 64 KiB; modeled identity fields only. Actual result envelopes checked against 256 KiB. Structured JSON capped at 240 KiB with small text reference rather than duplicated JSON.
- No web/scanning semantics, schema/sqlc sources, generated frontend targets, image tools (05), candidate search (07), stdio (03) or run engine (06) changes.

## TDD

First client tracer failed with unknown tool `library_find_series` on both transports; implementation made it green. Expanded real-archive/client scenario exposed unsupported source-links SQL table and failed; corrected to the existing `series_links` relation and passed. Existing tool discovery assertions now expect the four read-only tools, not only the prior single tool.

An intermediate oversized-title fixture made SQLite's existing gram-index trigger prohibitively expensive, causing test command timeouts. Changed only fixture data to an oversized directory basename (no physical file creation) to exercise response limits without indexing a huge title. This was a test fixture failure, not labeled baseline.

## Commands/results

All Go commands used `GOTOOLCHAIN=go1.26.10`, shared worker01 `gocache126`, this tree's `.tmp/gotmp`, and independent `.tmp/lintcache`.

- `go test -p 1 ./internal/bootstrap -run TestMCP -count=3 -timeout=3m`: PASS, latest 12.275s, after final real-PNG and inconsistent-ownership coverage.
- `golangci-lint run`: PASS, latest `0 issues.`
- `gofmt -l ./cmd ./internal`: PASS, empty.
- `go run ./cmd/tsgen` + `git diff --exit-code -- web/src/api/generated.ts`: PASS, 32 types generated, no diff. No frontend behavior changed.
- `git diff --check`: PASS.
- `go test -p 1 ./... -count=1 -timeout=10m` was run repeatedly, with >=1800s outer timeout. **No final all-green full run**:
  - First run: API and other packages passed; bootstrap's old single-tool count assertion failed, then updated to the four read-only tools.
  - `.tmp/backend-test-final.log`: API `TestRestorationCleanCheckStillNeedsConfirmationAndDuplicateCannotOverwriteLaterEdit` received 503 library_unavailable for its later edit; remaining packages including bootstrap/parser/bookcontent passed.
  - `.tmp/backend-test-rerun.log`: API `TestConfigFailedUpdatePreservesConcurrentExternalEditor/save` encountered Windows config rename Access denied; remaining packages passed.
  - `.tmp/backend-test-gate.log`: API passed (242.584s), bootstrap passed (14.223s); external `TestTransferPausedWhileQueuedForQuotaDoesNotStartRead` failed with context canceled and `TestPausedTransferRejectsChangedSourceOrStagingAndCleansUp/{staging_time,target_root}` missed pause boundary; remaining packages passed.
- Focused reruns of both named API tests with `-count=3` passed (1.028s). Focused reruns of both named external tests with `-count=3` passed (8.671s). These are observations, not proof of baseline or a substitute for the full gate.

## Open / integration

The final backend gate checkbox remains open; parent must rerun on integrated tip. Shared seams: `delegated.Service` gains content/access fields and `NewWithDiscovery`; bootstrap passes its existing content service and engine admission capability; MCP handler calls `registerDiscoveryTools`. Existing tool-count assertions must be reconciled with 06's additional tools. Owning specs and CHANGELOG were updated in this tree; no pending product/security decisions. RAR uses the bounded reader too, but new MCP fixtures are ZIP/CBZ; existing parser suite passed in full-run attempts.

Suggested commit subject: `feat(mcp): add scoped series discovery and local book clues`


## Independent merger04 integration review

Reviewed the worker guide, effort spec, ticket, owning API/application/bootstrap/bookcontent/parser specs, and the original `.tmp/backend-test*.log` failures (503 library_unavailable, Windows rename Access denied, and external pause/context-canceled failures). No baseline attribution is made. This review runs only in worker04 on `mcp-discovery-clues`, HEAD `d6945b04`; incoming merge is `0ab7dbb8` (merge-base `1c96d2fe`). Existing prior acceptance above is historical; this addendum supersedes the prior open *backend gate*, but not the new defect below.

### Conflict resolutions and boundaries

- CHANGELOG and API/application specs retain discovery/clues and proxy/runs contracts, rather than selecting a whole side.
- `delegated.Service` retains content/access **and** run access. `NewWithDiscoveryAndRuns` explicitly binds existing shared content, engine admission and run/scanning/query capabilities. `bootstrap.Open` still creates exactly one runtime; MCP does not create resources or read DB/files directly.
- MCP registers both discovery and run tools. The two conflicted discovery assertions now verify the exact public nine-name set, reject duplicates/unexpected tools and validate read-only annotations per tool (start/cancel are not read-only). Regular-account scan denial and existing authorization/password/revocation assertions are retained, not weakened.
- Static review confirms filter intersections (OR within libraries/explicit IDs, AND across time/missing fields), explicit-target preauthorization even for otherwise excluded targets, stable ascending-ID keysets, 20/default and 50/max, scope rechecks, book/series/library consistency, basename-only projections and no path input. Content access acquires/relinquishes library admission, IO lease and archive borrow; output contains identity fields, not reading state. `ReadMetadataLimited` checks declared size and actually reads at most 64 KiB + one sentinel byte; normal scan/web reads are unchanged. Existing client fixtures exercise real CBZs, invalid/missing clues, scope denial, ownership inconsistency and result limits.
- No edits to 03/06 tickets/acceptance/tests; their incoming changes remain intact. No stage/commit/merge/reset/abort commands were used. Git will still show unmerged index entries until the parent stages the resolved working files.

### Actual gates

Environment: `GOTOOLCHAIN=go1.26.10` (`go version go1.26.10 windows/amd64`); shared `GOCACHE=C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-01/.tmp/merger01/gocache126`; independent worker04 `.tmp/merger04/gotmp` and `.tmp/merger04/lintcache`.

- `go test -p 1 ./... -count=1 -timeout=10m`, outer timeout **1800s**: **PASS**, every backend package; API 158.494s, bootstrap 30.684s, external 9.538s. Full output `.tmp/merger04/backend-full.log`. No tests removed or relaxed to obtain this result.
- `go test -p 1 ./internal/bootstrap -run TestMCP -count=1 -timeout=5m`: PASS (9.430s), `.tmp/merger04/mcp-first.log`. Initial pre-resolution attempt failed on the known conflict markers after a local Python encoding error; markers were then resolved, not excluded from tests.
- `go test -p 1 ./internal/bootstrap -run TestMCP -count=3 -timeout=5m`: **PASS** (73.795s), `.tmp/merger04/mcp-repeat.log`; includes actual stdio proxy and run tests as well as 04 HTTP/TLS tests.
- `gofmt -l ./cmd ./internal`: PASS, empty `.tmp/merger04/gofmt.log`.
- `golangci-lint run`: PASS, `0 issues.`, `.tmp/merger04/lint.log`.
- `go run ./cmd/tsgen` followed by `git diff --exit-code HEAD -- web/src/api/generated.ts`: PASS, 32 types, no drift, `.tmp/merger04/tsgen.log`. SQL sources/schema were not changed by this resolution; no new sqlc output required. No frontend behavior changed.
- `git diff --check`: PASS after preserving LF line endings in edited Markdown. Conflict-marker scan of CHANGELOG/internal/cmd/effort files: empty.

### Blocking independent finding (not repaired in merger scope)

**04 clue truthfulness: trailing malformed XML is accepted as available.** `parser.ParseComicInfo` only calls `xml.Decoder.Decode` once; it returns after the first root without checking remaining bytes. `bookcontent.readComicInfo` therefore returns modeled fields with `status=available` for a malformed complete document. A real ZIP/CBZ containing `ComicInfo.xml` with `<ComicInfo><Title>Valid</Title></ComicInfo><broken` reproduces:

```text
xml="<ComicInfo><Title>Valid</Title></ComicInfo>" clue={"status":"available","title":"Valid"} err=<nil>
xml="<ComicInfo><Title>Valid</Title></ComicInfo><broken" clue={"status":"available","title":"Valid"} err=<nil>
```

Reproduction: `go run ./.tmp/merger04/clueprobe`; self-contained source `.tmp/merger04/clueprobe/main.go`, output `.tmp/merger04/clueprobe.log`. It invokes the real content service and real bounded archive reader, not a parser replacement. The MCP adapter forwards this status unchanged. This behavior exists before conflict resolution; no production parser fix or global scan semantic change was attempted. The ticket's clue-truthfulness checkbox is reopened. Parent must triage/fix this before closing 04; passing existing gates does **not** establish that acceptance item. Merge conflict text is ready for parent staging, but **04 is not fully acceptance-ready**.


## Targeted ComicInfo truthfulness repair (fresh TDD context)

Scope: only worker04 `mcp-discovery-clues`, starting clean HEAD `4839e85c`; merge-base with requested `a0245ff5` is `a0245ff5`. No parent-tree edits, stage, commit or merge. Status stays **ready-for-agent** pending parent review/commit. The reopened clue-truthfulness item is repaired and rechecked; the historical finding above remains the red reproduction record.

### Contract and root cause

`parser.ParseComicInfo` previously used one `Decoder.Decode`, which stops at the first root. It now reads tokens through EOF, decodes exactly one ComicInfo root with the same charset/lenient-number behavior, and rejects extra roots, non-XML-whitespace outside the root and decoder errors without returning partial metadata. Legal XML declaration in the prologue, comments, processing instructions and XML whitespace remain accepted. Declarations/directives after the root are rejected. This corrects the existing parser contract (invalid XML errors), not a new scanner-specific policy. No scanner, protocol adapter, content-service implementation or status strings changed: malformed evidence remains `corrupt`, absent remains `missing`; `unavailable`, `unreadable`, `too_large` mappings and 64 KiB decompression budget are untouched.

Decoder safety review: `xml.NewDecoder` keeps strict defaults; no Entity map or external resolver is configured. The standard decoder does not fetch external DTDs or expand custom declared entities. Regression cases preserve acceptance of an unreferenced external DOCTYPE and rejection of unknown/declared entity references; no DTD parser, dependencies or wider security policy added. Caller-owned byte/decompression limits remain unchanged.

### Red / green evidence

Logs are in ignored `.tmp/comicinfo-fix/`. All Go commands use Go1.26.10, guide's shared worker01 GOCACHE, this directory's independent `gotmp` and `lintcache` (settings in `env.sh`).

- Added public `TestParseComicInfoCompleteDocument` and extended existing `assertLocalClues` real CBZ fixtures, preserving its scope/pagination/result-size assertions. No new test host or fake parser. Tests cover exact broken tail, second root, extra text, legal XML misc; parser additionally covers prefix text, broken comment, unexpected closing element, trailing declaration/directive, non-XML space and entity boundaries. Existing BOM/charset/dirty-number tests remain.
- RED: `go test -p 1 ./internal/parser ./internal/bootstrap -run 'TestParseComicInfoCompleteDocument|TestMCPDelegatedSeriesLifecycle' -count=1 -timeout=5m` (`red.log`): parser accepted broken tail/second root/text tail/text prefix/broken comment; real production HTTP and TLS both returned `available` / `Valid` for `broken-tail.cbz`. This is the actual reported failure, before production edits. Initial test-authoring attempts hit unterminated Go literals and Python default-GBK decoding errors; corrected before behavioral red, not counted as regression evidence.
- GREEN: `go test -p 1 ./internal/parser ./internal/bookcontent ./internal/bootstrap -run 'TestParseComicInfo|TestMCPDelegatedSeriesLifecycle' -count=1 -timeout=5m` (`green.log`): PASS (bookcontent had no matching tests in this filtered run). An initial implementation-authoring attempt also had an unterminated literal, corrected before green.
- `go test -p 1 ./internal/parser ./internal/bookcontent ./internal/bootstrap -count=3 -timeout=10m`: PASS twice, `repeat.log` (bootstrap 52.689s) and `repeat-final.log` (128.039s). These are full packages, including bookcontent tests and integrated `TestMCPStdioAcceptedScanSurvivesEOFAndExplicitCancel`, `TestMCPStdioProductionProxy`, and saturated EOF regression; none removed/relaxed.
- Final focused `go test -p 1 ./internal/parser ./internal/bookcontent ./internal/bootstrap -run 'TestParseComicInfo|TestMCP' -count=3 -timeout=10m`: PASS (`mcp-final.log`, bootstrap 91.584s; bookcontent has no matching tests in this filtered command).
- Original independent repro `go run ./.tmp/merger04/clueprobe`: PASS, `clueprobe-final.log`: valid root still `available` / `Valid`; exact malformed tail now `{"status":"corrupt"}`, no partial title, error nil at content boundary as required by the status contract.

### Full gates and honest failure record

- `go test -p 1 ./... -count=1 -timeout=10m`, outer timeout **1800s**, ran three times:
  1. PASS, `backend-full.log` (API 159.076s, bootstrap 38.182s).
  2. FAIL, `backend-full-final.log`: `TestRestorationPersistenceFailuresCannotExposeHalfReleasedLibrary/result` at `relocation_restore_test.go` returned release 409 `relocation_plan_not_ready` / “Recheck this relocation plan before confirming”; API 299.003s, all other packages passed. No baseline attribution or unrelated fix.
  3. Final PASS, `backend-full-rerun.log` (API 186.894s, bootstrap 36.911s, scanner 31.005s), after final source/test formatting.
- The named failing API test was rerun with `go test -p 1 ./internal/api -run '^TestRestorationPersistenceFailuresCannotExposeHalfReleasedLibrary$' -count=3 -timeout=5m`: PASS 1.673s (`api-rerun.log`). Not a substitute for full gate; full gate subsequently passed as above.
- `golangci-lint run`: PASS, `0 issues.` (`lint-final.log`).
- `gofmt -l ./cmd ./internal`: PASS, empty (`gofmt-final.log`).
- `go run ./cmd/tsgen` + `git diff --exit-code HEAD -- web/src/api/generated.ts`: PASS, 32 types, no drift (`tsgen.log`). No SQL/schema/frontend changes, no sqlc or frontend gates required.
- `git diff --check`: final PASS (`diff-check.log`). One earlier check found trailing spaces in multiline XML test literals; removed, not suppressed.

### Remaining review / integration

No open functional acceptance defect in this scoped repair. XML parsing remains the existing standard-library/best-effort charset contract, not XSD validation or full DTD validation. New archive fixtures are CBZ; RAR continues through the same parser/bounded metadata API and its existing full-suite tests. Parent should review normal scan compatibility as the parser now correctly rejects invalid whole documents; valid-input tolerance tests and scanner/full suite pass.

Integration branch was observed at `acaecadd` (documentation-only successor of requested `a0245ff5`), not an ancestor of this tree's HEAD. Parent owns any safe integration; worker did not merge or edit 03/06 acceptance. All changes remain uncommitted.

Suggested fix subject: `fix(parser): validate complete ComicInfo documents before exposing clues`

## request_review rc_6c56374b — run-tool public error boundary

Fresh worker04 repair, initially clean `f6d2b19e2a6281c351fdd4fb89a38e7f3a41db96`; integration branch and merge-base were the same tip and remain so at final verification. Only this independent worktree was edited. No stage/commit/merge, parent-tree changes, new ticket implementation, or edits to 03/06 status/acceptance. **Ready-for-agent / pending review remains; the current full-backend gate is NOT green.**

### Boundary and actual fix

- `registerMCPRuns` routes all five handler errors through `runToolError` / `publicMCPToolError`. Only `errors.Is` matches for the existing delegated denial/validation sentinels select approved fixed messages; unknown errors become `run operation unavailable; retry later`. Even matching wrapped sentinels are replaced, never returned with their potentially private wrapper. No substring whitelist or secret-word filtering in production.
- `discoveryError` reuses this boundary, preserving its existing discovery validation/unavailable messages and replacing wrapped denial with the canonical sentinel. Existing HTTP credential error mapping already selects fixed messages; series lookup already returns a canonical denial. Neither needed changing.
- `delegated.StartScan` library lookup and `GetRun` snapshot lookup distinguish `sql.ErrNoRows` (unknown/denied) from actual storage failures. Storage failures retain their typed internal error and diagnostic; Get/Wait/Cancel now report genuine failures generically at MCP rather than misclassifying them as denial. No error is swallowed into a successful run or empty list.
- Existing `internal/api/SPEC.md` generic-error and application bounded-run contracts remain authoritative and unchanged. No protocol schema, SQL/schema, generated frontend response, runtime composition, or normal run lifecycle change. CHANGELOG records the public fix.

### Production regression / red → green

Evidence: ignored `.tmp/run-error-fix/`, Go1.26.10, guide's shared worker01 GOCACHE, independent `gotmp` / `lintcache`, environment in `env.sh`.

`TestMCPRunPersistenceErrorsStayPrivate` uses real production `bootstrap.Open`, temporary SQLite, and the official generic Streamable HTTP client. An actual SQLite `BEFORE INSERT` trigger aborts admission with synthetic private path/SQL diagnostics; a broken SQLite view then forces List/Get/Wait/Cancel read failures with a synthetic private table name. No business service/store mock replaces these failures. It observes raw wire responses (including JSON-RPC/SDK text), decoded text/structured results, returned errors and captured request-time slog output; failed responses must be errors without structured success data. A direct application invocation also proves `errors.As(*sqlite.Error)` and its internal diagnostic survive. After failed admission, public run listing is empty (no ghost run). Explicit denial/validation remain distinguishable; the existing real-archive run and stdio suites preserve normal completion/disconnect/reconnect/cancellation/revocation semantics.

- RED command: `go test -p 1 ./internal/bootstrap -run '^TestMCPRunPersistenceErrorsStayPrivate$' -count=1 -timeout=2m`, `red-final.log`. Scan text exposed the trigger diagnostic; list text exposed the private table; Get/Wait/Cancel incorrectly returned denial on storage failure. This was run before production edits. Earlier authoring runs supplied extra arguments / omitted required `timeout_ms`; fixed before the final valid red.
- GREEN: identical command, `green.log`, PASS. Added `TestMCPToolErrorsExposeOnlyCanonicalMessages` covers nil, arbitrary errors, wrapped denial/validation, and denial/validation text that is NOT a typed sentinel, for both run/discovery mappers.
- Initial repeat found test setup competition: startup history janitor could hit the deliberately broken database and log its own internal diagnostic (`repeated.log`, `log-probe.log`). Fixed test synchronization, not production logging or assertions: wait on the janitor's public terminal run state before injecting the request fault. Removed temporary diagnostic printing; log buffer is synchronized. `stable.log`: production regression passed 10 times. Internal background diagnostic logging is not globally prohibited by this patch; these checks isolate MCP request failures and do not inject faults into unrelated startup jobs.
- Final related repeat: `go test -p 1 ./internal/application/delegated ./internal/task ./internal/taskstore ./internal/bootstrap -count=3 -timeout=10m`, PASS (`related-full.log`), full packages including actual stdio process tests. `targeted-final.log` and `targeted-final2.log`: three repetitions of API MCP / bootstrap MCP+delegated+run tests also PASS. Some filtered packages had no matching tests; full related repeat above supplies their coverage.
- One early command mistakenly named nonexistent `./internal/mcpproxy` and used an overly broad `Test.*Run` regex; it failed setup and also selected an API relocation Windows rename failure (`repeated-final.log`). Correct proxy is `cmd/mcp-proxy` (no package-local tests); its actual process acceptance lives in bootstrap, repeatedly exercised above. No failed command is counted as passing.

### Gates and failures, without baseline attribution

Full backend command every time: `go test -p 1 ./... -count=1 -timeout=10m`, outer timeout **1800s**. Four attempts; **none passed overall**:

1. `backend-full.log`: API `TestRelocationBlocksNewConflictingLibraryRootsUntilEffectsFinish` failed renaming source → target, Windows Access denied; other packages passed.
2. `backend-full-rerun.log`: API `TestWriteBookComicInfoMergesInsteadOfReplacing` expected 200, received archive-write 500; other packages passed.
3. `backend-full-third.log`: API and MCP/bootstrap passed, scanner `TestRealWatcherStopsWhileDirectoriesAreRenamedAndRecreated` hung until the 10-minute package timeout. No timeout increase, weakening, or watcher fix attempted.
4. `backend-full-fourth.log`: API `TestRelocationPolicySQLFailureCompensatesAndAllowsNewConfirmation` remained prepared/recovering at its operation deadline; `TestRelocationPolicyPersistenceFailuresCompensateOrRemainBlocked/compensation` and `TestRelocationPolicyCrashRecoversFromActualDurabilityBoundaries/derived_effects` failed Windows source → target rename Access denied. Other packages, including scanner and MCP/bootstrap, passed.

Focused failing tests: first two earlier relocation failures plus ComicInfo test passed three times (`api-rerun.log`); timed-out watcher passed three times (`scanner-rerun.log`). These are not substitutes for a green full suite. No migration failure was observed in these four runs; historical migration failures in prior acceptance remain historical, not reclassified as baseline or erased. No unrelated production/tests changed.

- Final `golangci-lint run`: PASS, `0 issues.` (`lint-final.log`).
- Final `gofmt -l ./cmd ./internal`: PASS, empty (`gofmt-final.log`).
- `go run ./cmd/tsgen` + `git diff --exit-code HEAD -- web/src/api/generated.ts`: PASS, 32 types, no contract drift (`tsgen.log`). No frontend/SQL changes, so no frontend/sqlc regeneration needed.
- `git diff --check`: final PASS. An intermediate Python write converted CHANGELOG newlines to CRLF and produced a noisy diff; restored original LF before final check, leaving only the focused entry.

Remaining: review this boundary fix and obtain a clean full-backend gate; do not close 04, 03 or 06 based on targeted passes. No known remaining run-tool diagnostic disclosure in the covered paths. Suggested subject: `fix(mcp): sanitize run tool failures at the public boundary`.
