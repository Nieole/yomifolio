# 03 stdio proxy acceptance

## Scope / reproduction

- Worktree `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-03`, branch `mcp-stdio-proxy`; HEAD and merge-base both `1c96d2fe08616e4d1ebe52a46ee7478fb0e8c1d7`. Integration HEAD was still that commit at the last read. All changes uncommitted.
- Build: `go build -o mcp-proxy ./cmd/mcp-proxy`. Client launches that executable with `--url https://service.example` and child environment `YOMIFOLIO_MCP_TOKEN` set to a web-issued independent credential. For private-IP/localhost HTTP, both server `mcp.allow_lan_http` and helper `--allow-lan-http` must be explicit. Never put the secret in argv/URL. README has the user-facing setup.
- Test seam: actual built subprocess, generic official Go SDK v1.8.0 `CommandTransport`, protocol `2025-11-25`, connected to the real `bootstrap.Open` HTTP host. Real temporary SQLite/libraries, web setup/issuance/narrowing/revocation. Child cwd is an unrelated empty temporary directory: no server config or database is supplied. No second runtime or alternate application host.
- Generic JSON-RPC forwarding has no tool registry/business implementation. Raw tool schemas/results and future method names pass through unchanged; application authorization always remains upstream.

## Executed verification

Environment: `GOTOOLCHAIN=go1.26.10`; shared `GOCACHE=C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-01/.tmp/merger01/gocache126`; own `GOTMPDIR=.../mcp-library-03/.tmp/proxy`, `GOLANGCI_LINT_CACHE=.../mcp-library-03/.tmp/lint`; ignored `web/dist` copied from integration, no UI changes.

- RED: `go test -p 1 ./internal/bootstrap -run '^TestMCPStdioProductionProxy$' -count=1` failed because `cmd/mcp-proxy` did not exist. Earlier first compilation exceeded an outer 180s timeout; the subsequent red test completed normally.
- GREEN: same production subprocess test passed after implementation. A reconnect smoke initially expected the first request on a forcibly closed socket always to succeed; it observed a clear transport failure. Assertion now allows that failure and requires the next request to reconnect, rather than implementing unsafe automatic replay.
- Final targeted repetition: `go test -p 1 ./internal/bootstrap -run '^TestMCPStdioProductionProxy$' -count=3 -timeout=5m -v` PASS (34.024s). Each iteration covers initialize/discovery/read, live capability narrowing/revocation, two fresh-process connections, forced upstream socket loss and reconnection, malformed JSON returning a JSON-RPC parse error, clean EOF exit, EOF while an upstream request is blocked (upstream context release observed), public plaintext rejection, unavailable upstream failure and process reaping, invalid credentials, and no token in diagnostics. The public-plaintext subprocess exits with empty stdout. Generic client parsing and raw malformed-input parsing guard protocol-only stdout.
- `go test -p 1 ./... -count=1 -timeout=10m`: first run FAILED in existing `internal/external` tests `TestTransferPausedWhileQueuedForQuotaDoesNotStartRead` (context canceled) and `TestPausedTransferRejectsChangedSourceOrStagingAndCleansUp/cancel` (pause boundary timeout). No baseline claim: no controlled comparison was run. Immediate complete rerun PASSED all packages, including bootstrap and external. Logs are ignored `.tmp/proxy/full-test.log` and `full-test-retry.log`. The additive in-flight EOF subtest was added afterward and passed the three final targeted repetitions; production code is unchanged from the full successful gate.
- `gofmt -l ./cmd ./internal`: empty.
- `golangci-lint run`: `0 issues.`
- `git diff --check`: PASS.
- No frontend/SQL/contract/dependency changes; no frontend gates or generation required.

## Compatibility / decisions

- Supported upstream is this service's stateless Streamable HTTP JSON endpoint. No SSE, session IDs/resumption, reverse server requests or OAuth. Tested generic protocol is 2025-11-25, not a product-specific configuration guarantee. Negotiated protocol version is propagated.
- Transport bounds: 64 KiB stdio lines (server request bound), 8 concurrent upstream requests, 8 MiB response ceiling, 60s per-request timeout. Long operations must return server run identifiers, not keep one helper request alive indefinitely. Bounds are not authorization/LLM allowances.
- Same `config.MCPConfig.Origin` policy as HTTP config; TLS verification on; redirects refused, environment HTTP proxies disabled to avoid accidentally forwarding credentials elsewhere. Actual server retains source-IP/Host checks. Token only from child environment, removed after loading, never logged; a privileged local user may still inspect process memory/environment or client configuration. Stdout contains only JSON-RPC; generic diagnostics omit upstream payload/error text and addresses.
- EOF ends transport only, closes upstream requests/connections and reaps normally. Lost responses are not proof of nonacceptance: proxy does not replay business operations or synthesize cancel commands.

## Deferred, not claimed complete

