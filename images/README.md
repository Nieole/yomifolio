# Product screenshots and comic credits

The four `yomifolio-*.png` files are direct browser captures of the real application, using an isolated library of authorized comic samples. No business DOM, API responses, covers, or reader controls were substituted. The screenshot library is a fixture, **not a bundled or downloadable manga collection**.

## Views and provenance

| File | Content | Viewport / PNG pixels |
| --- | --- | --- |
| `yomifolio-library.png` | Four works, actual covers, saved reading progress | 1280 × 820 |
| `yomifolio-series.png` | Pepper&Carrot metadata and two episode archives | 1280 × 1100 |
| `yomifolio-reader-desktop.png` | Pepper&Carrot, paged double-page reading | 1440 × 1000 |
| `yomifolio-reader-mobile.png` | Pepper&Carrot, continuous vertical reading | 390 × 844 |

UI: English, Midnight theme, device scale factor 1. The mobile image uses Chrome touch emulation, not a physical phone. The browser produced the PNGs directly; they were not retouched, composited, or cropped afterward. Cover framing and reader scaling are the application's normal behavior. Capture machine clock: 2026-11-20 UTC; application source base: `3e8226cf`, with current `web/dist`.

Four real works produce five local CBZ samples. The fixture includes complete Pepper&Carrot episode stories, excerpts of the other independent comics, and a **cover-only sample** of the Japanese manga. Page counts reflect these samples, not the lengths of the published books. Summaries identify the samples; no ratings or publication history were invented. ComicInfo imports, cover generation, metadata edits, and progress all use the real scanner/application APIs. Directly placed archives appear as standalone books, not nested volumes.

## Artwork licenses and attribution

The application's [Apache-2.0 license](../LICENSE) does **not** relicense these comics. Reusing a screenshot must respect the licenses of every artwork visible in it, including attribution and applicable ShareAlike conditions. The credits below are part of the screenshot attribution; keep them with redistributed images. Licensors do not endorse Yomifolio.

### Pepper&Carrot — David Revoy

- Work: [Pepper&Carrot](https://www.peppercarrot.com/), by **David Revoy**. Artwork and scenario: David Revoy; English proofreading for Episode 1: Amireeti and Alex Gryson. Further episode credits appear on the linked source pages.
- License: [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/), confirmed on the [official license page](https://www.peppercarrot.com/en/license/index.html).
- Sources: [Episode 1: The Potion of Flight](https://www.peppercarrot.com/en/webcomic/ep01_Potion-of-Flight.html), [Episode 2: Rainbow Potions](https://www.peppercarrot.com/en/webcomic/ep02_Rainbow-potions.html), and David Revoy's [Book One cover artwork](https://www.peppercarrot.com/0_sources/0ther/artworks/low-res/2016-02-24_vertical-cover_remake_by-David-Revoy.jpg).
- Processing: official English low-resolution JPEG pages, in original story order. The first archive uses the official cover artwork followed by Episode 1's three story pages; the second uses Episode 2's two story pages. Episode title/credit sheets are not included in the reading samples; attribution is provided here. No panels were redrawn, recolored, or rearranged.

### ブラックジャックによろしく — 佐藤秀峰

**ブラックジャックによろしく 佐藤秀峰 / Give My Regards to Black Jack / SHUHO SATO**

- Source: the author's [Complete Edition release announcement](https://note.com/shuho_sato/n/n4a30c9db7916), using its [standalone Complete Edition 1 cover](https://assets.st-note.com/img/1719392768210-KcrhLzZSb1.jpg?width=1200).
- Permission: the author's [official secondary-use terms](https://www.densho810.com/free/index.html). This is a **custom permission, not a Creative Commons license**. It permits commercial and noncommercial reproduction, public transmission, and adaptation of this work, subject to its conditions. The sequel is excluded; electronic bookstores/Kindle have separate restrictions.
- Processing: the author-published cover JPEG was packaged unchanged into a one-image local CBZ. The complete volume was **not** downloaded or included. Official downloadable archives were unavailable during capture; the source is the author's own announcement, not a scan of a printed edition.
- **Publication follow-up:** send the public URL, description of this README/screenshot use, and contact information to `info@densho810.com` **within one month after publication**, as required by the terms. Preparation of these files is not proof that this report has been sent. The repository maintainer must complete it when publishing.

### Coffee For You — SacredVisionsStudio / Avery Nakashima

- Work, author download, and license declaration: [Coffee For You](https://sacredvisionsstudio.itch.io/coffeeforyou), by **SacredVisionsStudio / Avery Nakashima**.
- License: [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/).
- Processing: pages 1–9 of the author's 10-page PDF, including the real front cover, rendered as full-page 1200-pixel-wide images with MuPDF and converted to JPEG using FFmpeg. Original page order and artwork are preserved; this is an excerpt, not a complete edition.

### Les aventures inutiles de Superflu — Gee

- Work and publisher download: [Les aventures inutiles de Superflu](https://editions.ptilouk.net/superflu/), by **Gee**, Ptilouk Éditions; [source PDF](https://editions.ptilouk.net/files/les_aventures_inutiles_de_superflu_-_gee.pdf).
- License: [CC BY-SA 2.0 France](https://creativecommons.org/licenses/by-sa/2.0/fr/).
- Processing: pages 1–9 of the publisher's 51-page PDF, including the real front cover, rendered and converted as above. Original page order and artwork are preserved; this is an excerpt.

## Reproduce with your own authorized samples

Prerequisites: Windows, Node.js 24+ (native WebSocket/fetch), Python 3 (standard library), Go, and Chrome. Prepare current `web/dist` with the normal frontend build. PDF rendering tools are needed only when preparing PDF-sourced images; **the application imports CBZ images, not PDFs**.

Download artwork from the credited sources and comply with its current terms. Create a local JSON manifest; image paths are relative to that manifest. Each series supplies `title`, `author`, `language`, `summary`, `tags`, and `books`; each book supplies `title`, optional `number`, and an ordered `pages` array. Set `focus: true` on the series to show in the detail/reader captures. For example:

```json
[
  {
    "title": "Your authorized sample",
    "author": "Actual author",
    "language": "en",
    "summary": "Accurate description and sample scope.",
    "tags": ["Fantasy"],
    "focus": true,
    "books": [
      {"title": "Episode 1", "number": 1, "pages": ["cover.jpg", "page-01.jpg"]}
    ]
  }
]
```

From the repository root, with Node, Python, and Go on PATH:

```powershell
$env:CHROME_PATH = 'C:/Program Files/Google/Chrome/Application/chrome.exe' # default
node scripts/capture-demo.mjs .tmp/authorized-comics/capture-manifest.json
```

The manifest and source downloads are deliberately untracked. The script refuses occupied ports or an existing `.tmp/yomifolio-demo` directory. For another run, move **that demo directory only** to an unused name under `.tmp`. It overwrites the four named screenshots; it does not delete or reuse an existing library.

The script sets explicit database, cache, configuration, logs, executable, and browser-profile paths under `.tmp/yomifolio-demo`; `-data-dir` alone would isolate only logs. Server and Chrome debugging bind to loopback (`18080` and `19222`); integrations remain disabled. Browser requests outside the local origin are blocked, apart from data/blob URLs. No normal configuration, private library, or existing browser is used.

The real scan and cover tasks must complete before capture; visible images must load. Both child processes stop in `finally`. Local evidence remains in `.tmp/yomifolio-demo/{capture,tasks}.json` and `process.log`. The recorded run processed five archives and generated five covers with zero failures. All four images were opened and visually inspected. Reproduction is not promised to be byte-identical across browser versions, fonts, or reading-state timing.
