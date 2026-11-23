# Bounded RAR evidence regression fixtures

These are generated test bytes, not manga or third-party payloads. Tests need
only Go/rardecode, not WinRAR or Python.

- `solid-compressed.cbr`: real RAR5 LZ compression, 1 MiB dictionary, global
  solid. The first entry `000.bin` is **not** file-solid and emits 24 MiB.
- `solid-undersized.cbr`: same compressed streams; first declared size is 1,
  first file CRC removed, block header CRC recomputed. It is intentionally
  dishonest, but the dependency exposes 1 byte and successfully reaches the
  next file by decoding the hidden remainder. `make-undersized.py` records the
  exact transformation, not an error-return mock.
- `nonsolid-compressed.cbr`: single-volume non-solid RAR5, `<ComicInfo/>` and
  `page.jpg` containing `b'page' * 512`; the latter is genuinely compressed.

Reproduction (WinRAR CLI, used here version 7.x): create
`.tmp/solidfix/input/000.bin` with Python
`random.Random(42).randbytes(65536) * 384`; write `ComicInfo.xml` containing
`<ComicInfo/>` and `page.jpg` containing `page`. In that directory run:

```
Rar.exe a -ma5 -m3 -md1m -s -ds -ep -idq ../solid.cbr 000.bin ComicInfo.xml page.jpg
```

`-ds` is essential: it preserves the large predecessor first. Then run
`python internal/parser/testdata/make-undersized.py` from the repository root.
For the non-solid fixture, place the small XML and repeated page in a separate
folder and run `Rar.exe a -ma5 -m3 -md1m -s- -ep -idq <destination> ComicInfo.xml page.jpg`.
Timestamps may differ; output semantics, not byte-for-byte regeneration, are
asserted. RAR4 strict-header cases are generated independently in Go tests;
these compressed fixtures do **not** claim coverage of every LZ/PPM codec.

The dependency call trace uses a real non-seeking counting filesystem and a
1024-byte buffer: the hostile `Read` exposes 1 byte, then `Next` consumes 1264
additional compressed bytes. The original counterpart proves 24 MiB output;
source tracing connects `limitedReader.nextFile` to `decodeReader.nextFile`'s
solid-only `io.Copy`. The bounded APIs reject both files at the global header,
before returning a file to the SDK, rather than trying to count hidden output.
