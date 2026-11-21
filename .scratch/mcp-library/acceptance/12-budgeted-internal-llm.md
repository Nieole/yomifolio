# 12 — 内部模型次数额度验收

## request_review rc_69896dd6 修复：正常历史裁剪后的持久投影

本节为当前结果，下方 merger/worker 记录是历史证据，不代表当前索引状态。

- 唯一修改树 `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-12` / `mcp-model-budget`；起点 HEAD `0018c90c`。未 stage/commit/merge，未编辑父树、计划或05/08内容；票12仍 ready-for-agent。
- 原缺陷确实存在：正常 retention 删除完成运行后，预算查询依赖 `GetRun`，仍在 DB 的计数/结果也不可读。现在 `GetModelOperation` 保留预算读事务中的当前授权、真实用户/凭据归属、系列实际资料库检查及最终权限复查；关联历史使用引擎公开 `SnapshotOf`，**仅** `task.ErrRunNotFound` 降级到具体 `ModelOperationStatus` 的 `ModelHistoryUnavailable`（JSON `history_unavailable`）。存在的运行仍经 `visible` 当前授权/归属检查；拒权、其他存储错误不吞掉。
- 不新增表/迁移、不修改 retention、不永久保留 run；原 operation_id/run_id/allowance/used/remaining/逐项结果不变。缺失历史不伪造 completed/interrupted 等终态、不声称可继续查询/取消该 run；重复声明仍只读原操作，不重启、不重置、不重发。
- 复用既有 HTTP/真实 stdio helper 共用验收体：经生产 bootstrap + 通用 MCP SDK 创建并等待完成操作，确定 allowance=1/used=1/remaining=0、首项 uncertain、两项 unprocessed；仅将该完成历史的 finished_at 置于100天前，调用真实 `taskstore.Store.PruneRuns(TerminalAge=90天)`，断言恰好删除1条、run工具不可读。随后 lookup 与同额度声明全字段相同，仅 status 改为 history_unavailable，改变额度仍拒绝，AI调用数不增；重开服务后再次验证，并保留进行中消耗的 unknown/不重放验收。
- 重开后同用户其他凭据、其他管理员用户凭据不能查询/重申原操作；原用户降级、系列移出原资料库、移除 internal_llm、清空库范围后仍拒绝，恢复用户/系列实际归属后可再次读取原投影。移除模型能力后普通系列读取仍成功。同步沿现有模型屏障/运行等待，无 sleep、真实费用或新业务宿主。

### 本次实际红绿与门禁

环境：`GOTOOLCHAIN=go1.26.10`；共享 `GOCACHE=C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-01/.tmp/merger01/gocache126`；本树 `.tmp/review12-pruning/tmp` 和独立 `lintcache`。日志均在 `.tmp/review12-pruning/`。

1. 红：新增裁剪回归对原生产实现运行 `go test -p 1 ./internal/bootstrap -run '^TestMCPModelBudgetPersistsWithoutReplay$' -count=1 -timeout=3m`，真实删除历史后 `library_get_model_operation` IsError=true（`red.log`）。初次测试安排误写表名 task_runs 已修为实际 runs，不作为实质红测；首次修复误匹配 sql.ErrNoRows，检查存储端口后改为真正的 task.ErrRunNotFound。
2. 绿：HTTP/stdio 两种预算持久化验收通过（`green.log`）；补全当前授权反例后再次通过。重开管理会话最初误用首次setup helper触发409，已改为重开服务上的真实登录，不掩盖为产品失败。
3. `go test -p 1 ./internal/metadata ./internal/application/delegated ./internal/bootstrap -count=1 -timeout=10m`：全部通过，覆盖预算/metadata/delegated/bootstrap全包（`packages.log`）。
4. `go test -p 1 ./internal/bootstrap ./internal/api ./internal/application/delegated ./cmd/mcp-proxy -run 'Test.*(MCP|ModelBudget)' -count=2 -timeout=5m`：MCP两轮通过，delegated无匹配、proxy无测试文件如实保留（`mcp-two.log`）。
5. 一次完整后端 `go test -p 1 ./... -count=1 -timeout=10m`：全部通过，退出0（`full.log`/`full.exit`），包括本次实际 relocating 包；不引用旧review relocation/timeout结果。
6. `gofmt -l ./cmd ./internal` 空输出；本树独立 `golangci-lint run` 为 `0 issues.`（`gofmt.log`、`lint.log`）；`go run ./cmd/tsgen` 生成32 types，生成文件无diff（`tsgen.log`）；`git diff --check` 通过。

