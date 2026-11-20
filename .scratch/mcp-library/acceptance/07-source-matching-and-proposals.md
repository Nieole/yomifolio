# 07 — source matching and proposals

Status: ready-for-agent; implemented delta is UNCOMMITTED, pending independent parent review/integration.

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
