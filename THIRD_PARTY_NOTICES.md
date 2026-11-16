# Yomifolio — third-party notices and review status

Prepared 2026-11-16 from the current Go module graph, NPM lockfile and locally installed packages. **This is a partial notice inventory, not a legal audit, SBOM, patent clearance, or assurance of compliance.** Resolve the limitations below before representing a release's notices as complete.

## Scope and accompanying files

The root `LICENSE` is Apache-2.0 for original Yomifolio code; it does **not** replace third-party licenses. Root `NOTICE` attributes only original Yomifolio contributions. Third-party copyright, permission, disclaimer, patent and attribution texts remain independently applicable and are reproduced in `LICENSES/`.

Distribute this file, `DEPENDENCY_MANIFEST.md`, root `LICENSE`, root `NOTICE`, and the **entire `LICENSES/` directory together with the server binary** (or in a clearly associated accompanying notice archive delivered with the same release). Do not ship this index without the referenced legal texts. The current workflow uploads only server binaries and checksums; it does not distribute this collection. Notice delivery must be addressed before a new binary release.

- [Dependency manifest](DEPENDENCY_MANIFEST.md): exact versions, source locations, collected texts and explicit collection gaps.
- [Machine-readable legal-file inventory](LICENSES/manifest.json): per-file sources, collection dates, SHA-256 values, package/lock metadata. This is not an SBOM or a proof of inclusion in any particular binary.
- `LICENSES/go/`: original legal files from available modules, including nested legal files outside tests/fixtures.
- `LICENSES/npm/`: installed packages' root legal files/legal directories. Four packages' legal-only README excerpts preserve the original bytes; the inventory identifies exact source byte ranges. No application/library source is reproduced here.
- `LICENSES/codec-upstream/`: additional codec legal texts retrieved directly from upstream; libyuv's current branch texts are explicitly **not** proven to match the embedded binary.
- `LICENSES/go-runtime/`: Go runtime LICENSE/PATENTS for local and CI Go 1.27.4. Superseded Go 1.26.10 texts have been removed. Use the actual release toolchain's texts when rebuilding.

This deliberately conservative collection includes build/test/optional dependencies that may not be shipped in the production Web bundle. Presence here does not establish runtime linkage; absence of a lockfile license marker does not establish absence of licensing obligations.

## Go libraries

The current direct dependencies and their reviewed license families are:

| License family | Modules / versions |
|---|---|
| MIT | disintegration/imaging 1.6.2; gen2brain/avif 0.6.0 (Go wrapper only); go-chi/chi/v5 5.5.0; go-chi/cors 1.2.2; mozillazg/go-pinyin 0.21.0; natefinch/lumberjack.v2 2.2.1 |
| BSD-3-Clause | chai2010/webp 1.4.0 (also see nested libwebp); fsnotify/fsnotify **1.10.1**; golang.org/x/crypto 0.59.0; x/image 0.48.0; x/sync 0.25.0; x/sys 0.50.0; modernc.org/sqlite 1.62.0 (wrapper; embedded components differ) |
| ISC | nfnt/resize 0.0.0-20180221191011-83c6a9932646 |
| BSD-2-Clause | nwaples/rardecode/v2 2.4.6; no unRAR restriction appears in this module's license |
| MPL-2.0 | hashicorp/golang-lru/v2 2.0.7 — see source-code access below |
| Per-file MIT / Apache-2.0 | yaml.v3 3.0.1 — retain both LICENSE and NOTICE; do not reduce this to a single MIT label |

Indirect dependencies, including ebitengine/purego 0.11.3 (Apache-2.0) and tetratelabs/wazero 1.12.0 (Apache-2.0 with NOTICE), have their own entries and verbatim texts in the manifest. The actual module graph, not this short table, is the version inventory.

### SQLite and libc

