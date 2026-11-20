# Yomifolio

**Self-hosted manga & comic server**

[![CI](https://github.com/Nieole/yomifolio/actions/workflows/ci.yml/badge.svg)](https://github.com/Nieole/yomifolio/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/Nieole/yomifolio)](https://github.com/Nieole/yomifolio/releases/latest)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

English · [简体中文](README.zh-CN.md) | [Download](https://github.com/Nieole/yomifolio/releases/latest) · [Quick start](#quick-start) · [Documentation](#documentation)

Turn your local manga, comics, and artbook archives into a browsable library. Read in your browser, save books for offline reading, or connect through OPDS, the Mihon API, and KOReader sync—with different capabilities for each client.

![Yomifolio library browser with series covers and reading progress](images/yomifolio-library.png)

Your collection, with real covers and a place to continue reading. Screenshots feature **Pepper&Carrot**, **Give My Regards to Black Jack**, **Coffee For You**, and **Superflu**, used under their respective public permissions. [Comic credits, licenses, and screenshot details](images/README.md). Comics are not bundled with the server.

## Why Yomifolio?

- **Keep your collection local.** Scan ZIP/CBZ and RAR/CBR files into an embedded SQLite index. Basic library browsing and reading need no cloud database or LLM.
- **Find your next read.** Search series and books, build collections and reading lists, and review metadata suggestions before applying them.
- **Read your way.** Switch between paged, double-page, and vertical reading on desktop or mobile. Download books into this browser for offline use, or enable a supported client integration.
- **Share a server, not your reading history.** Each account has its own progress, bookmarks, comments, and reading statistics; administrators manage the shared library.

## Quick start

1. Download the binary for your platform from [Releases](https://github.com/Nieole/yomifolio/releases/latest): Linux AMD64, Windows AMD64, or macOS ARM64. Use the **actual asset filename** on the release page: older releases may still use `manga-manager` names; current source builds and release workflow outputs use `yomifolio`. This documentation and its screenshots describe current source; for downloaded binaries, consult that release’s notes for available features.
2. Put it in a writable working directory. On Linux/macOS, make the downloaded file executable (`chmod +x ./ACTUAL_FILENAME`), then run `./ACTUAL_FILENAME`; in PowerShell, run `.\ACTUAL_DOWNLOADED_FILENAME`. These are placeholders—use the actual downloaded filename, including `.exe` on Windows.
3. On the server, open [http://localhost:8080](http://localhost:8080), create the first administrator, add an absolute library path in Settings, and scan. Use archives you are authorized to store and read. The web interface supports English and Simplified Chinese.

> **Set up locally or on a trusted network before exposing the server.** It listens on `0.0.0.0:8080` by default and has no built-in HTTPS. Protected Web APIs reject access before setup; the public setup endpoint can still be claimed by anyone who can reach it. For Internet access, use a TLS reverse proxy and configure trusted proxies / secure cookies. See [deployment and security](docs/getting-started.md#deployment-and-security).

The default `config.yaml` is created in the **working directory**, not inside `data/`. `-data-dir` controls **logs only**; database and cache locations come from configuration (defaults: `./data/manga.db`, `./data/cache`). See [paths, configuration, and upgrades](docs/getting-started.md).

## From a folder of archives to your next read

### Find a book, then give it a place

- **Search across libraries** by series or book name and title; narrow the library view with author, tag, rating, publication status, and reading-progress filters.
- **Organize beyond folders:** curate collections, save smart filters, turn their current matches into a collection snapshot, or build an ordered reading list.
- **Keep a series together:** view books, authors, tags, descriptions, and progress in one place. Link prequels, sequels, and spinoffs, then explore relationships in a graph.

![Pepper&Carrot series details in Yomifolio, with real artwork, author, tags, and reading progress](images/yomifolio-series.png)

### Read comfortably on desktop or phone

- **Choose your rhythm:** page turns, double-page spreads, or continuous Webtoon scrolling; left-to-right or right-to-left in paged mode.
- **Make the page fit:** width/height/screen fitting, paged zoom and pan, themes, warm eye-comfort overlay, automatic border cropping, and image resampling.
- **Keep your place:** bookmarks with notes, saved progress, and whole-book downloads to the current browser. Offline progress queues for synchronization when you reconnect.

<p>
  <img src="images/yomifolio-reader-desktop.png" width="74%" alt="Desktop reader showing real Pepper&amp;Carrot pages in a double-page spread">
  <img src="images/yomifolio-reader-mobile.png" width="24%" alt="Phone-sized reader showing Pepper&amp;Carrot in continuous vertical mode">
</p>

Offline reading requires Service Worker / Cache Storage support, normally HTTPS or localhost. Browser storage limits and eviction apply; logout or user changes can clear local books. These downloads are not permanent backups. The phone view is a real touch-emulated browser capture, not a physical-device photograph. [Artwork attribution and capture details](images/README.md).

### Improve metadata without losing editorial control

Connect optional sources such as Bangumi, AniList, MangaDex, MyAnimeList, and Comic Vine. Compare suggested fields with current values, inspect sources and confidence, then apply or reject proposals individually or in batches. Lock fields you want to preserve.

Optional LLM assistance can suggest metadata, themed collections, and recommendations based on reading preferences. Suggestions remain subject to review; basic library use needs no AI service. Embedded `ComicInfo.xml` can be imported, and edited metadata can be written back to **ZIP/CBZ** archives; RAR/CBR write-back is not supported.

### Share the library, keep personal reading records

Administrators manage the shared collection and accounts. Each reader keeps their own progress, bookmarks, series comments, reading time, streaks, and period statistics. Accounts separate reading records, **not private library partitions**.

### Keep a growing collection manageable

- **Ingest at your pace:** choose scan profiles and optional folder watching; tune background IO for SSD, HDD, or network storage rather than competing with reading.
- **Review duplicates and missing files:** identity scans support content-based duplicate detection; selected originals can be moved to trash instead of permanently deleted. Scan cleanup has missing-root safeguards, but still needs backups.
- **Follow background work:** inspect task progress and logs, review failures, and retry supported jobs. External-library comparison helps identify and transfer missing books; supported library-path relocation preserves identities after you prepare the destination files.

For detailed behavior and limits, see [library, reading, and maintenance features](docs/features.md).

## Clients and integrations

| Entry point | What it provides | Setup and limits |
| --- | --- | --- |
| Web reader | Browsing, paged/vertical reading, per-user progress and bookmarks, browser-local offline books | Session cookie + CSRF. Offline support requires Service Worker / Cache Storage, normally HTTPS or localhost. |
| OPDS 1.2 | Catalog navigation, archive acquisition, and PSE page streaming | **Off by default.** Enable in settings; site username/password via HTTP Basic. Client support varies; do not assume progress write-back. Catalog: `/opds/v1.2/`. |
| Mihon API + companion source | Series/books, filters, collections, reading lists; companion source supports browsing, reading, and downloads | **Off by default.** HTTP Basic, `/api/mihon/v1`. See the [companion extension guide](extensions/mihon/README.md) for build/install and release prerequisites. Not an upstream-store extension; the standalone source does **not** provide page-level two-way sync. |
| KOReader | kosync-compatible device progress storage and matching to library books | **Off by default**, as is self-registration. Separate per-account sync keys; bind an account to a site user and match book identities before expecting site progress updates. Default base path: `/koreader`. |

Accounts created by an administrator must first change their password in the web UI before using any reading protocol. Arbitrary third-party readers do not automatically write progress back.

### Optional services and image enhancement

Online metadata sources and remote LLM endpoints may send library metadata to external services; some sources require API credentials. Local Ollama is also supported. Enable only the providers you trust.

For low-resolution pages, optional **waifu2x / Real-CUGAN** processing runs on demand while reading. It requires separately installed executables and suitable Vulkan hardware/drivers; it is not required for normal reading. See [provider requirements and optional tools](docs/features.md).

## Documentation

- [Getting started, deployment, configuration, and upgrades](docs/getting-started.md)
- [Library management, reader features, clients, and optional tools](docs/features.md)
- [Building, testing, and development](docs/development.md)
- [Example configuration](config.example.yaml) · [Changelog](CHANGELOG.md)

## Contributing and feedback

Read [CONTRIBUTING.md](CONTRIBUTING.md) for contribution and safe reporting guidance. For bugs and feature requests, [open an issue](https://github.com/Nieole/yomifolio/issues/new/choose) with the version, platform, steps to reproduce, and sanitized logs. Do not post credentials or private library files. Report security issues using the [private-channel guidance](SECURITY.md).

## License

[Apache License 2.0](LICENSE) · Yomifolio contributors. Third-party components retain their own licenses; see [notices and outstanding release checks](THIRD_PARTY_NOTICES.md). The code license does not grant rights to third-party comics or artwork.

### Scoped MCP access

Administrators can issue, inspect, narrow and revoke independent credentials at **Settings → MCP access**. Each credential belongs to a real account and explicitly selected libraries and capabilities; an empty library selection grants no access. Regular users remain read-only, without management or paid-model permissions. Password changes and resets revoke that account's MCP credentials. Save the secret at issuance: it cannot be viewed again.

Set `mcp.public_url` to the service's canonical HTTPS origin, then connect a Streamable HTTP MCP client to `<origin>/mcp` using `Authorization: Bearer <secret>`. This is static credential authentication, **not OAuth**; there is no authorization-server discovery or interactive authorization flow. `library_get_series` with `{ "series_id": 123 }` returns identity fields from an authorized library. Scan-control credentials can use `library_start_scan` (`library_id`) and `library_get_run`, `library_wait_run`, `library_cancel_run` (`run_id`), plus paginated `library_list_runs`. Runs are private to their credential and current restrictions. Waits are capped at 30 seconds; timeout/disconnect does not cancel accepted work. Revocation stops queued work before dispatch and running work at safe checkpoints, without undoing committed changes. Scan preparation does not launch separate cover/hash jobs. Retrying a start request is not idempotent: only an existing queued task may coalesce. Restart retains interrupted history but does not automatically replay delegated work. Metadata-write, model and cleanup tools are not implemented. The official Go SDK v1.8.0 client has been exercised with protocol `2025-11-25`; product-specific clients and other versions are not compatibility guarantees.

For LAN plaintext, explicitly set `mcp.allow_lan_http: true` and use a literal private/loopback IP (or localhost) in the HTTP origin. Only private/loopback clients are accepted. **Anyone intercepting LAN traffic can steal the credential.** Public HTTP and plaintext DNS hostnames are rejected. HTTPS reverse proxies must preserve the configured Host, strip untrusted forwarding headers, set accurate protocol/client-IP headers, and be narrowly listed under `server.trusted_proxies`; otherwise forwarded HTTPS is not trusted. LAN HTTP via a trusted proxy requires a private client IP header. Do not publish the backend around the proxy, put credentials in URLs, or log Authorization headers, request/response bodies or issuance responses in proxies/debug tooling. Cookie secure overrides do not enable MCP plaintext.
