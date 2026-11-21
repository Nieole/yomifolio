# 08 安全元数据补充、审计及撤销验收

> 原实施及独立 merger 记录保留为历史；最新失败撤销窄修与门禁结论见文末。

## 工作树与范围

- 唯一工作树：`C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-08`；分支 `mcp-enrichment-audit`，HEAD `b3083e4bd2e608521519f05fb1467fead8e0f6e8`。
- 全部改动未提交，未 stage、commit、merge 或操作 12/父树。接续现有未提交实现，不重做或删除功能；仅统一前代理引入的 CRLF，消除无关整文件 diff。
- 07 确认提案是唯一事实输入；新增同步 enrichment 应用用例、完整收据持久化、薄 MCP/管理员 HTTP、管理员操作历史和撤销交互。没有作者、封面、LLM、清理或第二个 Runtime。

## 实际验收

主接缝已由 effort spec 确认：真实 `bootstrap.Open` + 临时 SQLite + 通用 Go MCP SDK + 网页 HTTP；仅外部来源替换。`TestProductionMCPEnrichmentAuditUndo` 覆盖：

- 明确身份/版本确认提案后组合 Bangumi、AniList 字段；描述只补空、非空和既有评分不变；既有个人标签保留，来源 favorite/年份/站点标签不导入，内容标签追加；HTTP(S) 来源链接补充。
- 9/10 与 80/100 等权得到 8.5，完整审计保留原值、尺度及 `(9/10*10 + 80/100*10)/2 = 8.5`。来源适配回归验证 AniList 88/100、MAL 7.98/10、Bangumi 7.6/10 以及缺失评分不伪造；已有十分制 Rating 转换未变。
- 相同键同载荷返回原 ID、replayed=true/applied=false；同键不同载荷拒绝。
- 可控提案读取屏障（无 sleep）把并发编辑、新锁、撤权放在实际写事务之前：相关字段/锁导致整项 conflict；撤权拒绝；当前用户降级后网页查询/撤销与 MCP 撤销拒绝。
- 无关 publisher 修改不阻止新补充或撤销，也不会被覆盖。整组标签后续新增和当前字段锁阻止整项撤销，未部分恢复。
- SQLite 触发器在标签写入时报错，前面描述写入真实回滚，公开 State 仍为空，失败收据持久可查。
- 多系列 changed/conflict 独立报告；失败不回滚别的系列。公共应用测试另守护大收据后的响应预算和 unprocessed 剩余项。
- 管理员可在源凭据撤销后撤销，恢复原评分、描述、链接及原有标签；查询关联撤销 ID。关闭生产资源、关闭原 SQLite store，再以新连接和新生产图查询并撤销另一个持久操作。
- 旧普通编辑、扫描集合、普通提案守卫随完整后端门禁运行；不改这些入口语义。

网页交互 `metadataOperations.test.tsx`：展示前后/短理由/来源，确认撤销后采用权威结果，冲突不误报恢复，普通用户不请求/渲染，游标分页去重，网络失败后同键重试。两份自包含概念示意保留于 `.thinkrail/context/mockups/08-{before,after}.html`；不是浏览器截图或视觉回归证据。

## 红绿与发现的真实失败

1. 接续后的首次定向 Go 测试：bootstrap 编译失败，前代理把无返回值 Process.Close 当作有返回值；修复后通过。
2. 新增公开冲突审计断言先红：第一个相关字段冲突时 fields=[]，丢失所选来源；补充/撤销不再提前 break，保留全部相关审计，再整体复位 Changed/After，通过。
3. 新增大收据 Batch 用例先红：实现继续执行后续项，没有兑现描述中的响应预算。执行下一独立单位前预留最大 200 KiB 收据和剩余占位项；不足预算明确 unprocessed，通过。非法请求键不原样放大失败响应。
4. 前端初次 build 失败：SettingsSectionKey 缺 metadata-operations；接通非草稿 section 后通过。初次 eslint 还有 cleanup ref warning，修正请求 generation 检查后无警告。
5. Go 初次 lint 报 unused newMCPHandler wrapper，移除冗余 wrapper、保留单一注入函数；随后 gofmt 提示 host.go，格式化后 0 issues。
6. 第一次完整后端失败仅在旧 MCP 精确工具清单，未加入新六个工具。补齐精确名单和只读注解断言（不删守卫）后定向 bootstrap 通过；有明确修复后仅做一次纠正全量运行并通过，不是盲重试。日志保留所有失败。

