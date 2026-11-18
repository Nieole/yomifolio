# 01: 搜索等待、失败与旧响应隔离

**What to build:** 读者通过现有全局搜索就能区分正在等待、请求失败与成功无结果，并能重试；快速输入、切换范围、关闭或重置后，不再误选旧查询结果。本票沿用现有首批查询，不提前实现继续加载。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] 按本 effort 正式规格的搜索会话行为，区分空输入提示、防抖等待、请求中、成功有结果、成功无结果及失败。只有当前请求成功且为空才显示“未找到”；首批失败可以对当前查询立即重试。
- [x] 保留约 300ms 输入防抖；重试不重复等待防抖。新关键词或 target 立即清除旧结果与可选择资格，不仅在请求完成后才替换。
- [x] 请求取消与请求代际、查询归属校验共同生效；即使旧请求取消无效且晚到，也不能覆盖新查询或回填已关闭的搜索。
- [x] 关闭、重置、有效读者或连接上下文变化时使旧请求与结果失效；重新打开对保留输入重新查询，不复用未验证结果。不新增假想 transport adapter。
- [x] 键盘上下选择、回车与鼠标只作用于当前有效条目；无有效条目时不导航、不越界。系列进入详情、书进入阅读的行为保持不变。
- [x] 等待和错误文案遵循既有本地化，异步状态可被辅助技术感知；不改变权限、检索字段或既有首批排序。
- [x] 通过已有搜索会话与弹窗高层 seam，控制真实 HTTP adapter 的响应完成顺序，覆盖取消无效、快速输入、切范围、关闭/重开、重置、失败重试及键盘安全；不能只给弹窗注入静态 hits。
- [x] 本票前端测试、构建及 lint 通过；如修改 HTTP 行为，补实际入口回归。记录执行证据与未执行限制。本票不依赖归一化或新分页契约，不能等待后续票才验证自身行为。

## Answer

仅完成 01。实现提交：`944fbb74`（`search-session-reliability`）；文档与本 Answer 随后单独提交。

- `useGlobalSearch` 以会话对象和 application query scope 绑定响应归属，effect 生命周期作为请求代际；在取消外仍检查当前代际/active，渲染时拒绝旧归属结果。切词/范围立刻清空选择资格，关闭/重置取消在途请求，重开重新查询保留输入。复用既有 session 身份/连接作用域，不把 SSE 普通重连当作新的查询身份。
- 区分 idle、防抖等待、请求中、成功与失败；失败重试立即发送。中英文文案和 status/alert 通知、清空标签已补齐；只有当前条目可导航，系列详情与书阅读路径保持不变。
- 先观察回归测试失败，再实现为通过；hook 测试与真实 Layout＋弹窗组合测试使用 `apiClient.defaults.adapter` 控制完成顺序，并故意绕过取消模拟失效 transport，覆盖乱序、快速输入、切范围、关闭重开、重置、身份替换、失败重试及键盘/鼠标导航。不是仅注入静态 hits。
- 持久行为同步 `web/SPEC.md`，用户可见记录同步 `CHANGELOG.md`。before/after 位于 `.thinkrail/context/mockups/search-before.html` 与 `.thinkrail/context/mockups/search-after.html`，已 visualize。

验证（工作目录 `web`；PATH 使用本机 Zed 携带的 Node v24.22.1，未改依赖或锁文件）：

- `npm.cmd test` → 本轮曾完整通过 119 files、1134 tests（10:53 运行，85.35s）。首次全量运行暴露两个旧弹窗测试缺少新增状态 props，补齐夹具后重跑通过。但全量门禁并非稳定可复验：独立 review 连续两次运行均在既有 `RelocationEntry.test.tsx` 历史计划测试找不到 `relocation.checkedHint`，各为 118 files / 1133 tests 通过、1 test 失败。该入口懒加载 RelocationModal，测试使用默认 findBy 等待；单文件通过而全量失败符合负载相关超时，但未取得足够证据确认根因，不声称已修复。
- review 后复验：`npm.cmd test -- src/components/layout/RelocationEntry.test.tsx src/components/layout/SearchModal.session.test.tsx` → 2 files / 8 tests 通过（15.89s）。继续完整复验时，默认 `npm.cmd test` 超过 180s 被 harness 终止；`npm.cmd test -- --maxWorkers=2` 超过 360s 并出现多处 `[vitest-pool]: Timeout terminating forks worker`；`npm.cmd test -- --pool=threads --maxWorkers=2` 超过 300s 被终止。没有将这些超时当成通过，也未通过修改不相关测试或全局放宽超时掩盖问题。全量稳定性仍是交付限制，建议在 CI 标准 Node 26 环境复核。
- `npm.cmd run build` → TypeScript 与 Vite 构建通过，本树实际重建 `web/dist`。
- `npm.cmd run lint` → 通过，无输出错误。
- `git diff --check` → 通过；`spec_validate` → 无问题。
- 开始前及交付验证前 `git merge search-experience-integration` → Already up to date；`git merge-base --is-ancestor search-experience-integration HEAD` → 0，当前集成 tip 为 `86c510d6`，无冲突。
- 前两个独立计划步骤 request_review 均 approve；最终文档步骤指出全量测试不稳定，已调查并如实记录复验、超时与未确认根因，随后再次复核。

限制：全量测试曾通过但后续复验不稳定，不能据此宣称稳定全绿；未修改与本票无关的 Relocation 生产行为或测试。未运行真实浏览器或读屏器人工验收；jsdom 通知断言不代替读屏器验收。验证使用 Node 24 而非 CI 文档所列 Node 26.13.1。未改 Go/SQL/API 响应契约，因此未执行 Go 测试、Go lint 或生成器；未将前端 adapter 测试称作真实后端 HTTP 回归。分页、排名、归一化与大库性能留后续票，01 不依赖这些能力。未创建 PR，工作树保留供协调者集成。
