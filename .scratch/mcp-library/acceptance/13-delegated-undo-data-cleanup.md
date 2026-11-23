# 13 — 独立授权清理撤销材料

> 最新状态（2026-11-23 15:11 +08:00）：本树最终代码及现存 merger 测试增量已取得一次完整后端 PASS；独立 `13ea7e66` 对照亦 PASS。此前 Windows rename 失效仍未定位，**不是已确认修复**。下文 blocked/缺最终完整绿灯为各历史轮次结论，以末节“有界顺序对照”更新为准；票不自动关闭，父负责独立 review/集成。

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

## Fresh 独立审阅及补验收（本轮权威增量）

**结论：blocked（完整后端门禁未绿），保持 ready-for-agent，不关闭。** 本轮起止实现 HEAD 为 `e2b010ba`，base `13ea7e66` 为祖先；上文“结束 HEAD 仍为13ea7e66/实现未提交”是实施者交接时的历史状态，不代表本轮树状态。本轮只在指定13树增加未提交测试与本验收记录；没有 stage、commit、merge、reset、父 TODO 或11树操作。父仍负责提交、集成及独立 request_review。

### 审阅结果

独立读 `13ea7e66..e2b010ba`，重点核对 `enrichment.cleanupAuthority/Cleanup/StartCleanup/GetCleanup`、`delegated.PrepareRun`、`covermaterial.RemoveRetired`、`schema_cleanup.sql`、Undo、网页 `cleanupKeys`。未发现需要交父 freshfix 的已证实业务/安全缺陷，不代表穷尽证明：

- 当前 user ∩ credential ∩ 实际 library ∩ cleanup 在初始隔离事务、每文件 unlink 事务及终态事务重新裁决；SQLite immediate 写事务串行化授权提交与 unlink，不只是内存锁。metadata_write 不暗授 cleanup，guess ID 不授查询/回收资格。批次预检全部目标，混库拒绝，不先执行合法前缀。
- 先丢恢复快照/解除本操作引用并持久 pending、永久 tombstone，再回收。全局 book/series_stats/其他审计材料引用保护；发布 fence 覆盖普通换图和 retention 写入。Undo 的事务竞争只允许真实完整恢复或 unavailable，不伪报恢复。固定随机键、os.Root、Lstat，不接收路径、不扫藏书；无自动 janitor/reopen 回收。
- 每单元持久结果重读避免同键并发覆盖结果。部分 IO failure 终态 partial 不重试；SQLite 结果落盘失败留下 pending，可同键当前获权后继续。08/09/10 原意图摘要与 old-key 守卫保留。网页重试键按不可变 operation.id 分配，切换目标不会复用别条键；损失警告/禁用/不可恢复投影保留。未改 ADR0016，其 status 仍 draft。

### 新增真实公开验收

复用用户已确认的生产 bootstrap + 通用 MCP HTTP/真实 stdio + 管理 HTTP + 实际 SQLite/图片接缝，不替换清理、授权或文件函数；仅 `Options.Tasks.Publish` 挂进度同步屏障。屏障停在第一项完整持久提交后，持有任务引擎发布锁，**不持有元数据事务**；本试验不通过该屏障调用需引擎锁的取消接口。

