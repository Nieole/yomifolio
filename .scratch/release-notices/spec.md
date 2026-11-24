# Binary distribution notices

## Goal

Before the next binary release, reconcile actual release payloads with their third-party license, notice, patent-text and covered-source obligations. Root Apache-2.0 licensing applies only to original Yomifolio code.

## Scope

- Native Linux AMD64, Windows AMD64 and Darwin ARM64 server builds, their embedded Web output and runtime inputs.
- Exact embedded-codec provenance and relevant source/notice delivery.
- A notice delivery mechanism clearly associated with each released binary.

The current collection and known gaps are recorded in `THIRD_PARTY_NOTICES.md`, `DEPENDENCY_MANIFEST.md` and `LICENSES/manifest.json`. Snapshot integrity does not establish release readiness. This work does not require publishing a tag or replacing codecs without a separate design decision.

## Delivery contract

Keep the three existing bare-server-binary names. The package workflow records per-target actual build metadata; the release workflow associates a same-version legal-text ZIP, downloads it through the artifact action, extracts it and verifies byte identity and local notice references before publication. The ZIP contains root LICENSE/NOTICE, THIRD_PARTY_NOTICES.md, DEPENDENCY_MANIFEST.md, the complete LICENSES tree, and referenced screenshot provenance instructions. It does not contain covered source. SHA256SUMS covers the legal-text ZIP as well as binaries and metadata.

Publication fails closed on legal and delivery defects: missing applicable license, copyright, NOTICE or PATENTS texts, undetermined license applicability for a distributed component, unverified MPL-2.0 covered-source access or modification delivery, a missing or unverified same-version notice archive, and mismatched target/version/binary/dependency metadata. Unverified upstream build provenance of the prebuilt AVIF/libyuv/WASI inputs and missing reproducible-build or attestation evidence are recorded as deferred non-blocking risks: the gate reports them and passes only when nothing else is outstanding. Build observations are review inputs, not automatic legal clearance.
