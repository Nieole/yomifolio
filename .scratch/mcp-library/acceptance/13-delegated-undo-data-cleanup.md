# 13 — 独立授权清理撤销材料

## 范围与状态

- 唯一写入树 `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-13`，分支 `mcp-undo-material-cleanup`。开工 `git merge-base --is-ancestor 13ea7e66 HEAD` exit0，status 空；未 stage/commit/merge/reset，也未改父 TODO、11 或其他树。票保持 ready-for-agent，等待父 fresh merger / 独立 review。
- 已读外部 worker guide（旧前沿以用户本次说明为准）、AGENTS、effort/票13、08/10 acceptance、tracker/doc guidance、TDD、MCP builder/frontend design，以及 application/delegated/enrichment、database、seriesmeta、bootstrap/runtime、api、web 和 ADR0016 草案边界。covermaterial/taskcontrol 没有独立 owning SPEC，材料协调归 application、任务生命周期归 runtime。未把 ADR0016 提升 active。
- 沿用用户已确认接缝：生产 bootstrap、临时真实 SQLite/CBZ/PNG、通用 MCP HTTP、真实编译并启动的 stdio proxy、管理员 HTTP 与公开图片内容。仅既有外部元数据来源替身，无收费模型。未另建测试业务宿主或清理 goroutine 运行宿主。

## 实现策略与接口

- `enrichment.Service.Cleanup(CleanupInput)`：operation_id + request_key，仅该收据最多两份材料。有界同步单项合并真实 HTTP/stdio 入口取消；异步显式 1–20 项同库列表由 `StartCleanup(CleanupBatchInput)` 走同一 Runs.Start、PrepareRun、Checkpoint、Cancel，断线不取消已接纳运行，不登记自动续跑。
- MCP：`library_cleanup_metadata_operation`、`library_start_metadata_cleanup`、`library_get_metadata_cleanup`。cleanup 独立于 metadata_write/read，当前用户/凭据/实际库/cleanup 每调用及回收单元重新检查；cleanup-only 运行能沿既有 Get/Wait/Cancel/List 查询归属。无逐批确认、自授或扩权入口。
- 网页：`POST /api/mcp/operations/{operationId}/cleanup`，`GET /api/mcp/cleanups/{cleanupId}`；继承管理员、Cookie、CSRF 与改密守卫，no-store。管理历史提供损失警告、单项显式清理、固定键网络重试、partial/pending/complete 反馈和不可恢复提示；清理后不渲染恢复值/旧图片/Undo 按钮。不把清理当删除藏书。
- 原子隔离事务：当前授权 + 删除目标操作的恢复/完整来源快照 + 保留身份、字段名/短理由等最小审计 + undo_unavailable/cleaned_by + 解除仅该操作保留引用。检查所有 books/series_stats/其他操作引用；当前或共享材料记 protected。只有无引用规范随机键可写永久 tombstone，并与 pending 收据一起提交。数据库触发器覆盖普通 books/series_stats 和 retention 引用写入，阻止已退休键重发布。
- 隔离提交后才有限 unlink：每文件前检查点及事务当前授权；os.Root、规范随机键与 Lstat 拒绝链接/非普通文件，不递归遍历，不接受任意路径/URL。逐项持久化 deleted/failed，partial 不冒称全回收。并发同键在每个单元及终态提交前重读持久结果，不用旧本地副本覆盖已落库失败。Undo 与换图按 SQLite 发布点线性化：Undo 已提交的当前/保留引用保护材料；清理先提交则 Undo outcome=unavailable/applied=false。
- 原 08/09/10 applyDigest/undoDigest 和冻结夹具未改。清理使用独立 user/credential/request_key 命名空间，显式 V1 JSON intent（version、request_key、operation_id），不 hash 响应 State。原元数据 request_hash 永久保留；清理后原补充重放会诚实投影 undo_unavailable，而非伪造完整旧快照。
- 默认自动 retention、普通缩略图清理与重开均不删除材料。只处理显式收据归属的键；完全准备但从未形成收据的孤儿不扫，活跃 prepare 的新随机键不成为候选。protected 后来失去引用及终态 failed 文件允许保守残留，不承诺零残留/自动追扫。终态结果重放不自动重试失败；pending 仅原键在当前获权后可继续。