- 两种运行中情形：真实编译启动 stdio proxy 接纳两项 cleanup 后退出，第一项进度=1 时由真实管理员 HTTP 分别 DELETE 凭据、POST `/users/{id}/password` 重置执行者密码（不是模拟改 revoked 字段）。放行后真实运行为 cancelled、StartedAt 非零、Current=1；第二项审计仍可恢复，前后图片公开200。另一当前获权凭据能在同库完成剩余项，证明库预约/运行槽未泄漏。这里测的是**管理员重置密码撤销**，不是同用户主动 change-password 的运行中组合；原账号事务守卫仍保留。
- 真正的同库+跨库混合批次被 HTTP MCP 拒绝；同一清理 request_key 改 operation_id 被拒绝。原 cleanup-only、metadata-write-only、角色降级、外库 guessed ID 守卫未删改。
- **真实 Windows junction**：将私有 metadata-covers 子目录暂存，PowerShell 创建目录 junction 指向根外临时目录，放置与待清随机键同名的 sentinel；通过 MCP 清理得到 partial，根外 sentinel 字节不变，恢复原目录后当前 cover 公开可读。未 mock Lstat，也未 skip 权限失败；非 Windows 分支为 os.Symlink，但本机只实际执行了 Windows junction。未另测 Windows 叶文件 symlink、目录交换攻击的全部调度。
- 原真实 nonempty-directory IO failure、unlink 后 SQLite trigger failure → close/reopen/pending 同键续办、Undo/人工换图竞争、跨审计/系列共享保护仍在同一测试运行，全部保留通过。这是确定性故障 + 正常关闭重开，不是进程强杀/断电；没有新增 crash 恢复宿主。

### Actual checks（日志 `.tmp/13review/`）

环境 Go1.26.10，共享 guide GOCACHE，本树独立 gotmp/lintcache；Node26.13.1/npm11.20.0。

1. 一次完整后端：`go test -json -p 1 ./... -count=1 -timeout=10m`，外层1800s，**exit1**（`backend-full.json`、`backend-full.exit`）。唯一失败包 internal/api；精确失败 `TestRelocationPolicyCrashRecoversFromActualDurabilityBoundaries/runtime_apply`，`relocation_policy_crash_test.go:58`，`config.NewManager(disk).Save(...)` 的 `.config.yaml.tmp-* → config.yaml` rename 报 Access denied。这与实施者先前失败的 `TestRelocationCommitRejectsChangedConfirmationFacts/config` 的 source→target rename **不是同一位置**。本票 diff 未改 relocation、config、对应 crash 测试；失败发生在 crash 子进程结束后的配置保存阶段，本测试不调用 cleanup。但未做受控基线/锁持有者归因，**不能断言基线或已修复**；未盲重跑失败 relocation test/API 全包。本轮完整 bootstrap PASS（123.536s），本票生产公开测试 PASS（59.64s），其余全部有测试包 PASS。
2. 定向双轮：`go test -p 1 ./internal/bootstrap ./internal/application/enrichment ./internal/application/delegated ./internal/covermaterial ./internal/api -run 'MCP|Enrichment|Cleanup|Cover|Concurrent|Concurrency|Undo' -count=2 -timeout=10m` **PASS**（`focused-twice.log`，bootstrap 218.496s/api14.690s）。每轮真实编译启动 stdio；涵盖运行中撤权/密码重置及 junction。delegated 此正则无测试、covermaterial 无独立测试，不冒称该两包有新定向覆盖；文件验证在 bootstrap 公开测试中执行。
3. 最后仅补同键变目标、混库拒绝断言及故障恢复 defer 后，`go test -v -p 1 ./internal/bootstrap -run '^TestProductionMCPEnrichmentAuditUndo$' -count=1 -timeout=10m` **PASS**（`final-public.log`，51.05s），日志明确输出两种 running cancelled prefix=1 和真实 windows link sentinel 保留。完整后端没有在这些最后测试增量后再跑，未改业务代码。
4. 前端完整 `npm test -- --maxWorkers=2` **121 files/1157 tests PASS**；`npm run build`、`npm run lint` exit0（`frontend-full.log`、`frontend-build.log`、`frontend-lint.log`、`frontend.exit`）。本轮无前端源改动。
5. `gofmt -l ./cmd ./internal` 空、`git diff --check` exit0；独立 lintcache `golangci-lint run --allow-parallel-runners` **0 issues**。`go run ./cmd/tsgen` exit0；实际 PowerShell `sqlc generate` exit0；generated.ts 和 sqlc 三份产物再生成前后 SHA256 无漂移（`generation-lint.exit`、`generated-before/after.log`）。最后测试断言增量后的最终格式/lint记录为 `gofmt-final.log`、`lint-final.log`、`diffcheck-final.log`。
6. 调试失败如实保留：`barrier-first.log` 是误写 task.Run.Type 导致的编译红（不是业务 TDD 红）；修正为公开 Phase 后 `barrier-green.log` PASS。`junction-first.log` 为 PowerShell `-Command` 的参数传递写法错，未生成 junction；修正正确 quoting。`junction-green.log` 曾超时5m，栈停在后续原 cover-only batch 请求的 MCP schema 注册 `jsonschema.checkStructure`，不是撤权屏障或文件删除锁；未证明根因/未声称修复。随后本轮要求的一次完整门禁、双轮定向及最终公开运行均通过该生产测试。新增是既有实现的回归补验收，未借编译失败冒称业务缺陷 red→green。

