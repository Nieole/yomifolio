---
id: mm-mcp-stdio
type: module-design
title: MCP stdio transport proxy
status: draft
parent: mm-architecture
references: [mm-api, mm-config, mm-bootstrap]
---

## Boundary

`mcp-proxy` is a standalone transport adapter to an already running service, not an application host. It does not load a configuration file, open a database, assemble or recover a runtime, or implement business tools. It relays JSON-RPC methods and upstream results without a tool catalogue, so new server tools need no proxy business changes. Authorization remains upstream on every request; no grant snapshot or business results are cached.

## Transport and secrets

The command accepts a service origin and appends `/mcp`. Origin validation uses `config.MCPConfig.Origin`: HTTPS, or explicit private-IP/localhost HTTP opt-in, matching the server's configuration restriction. The server additionally enforces its real-client transport boundary. TLS verification is enabled, redirects are refused, environment HTTP proxies are not used. Credentials come only from `YOMIFOLIO_MCP_TOKEN`, not argv or URLs; the variable is removed from the process environment after loading. Neither protocol bodies, upstream errors nor secrets are logged. stdout is exclusively newline-delimited JSON-RPC; local diagnostics go to stderr. Environment delivery does not protect secrets from privileged local process inspection; client launch configuration must be protected.

## Lifecycle and compatibility

The supported upstream is the production stateless Streamable HTTP JSON-response endpoint. The proxy forwards requests/notifications (not batches), propagates the negotiated protocol version, and emits no reply to notifications. It does not provide SSE, session resumption, server-initiated requests or OAuth. It has bounded concurrent requests, request-line and response sizes, and a per-request timeout; these are transport bounds, not application allowances. The tested generic client is the official Go SDK v1.8.0 using protocol 2025-11-25.

Worker admission never waits on the stdin reader: when all upstream slots are occupied, excess requests receive JSON-RPC error `-32000` (`MCP proxy overloaded`) without being forwarded, and excess notifications are dropped without a reply. There is no pending-work queue or goroutine per excess message. Clients must bound concurrency and must not rely on notification delivery under overload; the proxy does not retry rejected messages. This keeps EOF detection independent of upstream completion while retaining bounded memory and concurrency. As with ordinary responses, clients must consume protocol stdout.

EOF or process interruption cancels outstanding HTTP transport requests and releases connections; it never issues a business cancel command. Later requests can reconnect after a socket failure. Failed requests are not replayed because a lost response does not prove a business operation was unaccepted. Accepted long-running work belongs to the server run lifecycle, not this process; its disconnect/reconnect integration acceptance depends on the run capability and is tracked separately.

## Verification boundary

`bootstrap_test.TestMCPStdioProductionProxy` builds and launches the actual command, uses the generic SDK client, and connects to `bootstrap.Open` with real resources and web-issued credentials. It covers discovery, series reads, current restrictions and revocation, fresh-process and upstream-socket reconnection, malformed input, EOF, upstream failure and unsafe plaintext rejection. `TestMCPStdioSaturatedEOF` holds every upstream slot open, submits an excess request and closes stdin; it verifies prompt process exit, overload rejection, transport release and no extra upstream request (including business cancellation). Business protection remains owned by the application/server specifications.
