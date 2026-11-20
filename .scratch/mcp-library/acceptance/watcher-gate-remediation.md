# Windows watcher gate remediation

## Scope

User approved isolating existing gate fixes while continuing MCP implementation, then required all execution to use background subagents in this chat. The stopped independent chat is not the implementation source. Integrated commit: `5f55ae1e`.

The Windows-only production watch-source adapter closes command admission, drains event/error delivery while accepted Add/Remove calls finish, then closes the native source and waits for the pump to exit. Non-Windows production sources are unchanged. No dependency upgrade, abandoned goroutines or weakened tests.

## Evidence

Full outside-repository diagnosis: `C:/Users/saber/.thinkrail/mcp-library-gate-diagnosis.md`. Implementation evidence: `C:/Users/saber/.thinkrail/mcp-library-watcher-fix.md`.

- Worker: deterministic regression red then green; gate regressions and race each 30 repetitions passed; real Windows churn 90 repetitions passed; related packages, one complete backend run, formatting and lint passed.
- Independent pre-merge reviewer: inspected the installed fsnotify v1.10.1 Windows implementation and all six changed files; verdict Approve, code merge-ready. New regressions with `-race -count=30` passed; diff check passed.
- Independent real churn `-count=30` FAILED in 32.118s: two `watcher_registry_test.go:153` assertions, `filesystem producer remained blocked`, after Stop had already returned; one also had temporary-directory cleanup failure. The original Stop deadlock did not recur. An earlier combined command hit its 150s outer limit and is not passing evidence. Retained reviewer log: gate-fix worktree `.tmp/review-watcher/churn.log`.

## Limits

This fixes the confirmed Close/pending-command deadlock, not every existing gate failure. The independent producer-exit failures have not been attributed to the diff or the environment. Unknown Access denied, recovery windows and external-transfer deadlines remain unresolved. A successful worker full run is recorded honestly but does not establish suite stability. Overall gate remediation and final MCP acceptance remain open.