For modernc.org/sqlite 1.62.0 preserve `LICENSE` (wrapper BSD-3-Clause), `LICENSE-SQLITE` (SQLite public-domain statement), `LICENSE-SQLITE_VEC` (sqlite_vec MIT), and the newly supplied `LICENSE-3RD-PARTY.md` (upstream's flattened third-party inventory and full legal texts). The latter includes inherited notices and MPL-2.0/BSD text for golang-lru; retain it whole, without treating its upstream linkage classifications as verification of our release. SQLite's public-domain status does not make its wrappers or all embedded extensions public domain.

For modernc.org/libc 1.81.2 retain **both** `LICENSE` and `LICENSE-3RD-PARTY.md`. The latter contains musl and other third-party copyright and permission texts. Consult those original texts rather than treating libc's top-level license as the complete set. Other modernc modules' collected legal texts remain separate.

## Embedded image codecs and patents

### WebP

chai2010/webp 1.4.0 includes libwebp 1.4.0. Its wrapper license alone is insufficient. This collection preserves `internal/libwebp-1.4.0/COPYING`, `PATENTS` and `AUTHORS`. Retain those copyright/permission and patent-grant texts with the distribution. Patent-grant texts are not a patent-clearance opinion.

### AVIF / WASM

gen2brain/avif 0.6.0 normally embeds `lib/avif.wasm.gz`. The default embedded payload brings codec obligations even if a platform never invokes AVIF conversion. Its MIT Go-wrapper license does not supersede these components:

- `lib/LICENSE.aom`: Alliance for Open Media copyright and BSD-style terms.
- `lib/LICENSE.libavif`: libavif and included per-file notices, including dav1d-derived code, IJG-related text and Apache-2.0 text. Retained whole; this is not an assertion that every optional application/contrib file is present in the WASM.
- Additional upstream AOM v3.14.1 `PATENTS` and dav1d 1.5.3 `COPYING` have now been retrieved successfully and are included in `LICENSES/codec-upstream/`.
- Additional libyuv `stable` branch `LICENSE` and `PATENTS` were retrieved 2026-11-16. These preserve upstream texts **as retrieved**, not verified legal texts for a known libyuv revision inside this WASM.

Conservative IJG acknowledgment, retaining the wording required by the bundled libavif legal text: **"this software is based in part on the work of the Independent JPEG Group".** Inclusion is precautionary; no claim is made that the optional IJG-related application files are linked into the WASM.

The module's `lib/Makefile` names libavif v1.4.2, AOM v3.14.1, dav1d 1.5.3 and libyuv **stable**, without a libyuv commit. Build recipes are evidence of intended inputs, not proof of the exact revisions used to create a prebuilt payload. The reviewed module files have SHA-256 fingerprints `92ffd604d276359c2df829effc615cd9f286c36ece5761644929e4396eb97360` (`lib/avif.wasm.gz`), `52181752f8d92a43dfd0ca33179d32fdf7452a669cc1e260f37fabb8319de6bf` (`lib/avif.wasm`), and `1404fe10eb61f9733e967ff1ffea5f02bde13d17fd78038f237ba32346e5c3ec` (`lib/Makefile`). **The exact libyuv source revision and prebuilt-WASM build provenance remain unresolved.** A current stable-branch license fetch does not close that provenance gap. Obtain the upstream build/source mapping and verify the corresponding legal texts, including any toolchain/runtime notices, before declaring this codec collection complete.

## Web dependencies and build-time data

Current direct production packages are @xyflow/react, @yui540/comimi, @yui540/comimi-react, axios, date-fns, react, react-dom, react-router-dom and react-virtuoso (MIT), and lucide-react (ISC plus Feather-derived MIT text **in the same LICENSE**). Both lucide texts are retained. Exact locked versions and all collected transitive/development legal files are in the manifest. Newly added collected dependency locations have MIT, 0BSD or BlueOak-1.0.0 texts; the new @humanfs/types location is labeled Apache-2.0 by metadata but lacks a collected license file. No new MPL family beyond golang-lru and lightningcss was identified; this is not clearance of uncollected packages.

The actual lock contains **329 package records including the root, 328 dependency records**; 292 dependency directories are present in this Windows worktree and 36 optional/platform locations are absent. Six present directories lack package.json (listed in the manifest), so directory presence is not proof of a complete installation or the recorded version. No version mismatch was detected among the 286 locations with package.json. Legal bytes for the five directory-only locations that supplied texts were independently matched to their exact published NPM tarballs and lockfile integrity values; this does not verify the local installations. The inventory retains the lock's transitive @emnapi/core and @emnapi/runtime 2.0.0-beta.2 entries as recorded, without asserting that they are stable releases. Uninstalled optional/platform entries are marked explicitly and are not represented as collected. This is a local inventory, not an inventory of every platform's npm-ci result or of the minifier's emitted code. The combined inventory contains **373 package entries and 376 legal files**, including 44 Go modules, one Go runtime, root LICENSE/NOTICE and four upstream codec texts.

NPM transitive/build metadata also includes MPL-2.0 (lightningcss 1.33.0 and its platform bindings; no 1.32.0 remains), CC-BY-4.0 (caniuse-lite 1.0.30001826), Apache-2.0, BSD, ISC, 0BSD and BlueOak-1.0.0. CC0-1.0 and MIT-0 are no longer lockfile license labels in this snapshot. The lock's license fields contain no GPL/AGPL markers; that observation is **not** a whole-tree or embedded-code license clearance.

- lightningcss is a CSS build tool. Its MPL license does **not by itself** make generated application CSS MPL-covered. If redistributing the tool, native bindings, or covered source files, evaluate their own notices/source requirements separately.
- caniuse-lite's CC-BY-4.0 text is preserved. If distributing its data or adaptations, retain the package's attribution/source information and identify modifications as required; a build-time input is not automatically a distributed dataset. Upstream sources: https://github.com/browserslist/caniuse-lite and https://caniuse.com/ (the underlying Can I use data project).
- System font fallbacks are configured; no font files were identified as bundled in this investigation. Adding a bundled font requires separate provenance/license review.

## MPL-2.0 covered source — obtaining it

hashicorp/golang-lru/v2 **v2.0.7** contains MPL-2.0 covered source files. Executable distribution must inform recipients how to obtain the corresponding covered Source Code Form under MPL-2.0; root Apache-2.0 does not relicense those files. Preferred upstream source locations for the version referenced here:

- Browsable source: https://github.com/hashicorp/golang-lru/tree/v2.0.7
- Exact module source archive: https://proxy.golang.org/github.com/hashicorp/golang-lru/v2/@v/v2.0.7.zip
- Upstream tag archive: https://github.com/hashicorp/golang-lru/archive/refs/tags/v2.0.7.tar.gz
- License text accompanies this distribution at `LICENSES/go/github.com/hashicorp/golang-lru/v2@v2.0.7/LICENSE`.

These are source-access instructions, **not a claim that source code is included in this legal-text archive**. Before releasing, verify that the links remain publicly accessible and that the build used this exact source without local changes/replacements. If covered files were modified, publish the corresponding modified source, retaining required notices and identifying modifications; an unchanged upstream URL is then insufficient. Mirroring the exact covered source alongside the release is recommended for durable availability, but this notice collection does not include a source archive. Do not impose terms restricting recipients' MPL source rights.

For any redistributed lightningcss 1.33.0 tool/binding, corresponding upstream source is available via https://github.com/parcel-bundler/lightningcss/tree/v1.33.0 and https://github.com/parcel-bundler/lightningcss/archive/refs/tags/v1.33.0.tar.gz (use the matching installed version and any applicable native-build inputs/modifications). The installed tool and Windows x64 binding both contain the full MPL-2.0 text and identify that repository in their package metadata. The source tag and archive, and golang-lru's browsable tag and module archive above, were reachable during this refresh; accessibility alone does not establish native-binary/source correspondence. Those source obligations are conditional on distribution of covered material, not merely on use of its compiler output. Preserve its MPL text and review binding/source correspondence if shipping the tools.

## Not bundled / content rights

waifu2x and Real-CUGAN executables/models are user-supplied optional external programs. The reviewed release workflow bundles neither them nor their models; their licenses are not granted by Yomifolio's root LICENSE. Reassess if future releases bundle them. Third-party comics, covers, samples, artwork, and user library files are not licensed by the project's code license. The current screenshots use project-original procedural demonstration artwork; see [their provenance](images/README.md). The project license does not authorize reuse of artwork shown in historical screenshots or third-party libraries.

## Release limitations / outstanding work

1. Close the libyuv revision / AVIF prebuilt-WASM provenance issue described above. Successful legal-text downloads do not prove binary correspondence.
2. The manifest records **8 collection gaps**: eight present NPM directories without collected full legal text: @humanfs/types 0.15.0, @napi-rs/wasm-runtime 1.2.6, @rolldown/binding-win32-x64-msvc 1.2.19, @tailwindcss/oxide-wasm32-wasi 4.3.5, its nested @napi-rs/wasm-runtime 1.2.2 and @tybys/wasm-util 0.10.3, natural-compare 1.4.0, and saxes 6.0.0. Six directory-only locations additionally lack package.json, so their version/source correspondence is unverified even where a legal file was collected. Do not synthesize copyright/license text from metadata. Resolve these if distributing them, or document evidence that they are outside the actual release payload. The previously missing Go graph modules (golang.org/x/net 0.62.0, x/term 0.48.0 and x/text 0.44.0) now have byte-exact LICENSE and PATENTS texts collected from their versioned module archives.
3. Reconcile the actual three native release builds, embedded Web output and native compiler/runtime dependencies with this collection. The current local Go list does not characterize every OS, CGO runtime, compiler support library, prebuilt native NPM binding, or WASI input. Some such inputs may require further notices; they are not cleared here.
4. Verify corresponding MPL covered-source availability, local modifications and source/build correspondence for the actual release. Do not claim sources are bundled in this collection.
5. Adopt a release delivery mechanism for this collection; uploading only the executable and checksum does not deliver the legal files. Publication of a new binary requires this follow-up; adding a root license is not enough.
6. Review any new screenshots/media, bundled fonts, data, AI executables or models. The code license does not grant rights in third-party content or user libraries.

Run `python scripts/check-license-notices.py` to check recorded legal-file bytes and dependency-input fingerprints. Text dependency inputs use canonical LF line endings, so a Windows CRLF checkout does not count as a dependency change; legal-file bytes are never normalized. When dependencies change, obtain their actual license/notice texts, review embedded components and source obligations, then update the collected texts, inventory and input fingerprints together. Do not merely update hashes to make the check pass.

The integrity check and this collection are **not a representation that a bare-binary release is ready or that all third-party obligations have been discharged**.
