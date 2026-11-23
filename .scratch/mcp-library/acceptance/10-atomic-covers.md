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


## 09 + 10 独立组合 merger 验证（本轮）

### 树与冲突处置

本轮唯一编辑树仍为 `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-10`，分支 `mcp-atomic-covers`。起点 HEAD **a4170973e12744f709a6cb7d5fc6f55d24940a3e**，已有父发起但未提交的 merge，MERGE_HEAD / integration tip 均为 **a92df90f7a85bba0626d4a3884f45ca01c826172**；结束时 tip 未变化。未 stage、commit、merge、reset、abort，未编辑父/其他树或 TODO。09 工单与验收文件是父已发起 merge 带入的内容，本轮未编辑。10 保持 ready-for-agent。

逐块组合而非整文件 ours/theirs：

- `service.go` 合并 cover 与作者 Input、State 读取/复制/集合比较、字段校正和事务检查；独立 `ExpectedState` 同时含可选 authors/cover，`.state()` 完整传递。9 个获准字段均可选择；cover-only 仍不强求提案，其他字段仍须确认来源。保留 10 的同事务 savepoint/失败收据，删除 09 已不适用的第二事务 failed helper，全部重放/正常/失败收据使用 `applyDigest`。
- `digest.go` 保留冻结 08/09 命令字段顺序/编码；新增显式 `coverSelectionV1` 与 `coverExpectedV1` 可选扩展，逐项映射 book_id/page/quality_compared/reason 及 expected book_id/path/locked/automatic。缺失时 omitempty，旧无 cover 字节不变。作者 nil/空组同义，非空身份及 from/to/reason 继续参与；无整个 Input/State hash、版本前缀、猜旧意图或宽松旧 hash fallback。
- `covers.go` 预期比较改用独立 ExpectedState 转换；`seriesmeta/enrichment.go` 同时保留 cover 状态和作者身份组及关联写入。实际当前库、授权、锁、源书归属、相关整组/标量/cover CAS 与所有写入仍在同一真实事务。
- 自动合并的 `undo.go` 专门对照双方核验：保留 `undoDigest(in)`、当前事务授权/锁/作者整组及 cover after 检查、旧材料恢复，以及 `writeState` + TryChange 同事务组合撤销。未退回只写文字或绕过失败 savepoint。
- API 保留 single/batch 的真实 `imageRequestContext(ctx, transportCtx)`，工具说明同时描述作者和封面；异步后台 run 断线继续语义未改。tsgen 同时保留 Author/CoverSelection/CoverState，保留 State nullable 与 cover omitempty 的区别，实际重新生成响应，无手写解决 generated.ts。
- 管理页自动合并后核验作者姓名/角色显示与封面前后/撤销方向/失败反馈共存；双语标签、两边测试、CHANGELOG 和 owning specs 均按段保留。没有添加 11/13 功能。

### 新组合回归与红绿

`verifyAtomicCovers`（生产 `TestProductionMCPEnrichmentAuditUndo`）新增同系列 cover + summary + 明确别名/角色校正：

1. 真实作者整组或 summary 在快照后变化，各自返回 conflict，公开系列、作者组及解码 HTTP 封面不半提交。
2. 真实 `BEFORE INSERT ON series_authors` 触发失败发生在 cover 和标量写入之后；整项 failed，公开旧图、空 summary、原作者组完整保留。
3. 成功同时得到 Canonical/Writer、Space journey 与实际解码 HTTP 蓝色封面；相同键不同 cover 比较理由或作者校正理由均被 MCP 拒绝。无关 publisher 在快照后改变不阻提交。
4. 作者组后改、summary 后改、当前 cover 锁各自阻止整项 Undo；去除冲突后一次 Undo 恢复精确原作者角色及文字/实际解码 HTTP 红色旧图（在测试 reset 之前验证），无关 publisher 保留。原封面读取/重开、manual 来源/归属保护、未提交图不可读、资源预算、真实取消和批次前缀幂等测试继续执行。

新增 `TestEnrichmentDigest09FrozenAndCoverIntent`：可信基准是 **a92df90f 的显式 V1 shape 所定义的固定原始 JSON 字节**，不是当前 structs 生成的 expected。无 cover 的真实作者校正命令仍得到原09 SHA；cover 每个选择/expected 字段及作者身份/角色/理由的变动均改变摘要。保留 `testdata/enrichment-base08.*` 原夹具不变，HTTP 与真实 stdio 重开原08 DB、旧单项/批量请求和 undo 重放继续绿。

红绿日志均在 `.tmp/10/combine/`，不冒充冲突文件尚不可编译时曾跑过旧生产实现：

