# 构建与开发

[中文首页](../README.zh-CN.md) · [English guide](development.md)

## 环境要求

部署优先选[预编译版本](https://github.com/Nieole/yomifolio/releases/latest)。源码构建需要 Go、Node/npm 与可用的 C 工具链：**WebP 编码依赖 CGO**，SQLite 驱动无需 CGO 不代表整个程序无需 CGO。复现检查时使用 [CI 工作流](../.github/workflows/ci.yml) 指定的工具链；Node 本地版本以 `.node-version` 为准，模块最低版本见 `go.mod`，不要混淆模块声明与 CI 实际版本。golangci-lint 的分析器必须支持所选 Go 的导出格式，仅用新 Go 重编旧 lint 不能补上分析器支持。

前端为 React 19 / Vite 8 / TailwindCSS 4、TypeScript，使用 React Router、React Virtuoso、React Flow；后端使用 chi、sqlc、内嵌 modernc SQLite 及 WebP/AVIF 等图像库。精确版本以 `go.mod`、`web/package-lock.json` 为准，不在两种语言重复维护依赖版本表。

## 原生构建

从仓库根目录，在 Unix 类 shell 执行：

```bash
cd web
npm ci
npm run build
cd ..
CGO_ENABLED=1 go build -o build/yomifolio ./cmd/server
```

Windows PowerShell 需要与 Go 兼容的本机 C 编译器：

```powershell
cd web
npm ci
npm run build
cd ..
$env:CGO_ENABLED = "1"
go build -o build/yomifolio.exe ./cmd/server
```

必须先构建**真实前端**。`web/web.go` 嵌入 `all:dist`，全新检出没有 `web/dist`，不可创建空壳 dist 绕过嵌入要求。前端生产构建先检查 TypeScript 再运行 Vite；`npm ci` 使用提交的锁文件，不重新解析依赖。

`build.sh` 先构建前端，再生成三个本地产物；Linux musl / Windows mingw 交叉编译另需 **Zig**，Darwin ARM64 步骤依赖适当的本机 macOS 环境。本地文件名为 `yomifolio-mac-arm64`、`yomifolio-linux-amd64`、`yomifolio-win-amd64.exe`，不要与带版本号的发布资产混淆。

`build.ps1` 是 Windows AMD64 原生 CGO 构建，也需要 C 编译器；仅当已有最新真实 dist 时才能使用 `-SkipFrontend`。脚本会注入版本、commit 和构建时间，直接 `go build` 则显示开发元数据。

## 开发与检查

先构建初始 dist 再启动后端。前端开发执行 `cd web && npm run dev`，Vite 将 `/api` 代理到 `http://localhost:8080`。使用隔离的开发书库与数据库，不向不可信网络暴露开发服务器。

```bash
scripts/check.sh          # 前端、静态分析、生成物漂移、文档与许可检查
scripts/check.sh --full   # 再加全量测试、race、CGO 构建、govulncheck 与 actionlint
```

脚本照 [CI 工作流](../.github/workflows/ci.yml) 复现检查，工具版本以工作流为准。CI 另在 Windows 与 macOS ARM64 上跑原生测试，平台差异本地复现不了。发布构建依赖复用 CI gate。

沙箱缓存权限有问题时，先建立 `.gocache`、`.tmp`，按 AGENTS 的 Unix shell 写法运行：

```bash
GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...
```

并行 worktree 用 `scripts/new-worktree.sh` 建树；`scripts/check.sh` 让各树共享 Go 构建缓存、各用一份 golangci-lint 缓存，直接跑 golangci-lint 时要自己设独立的 `GOLANGCI_LINT_CACHE`。

### 生成代码与工程约定

- SQL 查询/结构变动后执行 `sqlc generate`，检查并随源文件交付 `internal/database` 的生成结果。Windows 应在 PowerShell/cmd 运行 sqlc，Git Bash shim 可能报告假失败。`sql/query.sql` 保持 ASCII；工具版本及 caveat 见 AGENTS / CI。
- 导出的后端响应契约变动后执行 `go run ./cmd/tsgen`，交付 `web/src/api/generated.ts`，不要手工改生成契约。
- 所有权、架构与文档约定见 [AGENTS.md](../AGENTS.md)、[architecture.md](../architecture.md)、[文档风格](agents/doc-style.md)。内部工程问题与规格仍按 `.scratch/` 管理，公开 GitHub 反馈入口不改变约定。

### 依赖更新

先读 [web/README.md](../web/README.md)，按安装的锁文件和当前安全公告复核已有审计例外、可达性/仅构建期分析及上游阻塞。**不要执行 `npm audit fix --force`**：它可能降级路由或破坏依赖兼容。已有例外分析不等于所有依赖都完成安全审计或没有漏洞。

## 发布行为

[发布工作流](../.github/workflows/release.yml) 接受 `v*` tag 或手动 dispatch；通过 CI 后在原生 runner 以 CGO 构建 Linux AMD64、Windows AMD64 和 Darwin ARM64。只有 tag 运行发布 GitHub Release 资产，手动运行生成 workflow artifacts。当前产物命名为 `yomifolio-${version}-${os}-${arch}`（Windows 带 `.exe`），发布时附 SHA256 校验文件。

以发布页实际文件名为准，旧 release 可能仍叫 `manga-manager`；不要构造未发布的下载深链。本文与截图描述当前源码，下载二进制的功能以其 release notes 为准，不承诺新发布的时间。

贡献约定见 [CONTRIBUTING.md](../CONTRIBUTING.md)。依赖变更还需复核[第三方通知与发布限制](../THIRD_PARTY_NOTICES.md)；其完整性校验不是合规审计。

仓库展示上线、传播与效果复查见[维护者清单](discoverability.md)；清单不会自动发布版本或创建提醒。

版本变更记录在 [CHANGELOG.md](../CHANGELOG.md)。问题与建议可[新建 issue](https://github.com/Nieole/yomifolio/issues/new/choose)；本指南不要求创建或推送 release tag。