### 未关闭项及交接

- **阻塞：完整后端 Access denied 仍待父定位并取得最终全绿或明确接受阻塞；本票不能 done。** 没有已证实业务 freshfix；若父复现出本票影响再按具体位置修。
- 未做独立“第一项已完成时主动 cancel API”屏障（已有排队 cancel），未穷尽回收单项两个文件之间撤权、跨两个进程同时 unlink、强杀/断电、Windows 文件叶 symlink。事务和持久 fence 的代码审阅不冒充这些运行证据。所有目录/文件在用与审计保护守卫未 skip/放宽。
- UI mockups 仍为 `.thinkrail/context/mockups/13-before.html` / `13-after.html`，仅静态示意，不是浏览器截图。本轮只补测试/acceptance，不扩功能、不更新长期契约；父拥有提交/集成/独立 request_review。

## Fresh Windows rename 诊断（未解决，不关闭13）

本轮仅追加本节；开工已有13merger的 issue/acceptance、三个 bootstrap 测试修改全部保留。HEAD `e2b010ba`、分支 `mcp-undo-material-cleanup` 未动；未创建/切换对照树，未 stage/commit/reset/merge，未操作父 TODO 或11树。已读 AGENTS、diagnosing-bugs/TDD skills、外部 worker guide、Fresh 节及 `.tmp/13review/` 全部文件；完整日志扫描清单及失败事件摘录在 `.tmp/13diagnosis/review-log-inventory.log`。

### Facts / 真实路径

- 权威失败仍是 `backend-full.json` 的 `runtime_apply`：子进程确实以23退出后，父进程 `LoadConfigFile`，再由**新建独立 Manager** 的 `Save` 替换该子测试独有的 `config.yaml`，临时文件到目标 rename 报 Access denied。不能把这一步说成正在调用 cleanup 或仍在调用 crash 子进程。
- `AtomicWriteFile` 写临时文件、Sync、Close、Chmod、最后 `os.Rename`，没有发现遗失的自身临时句柄。`LoadConfigFile`、Save/Reload/Coordinate 的文件读取使用 `os.ReadFile`，读取返回前关闭句柄；没有证据证明 loader 漏 Close。
- `newAPIFixture` 只执行 `runtime.New` 和 `Recover`，不执行 `Start`，**本 crash fixture 不启动配置热重载 watcher**。生产 bootstrap 才通过 `Start` 开配置 watcher；`Close` 停配置 watcher、Shutdown 任务、停扫描 watcher、等待登记工作后释放 scanner/resources。已核对这些路径，但静态顺序不等于已经捕获原失败的锁持有者。
- stdio helper 的 config 引用仅用于 origin 校验；其输入是 CLI origin 与环境凭据，未调用 `LoadConfigFile`，不读该 crash fixture 的 YAML。HTTP worker 取消后 Wait，HTTP response body Close，transport CloseIdleConnections；没有以修改 stdio 代替本问题诊断。
- `13ea7e66..e2b010ba` 在 config、relocation crash/fixture、runtime 生命周期及 runtimecfg 的相关路径 diff 为空（`.tmp/13diagnosis/critical-path-baseline.diff`）。这只证明这些实现未被13直接改动，**不证明13整体负载/调度没有间接影响，更不证明基线失败**。

