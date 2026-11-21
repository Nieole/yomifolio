# 07 — source matching and proposals

Status: ready-for-agent; parent integration/review/closure pending. Independent merger pagination finding is now narrowly fixed and covered by red/green production-client regression (below); historical merger evidence is retained.

## Independent merge verification (18d10fa0 + ff5c177e)

- Worked only in `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-07`, branch `mcp-source-proposals`. Actual HEAD `18d10fa0b514794c4a6bcc451a85cc31f6935d18`, MERGE_HEAD and integration tip `ff5c177e8775bf615eeadc3be06a3fb6e2a57338`; merge-base `0d9ef6ce260c31d90d0d71dc82ace22ef85c7a8f`. The parent had already started the no-commit merge. The earlier identity statements below describe the implementation session, not this merge session.
- Resolved only CHANGELOG's textual conflict by retaining both the 07 feature entry and Windows watcher fix entry. No whole-file ours/theirs. `git diff MERGE_HEAD --` for watcher.go, watcher_source_gate.go, watcher_source_gate_test.go, scanner/SPEC.md, runtime/SPEC.md and watcher-gate-remediation.md is empty: incoming fix and its qualified evidence are intact.
- No staging, commit, merge/abort/reset, parent-tree edits, later-ticket implementation or 05 interface assumptions. The index intentionally still reports CHANGELOG as UU until the parent stages it; its worktree content has no conflict markers. Other permanent merger edits are this acceptance record and the 07 issue comment.

### Review finding — parent action required before approval

**P2: source pagination advertises a cursor the tool refuses.** `internal/application/delegated/matching.go`, SearchSources input validation rejects offset >1000, but next-page construction unconditionally advertises offset+limit when upstream total says more results exist. At offset=1000, limit=10, a valid one-candidate page with total=2000 returns next_offset=1010. Following that returned cursor fails with the canonical invalid-parameters error. This is an existing 07 functionality defect, not a merge conflict or a later-ticket request; reported without expanding the implementation. Do not expand the offset cap silently: suppress an unreachable next cursor (or explicitly describe the bounded-search end) while retaining the documented bound.

Reproduced through production bootstrap + official generic MCP client + deterministic factory. A temporary test copied the existing feature fixture, returned one candidate/total=2000 for query Boundary, searched offset=1000/limit=10, waited, read next_offset, then followed it expecting success. Actual `go test ./internal/bootstrap -run '^TestMerger07PaginationBoundary$' -count=1 -v -timeout=2m`: **FAIL**, logged `advertised next_offset=1010`, then library_search_sources IsError=true. Reproducer source retained locally in `.tmp/merger07/pagination-repro.go.txt`, output `.tmp/merger07/pagination-repro.log`; temporary Go test removed. No existing tests changed. Merge conflict resolution is ready, but feature approval is **blocked on this finding**.

### Actual checks on combined worktree

Environment: Go `go1.26.10 windows/amd64`; shared GOCACHE from guide; own GOTMPDIR `.tmp/merger07/go`, GOLANGCI_LINT_CACHE `.tmp/merger07/lint`. Existing ignored web/dist present; no frontend source changes.

- **One full backend gate**, outer tool timeout **1800s**: `go test -p 1 ./... -count=1 -timeout=10m` **PASS**. api 116.801s, bootstrap 15.648s, scanner 16.430s; all packages passed (`.tmp/merger07/full-backend.log`). No full-suite retry/skip/timeout relaxation. This success does not erase the unresolved producer-exit/rename evidence in watcher-gate-remediation.md or establish general suite stability.
- `go test ./internal/bootstrap ./internal/api -run 'TestMCP' -count=2 -timeout=5m` **PASS**, bootstrap 32.305s/api 0.326s (`mcp-rounds.log`). This includes all named MCP tests, real stdio proxy/EOF and accepted-run tests, and persistence/run-error privacy tests, twice.
- `go test ./internal/application/delegated ./internal/proposal ./internal/metadata ./internal/application/runtime -count=1 -timeout=3m` **PASS** (`related.log`).
- `gofmt -l ./cmd ./internal`: empty (`gofmt.log`); `golangci-lint run`: **0 issues.** (`lint.log`).
- `go run ./cmd/tsgen`: generated 32 types; `git diff HEAD -- web/src/api/generated.ts`: empty (`tsgen.log`). No SQL or UI changes, so no sqlc/frontend rebuild needed.
- `git diff --check`: clean; CHANGELOG marker search empty. Integration tip still ff5c177e at final verification.
- The pagination reproducer is an intentional additional failing check, not a failure of the otherwise passing checked-in suites. Parent must fix/review it; no gate was weakened to conceal it.

### Read-only boundary audit / reuse

