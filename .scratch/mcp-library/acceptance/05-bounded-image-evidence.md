# 05 — bounded image evidence acceptance

## Worktree and scope

- Dedicated tree: `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-05`, branch `mcp-image-evidence`.
- Initial HEAD and merge-base both `0d9ef6ce260c31d90d0d71dc82ace22ef85c7a8f`; final feature HEAD remains that commit, all delta uncommitted/unstaged.
- Integration branch advanced during implementation: observed `ff5c177e8775bf615eeadc3be06a3fb6e2a57338` at final check. Parent must integrate/review against that frontier; this worker did not merge or edit the parent tree.
- Read worker guide, AGENTS, effort spec, ticket, tracker/doc guidance, TDD/MCP-builder skills, GLOSSARY and relevant architecture/application/API/bootstrap/BookContent/parser/images specs. Confirmed acceptance seam was already approved by the effort spec: real production bootstrap, temporary SQLite/PNG/CBZ, generic SDK clients and actual stdio process; public BookContent lifetime checks supplement it.
- No 07 matching/proposal implementation, frontend changes, runtime duplication, schema changes, new modules, watcher fixes or Windows gate remediation.

## Delivered interface and budgets

- `library_get_series_cover({series_id})`: existing persisted series cover, selected by the catalogue's first covered book order (sort_number/name, stable ID tie-break). No automatic page-one substitute for a missing cover.
- `library_get_page_thumbnails({series_id,book_id,pages})`: exactly 1–3 distinct positive one-based selected pages. Explicit series/book/library consistency and current read authority are required. No cursor, original image, archive, arbitrary path or whole-book mode.
- Both return one SDK `image` content block per thumbnail, actual JPEG bytes/MIME, plus structured IDs and ordered page/width/height metadata. SDK v1.8.0 `ImageContent.Data` takes raw `[]byte`; the SDK performs base64 encoding. Tests decode the actual generic-client bytes, not a fabricated response.
- Fixed 384-pixel longest edge, aspect preserved/no upscale, JPEG quality 65; 96 KiB compressed per image. Application counts base64/JSON and reserves 4 KiB within 256 KiB. Adapter verifies serialized result and final HTTP output is capped at 256 KiB including caller ID/framing; overflow gives a short HTTP 413 rather than a partial image JSON.
- Separate source safety: 16 MiB selected decompressed entry (declared and actual), 16 million pixels and 16384 maximum source edge; two context-cancellable whole-evidence slots before input materialization. These are not the existing ordinary parser's 256 MiB safety ceiling and not a promise that three individually valid images always fit a response.
- Engineering rationale: three small visual clues are sufficient for identity comparison without exposing reading-resolution pages. Fixed quality/dimensions prevent arbitrary resolution requests. Cache-root confinement (`os.OpenRoot`) rejects traversal/symlink escape; errors are sanitized. No product/security decision was left undecided.
- Existing BookContent library admission/version retry, volume IO and parser borrows remain the resource owners. Strict evidence processing never falls back to raw bytes. The image adapter explicitly joins the outer HTTP context with SDK tool context, so disconnect/cancel while blocked on IO releases library admission. This does not change accepted background-run disconnect semantics.
- Reusable seams: `delegated.SeriesCover` / `PageThumbnails` complete application use cases; `bookcontent.Service.Evidence` identity-based materialization; `images.EvidenceThumbnail` strict JPEG generation; `parser.ReadPageLimited` caller-sized decompression. Future cover writes must still follow their own write authority/transaction/publication contracts, not reuse read prechecks as fencing.

## Red → green evidence

Local detailed logs are in ignored `.tmp/05/`.