- 初次组合测试 `combined-first.log` 真失败：宿主用 INSERT SELECT 添加后改作者，但 Canonical 身份尚未创建，插入零行，没有真正制造冲突。显式创建测试身份后 `combined-fixed.log` 通过；未改生产保护或弱化断言。
- 用只在 `.tmp` 的 Go overlay 故意移除新 cover intent 摘要映射：`red-cover-digest.log` 在首个 cover 变动处失败，exit1。故意从组合 writer 丢掉 authors：`red-combined-authors.log` 在真实作者触发器应 failed 却 changed 处失败，exit1。没有改 index/HEAD/其他树；正常实现的后续两轮与全量均绿。这是新守卫的 mutant red/green 证据，不声称发现两个新的生产缺陷。

### 本轮实际完整门禁

Go **1.26.10**；共享 GOCACHE 按 worker guide，本树 GOTMPDIR=`.tmp/10/combine/gotmp`、独立 lintcache=`.tmp/10/combine/lintcache`。Node **26.13.1** / npm **11.20.0**，使用既有 node_modules 后实际重建 dist。

- **两轮定向**：`go test -p 1 ./internal/bootstrap ./internal/api ./internal/application/delegated ./internal/application/enrichment ./internal/seriesmeta ./internal/bookcontent ./internal/covermaterial ./cmd/mcp-proxy -run 'MCP|Enrichment|Author|Cover|Evidence|Budget|Stdio' -count=2 -timeout=10m`，PASS / exit0，`targeted-two-rounds.log`。覆盖作者、cover、原08兼容、取消、MCP/stdio；covermaterial/proxy 无包内测试，不冒称该两包有定向用例。
- **完整 backend**：前端 build 后 `go test -p 1 ./... -count=1 -timeout=10m`（外层1800s），全部 PASS / exit0，`backend-full.log`。随后加强组合成功/Undo 的实际 HTTP 图片颜色断言（生产代码未变），先以 `final-exact-images-two-rounds.log` 再跑 enrichment/authors/旧08兼容/取消真实stdio两轮通过，再用同一全量命令 **完整全部通过**，`backend-final-full.log` / exit0。最终全部 Go 代码和测试均在后一次全量运行前完成；这是加强断言后的复验，无未知失败、盲 retry 或 skip。
- **完整 frontend**：`npm test -- --maxWorkers=2`，**121 文件 / 1155 测试 PASS**；`npm run build`、`npm run lint` 均 exit0，`frontend-{tests,build,lint}.log`。
- `gofmt -l ./cmd ./internal` 空，`gofmt.log` / `gofmt-final.log`；独立 cache `golangci-lint run` **0 issues.** / exit0，`lint.log` / `lint-final.log`（加强图片断言后再次通过）。
- `go run ./cmd/tsgen` 45 类型；再次生成前后 SHA-256 均为 `393c2c1f7f397bd995e62531a1e6156173edbd56f95451508f3f0ff70fa46a1b`，`tsgen-{before,after}.log` / `tsgen-final.log`。
- 本轮未新增 SQL 输入变化，但合并含原10 SQL，仍实际 PowerShell `sqlc generate` exit0；`query.sql.go` / `querier.go` 前后 hash 相同，`sqlc{,-before,-after}.log`。query.sql、schema_handwritten.sql ASCII 已核验，不声称原 schema.sql 全 ASCII。
- 最终 `git diff --check` 通过；工作文件没有冲突标记（许可证等号分隔线不是冲突）。父禁 stage，故 **git index 仍显示原 UU，这是刻意保留给父标记 resolved 的状态，不是源码仍含冲突**。新增未跟踪 `internal/application/enrichment/digest_test.go` 必须随父提交纳入。

### 11 / 13 可复用接点与 remaining

- 11：`enrichment.Service` / `ExpectedState`、`applyDigest` / `undoDigest` 是明确命令边界；复用实际事务 `AuthorizeTx`、字段/整组 expected 及 `MetadataJournal.TryChange`，不能在协议层另写 SQL 或在补充后以第二事务假装提案原子裁决。既有 `Operation`、Get/List/Undo 与管理页继续是权威收据投影。
- 13：`covermaterial` 不可变私有材料、`metadata_cover_materials` 多对多保留引用及 `CoverMaterialVisible` 是现有接口；`save` 为 changed 操作建立引用。须保护当前书/其他操作引用与活跃准备/Undo，不能按单条记录/年龄删除共享图。默认不清理，完整准备孤儿仍保守保留；未实现清理 API 或自动过期。
- 无已知本次组合 blocker；未证明断电/磁盘损坏等平台外保证，不把新的组合用例冒称完整强杀实验。**源码与门禁 merge-ready，父负责 stage/resolved 与提交，再发独立 review；票10仍 ready-for-agent。**