边界：run历史被裁剪后只知道历史缺失，无法恢复原运行终态；预算结果仍是已持久化结果，unknown不被猜测为成功。无新SQL输入/生成目标/前端改动，不跑sqlc或UI门禁。05取消锁与08undo收据仍由父级并行处理；父级集成提交后应立即独立重审。本节不批准未见到的后续组合变更，ADR0016仍draft。


## 工作树与结论

- 唯一修改树：`C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-12`，分支 `mcp-model-budget`。独立 merger 核验 HEAD `d6c92649062a0e265f568f215b0b26389680bf1c`；待完成的合入为 `implement-mcp-library` / MERGE_HEAD `46d06126df577d1c772c4781165772277a01ac1d`，merge-base `b3083e4bd2e608521519f05fb1467fead8e0f6e8`。
- 本次仅逐段协调 CHANGELOG 与 api/SPEC 冲突、复用预算验收增加真实 stdio helper 重连测试、更新12记录；其余05自动合入文件保持。未 stage/commit/merge/reset/abort；索引仍显示两个 UU，由父级检查、stage并完成已有 merge。
- 12 **merge-ready（内容与门禁）**；工单仍 ready-for-agent，ADR0016仍 draft。不等于整个 MCP effort 或08完成。05 RAR共享锁取消的另树窄修不由本票处理，也不以本轮绿色冒充该缺陷已修。

## API 与记账边界

- `library_assess_match(search_id, allowance)`：对07已完成/部分完成搜索的全部候选声明1–50次额度，返回独立 `model-budget:` operation_id、run_id。服务端操作永久绑定用户、凭据、实际系列/资料库、冻结证据及初始额度；同搜索重复声明只读原操作，改变额度拒绝，不重启运行。不能通过自选候选子集拆分原操作。
- `library_get_model_operation(operation_id)`：只向同用户/同凭据且当前仍有 read/internal_llm 权限者返回持久 used、remaining、逐项白名单 outcome、unprocessed 和运行状态。不能猜其他凭据操作；Subject 来自认证，不接受客户端身份参数。
- 当前账户许可 ∩ 凭据独立模型能力 ∩ 本次明确额度；普通用户即使凭据带模型能力也不能付费调用。使用01生产 AI 工厂与唯一 runtime/Runs，不另建模型配置或后台恢复器。
- 声明、run_id关联、预留、结果落库均在实际 SQLite 事务中重新鉴权并检查系列当前归属。原子 `used<allowance` 更新及唯一操作/item 约束控制并发；事务冲突中止，不带旧授权自动重试。普通读鉴权不替代写事务 fencing。
- used 是**准入即消耗的保守次数**，不是已确认收费量：失败、取消、解析错误、预留后未发出和崩溃都不退款；结果未安全保存保留 unknown，未预留项为 unprocessed。每项最多准入一次；不自动重试、续跑或恢复付费请求。remaining 不是继续重放的许可。
- 每个单元及实际生产 HTTP 模型请求前检查 Checkpoint/当前权限。生产 Responses/Chat Completions/Ollama 共用单次请求守卫：拒绝第二次 Do、禁止重定向，且只在委托路径移除 GetBody 重放配方，阻止 HTTP/2 REFUSED_STREAM/GOAWAY 透明重发已发送 POST。尚未发送的连接重新选择不算另一模型请求。普通网页/智能用例保留原有传输行为。
- HTTP/stdio断开不取消已接纳运行；重连读取原操作，服务重启只保留消耗及中断/未知结果，不重发。撤权停止后续请求与结果写入；数据库事务不能原子锁住远端发送，已发请求/费用不能追偿。
- 新服务端搜索可成为新的明确声明操作；按ADR0016接受 agent 转述，不独立核验真实用户批准，不承诺阻止恶意 agent 伪造新指令，不是货币成本上限或受信任人工审批。
- 只保存 consistent/different/uncertain/failed/invalid_response/unknown 标签；不保存模型原始输出/内部推理/生成评分，不写系列、不自动确认歧义或建提案。仍须07显式确认及字段来源。额度表不充当08元数据审计/撤销表。