### Hypotheses / 可证伪预测

1. 配置目标存在未允许 FILE_SHARE_DELETE 的读取句柄：保持这样的真实句柄时应得到相同错误，关闭后应恢复。此机制实验已证实；**原失败是否存在此句柄、由谁持有尚未证实**。
2. fixture/任务关闭未 drain：若成立，应能在 Close 返回及子进程结束后抓到本进程针对该唯一路径的剩余访问。当前静态检查未发现来源，未采到实际反例；不能据此宣布完全排除。
3. 子进程终止后的系统资源释放/其他进程文件访问：若成立，应在失败现场观察到相关句柄/进程或系统文件事件。尚无现场证据；不指责杀毒软件，也不据此加入重试、sleep 或替换权限逻辑。

### Actual checks / 非最终门禁

环境 `GOTOOLCHAIN=go1.26.10`；guide 指定共享 GOCACHE；本树独立 `.tmp/13diagnosis/gotmp`。本轮没有改实现，以下不是修复后的 green：

1. `go test -v ./internal/api -run '^TestRelocationPolicyCrashRecoversFromActualDurabilityBoundaries$' -count=1 -timeout=2m`：PASS，1.099s（`original.log`），三个真实边界完整保留。原单测在本次隔离环境未红。
2. 为检验终止后竞态，预先限定一次放大实验：相同正则 `-count=50 -timeout=3m`：PASS，66.975s（`crash-amplification.log`），共150个真实子进程崩溃边界。不是循环同一命令直到 green；其结果仅说明本次隔离放大**未再现**完整门禁故障，不抵消已有完整门禁红灯。
3. 独立临时诊断程序直接调用真实 `config.AtomicWriteFile`：保持真实 `os.Open(config.yaml)` 句柄，替换确定性失败 `Access is denied / syscall.Errno=5`；Close 同一句柄后替换成功（`handle-probe.log`）。这是 sharing 机制的阳性对照，不是原 crash 调用链的回归测试；没有把人为遗失句柄冒称实际根因或 TDD 红灯。程序已删除，采证日志保留。
4. `git diff --check` exit0。未改变业务/测试 Go 源，不需要新增格式、生成、lint或UI门禁；未冒称执行修复后定向迁移/config/cleanup/真实stdio双轮和完整backend。这些修复后要求仍未满足。

### Remaining / 父需提供的对照与现场证据

**结论仍 blocked，根因未确定，无最小修复可诚实交付。** 没有正确的、在实际 crash 调用链稳定红灯的回归接缝，故不添加猜测性的生产平台适配、全局读写锁、权限放宽、重试、timeout变化或测试豁免。真实 durability/crash/cross-manager 守卫全部未动；不把 API 测试迁到其他 tag。

下一步需要父创建独立临时对照 worktree，精确 ref `13ea7e66`，保留当前13树及所有未提交审阅修改。建议给出可写树路径，并以同 Go1.26.10/共享 cache/独立 tmp/dist、相同外层1800s完整后端命令作一次受控对照；即使基线一次通过，也不能证明间歇故障由13引入。后续应在真实失败现场采集该唯一路径的锁持有者/Windows文件事件及 errno，再选择能复现该持有者生命周期的屏障测试。当前环境未发现可用的 handle/handle64/procmon 工具，不能伪造锁归因。必要时由父提供现成采集能力。

本轮没有修改产品契约，故 owning SPEC/CHANGELOG 不增加未经证实的“Windows rename 已修复”或历史解释；若后续确定并修复资源契约，再同步对应 owning 规格与修复日志。父仍需最终完整后端全绿才能关闭13。


## 有界顺序对照（2026-11-23，本轮权威门禁增量）

**结论：13 本轮可实证通过完整后端，但此前失效仍未定位；无已确认修复。** 已消除“最终测试增量尚无完整后端绿灯”的证据缺口，不把一次 green 当修复、不把基线 green 当13导致历史失败的证据。没有添加 retry/sleep、放宽测试或更改资源契约。历史红灯及日志完整保留。

