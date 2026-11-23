# 10 — 自动封面与文字一起生效

## 范围与接续核验

- 唯一写入树 `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-10`，分支 `mcp-atomic-covers`；HEAD 与 `merge-base HEAD c4540ce4` 均为 `c4540ce40dfd3b464dc462ed13b9c7b2005edac2`。全部改动未提交/未暂存。未操作父树、09 或 11/13。
- 恢复时先核对 status/diff、既有实现及 `.tmp/10/` 日志，没有从头覆盖。中断前已有实现、红绿和完整门禁日志，但没有 acceptance10 或本票 mockups；不将它们当成本上下文自动通过的证明。
- 已读 worker guide、AGENTS、effort spec、票10、05/08 acceptance、tracker/doc guidance、TDD（含 tests/mocking）、MCP-builder/frontend-design 技能、领域词义及相关 owning specs。沿用 effort 已确认接缝：真实 `bootstrap.Open`、临时 SQLite/PNG/CBZ、通用 SDK HTTP 与真实 stdio 子进程、原网页 HTTP。只替换外部来源，不调用真实收费模型。

## 实现与持久边界

- `enrichment.Input.Cover` / `CoverSelection`：只接受同系列已授权归档的 book_id、一基 page、quality_compared=true 及短比较理由。允许 cover-only；同时有文字仍需明确确认的提案。来源 URL 不用于获取图片，不接受 URL、宿主路径或上传字节。
- `seriesmeta.CoverState`、`ArchiveCover`、`WriteEnrichmentCover` 归口来源保护和事务绑定写入。仅改善当前目录首个已有封面书的归档自动封面；人工手选、上传内容哈希键（即使解锁）、书锁及系列 cover 锁不允许绕过。最终事务重读相关预期值、锁、真实库/源书归属及当前 user ∩ credential ∩ library ∩ write。无关字段不阻止或被覆盖。
- `covermaterial` 以 O_EXCL 随机键在相邻私有 `<thumbnail-root>.metadata-covers/metadata-covers/` 先写、Sync、Close 不可变图片。旧图保留独立有界验证的原字节；新图复用05的归档句柄/准入、RAR/源字节/像素预算及384边、JPEG65、96KiB缩略图。准备另有可取消双槽。
- **SQLite只原子发布引用，不是跨存储事务。** 所有文字、封面引用、统计、完整收据及多对多 `metadata_cover_materials` 保留引用同事务提交；无提交后 rename/copy。常规写错同授权 savepoint 回滚整项，再记录 failed；日志/材料引用/COMMIT 自身不可用返回错误，不伪造持久收据。
- 准备失败不改当前引用。完成准备但未提交、冲突、回滚或提交结果不确定的材料不盲删；保守保留，避免删除可能已提交资源。原 cover 路由由已提交 book 引用解析虚拟键；原 thumbnail 路由必须有当前书或持久保留引用。普通文件限定于普通根，私有根不在文件名直读/普通清理树内；Windows大小写/尾点别名不能提前读准备图。
- 成功重开无发布恢复步骤，直接沿持久引用读取。Undo 在同事务检查所有实际变更字段及原 after 封面、当前锁和当前授权，整体恢复文字与保留旧图；后续人工封面/相关文字冲突，无关 publisher 保留。08失败Undo savepoint/幂等/审计保留；Apply常规写错也改为同事务savepoint，不另开事务记录旧失败。
- 默认不清材料，包括完整准备孤儿。13须协调活跃准备/Undo/发布，在当前书、派生投影及**所有**操作保留引用均受保护后才可清理；不能按单条记录或年龄删除共享材料。本票没有清理API/自动过期。

## 有效红绿与公开验收

接续时实际检查了原日志内容，而不是仅信文件名：

- `red.log` → `green1.log`：组合封面写入最初公开 MCP 拒绝，随后生产应用通过。
- `red-public-path.log`：新材料文件名路由404；`green-public-path.log` 修复后通过。
- `red-alias.log` / `red-win-alias.log`：未提交图经 Windows 大小写/尾点别名曾返回200真实PNG；移出普通根并限制路由后对应 green 日志通过。
- `red-evidence.log`：新封面 MCP 图片读取失败；`green-evidence.log` 加入私有虚拟键解析后通过。
- `ui-red.log`：找不到 Previous cover 图片；`ui-green.log` 加入管理页比较后通过。

本上下文随后实际运行了扩展测试和全部门禁。未为了制造新红而回退已有实现，也未声称重新现场执行了上述历史红阶段。

`TestProductionMCPEnrichmentAuditUndo` 调用 `verifyAtomicCovers`，真实公开验证：