**Ticket 06 → ticket 14 integration** must accept a real long-running run over this helper, disconnect after acceptance, prove server work continues, reconnect and query the same run, and verify explicit authorized cancellation separately. 03's transport EOF and reconnect tests do not establish accepted-run lifecycle semantics; no such completion is claimed. The corresponding combined ticket checkbox remains open. No undecided product/security choice was introduced.

## Review fix rc_8faa939c — saturated admission / EOF

- Scope: only the reviewed transport lifecycle defect. Started with clean `mcp-stdio-proxy` HEAD and integration merge-base `34eb50dd836b49844b1b4578c4437ed0b43212d4`. All changes remain unstaged/uncommitted, in worker 03 only. Integration advanced during verification to `02ec9f453a6fb28535b30abb52e37d6fedf366c4`; parent must reconcile against that tip (no worker merge performed).
- Root cause: the stdin scanner waited for an upstream slot after reading the ninth request; with eight blocked upstream requests it could not scan EOF, so transport cancellation waited for the upstream timeout.
- Decision: nonblocking admission, no pending queue and no goroutine for excess messages. Keep eight active upstream workers and existing line/response/time bounds. Excess requests receive JSON-RPC `-32000`, `MCP proxy overloaded`, using their request ID; excess notifications are dropped without reply. Neither is forwarded. This avoids hiding an arbitrarily large backlog behind goroutines or buffering and makes EOF observation independent of upstream completion. Ordinary protocol stdout consumption remains a client obligation. Compatibility boundary: callers must limit concurrency; overload does not guarantee notification delivery, queueing, replay or automatic retry. Requests already forwarded retain the existing lost-response/non-replay semantics. EOF still cancels only HTTP transport, never sends business cancellation.
- Regression seam: `bootstrap_test.TestMCPStdioSaturatedEOF` builds and launches the real executable. A controlled HTTP transport peer acknowledges arrival of eight requests while holding all responses open, then the client writes request nine and closes stdin. Within three seconds the child must exit successfully, all eight upstream contexts must release, request nine must receive the overload error on protocol-only stdout, and no extra upstream request (including cancellation) may occur. Child cleanup always waits/reaps. This transport-only peer supplements, not replaces, the existing real production-bootstrap/client integration.

### Actual gates for this fix

Same Go 1.26.10/shared build cache/worker-local temp and lint cache setup as above. Ignored frontend embed assets already present; no frontend, SQL, generated contract or dependency changes.

- **RED before production change:** `go test -p 1 ./internal/bootstrap -run '^TestMCPStdioSaturatedEOF$' -count=1 -timeout=2m -v` failed in 6.343s: `saturated admission prevented prompt EOF exit`. The test killed and reaped the stuck original child rather than waiting for its 60s upstream timeout. Evidence: `.tmp/proxy/rc_8faa939c-red.log`.
- **GREEN:** both real subprocess suites passed three repetitions after the fix. Final command: `go test -p 1 ./internal/bootstrap -run '^TestMCPStdio(SaturatedEOF|ProductionProxy)$' -count=3 -timeout=5m`, PASS 19.925s. Covers production initialize/discovery/read, credential narrowing/revocation, socket and process reconnect, malformed input, ordinary/in-flight/saturated EOF, protocol stdout, unavailable upstream, unsafe plaintext and secret-safe diagnostics. Evidence: `.tmp/proxy/rc_8faa939c-final-subprocess.log` (initial verbose green also retained).
- **Key security:** `go test -p 1 ./internal/bootstrap ./internal/config ./internal/application/delegated -run 'TestMCP|Test.*(Authoriz|Credential|Revok|Password)' -count=1 -timeout=5m -v`, PASS for bootstrap/config, including account/password management isolation, delegated series lifecycle, unsafe transport matrix and explicit private plaintext configuration. That filter matched no delegated-package tests; therefore separately ran `go test -p 1 ./internal/application/delegated -count=1 -timeout=5m`, PASS 1.486s (also covered by the full suite). Evidence: `.tmp/proxy/rc_8faa939c-security.log`, `rc_8faa939c-delegated.log`.
- **Complete backend:** `go test -p 1 ./... -count=1 -timeout=10m` with outer timeout 1800s, PASS all packages on this run; `internal/api` 412.810s, bootstrap 34.362s, external 17.606s. Evidence: `.tmp/proxy/rc_8faa939c-full.log`. No timeout, exclusions or retry needed.
- **Formatting:** `gofmt -l ./cmd ./internal`, empty.
- **Lint:** `golangci-lint run`, `0 issues.` Evidence: `.tmp/proxy/rc_8faa939c-lint.log`.
- **Diff hygiene:** `git diff --check`, PASS.

The 03 long-running accepted-run disconnect/reconnect acceptance remains OPEN for 06 → 14; this fix does not claim accepted-run survival or close that checkbox. Suggested subject: `fix(mcp-proxy): reject saturated admission without blocking EOF`.
