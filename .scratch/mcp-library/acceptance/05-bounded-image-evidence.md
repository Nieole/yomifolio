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