## 实际门禁

环境：Go `go1.26.10`；共享 GOCACHE=`C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-01/.tmp/merger01/gocache126`；本树 GOTMPDIR=`.tmp/08/gotmp`，GOLANGCI_LINT_CACHE=`.tmp/08/lintcache`；Node 26.13.1。

- `go test -p 1 ./... -count=1 -timeout=10m`：纠正后完整通过，`.tmp/08/backend-full-corrected.log`。首次失败日志 `.tmp/08/backend-full.log` 不隐藏。
- 此后增加更严格公开断言、关闭原 store 再重开及失败响应键大小保护；最终 `go test -p 1 ./internal/application/enrichment ./internal/bootstrap -run 'Enrichment|MCP' -count=1 -timeout=3m` 通过（`.tmp/08/backend-final-focused.log`），最后扩展的重开/同键不同载荷公开用例再通过（`.tmp/08/public-final.log`）。未把定向运行冒称又一次最终全量。
- `gofmt -l ./cmd ./internal`：空；`golangci-lint run`：最终 **0 issues.**（`.tmp/08/lint-delivery.log`）。`git diff --check`：通过。
- `npm test -- --maxWorkers=2`：121 文件、1149 测试通过（`.tmp/08/frontend-tests-final.log`）；随后新增分页和网络失败同键重试断言，定向该文件 5/5 通过（`.tmp/08/ui-final-focused.log`）。不冒称全套已运行 1151 项。
- `npm run build` / `npm run lint`：最后改动后通过，无警告（`.tmp/08/frontend-{build,lint}-delivery.log`）。
- `go run ./cmd/tsgen`：生成前后 SHA-256 一致，未漂移。
- PowerShell `sqlc generate`：退出 0，只有已知 parser 诊断；sqlc 生成文件无 diff。schema_handwritten 不进入 sqlc 模型输入。
- `sql/query.sql`、新 handwritten SQL 均 ASCII。

## 所属契约与复用

长期行为更新到 architecture、application、database、seriesmeta、metadata、api、bootstrap、web 的所属规格及 CHANGELOG；未改 ADR 状态或工单 triage 为 done。`enrichment.Service` 提供 State/Apply/Batch/Get/List/Undo，供同一生产图的网页及 MCP 复用；`MetadataJournal` 绑定真实写事务；授权继续复用 delegated.Subject/AuthorizeTx，不存授权快照；ContentTags/EnrichmentRating 是来源策略函数；State、EnrichmentLocked、WriteEnrichmentState 把字段锁解释和事务绑定写入归口 seriesmeta，应用只编排授权、预期值、收据与提交。最后做这项边界归位后，`.tmp/08/domain-boundary.log` 中 enrichment/bootstrap/seriesmeta 的 Enrichment/MCP 定向回归通过，tsgen SHA-256 仍一致，最终 lint 0 issues；不冒称这之后又全量测试。12 合并时共享装配/契约/CHANGELOG 文件可能冲突，父代理应合并而非覆盖。

无已知票 08 未满足项。限制：此轮未跑真实浏览器截图/像素测试，HTML 为概念示意；不宣称所有潜在并发顺序均已穷尽。清理、作者、封面与 LLM 是后续票范围，不是本票已实现能力。建议 commit subject：`feat: add audited metadata enrichment and safe undo`。


## 独立 merger：4b7265ab + 待完成合并 46d06126

**结论：存在具体阻塞，尚非 merge-ready。** 仅操作本票工作树；HEAD `4b7265ab9a60408998764f765763eaa86189be3b`，MERGE_HEAD `46d06126df577d1c772c4781165772277a01ac1d`。CHANGELOG 逐段保留 08、05（含 RAR 预算记录）、07，去除冲突标记，未整文件 ours/theirs。未 stage/commit/merge/reset/abort，索引仍显示 UU，须父代理确认并 stage。

