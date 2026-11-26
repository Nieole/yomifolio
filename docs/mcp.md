# Configuring MCP access

[English home](../README.md) · [中文](mcp.zh-CN.md) · [Features and clients](features.md) · [Deployment and security](getting-started.md)

MCP lets external AI clients read and edit the library through an **independent restricted credential**: capabilities and libraries are authorized per request on the server, and browser cookies are never agent credentials. It is a static Bearer credential over Streamable HTTP, **not OAuth** — there is no authorization-server discovery or interactive authorization flow.

This page covers setup order, the tools each capability unlocks, the identification main loop and troubleshooting; the security boundary lives in [features](features.md) and the "Scoped MCP access" section of the root `README.md`.

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
- The four capabilities are independent: `read`, `metadata_write`, `scan_control`, `cleanup`. An older credential that still carries `internal_llm` keeps its other capabilities; `internal_llm` itself grants nothing, is no longer shown, and is dropped the next time the credential is narrowed.
- The secret is shown **once at issuance**; save it then, otherwise issue a new one.
- Any account can be delegated; the credential works under that account's current permissions and restrictions. Every capability except `read` also needs the account to be an administrator, so a regular user's credential can only read and report.
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

A client only sees the tools its credential currently allows, and the initialization instructions describe exactly those tools.

| Capability | Tools |
| --- | --- |
| `read` | `library_find_series`, `library_get_series`, `library_list_books`, `library_get_book_clues`, `library_get_series_cover`, `library_get_page_thumbnails`, `library_identification_progress`, `library_get_identification_images`, `library_get_series_authors`, `library_find_duplicate_series`, `library_count_tags`, `library_report_series`, `library_get_metadata_operation`, `library_list_metadata_operations` |
| `metadata_write` | `library_claim_series`, `library_submit_verdicts`, `library_research_series`, `library_replace_identification`, `library_correct_series_field`, `library_correct_authors`, `library_improve_cover`, `library_add_series_relation`, `library_undo_metadata_operation` |
| `scan_control` | `library_start_scan` |
| `cleanup` | `library_start_metadata_cleanup`, `library_get_metadata_cleanup`, `library_cleanup_metadata_operation` |
| any capability | `library_get_run`, `library_wait_run`, `library_cancel_run`, `library_list_runs` |

Most tools work locally. These reach outside the machine: `library_research_series` searches the source sites; `library_get_identification_images` downloads candidate covers, only from each source's own image hosts; `library_claim_series` may queue a background refresh of stale evidence packets; `library_start_scan` chains an incremental evidence prefetch for the series the scan created. Searches are spaced per source by `identification.source_intervals_ms`.

Every successful result carries its JSON twice: as `structuredContent` and as a text block, so clients that read only text still get the whole result. Image tools put that JSON text block first, followed by the images in the order of its `images` list. Every tool description stays within 400 characters and ends with an `Example:` call you can copy.

Run tools only reach this credential's own runs and re-authorize against the capability that run belongs to. Every capability can create runs, so all four run tools are listed with any of them; calling `library_list_runs` still needs one of `scan_control`, `read` or `cleanup`, so a `metadata_write`-only credential follows its research runs by `run_id` instead.

Retired tools are neither listed nor answered: `library_search_sources`, `library_get_source_search`, `library_propose_match`, `library_get_match_proposal`, `library_get_enrichment_state`, `library_enrich_metadata`, `library_enrich_metadata_batch`, `library_assess_match` and `library_get_model_operation` get the protocol's unknown-tool error. Author correction and cover improvement live on as their own tools above.

## 5. The identification main loop

The agent's job is to say which source catalogue entry each series is. The server keeps that answer per series and source, derives an **identification status** from it (`unprocessed`, `identified`, `awaiting_owner`, `needs_recheck`, `not_found`, `owner_reviewed`), and fills the metadata itself.

Before the first claim, the server needs **evidence packets**: it searches the sources itself, with terms cleaned from the directory name and ComicInfo. Start **Prefetch identification evidence** from the library sidebar actions or **Settings → Maintenance**; it runs in the task center, can be paused or cancelled, and starting it again continues with the series not ready yet. After that, every scan prepares packets for the series it created.

With `metadata_write`, the loop is:

