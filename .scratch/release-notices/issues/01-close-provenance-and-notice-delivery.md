# Close embedded-codec provenance and binary notice delivery

Status: ready-for-agent

## Context

The Apache-2.0 root license and a conservative legal-text snapshot are present. The collection is not a complete audit or release clearance. The current release workflow prepares, re-downloads and verifies a same-version legal-text archive before publication. See the parent `../spec.md` and root `THIRD_PARTY_NOTICES.md` for scope and known limitations.

## Acceptance criteria

- Deliver the applicable full legal texts, attribution, patent texts and source-access instructions with each binary or a clearly associated same-version notice archive. Exercise the actual download/extraction path and verify no notice references are dangling.
- Reconcile the distributed components (Go/native/WASI runtimes, embedded Web assets, NPM packages) with the collection; resolve missing legal texts where applicable and justify exclusions with target/artifact evidence, not package metadata alone. Undetermined license applicability for a component that is actually distributed blocks publication.
- Verify exact MPL covered-source availability and any local modifications; provide corresponding modified source if needed. Do not label a legal-text archive as a source archive.
- Preserve byte-exact third-party texts, update source/version inventory and fingerprints, run `python scripts/check-license-notices.py` and its tests. Passing these checks is integrity evidence, not a substitute for the review above.
- Do not create or push a release tag as part of resolving this ticket unless separately requested. Any codec replacement or product behavior change needs its own approved design.

## Follow-up: upstream provenance (not a release gate)

- Map the AVIF v0.6.0 prebuilt WASM to its exact upstream libavif/AOM/dav1d/libyuv revisions and WASI SDK/compiler/runtime inputs, and verify the legal texts it implies. A `stable` Makefile reference and current-branch license downloads are not payload provenance. This investigation remains open and is tracked as a deferred non-blocking risk in `LICENSES/release-review.json`; recording it is not evidence that provenance is verified.
- Record reproducible-build or attestation evidence that ties published binaries to the recorded source inputs and toolchain. Also deferred and non-blocking; do not describe it as verified while it is outstanding.