### 独立发现：失败撤销缺持久实际结果审计

`Service.Undo` 中 `WriteEnrichmentState` 返回存储错误时直接返回，安全回滚，但不像 Apply.failed 保存失败收据。无法通过操作历史查询该次撤销的身份、request_key、前后与实际失败结果，同键重试也无失败结果可重放。完整持久实际结果审计尚有缺口。**不会误标已撤销、不会丢撤销材料，也不是半撤销或数据损坏。** 按本轮要求仅报告，不扩修生产功能。

真实 SQLite 公开应用探针 `.tmp/08/undo-probe/undo_test.go`：补 summary 成功后安装 BEFORE UPDATE ON series 的 RAISE(ABORT) 触发器，Undo 返回错误；State 仍为补充后值、原操作 UndoneBy 为空、List 只有原 enrich，缺 failed-undo 收据。`go test -v ./.tmp/08/undo-probe -count=1` 通过（表示缺口被证实，不是该行为验收通过）；日志 `.tmp/08/merger-undo-failure-probe.log`。需父代理修复/裁决此项后再核验。

### 核对范围及实际门禁

独立复查补空与锁、保守标签词表追加、链接、原始站点评分及等权公式、多来源确认、实际事务 AuthorizeTx/相关字段比较/整组标签、逐系列部分成功、持久身份/前后/重试 digest、真实重开、当前授权查询与撤销。网页仍挂在原认证及 CSRF 树；普通手工/扫描/提案入口语义未改。图片、匹配、运行及六个 enrichment 工具均留在同一注册路径；bootstrap 同一资源图注入 enrichment 应用服务，协议无新增 SQL 直连。作者/封面未擅自加入 08。

Go 1.26.10、约定共享 GOCACHE、本树 GOTMPDIR、独立 `.tmp/08/lintcache-merger`；Node 26.13.1。本次无门禁失败、无盲重试。以下日志均在 `.tmp/08/`：

- **最终组合完整后端一次**：`go test -p 1 ./... -count=1 -timeout=10m`，外层 1800s，全部通过；`merger-full-backend.log`。覆盖之前 domain-boundary 归位及此次 05 自动接线，不再仅依赖旧定向记录。
- **MCP/Enrichment 定向两轮**：`go test -p 1 ./internal/application/enrichment ./internal/application/delegated ./internal/bootstrap ./internal/api ./internal/seriesmeta -run 'Enrichment|MCP' -count=1 -timeout=3m` 均通过；`merger-focused-{1,2}.log`。delegated 包此 pattern 无匹配测试，完整后端已实际运行其测试。
- `npm test -- --maxWorkers=2`：**121 files / 1151 tests**；完整 `npm run build`、`npm run lint` 均通过；`merger-frontend-{tests,build,lint}.log`。未新增真实浏览器截图验收。
- `gofmt -l ./cmd ./internal` 空；`golangci-lint run` **0 issues.**；`git diff --check HEAD` 通过；`merger-{gofmt,lint}.log`。
- `go run ./cmd/tsgen` 42 类型，生成前后 SHA-256 一致；`merger-tsgen*.log`。PowerShell `sqlc generate` 退出 **0**，生成代码无 diff；`merger-sqlc.log`。SQL 未修改，未反复生成。query.sql 与 schema_handwritten.sql 均 ASCII。

### 09/10/11/13 复用及集成限制

- 09：复用 Subject/AuthorizeTx、单系列事务、MetadataJournal、expected-value/undo 结构；当前 State、白名单和写入器**没有作者关联组**，不是已实现作者校正。
- 10：复用同一事务/审计边界与 05 只读图片资格；**没有封面准备、旧图片保留引用或文字+封面原子提交协议**，现有 Undo 不恢复封面。
- 11：复用 MatchEvidence、提案 ID 和审计关联；Apply **不消费/关闭提案，不建立补充后关闭终态**，须继续复用普通提案终态事务保护。
- 13：复用操作身份、Journal 查询和独立 cleanup 授权基础；**无清理 API/清理状态/材料删除机制**，不能删除 payload 后仍宣称可恢复。上述失败撤销审计缺口应先处理。
- 核验时 implement-mcp-library 已前进到 `0018c90c`，本树仍只待合并指定 `46d06126`；**未集成或核验 12、另树的 05 RAR 取消锁后续修复**，全量绿灯仅覆盖本树实际组合。父代理负责后续安全集成和共享接线复核。