## Worker 历史验证（合并前）

环境：`GOTOOLCHAIN=go1.26.10`；共享01内容寻址 GOCACHE；本树 `.tmp/budget12-resume` GOTMPDIR 与独立 lintcache。无真实付费来源，无测试 sleep。

1. 生产 `bootstrap.Open` + 真实临时 SQLite + 通用 Go MCP 客户端 + 可控 AI 工厂：
   - `TestMCPModelBudgetPersistsWithoutReplay`：无额度不调用、额度耗尽及未处理项、重复/改额度、非模型查询继续、完成与进行中操作重开服务后保留消耗、不重发。
   - `TestMCPModelBudgetAuthorityDisconnectAndFailedAdmissions`：无独立能力、普通用户、并发重复/拆分声明不增加运行/额度、其他凭据不可读、断连重连、失败仍消耗、显式取消不重试、执行中移除模型能力阻止后续请求且 read 仍可用。
2. 三个生产来源的真实本地 HTTP 测试：`TestBudgetedModelRequestsNeverRedirectOrReissue`、`TestBudgetedModelUpstreamFailureDoesNotRetry`、`TestModelRequestBudgetPreservesOrdinaryProviderBehavior`：撤权守卫不发送、重定向与429不隐藏重复请求、普通路径仍可重定向和独立调用。
3. 接续复查发现原 Do 外围守卫不足以拦住 Go HTTP/2 内部重试。新增真实 TLS/h2 线端 REFUSED_STREAM 回归 `TestBudgetedModelDoesNotReplayHTTP2RefusedStream`：修复前失败，实际收到2次POST（`.tmp/budget12-resume/http2-red.log`）；仅委托路径清除 GetBody 后通过，同一准入仅收到1次。测试没有引入新依赖或伪造内部 SQL 顺序。
4. 修复后定向：`go test -p 1 ./internal/metadata ./internal/bootstrap -run 'Test.*Model' -count=1 -timeout=3m`，通过（`http2-green.log`）。
5. 最终完整后端：`go test -p 1 ./... -count=1 -timeout=10m`，退出0、全部通过（`full-final.log`、`full-final.exit`）。接续时曾完整通过一次；本次因明确发现并修复HTTP/2问题再跑最终门禁，不是对未知失败盲重试。
6. 最终 `gofmt -l ./cmd ./internal`：空；`golangci-lint run`：`0 issues.`；`git diff --check`：通过。日志为 `gofmt-final.log`、`lint-final.log`。
7. `go run ./cmd/tsgen`：生成32 types，`git diff --exit-code -- web/src/api/generated.ts` 无漂移。未改生成目标；模型工具响应不在网页 targets 内。
8. 新SQL ASCII解码检查通过。它是独立手写DDL、未改sqlc输入 `schema.sql` / `sql/query.sql`，无需运行sqlc。未改UI，未跑前端tests/build/lint；使用树内已有忽略的web/dist进行Go embed。

## 复用与未覆盖边界

- 复用：`delegated.Subject`、`PrepareRun`、`Checkpoint`、`AuthorizeTx`、`ExternalSources.ConfiguredAI`、`Runs.Start`；新增 `CallBudgetedModel` 供可信应用用例复用原子预留，`WithSingleModelRequest` 防止生产模型请求隐式重放。外部注入 AI 工厂属于可信系统接缝，须自身遵守单请求及取消契约。
- 独立 merger 已补真实 stdio 子进程预算验收（见下）；未做进程强杀或真实收费实验。票12要求跨请求/断线重连/重启记账，不要求强杀；通过重开生产服务与取消屏障检查持久消耗，不承诺外部效果 exactly-once。
- 准入、run_id关联与网络调用不是分布式原子事务；中途故障可停留 not_started/unknown，保守消耗不退款，客户端应查询而非自动重发。运行历史裁剪后的预算投影现由上方 rc_69896dd6 回归覆盖；元数据审计/undo、清理UI等不在12实现范围。
- 建议 subject：`feat(mcp): enforce durable per-operation internal model allowances`。

## 独立 merger 实际检查（05+07+12 组合树）

环境：`GOTOOLCHAIN=go1.26.10`（未用默认1.27.4）；`GOCACHE=C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-01/.tmp/merger01/gocache126`；本树 `.tmp/review12` GOTMPDIR、独立 `.tmp/review12/lintcache`。下面均为本次实际命令，不引用旧reviewer的lint结果。

