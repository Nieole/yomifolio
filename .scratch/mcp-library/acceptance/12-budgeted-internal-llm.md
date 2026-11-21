# 12 — 内部模型次数额度验收

## 工作树与结论

- 唯一修改树：`C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-12`，分支 `mcp-model-budget`，HEAD `b3083e4bd2e608521519f05fb1467fead8e0f6e8`。接续已有未提交实现，未删除/重做；所有变更仍未提交。未修改08或父树/TODO。
- 完成12实现及下述验证；工单仍为 ready-for-agent，供父级审阅/集成，ADR0016仍 draft。不等于整个 MCP effort 或08验收完成。

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

## 实际验证

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
- 本票没有新增独立 stdio 子进程预算烟测；stdio为同一HTTP的代理，没有本地运行时/计数，完整后端中既有代理回归通过，预算重连/重启语义在通用HTTP客户端边界验证。没有做真实进程强杀/真实收费实验；重开服务与取消屏障验证持久记账，不承诺外部效果 exactly-once。
- 准入、run_id关联与网络调用不是分布式原子事务；中途故障可停留 not_started/unknown，保守消耗不退款，客户端应查询而非自动重发。运行历史裁剪、元数据审计/undo、清理UI等不在12实现范围。
- 建议 subject：`feat(mcp): enforce durable per-operation internal model allowances`。
