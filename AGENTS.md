# Repository Guidelines

## Project Structure & Module Organization
`cmd/server/main.go` is the backend entrypoint. Core Go packages live under `internal/`:
`api/` for HTTP/protocol parsing, authorization and responses, `reading/` for reader-specific positions, read state, activity, time, bookmarks and projections, `organize/` for collections, smart shelves, relations, AI grouping adjudication and ordered reading lists, and `application/` for complete use cases, cross-feature runs and lifecycle (`runtime/` assembles application capabilities and owns recovery and shutdown; `bootstrap/` is the production composition root for application resources and HTTP adapters). `scanner/` owns library ingestion, `metadata/` owns scraping and LLM prompts, and `database/` owns SQLite and generated `sqlc` code. Supporting services live in `parser/`, `images/`, `koreader/`, `config/`, `runtimecfg/`, `logger/`, `booksort/`, `seriesmeta/`, `proposal/`, `relocation/`, `external/`, `storageio/`, `diskwork/`, `taskcontrol/`, `runhandle/`, `task/` and `taskstore/` (full-text search uses SQLite FTS5 inside `database/`). SQL sources are in `sql/query.sql` and `internal/database/schema.sql`; task/run DDL lives in `internal/taskstore/schema.go` because it has no sqlc queries. The Vite frontend lives in `web/`; routes are in `web/src/App.tsx`, connection/refresh coordination in `web/src/session/`, feature projections in `web/src/features/`, pages in `web/src/pages/`, shared UI in `web/src/components/`, and locale/theme helpers in `web/src/i18n/` and `web/src/theme/`. Screenshots and marketing assets are under `images/`.

## Build, Test, and Development Commands
Run the frontend locally with `cd web && npm run dev`. Build the frontend with `cd web && npm run build`; this is the standard verification step for UI changes. Lint the frontend with `cd web && npm run lint`. Run backend tests with a repo-local Go cache to avoid sandbox/cache permission issues:
```bash
# Use a repo-local cache directory (relative paths, works on any machine/OS):
GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...
```
提交前跑 `scripts/check.sh`：它照 `.github/workflows/ci.yml` 在本地复现 CI 的检查，只打印失败步骤的
摘要。`--full` 再加全量测试、race、CGO 构建与漏洞扫描，推 `main` 或交付一批票之前跑这一档；它要
半小时以上，放后台跑，再用前台 `until grep -q 'CHECK DONE'` 循环等它最后一行 `CHECK DONE exit=<n>`。

并行开多个 `git worktree` 时用 `scripts/new-worktree.sh` 建树，它把被忽略的 `web/dist` 拷进新树，
否则 `go:embed` 让 `go build` 第一步就失败。树里照样跑 `scripts/check.sh`：它让各树共享 `GOCACHE`、
各用一份 `golangci-lint` 缓存。直接跑 `golangci-lint` 也要每树一份 `GOLANGCI_LINT_CACHE`：共享的
lint 缓存把路径烤进了结果，源树一删就回放出一屏假阳性。

Use `./build.sh` for a full release-style build; it installs frontend dependencies, builds `web`, and cross-compiles binaries into `build/`. 升级前端依赖前读 `web/README.md`，复核 TypeScript/ESLint 兼容边界与安全 override，再对实际锁文件运行审计；不要用 `npm audit fix --force` 绕过兼容性检查。

## Coding Style & Naming Conventions
Go code should stay `gofmt`-clean and package-oriented; keep handlers thin and push logic into `internal/*` services. React/TypeScript uses the existing Vite + ESLint setup, 2-space indentation, PascalCase for components (`SeriesHeader.tsx`), and `useX` for hooks (`useReaderPreferences.ts`). Prefer small, behavior-preserving refactors over broad rewrites. 跑在任务里的代码写日志用带 ctx 的
`slog.InfoContext(ctx, …)` 一族——运行标识与**任务键**由 ctx 注入的 handler 补上，包级函数拿不到它们，
写成不带 ctx 的形式那一行就在「查看日志」里按运行过滤不到（不会变红，只会少一行）。请求处理与后台常驻
那些不跑在任务里的代码不受此约束。