1. `library_claim_series` hands out up to 20 unprocessed series with their packets: local clues (directory name, book count, first file names, ComicInfo series, authors and year) and up to 3 candidates per source with checkable match signals (`title_matches`, `author_matches`, `source_volumes`, `local_books`) rather than probabilities. The series are leased to this session for 30 minutes, so parallel sessions never get the same ones.
2. Decide each series from its packet and answer them all in one `library_submit_verdicts` call, each with a one-sentence reason: `identify` (cite `candidate_ids`, at most one per source; several sources at once are fine), `not_found`, or `escalate` with a category (`ambiguous_candidates`, `uncertain_edition`, `missing_from_sources` hand the series to the owner; `directory_grouping`, `suspected_duplicate`, `tag_problem`, `protected_value_doubt` only leave a note). Results come back series by series; a rejected one carries a `problem` code and a `message` with the next step. Retrying the same `request_key` replays the original results.
3. When no candidate fits, `library_research_series` searches again with your own term and merges the new candidates into the packet; then decide the series as above.

An empty claim says why in `empty_reason`: `all_processed` means stop; `prefetch_pending` or `all_claimed` means retry later. `library_identification_progress` counts the series per status and how many are ready to claim. The credential is the owner's permission: an agent should write directly rather than ask a person first.

What happens after `identify`: the server fills **empty** fields only, from the identified sources in a configurable priority (Bangumi first, with its Chinese title), computes a missing rating from every source's score, appends source links and tags that pass a vote threshold, blocklist and per-series limit, replaces a title that is still the directory name, and closes the series' pending proposals. Covers do not change. Every fill leaves a receipt (`operation_id`) that `library_undo_metadata_operation` reverses.

Outside the loop:

- **Corrections** (`metadata_write`): `library_replace_identification` replaces an agent's own wrong identification and takes back what it filled, unchanged since; `library_correct_series_field` fixes a title, summary, publisher, status or rating a source supplied and nobody changed since; `library_correct_authors` (read the evidence first with `library_get_series_authors`) and `library_improve_cover` fix authors and automatic covers. Every correction leaves an undoable receipt.
- **Protected values**: a person's edits, ComicInfo values, locked fields, manual covers and a person's identification are never changed by an agent. Report them instead: `protected_value_doubt` leaves a note; `disputed_owner_identification` (through `library_report_series`) sends the series to recheck.
- **Inspection** (`read`): `library_find_duplicate_series`, `library_count_tags` and `library_report_series` let a read-only credential inspect the library and leave notes without changing anything.
- **Relations**: `library_add_series_relation` adds a franchise relation such as a sequel between two series, with an undoable receipt; existing relations are never edited.

The owner handles escalations and rechecks on **Review center → Awaiting you**, and sees each series' identifications on its detail page.

## 6. Verify in order

1. Request `/mcp` without a credential → **401**. A 404 means `public_url` is empty or not applied; 502/503 means the proxy is not routing.
2. A wrong credential → **401**; public HTTP against a private-network setup → **403**.
3. List tools through the client: only the tools for the granted capabilities should appear, and they should disappear as soon as you narrow the credential.
4. Call `library_get_series` with `{"series_id": 1}`: it returns identity fields from an authorized library.
5. Try an unauthorized library → a uniform "delegated access denied" that ends with a next step (take IDs from this credential's own results such as `library_find_series`; if they already came from there, report instead of retrying). It deliberately reads the same for "missing" and "not allowed", so existence does not leak.
6. With `metadata_write`, the initialization instructions start with the main loop; `library_identification_progress` should show `prefetch.ready` above zero before the first claim.

## 7. Known limits

- Wait tools are capped at 30 seconds; a timeout or disconnect **does not cancel** accepted work — use `library_cancel_run`.
- `library_start_scan` is not idempotent: a queued scan may coalesce, otherwise another run is accepted.
- A claim holds at most 20 series and about 30 KiB; a series whose packet is not prefetched yet, or is older than 30 days and being refreshed, is not handed out. A lease lasts 30 minutes; after that another session's claim can take the series over.
- A submission is not atomic: each series succeeds or fails on its own.
- The server never accepts a match by itself, even when every signal agrees; each identification is an agent's or the owner's verdict.
- A note the owner has closed is not raised again for the same series and category.
- There is no internal model assistance over MCP. Allowance records from the retired `internal_llm` tools stay in the database without a way to read them.
- Restarting the service retains interrupted history but does not replay dispatched work; only the server's own incremental prefetch resumes.
- Exercised combinations are the official Go SDK v1.8.0 client with protocol `2025-11-25`; other clients and protocol versions are not compatibility guarantees.
- Behind an HTTPS reverse proxy: preserve Host, strip client forwarding headers, set accurate protocol and real client IP, and list only the proxy address in `server.trusted_proxies`. Never log credentials in URLs, proxy logs or debug captures.
