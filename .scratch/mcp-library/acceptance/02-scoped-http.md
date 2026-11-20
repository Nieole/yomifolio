# Ticket 02 acceptance

Worktree: `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-02`, branch `mcp-scoped-http`. HEAD and merge-base with `implement-mcp-library` were both `e0da1f3ad8243d63643f892e918187aac5c3cfa6`. All changes remain uncommitted; no parent tree or ticket 01 tree edits, commits, index operations, merges or review requests were made.

## Delivered boundaries

- `internal/application/delegated/service.go`: credential Issue/List/Narrow/Revoke, Authenticate, current Authorize, and minimal GetSeries use cases. Subject carries only user_id + credential_id, never a cached grant. Five capability strings are independent; an empty library/capability set means no corresponding access. Narrow cannot change user/name, add scope, or revive a revoked credential.
- `internal/application/delegated/sql.go`: real-user binding, SHA-256 token digest, current-account joins and transactional management. `schema_handwritten.sql` installs the credential table through normal migration. The plaintext secret has 256 random bits and is returned once, never listed or persisted.
- `internal/database/account_password.go`: MCP revocation occurs inside the existing password/session transaction. Real HTTP tests inject a failing revocation trigger and observe rollback through unchanged browser and MCP access, then observe successful change/reset invalidation.
- `internal/api/mcp.go`, Host dependencies and bootstrap: explicit application injection, no protocol DB access or duplicate runtime. `/mcp` is outside the Cookie auth tree; `/api/mcp/credentials` and `/{credentialId}` remain administrator Cookie/CSRF commands. Credentials cannot authenticate to or manage themselves through this tree.
- `SettingsMCPPage`, Settings/App routing, settings section key, locale catalogs and generated contracts: administrator issue/inspect/narrow/revoke interactions, one-time in-memory secret, authoritative mutation responses, unmount/user-scope response isolation. No server-config draft storage for credentials. Existing shared form, panel, dialog and navigation styling retained.
- Owning application/API/database/bootstrap/config/web specs and architecture updated; README deployment guidance, config template, CHANGELOG and dependency legal inventory updated. ADR 0016 remains draft.

## API / tool contracts

- `POST /api/mcp/credentials`: `{user_id,name,library_ids,capabilities}` → IssuedCredential (flat Credential + one-time token).
- `GET /api/mcp/credentials`: Credential array, no token/digest.
- `PATCH /api/mcp/credentials/{credentialId}`: same restrictions shape, subset only → authoritative Credential.
- `DELETE /api/mcp/credentials/{credentialId}`: durable/idempotent revocation → `{revoked:true}`.
- `POST /mcp`: static `Authorization: Bearer ymcp_…`, SDK initialize/tools/list/tools/call. `library_get_series({series_id})` returns `{id,library_id,name,title}` in structured and text tool content. Unknown/out-of-scope identifiers fail alike; no paths, user reading data, write, scan, model, cleanup or credential-management tools.
- Every HTTP request authenticates current credentials; each actual series read also authorizes its current library. Regular accounts cannot authorize any non-read capability, even when those flags were signed into a credential. Current account demotion, deletion, forced password change, scope narrowing and revocation are not cached.
- Future 06/08/12/13 entry points should accept a server-derived `delegated.Subject` and re-use `Service.Authorize` at their own admission/checkpoints. Persist the IDs, not a token or permission snapshot. Future transactional writes must add transaction-bound authorization rather than treating this read/checkpoint port as an atomic write guarantee. No future-ticket execution is implemented here.

## SDK and transport verification

The official `github.com/modelcontextprotocol/go-sdk` v1.8.0 source and README were available in the legitimate module cache. Dependencies were resolved by configured Go module tools. No blocked web fetch, SSRF bypass, proxy/safety-setting change or alternate runtime was used. SDK README and actual StreamableHTTPOptions/client initialization source were inspected.

Production bootstrap tests use the generic SDK client, explicitly request and assert protocol `2025-11-25`, initialize, discover the sole tool and call real SQLite-backed series over both HTTPS and explicitly enabled loopback HTTP. Credentials persist across reopening the production graph. These observations do not claim compatibility with every SDK-supported protocol or any particular agent product.

Stateless Streamable HTTP with JSON responses is intentional: no server-initiated requests, persistent MCP session, SSE resumption or OAuth flow. GET/DELETE do not establish sessions. This is static independent Bearer authentication, not an implementation of MCP OAuth authorization.