### 控制变量与 Actual checks

已读 AGENTS、diagnosing-bugs/TDD、config owning SPEC、GLOSSARY、父 worker guide、本文件诊断节及 `.tmp/13diagnosis` 现有证据；未重做50轮/150个 crash 边界实验。父提供的独立对照树源码保持不变（前后 git status 均空），仅从13复制 ignored `web/dist`、创建 ignored tmp。13 本轮只改本验收 notes，无源码/测试增量；原 merger 修改原样保留，未操作11、父 TODO、索引或历史。

- 两树均 Go1.26.10 windows/amd64，同共享 `GOCACHE=C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-01/.tmp/merger01/gocache126`；各自 `GOTMPDIR=<tree>/.tmp/13diagnosis/gotmp`，GOFLAGS 空；同一 Python runner 顺序等待退出后才启动下一树。
- 完整命令两次均为 `go test -json -p 1 ./... -count=1 -timeout=10m`，每次外层1800秒。未过滤测试、未加tag、未并行启动本轮两次 suite。包内原有并发保持不变。
- frontend embed 使用完全相同文件内容，排序相对路径+文件字节树 SHA256 均 `dbbb8cfbd47d4a6a95b670a5daf1fdb8ea2308fce6c0c9df7d89145c4aec70e4`；并非本轮前端重建/测试。
- **对照** `mcp-relocation-control` HEAD `13ea7e66f92203d80e7363ed968de756b6943802`，14:52:06.077→15:01:11.661 +08:00，PID6828，545.584s，**exit0**，38个有测试包 PASS/0 fail事件；API170.942s/bootstrap97.286s。
- **13** `mcp-library-13` HEAD `e2b010ba9a7c6aca92acdc629faa871fb99e6b7d` + 开工即存在的 merger 三份 bootstrap 测试及 notes，15:01:14.048→15:10:27.747 +08:00，PID42900，553.699s，**exit0**，38个有测试包 PASS/0 fail事件；API206.441s/bootstrap107.587s，`TestProductionMCPEnrichmentAuditUndo` PASS49.30s。两次 `TestRelocationCommitRejectsChangedConfirmationFacts` 和 `TestRelocationPolicyCrashRecoversFromActualDurabilityBoundaries` 均 PASS。
- 原样 stdout+stderr 合并保存 `.tmp/13diagnosis/{control,ticket13}-full.combined.jsonl`；退出码、PID、HEAD、前后status、命令、环境路径、embed摘要、起止时间及耗时保存对应 `*-full.meta.json`。runner=`controlled_suites.py`，摘要=`controlled-suites.summary.log`，完整证据读取/哈希与事件汇总=`evidence-inventory.txt`。没有超时或因超时终止进程。
- 对照顺序有缓存预热/时段差异，不是严谨性能比较；两个 commit 的实际测试集合随功能增加而不同，但运行方式/完整API负载不做筛选。11在别树独立处理stdio的并行工作存在；15:11只读进程快照确实看到 `mcp-library-11/.tmp/11/freshfix/gotmp/.../api.test.exe` PID13688（父Go PID52960）。未抓本轮全时段进程采样，不能承诺两轮系统总负载完全相同。

### 采集能力与归因边界

只读 `Get-Command`、`wpr -status`、`logman query -ets`、`logman query`、`Get-CimInstance Win32_Process`，结果在 `capture-capabilities-utf8.log`。只有系统 wpr/logman；handle/handle64/procmon 不在PATH。WPR未录制（查询exit0）；存在 NtfsLog、FilterMgr、系统遥测等已运行ETW sessions，但没有定义的用户collector。这些名称**不是目标文件打开/关闭/rename事件，更不是holder证据**，未据此归因任何安全软件。未导出或更改系统session、未启动trace、未请求管理员、未安装工具/改系统安全设置。首个查询的编码及CIM quoting失败保留于 `capture-capabilities.log`，已用独立ps1修正；无权限失败被伪称采集成功。

