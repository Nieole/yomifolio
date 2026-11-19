# Release Linux race gate repair

## Diagnosis

Release 44450640155, Linux job 135313415523 failed in `TestBookNormalizedHTTPLifecycleRepairAndLongMetadata`: `HTTP rebuild did not complete a new run (run_id=1)`. Original downloaded evidence: `.tmp/ci-release-linux.log` (test 78.42s, API package approximately 1240s).

This is a test wait-budget failure, not evidence of a Go data race or a production deadlock. The exact test reproduced unchanged on Windows/amd64 with Go 1.27.4, CGO enabled and GCC available: same assertion, 73.57s. WSL is not installed; no host configuration was changed.

Ranked hypotheses were: (1) real SQLite index work exceeds the 15s helper deadline under race instrumentation; (2) terminal publication lags completed work; (3) stuck/failed production work is hidden by the old diagnostic. A temporary probe changed only the wait budget to 120s and logged the HTTP run at 15s and at termination. Two runs showed `running` without error at 15s, then the expected `failed` at 15.582s and 21.920s. The latter run's persisted StartedAt/FinishedAt differ by 21.887s: this is actual run duration, not a 15s terminal-publication backlog. CPU profiling attributed 18.12 sampled CPU seconds to the real `rebuildBookSearchIndexContext` SQL execution path. SQLite and race/checkptr work dominate the profile. No race report occurred. Temporary probe logging was removed.

The synthetic `/one` library and `/long.cbz` book paths are deliberate: maintenance repairs derived indexes first, then a forced scan of the nonexistent library correctly fails. The test must await a **new failed run**, then assert repaired search results. It must not expect completed or accept an old terminal run. Real-archive maintenance success and historical-completed/new-failed behavior remain covered separately.

## Change

`internal/api/series_normalized_search_test.go`: use a two-minute rebuild budget (more than 5x measured instrumented rebuild duration), capped at the test deadline minus one second. The budget includes pre-launch listing, launch and polling HTTP requests through an explicit context. Poll every 100ms instead of 10ms. Include elapsed time, expected terminal state, selected run ID and full last observed snapshot on unsuccessful waits, including HTTP timeout exits. Preserve exclusion of every pre-launch run ID, pinning to the first new run, and strict expected-terminal-state assertions.

No production semantics, workflow, release version, race flags, or user-visible behavior changed. No changelog or owning-spec contract update is needed. The already-existing exact failing HTTP test is the regression seam; no mock-based or tautological timeout-unit test was added.

## Exact red / green commands

All commands ran after:

```bash
cd 'C:/Users/saber/.thinkrail/worktrees/manga-manager/workspace-3'
```

Go test commands used this environment prefix:

```bash
GOCACHE='C:/Users/saber/AppData/Local/go-build' TEMP=C:/tmp TMP=C:/tmp GOTMPDIR=C:/tmp
```

- **Red (unchanged source):** `go test -race ./internal/api -run '^TestBookNormalizedHTTPLifecycleRepairAndLongMetadata$' -count=1 -v` — FAIL, 74.951s package; `.tmp/ci-repair-baseline.log`.
- **Probe:** same command after temporary budget/diagnostic instrumentation — PASS; `.tmp/ci-repair-probe.log` (47.35s test, 15.582s rebuild).
- **Profile probe:** `go test -race ./internal/api -run '^TestBookNormalizedHTTPLifecycleRepairAndLongMetadata$' -count=1 -v -cpuprofile=.tmp/ci-repair-cpu.pprof -o .tmp/ci-repair-api.test.exe` — PASS; `.tmp/ci-repair-profile.log` (78.99s test, 21.920s rebuild). Profile summaries: `.tmp/ci-repair-cpu-top.log`, `.tmp/ci-repair-cpu-rebuild.log`.
- **Green repeated (final helper):** `go test -race ./internal/api -run '^(TestBookNormalizedHTTPLifecycleRepairAndLongMetadata|TestSearchMaintenanceWaitIgnoresHistoricalTerminalHTTP)$' -count=3 -v` — PASS, 176.880s. Long-metadata test durations 37.43s / 62.09s / 67.21s; all three actual-HTTP historical/new-failure regressions pass. `.tmp/ci-repair-green-repeated.log`.
- **Relevant search suite:** `go test -race ./internal/api -run 'Test(BookNormalized|SeriesNormalized|GlobalRank|GlobalCursor|Search)' -count=1 -v` — PASS, 489.802s; 27 top-level tests pass, six existing opt-in acceptance tests skip by their own environment gates. Includes real-archive full maintenance, missing-library failure, historical-run regression, protocols, upgrade, rank, cursor and long-metadata checks. `.tmp/ci-repair-search-race.log`.
- `gofmt -l ./cmd ./internal` — no output; `.tmp/ci-repair-gofmt.log`.
- `git diff --check` — clean.
- Main-workspace lint binary was 2.12.2 built with Go 1.27.2 and could not decode Go 1.27.4 export data (exit 7), despite printing `0 issues.`; **not counted as a pass**. `.tmp/ci-repair-lint.log`.
- Installed local lint with the current compiler: `GOBIN='C:/Users/saber/.thinkrail/worktrees/manga-manager/workspace-3/.tmp/tools' go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0` (same cache/temp environment above). Verified version 2.15.0 built with Go 1.27.4. `.tmp/tools/golangci-lint.exe run` with `GOLANGCI_LINT_CACHE='C:/Users/saber/.thinkrail/worktrees/manga-manager/workspace-3/.tmp/ci-repair-lint-cache-v215'` and the same cache/temp environment — exit 0, `0 issues.`; `.tmp/ci-repair-lint-v215.log`.

## Limitations / handoff

No local Linux execution (WSL unavailable). Full `go test -race ./...` was not run: the selected API search tests already took approximately eight minutes, and the release API package took approximately twenty minutes. Linux release rerun remains the final platform confirmation. The opt-in large acceptance/browser/upgrade-matrix audits were not enabled. No separate forced-timeout subprocess regression was added; cancellation/deadline bounding is explicit in the helper and its HTTP requests. All changes remain uncommitted; no git index/history mutation was performed.
