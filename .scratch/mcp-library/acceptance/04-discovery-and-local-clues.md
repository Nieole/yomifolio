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