- Query/read guards search and proposal reads; MetadataWrite plus current administrator guards proposal creation. `confirmedProposalStore.ExecTx` begins the actual SQLite write transaction, loads current series/library, calls `AuthorizeTx` on that same tx, then Queue refreshes values/locks using that tx. No auth-snapshot retry found; ordinary Queue callers keep default snapshot/dedup behavior. This is static transaction-path verification plus existing authorization/feature regressions, not a newly added concurrency proof.
- Explicit identity AND edition confirmation and short summary required; no confidence-based first-choice path. Field origins and modeled facts persist in raw_payload, reopen test passes, reused proposal returns old evidence, no-change/all-lock outcomes do not persist a new record. URLs/candidate IDs are compared as data, never fetch inputs. Configured factory nil result does not fall back to an additional network provider.
- Cache allocation/expiration under MatchingAccess.mu enforces global 64 entries and 30 minutes on create/read; random search ID plus exact Subject binding prevents cross-credential reuse. Read rechecks actual series/library and current authority; source units use PrepareRun/Checkpoint. These cache/concurrency/expiry observations are code audit, not dedicated clock/concurrency tests. Existing feature test covers cancellation, narrowing, partial/total source failures, account role and reopen persistence.
- Matching adapters use canonical publicMCPToolError; source errors never enter SourcePage or run result text. Production source request logging is pre-existing metadata logging, not new credential/reading-state logging; no new raw errors, tokens, SQL or host-path logging found in 07 paths. Existing run-error privacy regressions pass.
- **08 pointers:** delegated.MatchProposal / GetMatchProposal, metadata.MatchEvidence / FieldEvidence / SourceFacts, proposal.Get and field Source/SourceURL, plus confirmedProposalStore.ExecTx as the actual transaction-fencing example. These are proposal evidence, not operation audit/undo or enrichment rules.
- **12 pointers:** delegated.Subject, PrepareRun, Checkpoint and AuthorizeTx; bootstrap MatchingAccess.Source uses the existing ExternalSources factory. Search intentionally never selects LLM aliases; 12 must implement explicit allowance/persistent usage independently, not treat search_id/run_id as an operation budget ID. 05 remains unintegrated and no 05 API is assumed.

## Identity / scope

- Dedicated worktree: `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-07`, branch `mcp-source-proposals`.
- Initial HEAD and merge-base with requested start: `0d9ef6ce260c31d90d0d71dc82ace22ef85c7a8f`.
- Integration branch advanced during work to `ff5c177e8775bf615eeadc3be06a3fb6e2a57338` (watcher fix `5f55ae1e` + gate documentation). Worker HEAD remains `0d9ef6ce`; parent must safely integrate against that newer tip. No worker merge/rebase performed.
- No stage, commit, merge, parent-tree changes, watcher/rename repairs, UI changes, LLM calls or real catalogue traffic.

## Delivered boundary

- Tools: `library_search_sources`, `library_get_source_search`, `library_propose_match`, `library_get_match_proposal`.
- `delegated.Service` owns complete use cases; `MatchingAccess` binds the production source factory/current config, shared `Runs.Start` and existing storage. No HTTP-route reuse, protocol DB access or second runtime.
- read capability governs source exploration and proposal reads; persistence additionally requires metadata_write and current administrator authority. `confirmedProposalStore.ExecTx` calls `AuthorizeTx` in the actual SQLite transaction, checks the actual series library, and Queue refreshes values/locks there. No cached grant or transaction-conflict retry.
- Catalogue selection includes three public sources and configured MAL/ComicVine. Existing search adapters already return modeled candidate details; the read tool returns all those snapshots, never FetchSeriesMetadata's first-title match. Returned URL/cover/candidate IDs are data only, not arbitrary network-fetch inputs.
- Both identity and edition confirmations are required. Supported fields map to candidate IDs from the owned search; cover/link fields are rejected. No metadata application. Queue outcomes remain distinct, including reuse/no_changes/all_fields_locked/rejected_before.

## Evidence ownership / 08 reuse

- Search evidence is bounded transient memory: global 64 searches, 30-minute lifetime, lost on restart. Runs retain their normal durable ownership/status. Search IDs are not proposal IDs or durable operation IDs. Queries are <=256 bytes, per-source limit 1–10, offset 0–1000 and aligned to limit, each candidate <=32 KiB and <=100 tags/authors, total result <=240 KiB.
- Newly created proposal `raw_payload` atomically retains `SeriesMetadata.MatchEvidence`: genuine subject IDs, search query, explicit identity/edition decisions, <=1000-byte verifiable summary, field-to-source mapping and selected candidates' complete modeled source facts. No separate reasoning field is accepted.
- Field rows retain their individual source/source_url. Reused proposals return their original evidence without pretending the new summary was saved. No-change/all-locked/rejected outcomes do not save a new evidence record.
- `proposal.Get` returns current field values and locks. Restart acceptance reads persisted matching evidence from the new service graph, without needing its old search cache.
- 08 can consume these confirmed source snapshots and field origins, but must implement its own enrichment policies, operation ID, before/after audit and undo persistence. Existing source rating normalization remains evidence, not an LLM score; aggregate-rating application is not implemented here. Raw cover/link evidence does not add proposal adjudication support. 11 existing-proposal adjudication and 12 internal LLM remain deferred to their tickets.