- 阅读 worker-guide、AGENTS、effort spec、票12、原acceptance、ADR0016及 application/api/metadata/bootstrap/database/architecture/proxy 长期契约，按真实diff逐项检查。未发现12功能阻塞。永久归属/不可变额度、实际事务内当前授权及原子预留、失败不退款/同item不重试、重启不恢复付费发送均保留。
- 三个生产来源的统一发送点均经 `modelRequest`；上下文外直接沿原 `client.Do`，上下文内复制 client 但复用 Transport，不新运行时、不修改共享client；禁重定向及清除GetBody防止h2重放。真实TLS/h2 REFUSED_STREAM回归与普通调用兼容测试均实际通过。可信注入工厂的契约不是对任意恶意替身的网络拦截保证。
- `Subject` 来源仍是服务端认证；所有模型协议错误经 `publicMCPToolError` 转通用错误，持久化只有白名单标签，不透传provider错误/SQL/path。模型辅助不写系列、不生成评分或持久推理；额度声明不被当作独立人工批准。
- api冲突逐段合并：图片与模型工具同时列出，保留07工具；05图片请求取消与已接纳后台运行断线继续语义不混淆。所有工具经过最终256KiB响应硬限；生产默认来源工厂与同一runtime绑定未丢失。
- 新 `TestMCPModelBudgetStdioReconnectWithoutReplay` 复用原预算持久化 fixture/屏障，通过通用SDK `CommandTransport` 启动真实构建的 `cmd/mcp-proxy`，helper在空临时目录工作。跨请求消费后关闭helper、启动新helper连同一生产服务，重复声明返回相同 operation_id/run_id/used/remaining，AI调用仍为1；随后在进行中AI屏障处关闭helper与服务，再重开bootstrap/第三个helper读取，消耗保留、unknown/unprocessed准确、不重发。无sleep、无新业务实现。原HTTP持久化测试保留并复用同一验收体。

实际命令及结果（日志均在 `.tmp/review12/`）：

1. `go test -p 1 ./internal/metadata ./internal/bootstrap -run 'Test.*(Model|Budget)' -count=1 -timeout=3m`：通过，`targeted.log`。
2. `go test -p 1 ./internal/metadata -count=2 -timeout=3m`、`go test -p 1 ./internal/bootstrap -run 'Test.*(Model|Budget)' -count=2 -timeout=3m`：两轮通过，`metadata-two.log`、`budget-two.log`。
3. `go test -p 1 ./internal/bootstrap ./internal/api ./internal/application/delegated ./cmd/mcp-proxy -run 'Test.*MCP' -count=2 -timeout=5m`：两轮通过，`mcp-two.log`（后两个包无匹配测试/无测试文件，如实保留输出）。
4. 一次组合完整后端：`go test -p 1 ./... -count=1 -timeout=10m`，外层1800s，全部通过，退出0，`full.log`/`full.exit`。无失败盲重试或删测试。
5. `gofmt -l ./cmd ./internal` 输出空；本次独立 `golangci-lint run` 为 `0 issues.`，`gofmt.log`/`lint.log`。
6. `go run ./cmd/tsgen` 生成32 types，`git diff --exit-code -- web/src/api/generated.ts` 无漂移，`drift.log`。`sql/query.sql` 与新 `schema_model_budget.sql` 均ASCII；sqlc输入未变，无需sqlc。未触及UI，不跑前端门禁。
7. 最终 `git diff --check` 通过（`diff-check.log`）；冲突文本标记为零。最初检查发现本次Python文本写入把两个冲突文档误转CRLF，已恢复原LF；不是既有功能失败，没有因此重跑后端。Go文件未随之改变。

### 真正剩余门禁

- 父级审阅本树内容后，stage两个已解决文本冲突并完成当前merge；本子任务禁止索引/历史变更，因此UU仍存在，不表示内容冲突尚存。
- 父级接入05共享RAR锁取消窄修、08及任何后续变动后，需在最终组合树重新验收；本报告不批准尚未见到的变更，也不宣称整个effort交付。
- 不存在12特有未完成验收：stdio预算子进程缺口已补；进程强杀/真实收费不属本票必需门禁。ADR0016仍draft，正式决策审阅归父级/维护者。
