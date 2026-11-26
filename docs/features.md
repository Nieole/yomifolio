# Features, clients, and optional tools

[Home](../README.md) · [中文指南](features.zh-CN.md) · [Deployment](getting-started.md)

## Local library and metadata

- ZIP/CBZ and RAR/CBR archives; embedded `ComicInfo.xml` import. Edited ComicInfo can be atomically written back to **ZIP/CBZ only**; RAR/CBR are read-only for write-back. No EPUB/PDF support is implied.
- Embedded SQLite (`modernc.org/sqlite`, whose driver itself is CGO-free). The application still requires CGO for WebP encoding when building from source.
- Generated 400-pixel-wide cover thumbnails, WebP by default; AVIF is an optional cover format, with JPEG fallback if cover processing fails. AVIF is not the reader's direct page-output format.
- Storage-aware IO policies for scan/archive/cover/hash concurrency, reducing background work while reading, idle-only heavy work, and same-disk page-cache controls. Tune for SSD/HDD/network storage rather than assuming more workers are always better.
- Scan profiles: `fast_scan`, `metadata_scan` (default), `identity_scan`, `repair_scan`. The profile determines archive/metadata/identity work; the default does not calculate full content hashes. Identity/repair scans provide hashes needed for content-based duplicate detection.
- Archive page extraction enforces a 256 MiB uncompressed-entry limit.
- Optional recursive `fsnotify` watching per library (`watch` scan mode), debounced incremental scanning, and missing-record cleanup. Cleanup aborts when the root is unavailable or more than 50% of series appear missing. These guards do not replace backups.
- Duplicate removal can remove index records alone or move originals to a trash directory beside the active database (default `data/trash`), rather than permanently deleting source files. Review selections first; trash is not an automatic backup policy.
- Library path relocation preserves identities for supported moves. Prepare the target files yourself: this does not move source files or infer arbitrary within-library rearrangements.

## Search and organization

Search covers **series/book names and titles**, across libraries. SQLite FTS5 trigram indexing accelerates longer queries; short-query gram indexing and substring fallbacks handle cases where trigram is unsuitable/unavailable. Results include covers and normalized relevance scores; OpenSearch is exposed through OPDS. Authors/tags are structured filters, not full-text targets.

Use manual collections, ordered reading lists with item progress, or smart filters based on rating, read progress/status, days since addition, tags, author, publication status, or initial. Smart-filter conditions are single-valued and combined with AND; freeze current matches into a static collection snapshot when needed.

Series relationships appear in series/library force-directed graphs; connected components also produce system “Franchise” collections. Large graphs are bounded (server approximately 4,000 relationships; frontend approximately 200 nodes), not complete unlimited visualizations. Tags support rename/merge/delete; authors currently support listing/search rather than equivalent management actions. Custom series fields are key/value pairs saved as a replacement set. Bulk editing supports tags, publication status/publisher, favorites, and read/unread state.

## Users and reading

Administrators manage the shared library/system; regular users browse and maintain their own reading data. Per-user progress, bookmarks with notes, series comments, and reading statistics include streaks, time, and period breakdowns. Initial administrator setup can adopt legacy global reading/activity data and orphaned KOReader accounts. User roles do not imply private per-user library partitions.

The web reader supports:

- Virtualized vertical Webtoon scrolling and paged reading, including double-page spreads. LTR/RTL affects paged mode; vertical mode remains vertical.
- Original, fit-width, fit-height, and fit-screen sizing; configurable page preload, next-book prefetch, and release of images outside the working window.
- Paged zoom/pan: pinch, double-tap 2.5×, Ctrl+wheel, dragging, up to 4×.
- Base/Comimi reader themes, warm eye-comfort overlay, automatic border cropping, and resampling filters.
- Session image-format/quality overrides, including WebP/JPEG delivery. AVIF is for cover thumbnails, not direct reading output.
- Whole-book offline download, cache management/deletion, and queued progress synchronization on reconnect. Cache Storage/Service Worker support and browser storage limits apply; logout/user changes preserve isolation by clearing the prior owner's local data. See [deployment requirements](getting-started.md#deployment-and-security).

## Clients and progress

All three protocol integrations default to **disabled**. Enable them separately; there is no universal cross-client sync guarantee.

| Integration | Address | Authentication | Scope |
| --- | --- | --- | --- |
| OPDS 1.2 | `/opds/v1.2/` | HTTP Basic, site username/password | Navigation/acquisition/PSE, continue/recent, collections, reading lists, smart collections, OpenSearch. Client support for acquisition/PSE varies; OPDS access alone does not guarantee progress write-back. |
| Mihon API | `/api/mihon/v1` | HTTP Basic, site username/password | Series/books, tag/author/status filtering, collections/lists, and book-progress POST. A matching adapter is necessary; no claim of an available upstream-store extension. |
| KOReader kosync | `/koreader` by default | Separate per-device-account sync key | Device progress storage, document matching, and reconciliation into the bound site user's progress when a book and page count are known. |