## TDD 与实际公开覆盖

日志均在本树 `.tmp/13/`。

- `red.log`：先加生产公开清理/不可撤销断言，缺工具失败；接线后最初 `green.log` 发现清理 DDL 未被迁移安装，修正后 `green2.log` 通过。批量发现断言先因两个工具未注册失败（`batch-green.log`），完整接线后 `batch-green2.log` 通过。
- `ui-red.log`：先加损失警告、无逐批确认、partial 与固定键重试测试，2 failed/9 passed；实现后 `ui-green.log` 11/11 通过。
- 生产 `TestProductionMCPEnrichmentAuditUndo` 复用原真实10封面夹具，新 `verifyCleanup`/`verifyCoverCleanup` 验证：
  - MetadataWrite 不隐含 Cleanup；cleanup-only 不获 Read；当前用户降级后 MCP/网页拒绝；真实 foreign-series/library 归属的猜测 operation_id 拒绝，不改变其公开审计。
  - 正常 run history retention 后旧/新图仍公开可读；独立获权 agent 不经逐批确认自主清理。
  - 原 enrich 与 Undo 收据多对多共享保留图；清理一条不误删另一条引用。清最后一条后真正无引用文件不存在且公开404，当前红图仍实际解码为红色；退休键 UPDATE 被发布 fence 拒绝。另一 series 的 current cover 也单独保护。
  - 真实并发 Undo/cleanup 只得到完整 changed 或 unavailable；changed 时恢复图仍公开可读。并发普通人工换封面/cleanup 后公开封面仍可读。
  - 真实 stdio 子进程执行文件故障清理：将材料替换成非空目录，不递归删，结果 partial；修复故障后同键仍返回原 partial，ID不变。
  - 真实 SQLite trigger 令 unlink 后的结果写入失败：HTTP500，但最小审计已 undo_unavailable、结果 pending；关闭服务/连接并重开后公开查询 pending，原管理员键重放完成回收，不虚报未完成工作。终态 partial 及 cleaned_by 审计亦跨重开保持。
  - 真实 stdio 接纳排队 cleanup run 后 helper 退出；放行后同一引擎完成，可用 HTTP 查询持久结果。队列中的显式取消、撤销凭据各自阻止开始，未处理项目仍有原恢复材料。
- 中间测试编排失败诚实保留：`physical.log` 把当前封面保护断言放得过晚，`physical3.log` 在更换配置 authority 后仍用原TLS MCP客户端，分别修正位置/配置而非放宽保护；扩展清理后原10“无 retention 引用即未发布”的测试前提不再成立，`races.log` 暴露该点，查询补上**当前 books 引用**，原404/Windows别名断言保留，`races2.log` 通过。
- `pending.log`、`retention.log`、`focused-final.log`、`final-focused.log` 通过，覆盖持续扩展后的原08持久收据/旧数据库重放、09作者、10原子cover/text、真实HTTP/stdio取消和本票清理。没有用删除原断言/弱测试掩盖失败。

## 实际门禁（不是全部通过）

环境：Go1.26.10；共享 GOCACHE 按 guide，本树 `.tmp/13/gotmp`，独立 `.tmp/13/lintcache`；Node26.13.1/npm11.20.0。从10复制 ignored dist/node_modules 后，确实本树完整测试/重建。

