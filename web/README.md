# 前端依赖与审计

使用 `.node-version` 指定的 Node LTS 与 `npm ci` 安装锁文件；升级后须通过 lint、Vitest 和生产构建，并复核[第三方通知](../THIRD_PARTY_NOTICES.md)。CI 与发布使用相同工具链，不用 `npm audit fix --force` 绕过兼容性检查或降级依赖。

## 兼容边界

TypeScript 保留在 6.0.3：最新稳定 `typescript-eslint@8.72.2` 的 peer 范围为 `>=4.8.4 <6.1.0`，尚不支持 TypeScript 7。升级编译器前先确认解析器支持，不禁用 ESLint 或抑制版本警告。

Rolldown 的稳定版可选 WASM 绑定精确依赖 `@emnapi/core/runtime@2.0.0-beta.2`；这是获准保留的上游传递依赖例外，不代表本项目主动采用预发布工具链。当前 Windows 验证使用原生绑定；三个发布平台也提供原生绑定。不要强换 1.x 破坏其精确依赖契约；WASM 后备路径未在本机验证。

## 安全覆盖项

- `esbuild` 由 override 约束到带安全修复的 `^0.28.2`；Vite 的内部打包器不代表其他工具不会拉入 esbuild。
- `@typescript-eslint/typescript-estree` 的 `minimatch` 定向覆盖为 `^11.0.8`。其原依赖 10.x 落在 GHSA-g3hv-75wg-m249 的受影响范围（<11.0.1）；调用方只使用具名 `minimatch(filePath, pattern, { dot: true })`，升级后须验证 project-service 的 `allowDefaultProject` 匹配和完整 lint。覆盖不扩展到无关包，不单独强升 brace-expansion 破坏调用契约。

当前锁文件在 2026-11-16 的 `npm audit`（含开发依赖）结果为 **0 vulnerabilities**；这不是无漏洞保证。旧路由与 brace-expansion 例外不再适用于当前锁文件；每次升级重新审计，不沿用旧版本的可达性结论。
