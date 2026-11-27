# Building and development

[Home](../README.md) · [中文指南](development.zh-CN.md)

## Requirements

Prefer [prebuilt releases](https://github.com/Nieole/yomifolio/releases/latest) for deployment. Source builds need Go, Node/npm, and a working C toolchain: **WebP encoding requires CGO**, even though the SQLite driver itself does not. Use the toolchain versions pinned in [CI](../.github/workflows/ci.yml) and `.node-version` when reproducing CI results. `go.mod` declares the minimum Go version required by the dependency graph, not the CI toolchain. The linter must support that toolchain's export format; rebuilding an older golangci-lint with a newer Go compiler does not update its analyzers.

The frontend uses React 19, Vite 8, TailwindCSS 4, TypeScript, React Router, React Virtuoso, and React Flow. The backend uses chi, sqlc, embedded modernc SQLite, and image libraries including chai2010/webp, gen2brain/avif, nfnt/resize, disintegration/imaging, and x/image. Exact versions belong in `go.mod` and `web/package-lock.json`, not a duplicated version table.

## Native build

From the repository root, on a Unix-like shell:

```bash
cd web
npm ci
npm run build
cd ..
CGO_ENABLED=1 go build -o build/yomifolio ./cmd/server
```

Windows PowerShell (with a Go-compatible native C compiler installed):

```powershell
cd web
npm ci
npm run build
cd ..
$env:CGO_ENABLED = "1"
go build -o build/yomifolio.exe ./cmd/server
```

Build the **real** frontend before Go. `web/web.go` embeds `all:dist`; a fresh checkout has no `web/dist`, and Go compilation fails without it. Do not add dummy assets just to satisfy embed. Frontend production build runs TypeScript project checking before Vite bundling. `npm ci` preserves the committed dependency lock rather than resolving a new one.

`./build.sh` builds the frontend and three local outputs, and additionally needs **Zig** for Linux musl / Windows mingw cross-compilation; its Darwin ARM64 step expects an appropriate native macOS environment. Local outputs use `yomifolio-mac-arm64`, `yomifolio-linux-amd64`, and `yomifolio-win-amd64.exe`, not the versioned release naming scheme.

`./build.ps1` builds a Windows AMD64 binary using native CGO. Set up the C compiler and CGO environment; its `-SkipFrontend` switch is only appropriate when a current real dist already exists. These scripts inject version/commit/build-time metadata. A plain `go build` reports development metadata.

## Development loop and checks

Run the backend after building initial dist. For live UI development use `cd web && npm run dev`; Vite proxies `/api` to `http://localhost:8080`. Use a non-production library/database and do not expose the dev server to an untrusted network.

```bash
scripts/check.sh          # frontend, static analysis, generated-code drift, docs and notices
scripts/check.sh --full   # adds full tests, race tests, CGO build, govulncheck and actionlint
```

The script reproduces the [CI workflow](../.github/workflows/ci.yml) locally and takes tool versions from it. CI also runs native tests on Windows and macOS ARM64; platform differences cannot be reproduced locally. Release builds first depend on the reusable CI gate.

For sandbox/cache permission issues, create `.gocache` and `.tmp`, then use the documented Unix-shell form:

```bash
GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...
```

Create parallel worktrees with `scripts/new-worktree.sh`. `scripts/check.sh` shares the Go build cache across worktrees and gives each its own golangci-lint cache; set a worktree-specific `GOLANGCI_LINT_CACHE` yourself when running golangci-lint directly.

### Direct database tooling

Prefer the server's write APIs and maintenance rebuild task over external SQLite writes. Series and book triggers require the deterministic SQL function `search_normalize(TEXT)` on **every writing connection**, with exactly the `database.NormalizeSearchText` semantics documented in [the database contract](../internal/database/SPEC.md). Importing that Go package registers it on the default modernc driver, including pooled and maintenance connections. A plain SQLite CLI or another driver does not provide it; read-only inspection is fine, but do not bypass/drop triggers or swallow missing-function errors to make writes succeed. Such bypasses leave stale indexes and silently lose search hits. External tools must either use a supported connection or implement the identical function before writing.

After a failed upgrade, fix the reported error and rerun startup migration; its completion marker is only written after successful backfill. The entire migration is **not atomic**: earlier DDL/backfills may already have committed. Each series or book search rebuild transactionally replaces its own projection and both indexes, not the whole migration or all maintenance work. Use the normal backup/restore procedure before upgrades or external maintenance.

### Generated code

- SQL query/schema changes: run `sqlc generate` (CI uses sqlc v1.31.1), verify generated files under `internal/database`, and include them with the source change. On Windows run sqlc in PowerShell/cmd rather than a Git Bash shim, which can report a false failure. SQL sources in `sql/query.sql` must remain ASCII; follow [AGENTS.md](../AGENTS.md) for sqlc caveats.
- Backend response contracts exported by tsgen: run `go run ./cmd/tsgen` and include the resulting `web/src/api/generated.ts` changes. Do not hand-edit generated contracts.
- Follow the documented ownership and generation conventions in [AGENTS.md](../AGENTS.md), [architecture](../architecture.md), and [documentation style](agents/doc-style.md). The internal engineering issue/spec workflow remains `.scratch/`; public GitHub issue intake does not replace it.

### Dependency updates

Read [web/README.md](../web/README.md) before changing frontend dependencies. It records reachability/build-only reasoning and upstream blockers for audit exceptions; reassess against the installed lockfile and current advisories. **Do not run `npm audit fix --force`**: the proposed change can downgrade routing or break dependency compatibility. This documentation is not a claim that all dependencies have been audited or are vulnerability-free.

## Release behavior

The existing release workflow accepts `v*` tags or manual dispatch. Native runners build Linux AMD64, Windows AMD64, and Darwin ARM64 binaries with CGO enabled, after CI. Only tagged runs publish GitHub Release assets; manual runs produce workflow artifacts. New outputs are `yomifolio-${version}-${os}-${arch}` plus `.exe` on Windows, with SHA256 checksums for published assets. Older release assets may retain `manga-manager` filenames; use the filenames and release notes on the actual release page. See the actual [workflow](../.github/workflows/release.yml) rather than creating speculative asset URLs.

For contributions, read [CONTRIBUTING.md](../CONTRIBUTING.md). Dependency changes also require review of the [notice inventory and release limitations](../THIRD_PARTY_NOTICES.md); its integrity check is not a compliance audit.

For repository publication, outreach and follow-up metrics, see the [maintainer checklist (中文)](discoverability.md); it does not publish a release or schedule reminders.

Version changes belong in [CHANGELOG.md](../CHANGELOG.md). For bugs or suggestions, [open an issue](https://github.com/Nieole/yomifolio/issues/new/choose). This guide is not an instruction to create or push a release tag.