1. 定向：`go test -p 1 ./internal/bootstrap ./internal/application/enrichment ./internal/api -run 'MCP|Enrichment' -count=1 -timeout=5m` PASS（`focused-final.log`）。最终同键持久结果重读增强后：`go test -p 1 ./internal/bootstrap ./internal/application/enrichment ./internal/application/delegated ./internal/database -run 'MCP|Enrichment|Delegated' -count=1 -timeout=5m` PASS（`final-focused.log`）；后两包该正则无匹配，不冒称其有定向测试，它们在一次全量中实际通过。
2. **完整后端仅一次**：`go test -p 1 ./... -count=1 -timeout=10m`，外层1800秒（`backend-full.log` / exit1）。唯一失败是新增 `/api/mcp/cleanups/{cleanupId}` 未登记到 `TestNoReadEndpointLeaksHostPath` 的路径参数替换器。修复只是增加 cleanupId 占位，让真实普通用户请求也被 sweep 覆盖，没有 skip/豁免；定向 `go test -p 1 ./internal/api -run TestNoReadEndpointLeaksHostPath -count=1 -timeout=3m` PASS（`route-sweep-green.log`）。按用户一次完整约束未再跑全仓；最终代码不能宣称完整后端绿灯。
3. 路由修复后实际 API 全包：`go test -p 1 ./internal/api -count=1 -timeout=10m`（`api-after-route-fix.log` / exit1），出现 `TestRelocationCommitRejectsChangedConfirmationFacts/config` 的 `rename source target: Access is denied`。未改 relocation，未盲重试、未宣称已有受控基线证据。此包整体仍非通过，留父审阅/集成门禁处置。
4. 前端 `npm test -- --maxWorkers=2` PASS：121 files/1157 tests（`frontend-full.log`）；`npm run build` PASS（`ui-build-first.log`）；`npm run lint` PASS（`frontend-lint.log`）。本轮后续未改前端源。
5. Go `gofmt -l ./cmd ./internal` 空；独立 cache `golangci-lint run --allow-parallel-runners` 0 issues（`lint-last.log`）。曾普通 `golangci-lint run` 因全局另一个 runner 锁被拒（`lint-final.log` exit3），改用明确允许并行、仍独立缓存，并非把未执行当通过。
6. `go run ./cmd/tsgen` 47 types，最终生成前后 SHA-256 相同（`tsgen-final.log`、`tsgen-before.log`/`tsgen-after.log`）。首轮漏 MaterialResult target 确实失败，已补 target 后实际生成；无手写 generated.ts。
7. 实际 PowerShell `sqlc generate` exit0（`sqlc.log`），既有 query.sql.go/querier.go/models.go 无 diff；本票是独立 handwritten schema_cleanup.sql，无 sqlc 输入改变。新 SQL ASCII；新表/触发器走统一 Migrate，避免将 trigger body 给按分号拆分的既有 schema 执行器。
8. 最终 whitespace/mockups/format 检查记录在本目录日志；没有新增依赖、付费模型、浏览器截图或强杀/断电实验。

## 界面及 remaining

- 静态 inline-CSS mockups：`.thinkrail/context/mockups/13-before.html`、`13-after.html`；无 scripts、controls、外链，非真实浏览器截图。沿用原设置页样式，仅新增损失告知/清理动作/权威结果。
- 长期契约更新 application、database、api、bootstrap、architecture、web；CHANGELOG 与生成契约同时更新，ADR0016 仍 draft。票最后门禁项未勾。
- **明确未测**：没有确定性卡住“已开始批次两项之间”再撤权/取消的生产屏障试验；本轮确实覆盖排队撤权/取消、已接纳stdio断线继续、每文件/每项目的生产当前事务授权与共享检查点接线，不把它们冒称所有运行中调度已穷尽。没有实际软链/Windows junction 故障试验、拔电/kill -9 或跨平台磁盘损坏保证；路径拒绝由固定键与 os.Root/Lstat 实现，非普通目录真实故障已测试。
- 全仓门禁尚非最终绿灯（上述实际失败），保护后后来失引用与终态filefail残留是有意保守策略，不是自动回收承诺。父应 fresh 合并/独立审阅，重点看退休触发器、并发清理结果、Scope当前授权、运行中检查点及 11 组合；不要直接标 done。

最终交付检查：`lint-delivery.log` 为最终同键结果重读代码后的 **0 issues**；`gofmt-delivery.log` 空，`diffcheck.log` exit0。mockups 静态检查无 script/controls/external resources，新 schema_cleanup.sql ASCII。generated.ts 最终 SHA-256 `759ffbed37fd6b2dddcbf3c98d136620c8db08a829dcd2d727e7cd6c7e98b531` 与再生成前后完全一致。结束 HEAD 仍13ea7e66，祖先复核通过，全部改动未提交。
