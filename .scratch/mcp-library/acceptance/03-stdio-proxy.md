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