`mcp.public_url` defaults empty/disabled. HTTPS is required publicly; LAN HTTP requires explicit opt-in, private/loopback literal host (or localhost) and private/loopback client IP. A canonical Host and matching optional Origin are checked; duplicate Origin is rejected. Only explicitly trusted peers can assert external TLS or client IP; trusted LAN proxies without a client IP are rejected. SDK localhost-only host checking is replaced by this stricter canonical-authority gate to support configured HTTPS proxies. Cookie Secure overrides do not influence MCP TLS eligibility. Body sizes are bounded; SDK payload logging is not enabled, management responses are no-store, errors are generic and existing request metrics log paths rather than tokens/query strings. Proxy operators must preserve Host, strip client forwarding headers and avoid secret/body logging. LAN interception remains an explicit deployment risk.

## TDD and actual verification

Red/green observations:

1. Initial production lifecycle test failed at credential issuance with HTTP 404. Application management, persistence, explicit bootstrap injection and the SDK adapter then made initialize/list/call/revoke pass.
2. Transport tests exposed duplicate Origin and missing LAN proxy client identity as HTTP 401 instead of required 403. Both guards were added and the production boundary tests passed.
3. Config tests rejected the missing MCP value checks and existing field-timing coverage failed for the new fields. Origin validation and immediate publication scopes made them pass.
4. Settings interaction tests initially could not resolve the new page. The page, generated types and navigation then passed issuance/secret dismissal, authoritative narrowing/revocation and conflict preservation tests. Supplemental real-SQLite public Authorize tests cover every capability independently, role demotion and account deletion without inventing future write/paid tools.

Final successful commands (Go uses `GOTOOLCHAIN=go1.26.10`, shared content-addressed GOCACHE and this tree's GOTMPDIR/lint cache):

- `go test -p 1 ./... -count=1 -timeout=10m`: **PASS**, including API, bootstrap, database, delegated authority, existing protocol/password/concurrency and shutdown regressions. Final log: `.tmp/backend-tests-confirmed.log`.
- `gofmt -l ./cmd ./internal`: empty. `golangci-lint run`: **0 issues**.
- Node v26.13.1/npm 11.20.0, `npm test -- --maxWorkers=2`: **120 files / 1146 tests passed**. `npm run build`, `npm run lint`: **PASS**. Log: `.tmp/frontend-tests-workers2.log`.
- `sqlc generate` via PowerShell in this worktree: exit 0, no generated Go binding drift (new DDL is handwritten-schema-only).
- `go run ./cmd/tsgen`: generated 32 types; repeat generation had identical SHA-256.
- `python scripts/check-license-notices.py`: **388 legal files verified**; existing eight collection gaps are unchanged. New module-cache legal texts and dependency-input hashes recorded.
- `git diff --check`: PASS. `scripts/check-doc-style.sh` eventually printed its pass result in `.tmp/doc-style.log`; its initial outer 300-second invocation timed out while the Windows subprocess continued, so this is not presented as a fast check.

Honest transient failures, not suppressed or weakened:

- Two unrestricted frontend suite runs had varying existing layout/library/router tests fail at asynchronous UI waits. Those cases passed in isolation; the complete unchanged assertion set passed with two workers. No test timeout/assertion was weakened and no project-wide test config was changed.
- One intermediate complete backend repeat failed the existing `TestRelocationPolicySQLFailureCompensatesAndAllowsNewConfirmation` at its prepared-operation wait. The exact test passed three consecutive isolated repeats, and the subsequent complete backend run passed. Logs remain in `.tmp/backend-tests-final.log` and `.tmp/backend-tests-confirmed.log`.

## Mockups and handoff

Self-contained, inline-CSS, no-script/no-control HTML mockups (in this tree, ready for parent to copy/display):

- `.thinkrail/context/mockups/mcp-02-before.html`
- `.thinkrail/context/mockups/mcp-02-after.html`

Visual tokens reuse the existing dark settings palette (#090c12, #181c27, #111827, #8b5cf6, #9ca3af), Inter/Segoe UI typography, left-aligned settings navigation, shared rounded panels and visible authorization warnings. These are mockups, not claimed browser screenshots.

All ticket acceptance items have verification evidence. Tracker remains ready-for-agent until the parent owns commit/review/integration; no done/claimed/resolved state was set by this implementation agent. No product/security decision requires reopening. Deferred functionality is only the explicitly out-of-ticket stdio/series-discovery/evidence/scanning/writing/model/cleanup work. Suggested commit subject: `feat(mcp): add scoped HTTP credentials and series access`.
