# Close embedded-codec provenance and binary notice delivery

Status: ready-for-agent

## Context

The Apache-2.0 root license and a conservative legal-text snapshot are present. The collection is not a complete audit or release clearance. The current release workflow uploads executables and checksums without the associated legal files. See the parent `../spec.md` and root `THIRD_PARTY_NOTICES.md` for scope and known limitations.

## Acceptance criteria

- Obtain and verify the actual build/source mapping for the AVIF WASM in `github.com/gen2brain/avif` v0.6.0, including the exact libyuv revision. A Makefile reference to `stable` and current-branch license downloads are not proof of payload provenance.
- Reconcile all three native release targets, Go/native/WASI runtimes and embedded Web assets with their actual distributed components. Resolve missing legal texts where applicable; justify exclusions with target/artifact evidence, not package metadata alone.
- Verify exact MPL covered-source availability and any local modifications; provide corresponding modified source if needed. Do not label a legal-text archive as a source archive.
- Deliver the applicable full legal texts, attribution, patent texts and source-access instructions with each binary or a clearly associated same-version notice archive. Exercise the actual download/extraction path and verify no notice references are dangling.
- Preserve byte-exact third-party texts, update source/version inventory and fingerprints, run `python scripts/check-license-notices.py` and its tests. Passing these checks is integrity evidence, not a substitute for the review above.
- Do not create or push a release tag as part of resolving this ticket unless separately requested. Any codec replacement or product behavior change needs its own approved design.