- 成功：普通 `/series/info/{id}` 的简介、`/covers/{book}` 解码后的蓝色内容、按文件名图片、MCP ImageContent 同时反映新图；Undo后普通系列简介与红色旧图一起恢复。
- 安全拒绝：普通人工选页换图后的MCP写入、锁/人工来源、跨库源书、非法页、未比较声明及多余URL字段；不扩大05下载/解码边界。角色在请求进行中降级后不获写权限。
- 真实准备写失败（私有目录被文件阻挡）、文字预期冲突、封面UPDATE触发器失败、文字UPDATE在封面已更新后失败、材料引用INSERT失败、延迟外键触发真实COMMIT失败：实际HTTP图片与公开系列状态不半生效。文字失败的failed receipt同键重放仍不执行。
- 未提交孤儿按文件名404（Windows额外验证大小写/尾点别名），不是只检查文件存在。
- 真实stdio执行组合写入及同键重放；普通缩略图cleanup后旧/新材料仍可读。后续人工换图、当前书锁、相关简介变化分别阻止组合Undo，无关publisher不阻止。
- 关闭生产图和原SQLite连接，新连接/新图重开后公开读取新图与文字、查询持久failed收据、执行管理员HTTP Undo；原普通旧缓存文件已删除，仍能恢复保留旧图。第二次重开仍读到红色旧图。

管理页8个组件测试包含封面前后、比较理由/风险、权威Undo反向图片、failed结果不误标已撤销/不显示伪恢复图片；保留原分页、同键网络重试、普通用户不请求等守卫。组件mock不替代后端授权验收。

## 本上下文实际 checks

环境：Go `go1.26.10 windows/amd64`；guide共享GOCACHE，本树 `.tmp/10/gotmp` 和独立 lintcache；Node `v26.13.1` / npm `11.20.0`。日志均在 `.tmp/10/`，以下是本次 `resume-*`，不是中断前门禁的重述。

1. `go test -p 1 ./internal/bootstrap -run TestProductionMCPEnrichmentAuditUndo -count=1 -timeout=3m` — PASS，`resume-focused.log`，包含本次增加的普通系列HTTP/文字失败回滚/幂等/重开审计断言。
2. **完整后端一次** `go test -p 1 ./... -count=1 -timeout=10m`，外层1800秒 — PASS，`resume-backend-full.log` / `.exit=0`；API 143.433s。没有失败、盲重试或放宽测试；所有当前后端测试改动均在该次运行之前。
3. `gofmt -l ./cmd ./internal` — 空；`golangci-lint run` — **0 issues.**，`resume-gofmt.log` / `resume-lint.log`。
4. `npm test -- --maxWorkers=2` — **121文件 / 1154测试通过**，`resume-frontend-tests.log`；`npm run build` / `npm run lint` — PASS，`resume-frontend-{build,lint}.log`。封面管理页先定向8/8通过，`resume-ui-focused.log`。
5. `go run ./cmd/tsgen` — 44类型，生成前后SHA-256一致，`resume-tsgen*.log`。SQL输入确有修改，实际在PowerShell运行 `sqlc generate` — exit0，`resume-sqlc.log`；query.sql.go/querier.go生成前后SHA-256一致，`resume-sqlc-{before,after}.log`。query.sql和handwritten SQL均ASCII。
6. `git diff --check` — PASS；mockups检查无script、控件、外链，inline CSS自包含。

## 界面、契约与限制

- Mockups：`.thinkrail/context/mockups/10-before.html`、`10-after.html`。是静态概念示意，彩色图块非真实藏书图片；没有浏览器截图或像素回归证据。
- 接口继续使用 `library_get_enrichment_state`、`library_enrich_metadata{,_batch}`、既有历史/Get/Undo与管理员 `/api/mcp/operations`；没有新增runtime或独立图片API。生成契约在 `web/src/api/generated.ts`。
- 长期契约归 architecture、application、seriesmeta、database、bookcontent、api、bootstrap、web所属规格；用户变更进入CHANGELOG。质量比较是agent显式声明，不证明客观质量。新封面是有界缩略图，不提供原图分辨率。
- 故障注入证明的是文件准备错误、SQLite语句/提交失败及正常资源关闭后的真实重开；没有做强杀进程/拔电/磁盘损坏实验，不保证平台未提供的目录元数据断电持久性，也不承诺恢复被外部删坏的材料。已完整准备孤儿默认占空间，清理由13另行实现。
- 票10维持ready-for-agent，只勾实际验收项；父负责独立review、处理与09未来组合的共享文件并提交。未宣称票11/13或全effort完成。


## 独立审阅阻塞修复：真实 transport 取消（本轮实跑）