## TDD / checks actually run

Environment: `GOTOOLCHAIN=go1.26.10`; shared `GOCACHE=C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-01/.tmp/merger01/gocache126`; this tree's `.tmp/go` and `.tmp/lint`. Ignored web/dist copied from integration, no frontend sources changed.

- RED: `go test ./internal/bootstrap -run '^TestMCPMatchingConfirmedProposal$' -count=1` failed because `library_search_sources` was unknown (`.tmp/07-red.log`).
- GREEN: same bootstrap/client seam passed after implementation (`.tmp/07-green.log`). Development caught an SDK schema cycle; source facts now have a nonrecursive evidence type.
- Expanded acceptance initially hit the outer 180s timeout; the controlled source barrier was restricted to the first source, then the expanded test passed. No tests were skipped or weakened.
- `go test ./internal/bootstrap ./internal/application/delegated ./internal/proposal ./internal/metadata -count=1 -timeout=3m`: delegated/proposal/metadata passed; bootstrap exposed the intentionally exact tool-discovery allowlist, which was extended with all four tools and correct annotations.
- **One complete backend gate:** `go test -p 1 ./... -count=1 -timeout=10m` **PASS** (`.tmp/07-full-backend.log`). api 256.340s, bootstrap 46.983s, scanner 84.526s, taskstore 50.652s. No watcher or Windows rename failure observed in this run; no blind full-suite retries.
- Subsequent assertions added only to tests: `go test ./internal/bootstrap -run 'TestMCP' -count=1 -timeout=3m` **PASS** (37.784s; `.tmp/07-mcp-final.log`), then final feature test **PASS** (2.211s; `.tmp/07-final-feature.log`).
- Follow-up scalar validation RED: source rating 12 was incorrectly accepted (`.tmp/07-field-red.log`). GREEN adds nonnegative source ID/volume and normalized rating 0–10 validation; JSON encoding rejects non-finite numbers. Cancellation-in-progress is also projected as cancelled so it cannot produce a new proposal.
- Final related gate after those narrow changes: `go test ./internal/bootstrap ./internal/application/delegated ./internal/proposal ./internal/metadata -count=1 -timeout=3m` **PASS** (15.805s / 1.414s / 2.646s / 3.747s; `.tmp/07-final-green.log`). Full backend was deliberately run only once as requested, before these final narrow guards; relevant packages and lint were rerun.
- Final `gofmt -l ./cmd ./internal`: empty.
- Final `golangci-lint run`: **0 issues.**
- `go run ./cmd/tsgen`: generated 32 types, **no tracked generated.ts drift**. No SQL schema/query changes or sqlc generation needed; no new modules/notices.
- `git diff --check`: clean after preserving repository LF endings.

## Acceptance observations

Production bootstrap + real SQLite + generic official MCP client + deterministic external factory checks: three sources/two candidates; configured five-source behavior without restart; full details and explicit alternate-edition ambiguity; no proposal from unconfirmed exploration; partial failure, total failure and oversized-count rejection; cancellation reaches source context; source barrier plus scope removal blocks subsequent access; read-only narrowing blocks proposal creation; a regular account with both capability strings can search but cannot persist; mixed Bangumi summary/AniList publisher evidence; queued vs reused vs no_changes vs all_fields_locked; updated current lock projection; unknown-series/cross-proposal identity rejection; arbitrary URL candidate IDs and unsupported cover fields rejected; matching evidence readable after closing and reopening production bootstrap. Existing ordinary proposal behavior and authorization regression suites passed in the full gate.

## Limits / handoff

- Search cache is deliberately temporary; restart does not resume searches or reissue external requests. Confirm and persist evidence before expiration if needed.
- External sources' modeled fields are the available evidence; this does not assert complete edition catalogues or independently prove agent-provided match judgments.
- No new product/security decision remains unresolved for 07. Independent review/integration and combined gates with parallel 05 remain parent's responsibility. Shared wiring, specs, CHANGELOG and tool-discovery set require preserving 05 additions when integrating.
- Suggested commit subject: `feat(mcp): search catalogue sources and persist confirmed match proposals`.

## Pagination boundary remediation — fresh-context TDD