1. `red.log`: production generic HTTP client failed `unknown tool library_get_page_thumbnails` before tools/application/content implementation. `green1.log` passed after implementation (the fixture was moved after existing catalogue traversal so it did not alter that test's expected books).
2. `red2.log`: oversized selected entry produced only generic unavailable text, failing the required actionable budget result. Mapped the parser's limited-entry sentinel to the evidence budget; `green2.log` passed HTTP/TLS and actual stdio contract checks.
3. `red3.log`: raw public HTTP protocol request with a valid long JSON-RPC ID and two otherwise valid noisy thumbnails produced **265692 bytes / HTTP 200**. Added bounded final HTTP output; `related2.log` passed including short 413 and no partial image result.
4. `cancellation.log`: real generic-client cancellation and actual HTTP socket disconnect, with production IO permits held, left content read admission occupied (10-second bounded observation). This exposed SDK stateless tool context not being tied to the HTTP request. Explicit image-request context joining fixed it; `cancellation-green.log` passed both modes over HTTP and TLS while the IO gate remained blocked. No blind sleep was used as completion proof: tests observe public `Engine.LibraryConflicts` and borrow counts.

## Actual checks

Environment: `GOTOOLCHAIN=go1.26.10`; shared guide GOCACHE, per-tree GOTMPDIR/lint cache. Copied ignored integration `web/dist` solely to satisfy Go embed; no UI rebuild was needed.

- **One full backend run**: `go test -p 1 ./... -count=1 -timeout=10m` — **PASS**, all packages, `.tmp/05/full.log`. Included `internal/api` (316.297s), bootstrap (58.565s), scanner, parser publication/rename and BookContent handle regressions. No full-suite failures occurred in this run; no watcher/Windows issue is claimed fixed or diagnosed here. This run preceded the later additional cancellation regression and its image-context fix; no second full-suite retry was performed.
- **Final changed-domain rerun after the cancellation fix and final content/page assertions**: `go test -p 1 ./internal/bootstrap ./internal/bookcontent ./internal/parser ./internal/images ./internal/application/delegated -count=1 -timeout=10m` — **PASS** (`final-related2.log`). Bootstrap 17.417s; includes real HTTP/TLS image calls, actual built stdio proxy, full image content/size/metadata, page bounds/count/duplicates, invalid/missing objects, credential narrowing, cross-library and series checks, corrupt input, large selected entry, decode bomb, aggregate budget and long-ID wire cap. Existing content/publication/rename regression packages were not weakened.
- Supplemental public `TestEvidenceReturnsAdmissionAndBorrowOnEveryExit`: successful page/cover, out-of-range, pre-cancel, cancellation at admitted read, missing source, wrong library, invalid/missing cover, no upscale; validates no active archive borrow/library reader and successful rename after eviction.
- `gofmt -l ./cmd ./internal` — **empty**.
- `golangci-lint run` — **0 issues**, including final rerun (`final-lint2.log`).
- `go run ./cmd/tsgen` — generated 32 types; **no generated.ts diff**. SQL bindings/schema unchanged; no sqlc generation required.
- `git diff --check` — **PASS**.
- Optional `bash scripts/check-doc-style.sh` — **not completed**: timed out at 180s, log reports MSYS `dofork ... 0xC0000142` / `fork: Resource temporarily unavailable`. Not called a baseline defect and not retried. New Go files have no oversized file headers; spec frontmatter/graph edges unchanged.

## Remaining gate / handoff

- Implementation and targeted acceptance are green. Parent review/integration and the final combined-tree backend gate remain, especially because integration advanced and the one full run preceded the last cancellation-specific fix. Ticket remains `ready-for-agent`, not done; its final combined-gate checkbox is intentionally left open.
- Optional whole-repository doc-style script remains uncompleted due to the reported process-resource error; mandatory Go format/lint are green. No frontend gate applies (no frontend source changed).
- Synchronous image decoder instructions cannot be interrupted mid-call; limited source dimensions/bytes plus two slots bound this work, and cancellation is observed at processing boundaries. There is no cross-request thumbnail-count quota: repeated requests can only obtain the same low-resolution JPEG evidence, never original book bytes.
- Suggested commit subject for parent: `feat(mcp): expose bounded cover and page image evidence`.

## Narrow RAR resource-budget maintenance (after 4a22b209)

This section supersedes the earlier worktree/frontier snapshot for this maintenance only. Work is in the same dedicated tree, HEAD `4a22b209`, with the pre-existing `ff5c177e` merge still in progress (`CHANGELOG.md` index remains UU although its text is resolved). No stage/commit/merge/abort/reset or parent-tree edit was performed. Parent reported frontier `b3083e4b` including 07; this worker did not integrate it. Ticket remains ready-for-agent; the final combined gate stays unchecked. This is not completion or a retry of the previously platform-interrupted broad review.

### Defect and fix

- Existing `.tmp/merger05/rar-probe.log` records approximately 64 MiB allocated by ordinary `GetPages` before selected-entry rejection. This historical record was read, not rerun. No new exceptional archive, payload or risk probe was made.
- `parser.GetPagesLimited` now serves evidence manifests; both it and `ReadPageLimited` create RAR readers through one helper with **16 MiB `MaxDictionarySize` passed before initialization**. Installed `rardecode/v2 v2.4.6` already supplies this option; dependency versions are unchanged. Ordinary `GetPages`, page session/cache and metadata scanning retain their original defaults.
- The helper supplies a context-aware filesystem at reader construction, covering initial headers and each volume's reads. Enumeration checks context between entries. Skipped entries are explicitly drained through a context/byte-limited discard reader, avoiding the dependency's hidden solid-archive `Next` drain; no skipped bytes enter the ordinary page cache. Dictionary overflow at creation/Next/read is normalized to the existing byte-budget sentinel. BookContent maps manifest and selected-page budget errors to `ErrEvidenceLimit`; no ordinary/raw download fallback.
- Conservative evidence-only limitation: all RAR skips (including non-solid entries, since the exported header does not expose archive-wide solid state) are drained with per-entry declared/actual limits. Consequently an unselected oversized RAR entry can reject evidence, and manifest work can decode intervening/all entries. This bounds retained buffers/dictionaries, **not total archive traversal CPU or a hard wall-clock deadline**. Ordinary large-book reading/scanning is untouched. Existing source bytes, pixels, two slots, response limits and access owners remain unchanged.

### Deterministic regressions and checks

Logs: ignored `.tmp/merger05/budget-*.log`; toolchain/cache configuration from existing `env.sh`.

- `budget-red.log`: new parser regressions first failed to compile for missing bounded-manifest/helper API. Green tests use committed normal `testdata/vol.cbr`, injected reader creation errors, and small in-memory readers only. No archive generation was needed.
- `TestEvidenceRarReadersShareCreationBudget` inspects applied **real dependency options** at both opens, validates identical fixed dictionary and cancellable filesystem settings, normal fixture manifest/page bytes, no ordinary-session cache/cursor pollution, unchanged ordinary manifest/page/metadata reader path, and explicit budget classification.
- Cancellation regressions cover pre-cancel, cancellation during enumeration, volume open/read and explicit skip drain, plus actual-vs-declared skip-byte overflow. BookContent regression cancels precisely at manifest admission using the normal fixture, checks cancellation takes priority over page range, and verifies library/borrow release.
- `go test -p 1 ./internal/parser ./internal/bookcontent ./internal/images ./internal/application/delegated ./internal/bootstrap -count=1 -timeout=10m` — PASS (`budget-related.log`, then final rerun `budget-related-final.log` after adding final regressions). These are complete related-package suites, including production MCP HTTP/TLS/stdio and resource-lifetime tests, not just named tests.
- `golangci-lint run` — 0 issues (`budget-lint-final.log`); `gofmt -l ./cmd ./internal` — empty (`budget-gofmt-final.log`).
- `go run ./cmd/tsgen` — generated 32 types, no generated.ts diff (`budget-tsgen.log`); `git diff --check` — PASS (`budget-diffcheck.log`). No SQL or frontend source changes.
- Existing pre-maintenance `.tmp/merger05/full.exit` is `0`; that historical full run is not a post-fix full gate. No additional full-suite rerun was attempted. Latest combined-tree gate and parent integration remain open.