- 仍只在原 `mcp-library-10` / `mcp-atomic-covers` / `c4540ce4` 工作；原有全部 delta 保留，没有 stage/commit/merge/reset，没有写父树、09 或 TODO。保持 ready-for-agent，等待父 fresh 独立重审，不提前 done。
- 重新阅读 AGENTS、外部 worker guide、TDD（含 tests/mocking）、MCP builder、effort/票10/本 acceptance、相关 API/application/bookcontent/bootstrap/storageio 规格和实际 `covermaterial`、SDK v1.8.0 源码。实际 SDK `serveStateless` 建立 ephemeral session 后 defer Close，默认工具上下文不能代替 HTTP 生命周期；没有通过 SDK 升级或全局启用取消改变后台运行语义。
- 窄修 `registerMCPEnrichment` 接收入口 `r.Context()`；单项 Apply 和整批 Batch 复用 `imageRequestContext`。合并 ctx 贯穿证据读取、cover/image 槽、卷 IO 等待、准备和现有 BeginTx/读写/收据/Commit。没有 prepare 后换回 SDK ctx 或 WithoutCancel。未改授权、图像预算、运行注册或已接纳运行断线继续语义。
- 新 `mcp_cover_cancel_test.go` 接到生产 enrichment 验收夹具：真实 TCP HTTP POST 后关闭 socket；真实编译/启动 mcp-proxy 子进程，初始化后关闭 stdin 产生 EOF、取消真实上游。没有以直接 cancel 应用 ctx 冒充 transport。HTTP/stdio × single/batch，每组连续三次，在卷 IO 许可始终占满时验证请求确实先进入 ReaderWaiting，断连后 ReaderWaiting 清零、LibraryConflicts 不再 wait_for_reads、归档 borrow 清零；第三次仍能进入 IO，动态证明双 cover 槽和图片槽未被前两次耗尽。
- 每次 batch 首项在另一个真实系列先提交文字与新 changed 收据，第二项组合 cover/text 被卡 IO 后断连。恢复后逐个公开查询文字、操作审计，同键重放 `replayed=true/applied=false/outcome=changed`；原组合操作也仍返回原 ID。被取消的系列公开 State 完全一致，并继续执行原普通系列 HTTP/解码封面验证、原子失败/Undo/重开测试。
- **Red → Green**：修实现前 `.tmp/10/cancel-red.log`（真实 HTTP single）及 `cancel-red-stdio.log`（真实 stdio batch）均在确认进入 IO 后因 `disconnected cover released IO wait` 超时失败；没有内部 ctx cancel。初版测试的父级 Fatal 报告随后改为子测试自身报告。最小修复后 `cancel-green.log` 通过，最终扩展真实 changed 批次前缀后 `cancel-green-prefix-changed.log` 通过。未回退既有实现来伪造历史红测。
- **最终两轮定向回归**：`go test -p 1 ./internal/bootstrap ./internal/api ./internal/application/delegated ./internal/application/enrichment ./internal/bookcontent ./internal/covermaterial ./cmd/mcp-proxy -run 'MCP|Enrichment|Cover|Evidence|Budget|Stdio' -count=2 -timeout=10m` — PASS，`cancel-final-regression-two-rounds.log` / exit0；含图片、cover、enrichment、额度、run、真实stdio。delegated 在此正则下无匹配，covermaterial/proxy 无包内测试；不将它们冒称有定向测试，完整包由下一项覆盖。
- **最终完整后端**：Go1.26.10、guide共享 GOCACHE、本树 `cancel-gotmp`，`go test -p 1 ./... -count=1 -timeout=10m`（外层1800秒）— PASS，`cancel-final-backend-full.log` / exit0，包含所有相关完整包。之前版本也完整通过一次（`cancel-backend-full.log`），最终证据以前者为准；没有失败盲重试。
- **最终门禁**：`gofmt -l ./cmd ./internal` 空（`cancel-final-gofmt.log`）；独立 `cancel-lintcache` 的 `golangci-lint run` **0 issues.**（`cancel-final-lint.log` / exit0）；`go run ./cmd/tsgen` 44类型，生成前后 SHA-256 一致（`cancel-final-tsgen*.log`）；最终 `git diff --check` 空（`cancel-final-diffcheck.log`）。修正文档写入换行产生的临时 CRLF diff 后才记录最终 diffcheck；无 SQL/生成契约新增修改。
- 前端本轮无修改，仅 Node26.13.1 下 `npm test -- src/pages/settings/metadataOperations.test.tsx --maxWorkers=2` — **1文件/8测试通过**（`cancel-ui-history.log`）。上节完整1154测试/build/lint属于前轮既有结果，本轮没有重跑，不能混称本轮全前端门禁。
- 准备成功到提交的取消一致性：现有 `BeginTx(ctx)` 及全部查询使用同一个 merged ctx，取消会使事务不能正常继续发布；COMMIT 与断连竞争仍可能已经完整提交，因此客户端须同键查询，不能把断连解读为未提交。没有声称通过可抢占的文件 Sync 或拔电实验；已完整准备孤儿仍按原方案保守保留。
- 本轮取消修复没有改 Input/State/Undo 的 JSON 形状、hash 函数、请求摘要、持久收据版本或重放命名空间，**未新增 commandhash/旧收据兼容变化**；同键审计实测如上。09 的稳定 hash 修复仍在其独立树，由父后续组合并做旧收据兼容验收；本树不提前宣称组合兼容已通过，也未触碰09。