Complete web first-login password change before protocol use. Do not assume that a client using a downloaded archive reports progress to the server.

KOReader supports binary-hash and path-based document matching (`binary_hash` default; optional extension ignoring for path mode). Binary matching uses a full-file MD5 identity, **not** a quick sampled fingerprint. Path matching normalizes separators/case and uses the final three path components; it is not an arbitrary absolute-path equivalence guarantee. Generate missing book identities through the relevant identity/repair work, then use diagnostics/reconciliation for unmatched device records. Finding a book alone does not guarantee progress application if page counts or account ownership are unresolved.

Self-registration (`allow_registration`) is off by default. Administrators can create accounts, rotate keys, enable/disable/delete them, and bind them to site users. Self-registration attempts automatic binding only on a single-user site; multi-user ownership is not guessed. Use TLS for credentials and sync traffic outside a trusted network.

## Online metadata and AI (optional)

Basic scanning/reading does not need any provider. Online scrapers contact third-party services:

| Source | Requirement |
| --- | --- |
| Bangumi (default), AniList, MangaDex | No API key required by this integration |
| MyAnimeList | MAL Client ID configured in Settings |
| Comic Vine | API key configured in Settings |

HTTP sources use bounded exponential retry for 429/5xx and honor `Retry-After`. Availability, provider policies, and network reachability still apply.

LLM settings support Ollama and compatible OpenAI `/v1/responses` or `/v1/chat/completions` endpoints. Configure provider, endpoint, model, key, and timeout in Settings; a connection test includes SSRF checks. Ollama may be local; remote endpoints send prompts/library metadata outside the host and may incur charges. Configure only trusted providers and protect plaintext credentials.

Evidence prefetch can target all libraries or one library and runs in the task center: it searches every source, spaced per source, for unprocessed series without an evidence packet and stores the packets; it can be paused or cancelled, and starting it again continues with the series not ready yet. After every library scan (including agent-started ones) an incremental prefetch of that library prepares packets for the series the scan created, without touching the older backlog; it resumes after a restart. A claimed packet older than 30 days is not handed out but refreshed in the background first, and enabling MyAnimeList or Comic Vine reopens not-found series for that source only. In single-series manual search, picking an entry of a catalogue source records it as the owner's identification of the series and fills the series by the same policy as an agent's identification (empty fields only, configurable source priority, filtered tags); picking an entry you excluded before asks first and lifts the exclusion. LLM results do not point at a source entry, so they still become proposals: review them field by field, compare current/proposed values and confidence, then apply/reject individually or in batches. Repeated equivalent candidates for one series are deduplicated. **Review center → Awaiting you** collects the series agents escalated and the ones a new book contradicts: pick or exclude candidates, conclude not found, re-search, confirm a questioned identification, or mark the series owner-reviewed so identification leaves it alone. The series detail page lists each source's identification with who confirmed it and when, and can replace or withdraw it; the library filters include the identification status. Lock fields to prevent scraping changes; applied fields retain source, URL, confidence, and time. Bangumi supplies a clickable `bgm.tv` source link; other provider source URLs remain in proposals.

AI grouping considers up to roughly 50 series not already in collections and asks for 3–5 themed collections, subject to review. AI recommendations use reading-tag preferences and candidate series, with reasons and a language-specific 24-hour cache. Model output is a suggestion, not verified bibliographic truth. The review inbox summarizes metadata and grouping work without conflating the two kinds of proposals.

## Super-resolution (optional)

Install [waifu2x-ncnn-vulkan](https://github.com/nihui/waifu2x-ncnn-vulkan/releases) or [Real-CUGAN ncnn Vulkan](https://github.com/nihui/realcugan-ncnn-vulkan/releases) separately. These require suitable Vulkan-capable hardware/drivers and have their own licenses.

1. Download/extract the chosen engine's official release.
2. In Settings' processing-engine section, save the executable's **absolute path**. Path validation checks existence, not successful execution or GPU compatibility. Non-absolute configured paths are ignored at reading time in favor of PATH / `./bin` lookup.
3. Choose the engine in the reader filter selector, then scale (1/2/4/8), denoise, and output format (WebP/PNG/JPG).

Processing is on demand while reading, not a pre-baked rewrite of every page. Engines have bounded concurrency, separate caches (memory/disk), and Lanczos3 fallback on failure. Configuration can reload without restarting where supported; these tools are not required for normal reading.