Worked only in `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-07`, branch `mcp-source-proposals`, HEAD `18d10fa0b514794c4a6bcc451a85cc31f6935d18`, MERGE_HEAD `ff5c177e8775bf615eeadc3be06a3fb6e2a57338`, merge-base `0d9ef6ce260c31d90d0d71dc82ace22ef85c7a8f`. Read worker-guide, AGENTS, 07 issue/effort spec/acceptance, owning application/API contracts and merger reproducer. No staging/commit/merge/reset/abort, parent/other-tree edits, watcher changes or later-ticket scope. Existing merger records retained; CHANGELOG worktree text preserves both integrated entries and adds only this bugfix, while its index remains UU for the parent.

### Decision / bounded behavior

- Keep `limit` 1–10 and aligned `offset` 0–1000 unchanged. Never clamp a cursor to 1000 (invalid for limits 3/7/9), increase the offset budget, fetch an extra page or discard extra returned candidates to manufacture a valid cursor.
- `next_offset` remains offset+limit only when it is a legal continuation with the same limit. A nonempty page indicating further source results beyond the allowed next offset omits that cursor and returns `truncated=true`. Source status stays available and original `total` is retained: bounded stop is **not** source exhaustion. Reaching source total does not mark truncated. No change to source failures, permissions, cancellation or proposal logic.
- Additive SDK-generated SourcePage schema exposes optional boolean `truncated`; API tool descriptions and owning `internal/application/SPEC.md` describe the same bounded continuation. No new budget, operation ID or proposal field.

### TDD and actual checks

Environment: `GOTOOLCHAIN=go1.26.10`, shared guide GOCACHE, this tree's `.tmp/merger07/go` and `.tmp/merger07/lint`. Logs below live in `.tmp/merger07/`.

- New independent file `internal/bootstrap/mcp_matching_pagination_test.go`, `TestMCPMatchingPagination`: production bootstrap + real SQLite + generic official MCP SDK client + deterministic external-source factory. Synchronization uses the existing delegated `library_wait_run`, no sleep/poll loop. No application-service replacement or private-method coupling.
- **RED before production changes:** `go test ./internal/bootstrap -run '^TestMCPMatchingPagination$' -count=1 -v -timeout=2m` failed on missing truncation and unreachable cursors 1010/1002/1001/1008, while normal pagination and total-tail cases passed (`pagination-red.log`). Fixture compile/type and metadata JSON key mistakes were corrected before this behavioral red run, not hidden as bug evidence.
- **GREEN:** same command passed all 13 subcases (`pagination-green.log`). Covers offset1000/limit10 (full and one-result pages), last legal aligned offsets999/3, 994/7, 999/9, penultimate pages followed using the actual returned cursor, normal two-page traversal, short total tail, total reached at offset1000/limit10 and at offset999/limit3. Asserts original total, exact candidate contents/count, unchanged per-page budget, no invalid cursor or false truncation at total, no prefetch (54 source calls), and that invalid offset/unaligned cursor/limit requests never reach sources.
- `go test ./internal/bootstrap ./internal/api -run 'TestMCP' -count=2 -timeout=5m`: **PASS**, bootstrap 27.294s/API 0.256s (`pagination-mcp-rounds.log`). Retains existing matching/source error, current authorization, cancellation, proposal, stdio and run-error privacy acceptance twice.
- `go test ./internal/bootstrap ./internal/application/delegated ./internal/metadata ./internal/proposal ./internal/application/runtime -count=1 -timeout=3m`: **PASS**, 17.366s/1.735s/3.864s/2.935s/2.956s (`pagination-related.log`). Includes all bootstrap tests, not just matching.
- `gofmt -l ./cmd ./internal`: empty (`pagination-gofmt.log`); `golangci-lint run`: **0 issues.** (`pagination-lint.log`).
- `go run ./cmd/tsgen`: 32 types, `git diff HEAD -- web/src/api/generated.ts` empty (`pagination-tsgen.log`). No SQL/UI/module changes; no sqlc/frontend rebuild needed.
- `git diff --check`: clean; CHANGELOG has no conflict markers. Watcher files/specs and watcher-gate-remediation remain byte-identical to MERGE_HEAD. No full backend rerun: independent merger already passed the combined backend once, and this narrow response/cursor change is covered by relevant packages and repeated MCP suites above. Earlier qualified watcher stability limitations remain unchanged.

### Remaining handoff

Pagination finding is addressed; independent review still belongs to parent. Status stays **ready-for-agent**, not done. Parent must finish the existing merge/index resolution, integrate and commit, then request_review. No new unresolved product/security decision. Search total remains the upstream report, not an independently verified catalogue completeness claim; existing temporary evidence lifetime and authorization checks still apply to every continuation.
