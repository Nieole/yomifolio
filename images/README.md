# Yomifolio product screenshots

These four PNGs are **real browser captures of the current application**, not UI mockups or the README proposal. Captured on **2026-11-16 (UTC, capture machine clock)** from source revision `a9a37b99` with the existing, up-to-date `web/dist`.

| File | View | CSS viewport / PNG pixels | Size |
| --- | --- | --- | --- |
| `yomifolio-library.png` | Library browsing, 12 fictional series | 1440 × 1000 | 381,731 bytes |
| `yomifolio-series.png` | The Lantern Atlas: metadata, progress and three books | 1440 × 1000 | 500,755 bytes |
| `yomifolio-reader-desktop.png` | Fit-screen, paged, double-page reading | 1440 × 1000 | 463,144 bytes |
| `yomifolio-reader-mobile.png` | Touch viewport, single-column continuous reading | 390 × 844 | 181,210 bytes |

Device scale factor is 1. The mobile image uses Chrome touch/device emulation, not a physical phone. UI language is English and the application theme is Midnight. Reader toolbars are the application's own controls. Screenshots are direct CDP PNG output, without retouching, compositing, cropping or substituted business DOM/API responses. This provenance statement applies to the four files above; no third-party comic artwork is included in this screenshot set.

## Original demonstration material

`scripts/demo-art.mjs` draws original geometric comic illustrations with browser Canvas: a lantern-lit city, bridges, a scarf-wearing traveler, night skies, ink-like architectural lines and narrative panels. Series vary in palette, skyline, celestial motifs and character position; the three featured covers also vary by book. Each archive contains a cover and six illustrated story pages, with fictional narration/dialogue. This is deliberately stylized procedural comic art, not a reproduction of published manga.

The twelve fictional works are:

- The Lantern Atlas
- Cloudline Courier
- The Quiet Observatory
- Moss & Mercury
- Tideglass Station
- Paper Moon Orchard
- The Copper Heron
- Winter Signal
- A Garden Above
- The Last Tram Home
- Inkstone Harbor
- Small Hours Society

“Rowan Vale”, “Paper Lantern Studio”, the stories and their metadata are fictional demo identities. There are 17 CBZ books in total; The Lantern Atlas has three. Archives are placed directly under each series directory, so the real UI correctly labels them standalone books, not nested volumes. ComicInfo.xml is imported by the real scanner. The featured summary, tags, rating, author and reading progress are saved through authenticated application APIs, not injected into the screenshot.

These are project-original demonstration assets; no real manga IP, personal reading library, personal account or personal configuration was used. No external image/font downloads, scraper, LLM or AI image-upscaling service was used. This note records provenance, not a separate license grant; consult the repository's licensing terms for reuse.

## Reproduce

Prerequisites: Windows, Node.js 24+ (native WebSocket/fetch), Python 3 (standard-library `zipfile` only), Go, and Chrome. No Playwright, Puppeteer, Pillow or additional npm package is needed. The captured run used Node 24.21.0, Go 1.27.4 and Chrome 157.0.8103.51. Prepare current `web/dist` using the project's normal frontend build if it is missing or stale; the script builds `./cmd/server` from current source.

From the repository root, in PowerShell with Node, Python and Go on PATH:

```powershell
$env:GOCACHE = Join-Path (Get-Location) '.gocache' # optional; an existing shared cache is also suitable
$env:CHROME_PATH = 'C:/Program Files/Google/Chrome/Application/chrome.exe' # default
node scripts/capture-demo.mjs
```

The script refuses occupied ports and an existing `.tmp/yomifolio-demo` directory. For another run, first move that **demo directory only** to an unused name under `.tmp`; it never deletes/reuses an existing library. The four screenshot destinations above are intentionally overwritten on success.

All runtime paths are explicit absolute paths beneath gitignored `.tmp/yomifolio-demo`: a new YAML configuration, SQLite database, cache, logs, generated artwork/CBZ library, server executable and independent Chrome profile. `-data-dir` alone does **not** isolate the database/cache; the script sets those separately. The server binds only `127.0.0.1:18080`, Chrome debugging only `127.0.0.1:19222`; protocols remain disabled. The browser blocks requests outside the local origin (data/blob images excepted) and disables background services/DNS resolution for outside hosts. The script does not run `cmd/sampledata`, touch normal application configuration or connect to an existing browser.

The foreground orchestrator starts the server and Chrome, creates a fresh demo administrator, imports CBZs through the library API, waits for scan and independent cover tasks to complete, sets real metadata/progress, then visits `/library/{id}`, `/series/{id}` and `/reader/{id}`. It waits for visible images to have nonzero natural dimensions, rather than relying on network-idle with SSE. Both child processes are stopped in `finally`; do not detach the orchestrator. Runtime evidence stays in `.tmp/yomifolio-demo/{capture,tasks}.json` and `process.log`. No demo credentials are needed outside this disposable fixture.

## Checks for this capture

- Real scan: 17 archives processed, zero failed; independent cover generation: 17 covers, zero failed; both tasks `completed`.
- All four final files were opened and visually inspected: populated UI/artwork, no error/login/loading screen or modal dialog.
- PNG dimensions match the table; every file is below 0.5 MiB. Server/debugging ports were closed after capture.
- Scripts pass `node --check`; only new screenshot/material documentation and reusable scripts were added.

SHA-256:

```text
34aedd89fa014f94a476db7d0889a3455ea570b952a9c8ba2d889e206d872597  yomifolio-library.png
0a136247ddac30c9e9eb429810a3738ab69976211c0dd67a369af08fbf68ee4e  yomifolio-series.png
490fe8bb295ce8498fd9229f76cd2f8adc4081ff663cb88372686e8954944205  yomifolio-reader-desktop.png
af56ab2e067983d14d3a869d8bf03a16dd3a88da350e47fb6d9d54be5637093b  yomifolio-reader-mobile.png
```

Reproduction follows the same process/content but is not promised to be byte-identical across browser versions, system fonts, scan scheduling or persisted reading progress.