## Undo 失败审计窄修（待父 review）

仅在本票唯一工作树、现有 HEAD `4b7265ab` / 待完成合并 `46d06126` 上修复独立 probe 指出的阻塞；未修改计划、其他票或其他树，未 stage/commit/merge/reset/abort。CHANGELOG 仍保留原 05/07/08 文本且索引 UU 留给父处理。当前 Status 仍 ready-for-agent，前述“尚非 merge-ready”是修复前历史结论，不冒称父 review 已通过。

### 实现及可观察行为

- `enrichment.Undo` 在已读取当前权限、锁、after-state、请求键的同一事务中，通过 `MetadataJournal.TryChange` 建立包住全部元数据/集合/统计写入的 savepoint。常规写入错误整体回滚这些写入，再于该外层事务保存 failed-undo 收据并提交。没有 rollback 后另开事务的竞态窗口，也不将失败事务标记为 changed。
- 收据保留真实 user/credential 身份、原 operation 关联、request_key 和请求意图 hash；before/after 相同，fields.changed/applied 均 false，通用固定失败理由不保存 SQL、trigger 诊断、路径或 secret。原操作的 after/撤销材料不变，UndoneBy 不会指向失败尝试。
- 同 key 同 intent 返回同 ID、failed/replayed=true/applied=false，不再写入；同 key 不同原 operation 拒绝。不同 key 后续仍可撤销；失败重放与另一 key 并发成功时，原操作仅关联真实成功收据。
- 授权拒绝发生在尝试之前，不创建可供未经授权对象探测的失败审计。savepoint、日志写入或 commit 自身失败不承诺持久失败收据，返回不可用错误；外层事务回滚。无法在存储本身不可用时承诺审计一定持久。
- 复用既有网页/MCP Get/List、失败投影和文案，不新增响应字段、UI、SQL schema 或 sqlc 查询。检查现有 UI：仅 outcome=changed 才标原项 undone，failed 使用现有失败文案且原操作仍可撤销，因此无需 UI 兼容改动。

### 红绿与实际门禁

主接缝沿用已同意的生产 `bootstrap.Open` + 临时真实 SQLite + 通用 Go MCP SDK / 管理员 HTTP，不 mock 内部事务。扩展 `TestProductionMCPEnrichmentAuditUndo`：DELETE series_tags 的真实 RAISE(ABORT) trigger 在标量更新之后失败，断言标量/标签均保持补充后状态、撤销材料不变、failed receipt 身份及 key/parent 对应、字段无实际变化、无私密诊断、公开查询、去除故障后的同键仍不执行、换 intent 拒绝、不同 key 合法成功、失败重放并发不遮盖成功、关闭原连接并重开生产图后仍可查。另用日志 INSERT trigger 验证记录自身故障返回公开错误且无虚假收据；当前降权也返回拒绝且不留尝试收据。已有撤权/锁/标签整组 CAS/无关字段守卫未削弱。

环境：Go1.26.10，约定共享 GOCACHE，本树 `.tmp/08/gotmp`、独立 `.tmp/08/lintcache-undo-fix`。日志均在 `.tmp/08/`。

