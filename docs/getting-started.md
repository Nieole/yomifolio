# Getting started and operating Yomifolio

[English home](../README.md) · [中文指南](getting-started.zh-CN.md)

## Download and first run

Prefer a [prebuilt release](https://github.com/Nieole/yomifolio/releases/latest) unless developing the project. Targets are Linux AMD64, Windows AMD64, and macOS ARM64. Select the actual asset for your OS/architecture, not GitHub's source-code archive. Older releases may still have `manga-manager` filenames; current source builds and release workflow outputs use `yomifolio`. This documentation and its screenshots describe current source; features in downloaded binaries depend on that release's notes. Do not construct download URLs for unpublished assets. If a release supplies SHA256 checksums, compare the downloaded file against them.

1. Create a writable working directory owned by the service account. Put the binary there. The service account needs read access to archives; metadata write-back and moving files to trash also need write access.
2. Linux/macOS: `chmod +x ./ACTUAL_FILENAME`, then `./ACTUAL_FILENAME`. PowerShell: `.\ACTUAL_DOWNLOADED_FILENAME`. Use the actual downloaded filename, including `.exe` on Windows; do not append another extension.
3. Open `http://localhost:8080` on the server and create the administrator **before public exposure**. For a headless host use a trusted LAN or SSH tunnel; your browser's `localhost` otherwise points to your own computer.
4. In Settings, add the absolute directory containing ZIP/CBZ or RAR/CBR archives, then scan. EPUB/PDF are not supported archive formats. The English/Simplified Chinese UI follows supported browser language preferences and can be switched in the UI.

The binary embeds the web UI. End users do not need Node, Go, a cloud database, or an LLM for basic reading. Optional scrapers, remote models, and remotely hosted cover images require network access.

## Paths and command-line options

Defaults are relative to the process **working directory**:

| Item | Default | How to change it |
| --- | --- | --- |
| Configuration | `config.yaml` | `-config` / `YOMIFOLIO_CONFIG` |
| Logs | `data/yomifolio.log` | Directory: `-data-dir` / `YOMIFOLIO_DATA_DIR` |
| SQLite database | `./data/manga.db` | `database.path` in YAML |
| Cache | `./data/cache` | `cache.dir` in YAML |

If configuration is absent, the server generates it. You may instead copy [config.example.yaml](../config.example.yaml) to `config.yaml` and edit it before launching; the template is not exhaustive. For loopback-only setup, set `server.host: 127.0.0.1`. `-data-dir` is **not** a global storage-root override. Prefer absolute database/cache paths under a service manager and explicitly set its working directory.

```text
ACTUAL_FILENAME -config /absolute/path/config.yaml -data-dir /absolute/path/logs
ACTUAL_FILENAME -version
```

Path precedence: explicit CLI flag > trimmed, nonempty `YOMIFOLIO_CONFIG` / `YOMIFOLIO_DATA_DIR` > corresponding legacy `MANGA_MANAGER_CONFIG` / `MANGA_MANAGER_DATA_DIR` > default. Whitespace-only environment values count as unset.

### Configuration updates

Settings and YAML feed the configuration manager. File watching debounces saves for 500 ms; unchanged content is skipped. Invalid reload values are rejected and the current runtime configuration is retained, so disk and runtime can differ until you correct and save the file. Not every field is live: consult Settings' field-effect descriptions or `GET /api/system/config` (`field_effects`) for restart/rebuild scope. Changing a database path does not move a database.

### Permissions and backups

Configuration contains plaintext model API keys and scraper credentials. Newly generated files and settings saves use `0600` on systems supporting POSIX permissions. Existing/manually copied files can retain broader permissions; on multi-user Unix hosts use `chmod 600 config.yaml`. On Windows restrict files and parent directories with NTFS ACLs.

SQLite contains bcrypt password hashes, sessions, and CSRF tokens; database creation follows the host's umask, not the config-file permission policy. Protect it and its directory separately, including backups. Before upgrading, stop the service and back up the database, configuration, and original archives (plus cache if desired); stopping avoids an inconsistent live SQLite copy. Thumbnails and browser caches are not backups of your books.

## Deployment and security

- The server defaults to **`0.0.0.0:8080`** and serves **HTTP, not HTTPS**. Use a firewall or loopback binding during setup. Before an account exists, protected Web APIs return `401`; this is **not** API pass-through. Public auth/setup endpoints and the health probe remain. Anyone reaching setup can attempt to become the first administrator.
- For Internet access, terminate TLS at a reverse proxy such as Caddy/Nginx and block direct backend access. Proxy the same origin, API paths, downloads, and long-lived `/api/events` SSE traffic; avoid buffering/short timeouts that interrupt events or downloads. There is no built-in TLS or official container deployment implied here.
- Set `server.trusted_proxies` only to actual proxy IP ranges (CIDRs). Forwarded client IPs are trusted only from those peers; this affects login/protocol rate limiting. Do not trust all networks just to make forwarded headers work.
- `server.cookie_secure: auto` sets Secure for direct TLS or a trusted proxy reporting `X-Forwarded-Proto: https`. Untrusted forwarded headers are ignored. With TLS termination, configure the actual trusted proxies or explicitly set `cookie_secure: always`; the latter does **not** fix client IP attribution. `never` is an HTTP escape hatch, not an Internet recommendation. Secure cookies will not work over ordinary HTTP.
- Web authentication uses HttpOnly session cookies, SameSite=Lax, bcrypt passwords, and session-bound CSRF tokens for mutations. Preserve `X-CSRF-Token` through the proxy. Do not disable CSRF to work around proxy mistakes. The retired shared API token is not an authentication shortcut.
- Prefer exact `server.allowed_origins` entries if cross-origin access is needed. Wildcards disable credentialed CORS; they do not make cross-origin cookie login work. Same-origin reverse proxying is the simplest default.
- Security headers include nosniff, DENY framing, no-referrer, Permissions-Policy, and Content-Security-Policy. Login/Basic-auth failures are rate limited per client IP with backoff. These safeguards are not a claim of a complete security audit.
- Administrator-created users must change their password on the web before protocol access: OPDS/Mihon return `401`, and their bound KOReader accounts return `403` until then. Administrators cannot delete themselves; the last administrator cannot be removed/demoted.

OPDS, Mihon, and KOReader are **all off by default**; KOReader self-registration is also off. Enable only what you need. HTTP Basic and sync keys need TLS over untrusted networks. See [client capabilities](features.md#clients-and-progress).

Offline books require Service Worker and Cache Storage, normally available on HTTPS or localhost. A phone accessing a LAN HTTP address is not localhost. Browser quota/eviction, site-data clearing, logout, and user changes can remove local books. Offline progress can queue and synchronize after reconnecting under the same account; local storage is not permanent backup.

## Upgrading from Manga Manager

Keep the existing working directory, database, configuration, and cache. Stop the old process before replacing its executable. The database remains `./data/manga.db`; OPDS `urn:manga-manager:*` identities remain stable. Existing library files need no rename.

- Legacy path environment variables remain fallbacks below Yomifolio variables and CLI flags.
- A validated legacy `mm_session` cookie transitions to `yomifolio_session` without changing identity/CSRF. A present new cookie never falls back to the old one, even if invalid. Logout revokes and clears both; the transition does not bypass authorization/password-change checks.
- Ordinary browser preferences prefer the new key and copy an old key only when missing; failed copies retain old data for retry.
- Offline indexes, page caches, unsynced progress, and owner records retain stable underlying storage names. There is **no** copy/migration or parallel data set. User isolation and logout cleanup remain unchanged.
- New logs go to `yomifolio.log`; old files are retained, not renamed or deleted by the rebrand.

## Routine operations

The task center shows runs, SSE progress, history, and controls supported by each task; pause/resume/cancel/retry are not universal. Resumable work can requeue after restart; interrupted destructive/billable work may need an explicit decision. Retention, throughput samples, run slots, and automatic backoff are configured under `tasks` in the example YAML.

Administrator log viewing supports levels and run/task filtering. Structured `slog` output uses Lumberjack rotation: 10 MB per file, five backups, 28-day retention, gzip compression. Diagnostics find empty books, missing covers/metadata, duplicate/missing identities, and unmatched KOReader records, with appropriate scan/scrape/identity/reconciliation actions.

`GET /api/health` returns service/database state; database ping failure gives `503`. This does not prove library disks or external providers are available.