## Documentation & Comments
注释与文档只写**当前的结果**；历史归 `CHANGELOG.md` 与 `docs/adr/`。每段内容有唯一**归属**，环境（`package.json`、`config.example.yaml`、`--help`）本身也是一处。引用代码写符号名——行号、出现次数、代码行数都会**腐坏**。分**层**写，每层只答一个问题，答不下就往下沉：Go 是 package doc → 文件头（≤5 行）→ 符号 doc → 行内，前端去掉 package doc 这层。文件头可选——说不出一句从文件名与 package doc 推不出来的话，就不写。

定位或维护 ThinkRail 长期规格、写文件头、处置旧注释或判断内容归属时，见 `docs/agents/doc-style.md`；`scripts/check-doc-style.sh` 是配套检查器。

## Testing Guidelines
Add or update `_test.go` files in the touched backend package, following the existing table-driven style in `internal/api/*_test.go` and `internal/scanner/*_test.go`. Frontend changes must pass `npm run test` (vitest) and `npm run build`. If you change SQL in `sql/query.sql` or `schema.sql`, regenerate the Go bindings with `sqlc generate` before testing. If you change a Go response struct that is a generated frontend contract (see `cmd/tsgen`'s `targets`, e.g. `api.RunSnapshot`), regenerate `web/src/api/generated.ts` with `go run ./cmd/tsgen` (CI checks it for drift).

> **sqlc 必须在 PowerShell（或 cmd）里运行，不要用 Git Bash / MSYS。** `sqlc generate` 会向 stderr 打印一批 `mismatched input ...` 诊断——这些是非致命噪音，sqlc 会自行恢复并正常生成。在 PowerShell 下命令退出码为 0、产物与仓库一致；但在 Git Bash 下经 scoop shim 调用会返回**假的退出码 1**，看起来像“管线坏了”，其实文件没问题。判断 sqlc 是否成功以 PowerShell 的退出码和 `git status`（有无产物 diff）为准。

> **`sql/query.sql` 只能写 ASCII，注释也是。** sqlc 替换 `sqlc.arg()` 用的是字节偏移，文件里一旦出现中文，
> 从那里往后的每条查询都会被切错位置，报出一屏 `mismatched input` 并**整份生成失败**（退出码 1、产物无变化）。
> 中文说明写到调用侧的 Go 文档里。另外 sqlc 看不见 `ON CONFLICT ... DO UPDATE SET` 里的绑定参数：写在那儿的
> `?` 或 `sqlc.arg()` 会被静默丢掉、一个参数都不绑，运行期才炸——那里要分支就拆成两条查询。

## Commit & Pull Request Guidelines
Recent history uses Conventional Commit prefixes such as `feat:` and `fix:` with short imperative summaries. Keep commits scoped to one change set. For user-visible changes, update `CHANGELOG.md` in the same batch. Pull requests should describe the behavior change, list verification commands run, and include screenshots for UI work.

## Agent skills

### Issue tracker

Engineering tickets and effort records live under `.scratch/<feature-slug>/`, not on GitHub; see `docs/agents/issue-tracker.md`. Durable ThinkRail specs live at the repository root and beside their owning modules; their lifecycle and temporary task-spec rules are in `docs/agents/doc-style.md`.

### Triage labels

The five canonical triage roles, used verbatim as the `Status:` value in each issue file. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` plus `docs/adr/` at the repo root. See `docs/agents/domain.md`.

## Configuration & Runtime Notes
Runtime config lives in `config.yaml`, which is gitignored and never committed (it may hold secrets such as `llm.api_key`). `config.example.yaml` is the tracked template; the server also generates a default on first run. `README.md` documents the flags and env vars that override the config path and log directory — both resolve to absolute at startup, and the database and cache locations come from `database.path` / `cache.dir`. Generated runtime data belongs in `data/` and should not be committed. When editing runtime config flows, `config.Manager` is the source of truth rather than mutating copied snapshots inline.