本轮没有实际 rename 失败现场，故没有可诚实报告的失败path/errno/RestartManager holder快照；未增加失败fixture或事后拿已删除临时路径查询充当现场证据。此前人为持有 `os.Open` → errno5、Close → 成功，仅证明 Windows replace 对不共享删除的打开句柄敏感；**没有证明历史失败一定由句柄引起、持有者是谁，也没有证明基线在真实suite中失败**。watcher未启动、ReadFile关闭、stdio不读该YAML的既有检查仍成立，不扩张为完全排除自身生命周期问题。

### Remaining / 父可采取的结论

- 本轮限定的两次有意义完整suite已用完，停止追加重跑。没有TDD最小修复：实际调用链未再红、无可定位自身句柄生命周期缺陷；不从人为holder阳性对照直接造生产retry。
- 最新状态应记作“13完整门禁实证PASS、历史Windows rename间歇失败未定位”，而非“Windows rename修好了”或“已确认基线不稳”。前端/格式/lint/生成物沿用前轮准确记录，本轮只改notes、不冒称重新执行。
- 若父要求关闭历史根因，需要**下一次失败当场**的文件打开/关闭/rename时序与进程/holder证据（可用失败点只读RestartManager快照作补充，空结果不排除瞬时/内核访问）。当前没有证据表明必须用户提权采集；若选需管理员的文件事件采集，应先由父/用户明确批准有界方案，不在本轮自行启动。父决定是否接受已知未定位的环境间歇风险并继续集成/独立review；本票仍不自动done。


## Canonical review rc_000c6675 fresh 窄修（2026-11-23，本轮权威增量）

**结果：已修复刷新后 pending 清理不可检查/续办的 finding，保持 ready-for-agent，未 done，等待父独立重审。** 唯一写树 mcp-library-13，分支 mcp-undo-material-cleanup，开工/交付 HEAD 均 5e7dfaf0；开工 status 空。未 stage/commit/merge/reset、未改父 TODO、11 或其他树。

### 当前实现与边界

- `useOperationCleanups` 从持久 `Operation.cleaned_by` 去重查询真实清理结果；完整卸载再挂载仍显示 pending，不再因 undo_unavailable 隐藏查询/续办能力。每条结果显示 pending/partial/complete；隔离操作不再创建新键。查询失败可重新读取，同键 POST 失败（含取消样式）可再次续办。原损失警告、无逐批确认、无 Undo/恢复值/旧图片的约束不变；不使用 localStorage。
- `InspectCleanup` 的可选 `retry_request_key` 仅为**原网页执行者本人**的 pending 请求提供。读取数据库中既有 request_key，并检查原 1–80 字节 printable ASCII 格式、原 V1 cleanupDigest 与 operation_id、当前库/管理员权限；不改变任何已有键、摘要或隔离记录。原 POST 自身仍重查当前权限并用同键摘要拒绝异目标。不会将 agent/其他管理员的 namespace 转给检查者；那些结果可检查，续办仍需原执行者当前获权后原键调用。响应不返回 Bearer/宿主路径/SQL 错误。
- UI 查询/重试身份归 hook，页面只呈现动作和采用权威结果；没有巨型组件重写。API/application/web owning specs、CHANGELOG、13-after 静态 mockup 已更新，13-before 未改（SHA256 196774ac374cb964a6c5c0dc6c5b6404d3a2613afef3e53fb150bda67d23f199）。

### TDD / 接缝

沿用用户指定的设置页/API 边界及生产 bootstrap + SQLite/真实图片/HTTP/MCP/真实 stdio 接缝，不引入替代业务宿主。