- **先红**：`go test -p 1 ./internal/bootstrap -run TestProductionMCPEnrichmentAuditUndo -count=1 -timeout=3m`，真实写错误仍返回 IsError、无 failed 收据，新增公开断言失败（`undo-fix-red.log`）。
- **再绿**：同命令通过（`undo-fix-green.log`）。后增日志故障/拒权/并发/重开覆盖后，最终 MCP/Enrichment 两轮通过：`go test -p 1 ./internal/application/enrichment ./internal/application/delegated ./internal/bootstrap ./internal/api ./internal/seriesmeta ./internal/database -run 'Enrichment|MCP' -count=1 -timeout=3m`（`undo-fix-focused-{2,3}.log`；第一轮扩展前通过另见 `undo-fix-focused-1.log`）。此 pattern 在 delegated/database 无匹配，以下相关全包运行确实覆盖它们。
- **相关六包全部测试**：上述六包不加 -run，`-count=1 -timeout=10m` 全通过（`undo-fix-packages.log`），含 API 全包 132.5s。
- `gofmt -l ./cmd ./internal` 空（`undo-fix-gofmt.log`）；Go1.26.10 `golangci-lint run` **0 issues.**（`undo-fix-lint.log`）。`go run ./cmd/tsgen` 42 types，生成前后 SHA-256 完全一致（`undo-fix-tsgen*.log`）。`git diff --check HEAD` 最终通过；编辑器曾产生 CRLF 全文件 diff，已仅对本次修改文档恢复 LF，无无关整文件改动。
- 未改 schema/query.sql，不需重跑 sqlc；手写 savepoint SQL 为 ASCII。未改 frontend，不重跑 test/build/lint；merger 前端全套证据仍是前节记录，不冒称本轮重跑。未重跑整个仓库全量后端：按此次窄修要求运行相关全包，父后续组合再做最终全量。

边界：常规 SQLite ABORT 与日志 INSERT 失败已实证；没有宣称穷尽硬件 I/O、进程崩溃或所有并发调度，savepoint/commit 不可用按错误路径处理，不伪造成功或持久承诺。作者09/封面10/裁决11/清理13及12均未扩实现。建议提交标题（仅供父使用）：`fix: persist failed metadata undo receipts atomically`。


## 最终组合 merger：55e5f6e3 + 待完成 b4900a65

**08 合并内容与本轮门禁 merge-ready；整批最终批准仍等待已知 12 pruning 修复。** 唯一编辑树为 `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-08`，分支 `mcp-enrichment-audit`；HEAD `55e5f6e3f968d02f778e37739599aefb83a6a292`，MERGE_HEAD `b4900a659245eb28bba2738e7a79675f3abcd288`。未 stage/commit/merge/reset/abort，未编辑父树或其他树。9 个 UU 的工作区冲突标记已全部逐段消除；索引仍为 UU，留父级确认并完成现有 merge。

### 合并与复查

- CHANGELOG 保留 05 字典预算及取消锁、07、08 补充/撤销及失败收据、12 模型额度；API/application/bootstrap/database/metadata 契约同时保留两方行为，去掉已不准确的“无 LLM/元数据写入”限制。API 公开名单完整含 08 六工具和 12 两工具。
- `mcp.go` 在同一认证注册路径保留 discovery/images/runs/matching/enrichment/model budgets；精确工具发现与只读注解测试包含全部 23 工具，没有删除已有断言。HTTP 与真实 stdio helper 均运行生产 bootstrap。
- `bootstrap.Open` 只创建一个 delegated 实例，在该实例绑定 MatchingAccess + ModelBudgetAccess，并把同一实例交给 enrichment 与 HTTP；12 继续使用同一数据库、配置、Runs.Start 和 ExternalSources.ConfiguredAI。保留统一 Migrate 安装模型表、三个生产来源实际 HTTP guard/单请求计数、05 RAR 可取消会话锁及资源寿命测试。03/04/06 的已合入 done/closure 原样保留，08 仍 ready-for-agent。
- 独立审查 Undo：写入、派生统计全部使用同一 tx-bound Queries，savepoint 覆盖完整写入；只有 ROLLBACK TO 与 RELEASE 均成功才保存 failed 收据。原操作/撤销材料不改，唯一成功 parent 关联只看 changed。同一授权事务内记录，无双事务窗口；固定理由脱敏。日志插入或 commit 错误直接返回，defer Rollback 中止整个外层事务，不发布 refresh 或声称收据持久。现有真实 DELETE/INSERT trigger、同键重放/异意图、拒权、并发另一键成功、重开查询回归均通过。此次不改 Undo 实现或扩展重构；不声称穷尽硬件/崩溃故障。

