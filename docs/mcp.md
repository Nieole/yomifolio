# Configuring MCP access

[English home](../README.md) · [中文](mcp.zh-CN.md) · [Features and clients](features.md) · [Deployment and security](getting-started.md)

MCP lets external AI clients read and edit the library through an **independent restricted credential**: capabilities and libraries are authorized per request on the server, and browser cookies are never agent credentials. It is a static Bearer credential over Streamable HTTP, **not OAuth** — there is no authorization-server discovery or interactive authorization flow.

This page covers setup order and troubleshooting only; the security boundary and limits live in [features](features.md) and the "Scoped MCP access" section of the root `README.md`.

## Order of work

1. Configure `mcp.public_url` — in **Settings → MCP access → Server settings**, or in `config.yaml` (below).
2. Issue a credential in the same page.
3. Configure the client and confirm a read-only tool first.

The settings page repeats steps 1–3 as a collapsible guide: the derived `/mcp` endpoint, both client configurations below, and a button that opens this document without leaving the app.

## 1. Server: `public_url`

Set in **Settings → MCP access → Server settings** (takes effect immediately), or in `config.yaml` (in the **working directory**, not inside `data/`):

```yaml
mcp:
  public_url: "https://library.example.com"   # the origin clients actually connect to
  allow_lan_http: false
```

- Clients connect to `<origin of public_url>/mcp`; the `/mcp` path is appended by the server and by the stdio helper, so leave it out of `public_url`.
- Both fields take effect **immediately** (config hot reload); no restart needed.
- With an empty `public_url`, `/mcp` answers **404** — the most common reason a client cannot connect.
- The request **`Host` must equal `public_url`'s host, port included** (case-insensitive). A reverse proxy must preserve that authority verbatim: rewriting Host to `127.0.0.1:8080` makes every MCP request fail.
- With `https`, the server requires the request to actually be TLS — direct TLS, or a proxy listed in `server.trusted_proxies` reporting `https`.
- With `http`, you must also set `allow_lan_http: true`, and the host must be a literal `localhost` or private IP. **Local debugging needs that switch too**, and only private/loopback clients are accepted: anyone intercepting LAN traffic can steal the credential.

## 2. Issue a credential

**Settings → MCP access → Issue credential**: pick the account, name, libraries and capabilities.

- **An empty library selection is not "all libraries"**: it grants no access.
- The five capabilities are independent: `read`, `metadata_write`, `scan_control`, `internal_llm`, `cleanup`.
- The secret is shown **once at issuance**; save it then, otherwise issue a new one.
- Any account can be delegated; the credential works under that account's current permissions and restrictions, and a regular user gains neither management nor paid-model rights.
- Changing or resetting that account's password revokes all of its MCP credentials. Revocation stops queued work before dispatch and running work at a safe checkpoint, and **does not undo committed changes**.
- Narrowing capabilities or libraries also applies immediately: the next tool listing no longer shows the withdrawn tools. The listing only discloses what the credential may use; a call to a tool it omits is still refused with the same uniform denial, because every tool authorizes its own operation.

## 3. Client configuration

Field names differ per client (some use `type: "http"`, others `httpUrl` or `transport`). The requirements are always the same four: the URL points at `/mcp`; exactly one `Authorization: Bearer <secret>` header; never put the secret in a query string (requests with a query string are rejected outright); never send a browser cookie alongside it.

### Direct HTTP endpoint

```json
{
  "mcpServers": {
    "yomifolio": {
      "type": "http",
      "url": "https://library.example.com/mcp",
      "headers": { "Authorization": "Bearer paste-issued-secret" }
    }
  }
}
```

If the client sends `Origin`, it must equal `public_url`'s origin, or the request is refused.

### Clients that can only launch a subprocess (stdio)

A standalone helper is enough; it needs no database, config or library access and only talks to the running service:

```bash
go build -o mcp-proxy ./cmd/mcp-proxy
```

```json
{
  "mcpServers": {
    "yomifolio": {
      "command": "/opt/yomifolio/mcp-proxy",
      "args": ["--url", "https://library.example.com"],
      "env": { "YOMIFOLIO_MCP_TOKEN": "paste-issued-secret" }
    }
  }
}
```

- For private-network plaintext, add `--allow-lan-http` as well (matching the server option).
- The secret is read only from the environment, never from argv or a URL, and is removed from the process environment after loading. Protect the client's config/environment: environment delivery does not hide it from privileged local processes.
- Certificate verification cannot be disabled; redirects and environment HTTP proxies are not followed. The supported upstream is this service's stateless JSON-response endpoint, **not** arbitrary SSE/sessionful servers.
- EOF on stdin exits the helper without a business cancel; dropped connections are not replayed, so query accepted runs after reconnecting (a lost response does not mean a rejected operation).

## 4. Tools by capability

A client only sees the tools its credential currently allows.

| Capability | Tools |
| --- | --- |
| `read` | `library_get_series`, `library_find_series`, `library_list_books`, `library_get_book_clues`, `library_get_enrichment_state`, `library_get_metadata_operation`, `library_list_metadata_operations`, `library_get_series_cover`, `library_get_page_thumbnails`, `library_search_sources`, `library_get_source_search`, `library_get_match_proposal` |
| `metadata_write` | `library_enrich_metadata`, `library_enrich_metadata_batch`, `library_undo_metadata_operation`, `library_propose_match` |
| `scan_control` | `library_start_scan`, `library_list_runs`, `library_get_run`, `library_wait_run`, `library_cancel_run` |
| `internal_llm` | `library_assess_match`, `library_get_model_operation` |
| `cleanup` | `library_start_metadata_cleanup`, `library_get_metadata_cleanup`, `library_cleanup_metadata_operation` |

Source search leaves the machine: `library_search_sources` and `library_get_source_search` contact external source sites; every other tool works locally.

Run tools only reach this credential's own runs and re-authorize against the capability that run belongs to. Every capability can create runs, so `library_get_run`, `library_wait_run` and `library_cancel_run` are listed with any of them, while `library_list_runs` needs one of `scan_control`, `read` or `cleanup`.

## 5. Verify in order

1. Request `/mcp` without a credential → **401**. A 404 means `public_url` is empty or not applied; 502/503 means the proxy is not routing.
2. A wrong credential → **401**; public HTTP against a private-network setup → **403**.
3. List tools through the client: only the tools for the granted capabilities should appear, and they should disappear as soon as you narrow the credential.
4. Call `library_get_series` with `{"series_id": 1}`: it returns identity fields from an authorized library.
5. Try an unauthorized library → a uniform "delegated access denied", deliberately not distinguishing "missing" from "not allowed", so existence does not leak.

## 6. Known limits

- Wait tools are capped at 30 seconds; a timeout or disconnect **does not cancel** accepted work — use `library_cancel_run`.
- `library_start_scan` is not idempotent: a queued scan may coalesce, otherwise another run is accepted.
- `internal_llm` allowance is a **user-relayed budget**: admissions are consumed atomically before the request and are not refunded on failure, and it is not a hard monetary cap.
- Restarting the service retains interrupted history but does not replay dispatched work.
- Exercised combinations are the official Go SDK v1.8.0 client with protocol `2025-11-25`; other clients and protocol versions are not compatibility guarantees.
- Behind an HTTPS reverse proxy: preserve Host, strip client forwarding headers, set accurate protocol and real client IP, and list only the proxy address in `server.trusted_proxies`. Never log credentials in URLs, proxy logs or debug captures.