- UI 先红 `ui-red.log`：11 passed/1 failed，完整卸载再挂载无法显示 pending。实现后 `ui-green4.log` 12 passed；追加去重同 result 多操作（只允许绑定 operation 使用 retry identity）、他人 pending 不分配替代键、lookup failure 重读，`ui-additional.log` 15 passed。
- 后端先红 `backend-red.log`：生产关闭重开后 GET pending 缺 retry identity。实现后 `backend-green.log` PASS。最终生产测试增量：真正 SQLite trigger 令隔离/unlink 后结果写入失败，持久 undo_unavailable/cleaned_by/pending；正常关闭进程宿主和连接、重新 Open，真实管理员 GET 恢复**原** request_key，用该返回键 POST 完成同 ID/replayed=true，并用该键改目标真实 HTTP400。另一个凭据产生的真实 pending 只供管理员检查，不暴露其 retry identity。
- 双轮保留原跨库拒绝、多引用/当前图片保护、Undo/换封面竞争、真实 stdio、运行中撤权/改密和真实 Windows junction sentinel 保护，未 skip/放宽。

### Actual checks（完整日志 `.tmp/13freshfix/`）

环境实际 Go1.26.10 windows/amd64，共享 guide GOCACHE，本树独立 gotmp/lintcache；fnm Node26.13.1/npm11.20.0。没有把 reviewer 未初始化 fnm 当工具缺失。

1. 相关双轮 `go test -p 1 ./internal/bootstrap ./internal/application/enrichment ./internal/application/delegated ./internal/covermaterial ./internal/api -run 'MCP|Enrichment|Cleanup|Cover|Concurrent|Concurrency|Undo' -count=2 -timeout=10m` **exit0**：bootstrap254.201s/enrichment1.544s/api14.122s。delegated 正则无匹配、covermaterial 无独立测试；实际文件保护在 bootstrap 验证（`focused-twice.log/.exit`）。
2. **本轮仅一次完整后端** `go test -json -p 1 ./... -count=1 -timeout=10m`，外层1800s，15:31:34→15:40:42，**exit0，38个有测试包PASS，0 fail事件**。生产 `TestProductionMCPEnrichmentAuditUndo` PASS56.75s（`backend-full.jsonl/.exit`）。本轮未出现 Windows rename failure，未盲重试，也不宣称历史间歇失败根因已修复。
3. 前端完整 `npm test -- --maxWorkers=2` **121 files/1161 tests PASS**（`frontend-full.log/.exit`）；`npm run build` **exit0**（最终 `frontend-build-final.log/.exit`；仅非失败 PLUGIN_TIMINGS 提示）；`npm run lint` **exit0**（`frontend-lint.log/.exit`）。
4. `gofmt -l ./cmd ./internal` 空；独立 cache `golangci-lint run --allow-parallel-runners` **0 issues**（`gofmt.log`、`lint.log/.exit`）。`git diff --check` exit0。
5. 实际 `go run ./cmd/tsgen` 47 types；generated.ts 增加可选 retry_request_key，再生成前后 SHA256 均 `9a0ba1c06bd00072b5a9bd088d95136cc465fc3df0b244d0606dfa3f1e25daf6`（`tsgen-drift.log`、`generated-before/after.log`）。无 sqlc 输入/DDL 变更，不需重新生成 SQL bindings；新 SELECT 位于应用现有 handwritten cleanup journal 边界。

### Remaining / 不冒称的覆盖

- UI reload 用真实契约的 API fixture，后端真实持久故障/重开 HTTP 单独覆盖；不是运行浏览器连接后端的端到端截图测试。mockup 是静态示意。
- 本轮未新增“恰在单项隔离提交后主动断开 HTTP”的确定性屏障；生产重开用结果写入故障稳定制造相同持久 pending，UI 续办 cancel/retry 用 API 拒绝模拟；既有真实排队 cancel/运行中撤权/stdio 行为继续在双轮及完整后端中执行。不能把这些冒称全部取消调度已穷尽。
- 不扩权为管理员代其他执行者重试，也不自动追扫 terminal partial 或 protected 后失引用材料。历史 Windows rename 间歇失效仍无根因确认。父负责 fresh 独立 review、提交与集成，不自动关票。