### 本轮实际 checks

日志统一在 `.tmp/08/final-combination/`。Go `go1.26.10`，约定共享 GOCACHE；本树该目录下 GOTMPDIR=`gotmp`、独立 GOLANGCI_LINT_CACHE=`lintcache`。Node `26.13.1` / npm `11.20.0`。

1. `go test -p 1 ./internal/application/enrichment ./internal/application/delegated ./internal/bootstrap ./internal/api ./internal/seriesmeta ./internal/database ./internal/metadata ./internal/parser -run 'Enrichment|MCP|Model|Budget|Evidence' -count=2 -timeout=5m`：两轮通过，`focused-two.log`/exit 0。包括生产 bootstrap 所有 MCP 测试、真实 stdio/模型重连、事务失败及图片预算/取消；delegated/database 此 pattern 无匹配，下面全量确实运行其测试。
2. **完整后端仅一次**：`go test -p 1 ./... -count=1 -timeout=10m`，外层 1800s，全部通过，`full.log`/exit 0。没有未知 rename/迁移故障或盲重试、弱化测试。
3. `gofmt -l ./cmd ./internal` 输出空，`gofmt.log`；`golangci-lint run` 为 **0 issues.**，`lint.log`/exit 0。
4. 前端完整 `npm test -- --maxWorkers=2`：**121 files / 1151 tests** 通过，`frontend-tests-configured.log`；`npm run build`、`npm run lint` 均通过，`frontend-{build,lint}.log`。初次 bash 未初始化 fnm，npm not found（exit 127，`frontend-tests.log`），没有执行到测试；按 worker-guide 初始化 fnm/use 后才执行上述成功全套，未把环境失败隐藏成测试成功。
5. `go run ./cmd/tsgen` 生成 42 types，生成前后 SHA-256 完全一致（`tsgen{,-before,-after}.log`），无响应契约漂移。
6. `git diff HEAD` 确认 sqlc 输入 `sql/query.sql`、`internal/database/schema.sql` 及生成 `models.go`/`query.sql.go` 无变化；本轮未重复 sqlc。新增模型 DDL 是合入的独立 handwritten 输入，统一迁移与全部数据库/bootstrap/系列事务回归通过，证明现有生成绑定与实际 schema 兼容。query.sql、schema_handwritten.sql、schema_model_budget.sql 均 ASCII；savepoint SQL 亦 ASCII。
7. 最终 `git diff --check HEAD` 通过，冲突文本标记为零。长期规格只合并/调和当前能力归属，不改变 ADR0016 draft。

### Remaining / 父级动作

- **已知 12 正常运行历史 pruning 后预算操作投影失败尚在本组合代码中**：GetModelOperation 仍依赖 GetRun，历史裁剪后持久额度不能正常公开投影。用户已安排 12 专属树修复；本轮不重复实施，也不以全量绿灯宣称该未覆盖场景已修复。父级须稍后组合该窄修及其回归后再批准整个组合。
- 无本轮新发现的实质阻塞；08 failed-undo 收据原子性阻塞已由先前窄修解决，本轮再次验证。父负责索引冲突标记、完成 merge/提交及 request_review。09/10/11/13 和真实浏览器像素/收费实验仍不在此次完成范围。

### Final integration and independent review

- `baaae3a0` completes integration with the independently approved 12 pruning fix. Latest enrichment/delegated/bootstrap full package tests passed; all merge markers removed.
- Host `request_review` rejected the initial call because no change set had been attached to this step; this rejection is not an approval. A fresh read-only reviewer instead inspected the exact complete range `de3a7e56..baaae3a0` (44 files), including the failed-undo receipt fix, and returned **Approve** with no new blocking findings.
- Independent Go1.26.10 enrichment/seriesmeta/bootstrap/metadata targeted tests passed, frontend operation-history tests passed 5/5, exact diff whitespace and related Go formatting checks passed. This reviewer did not repeat full-project gates or paid model calls; those remain the separately recorded combination checks above.
- Ticket08 is complete. Subsequent09/10/11/13 and final14 acceptance remain open; no actual browser screenshot was produced.
