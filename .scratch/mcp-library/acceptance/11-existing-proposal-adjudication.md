# 11 — 已有提案的受保护裁决

## 范围与公开接口

唯一编辑树 `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-11`，分支 `mcp-proposal-adjudication`。起点和结束 HEAD 都是 `13ea7e66f92203d80e7363ed968de756b6943802`；`git merge-base --is-ancestor 13ea7e66 HEAD` exit0，集成分支仍同一提交。未 stage/commit/merge/reset，未改父树、13 或其他树/TODO。11 保持 ready-for-agent；**功能验收已有实测，完整后端门禁未通过，不能称 merge-ready/Approve**。

- `library_enrich_metadata{,_batch}` / `enrichment.Input.adjudication={mode,reason}` 是完整公开裁决入口，复用原单系列审计、永久请求键及实际内容 Undo。一次一个 proposal_id，支持七个现有提案字段；不虚构 07 未支持的 cover/source_links 提案字段。字段可以来自该提案内不同已确认来源。
- `partial` 只消费实际写入字段；剩余 pending，最后一个实际字段处理完则 applied。保护的非空描述/评分不被消费；标签筛噪追加，作者保留规则和明确的系列内别名/角色例外沿用09，不改共享身份。
- `supplement_and_close` 补充后在同事务关闭剩余差异；允许空 fields 对先前部分接受的提案执行纯关闭。`reject` 要求空 fields，只拒绝剩余差异。若此前或本次有接受，终态 `supplemented_closed`，否则 `rejected`。pending 期间的 applied_at 持久记录首次接受，关闭/拒绝/Undo 都不清除；全部字段处理完推进 applied 时沿用原终态 CAS 时间约定；既有人工部分应用仍有来源沿革指向原提案时也识别接受，不猜已丢失历史。
- `library_get_match_proposal` 在同一读事务重查实际资料库、read + metadata_write 和当前账户，返回当前 status、accepted、剩余字段/当前锁和原证据。旧提案缺07身份/版本确认时可读取、明确拒绝，不能自动应用或伪造 confirmation。需要采纳的来源应重新明确匹配；复用旧提案不会升级旧证据。这是保守安全输入策略，不是新增审批绕过。
- 收据 `adjudication` 保存 proposal_id、原 mode、短 reason、当时 status/accepted。管理页依据服务器结果而不是请求 mode 显示“全部拒绝 / 部分已应用仍 pending / 补充后关闭”；reject 命令在先前有接受时不会误报全部拒绝。纯关闭无内容 Undo，先前实际变更仍可单独符合条件地撤销。

## 持久边界与并发结果

新增窄文件 `internal/application/enrichment/adjudication.go` 与 `internal/proposal/adjudication.go`；后者归口当前 pending/剩余字段检查与消费/推进，应用组合当前授权、expected/locks、来源策略及 Journal。API 不接触 SQL、不组装新 runtime。`proposal.Read` 允许同一事务投影当前字段，普通人工 Get/Apply/Reject、fill_empty 与集合替换未改策略。

实际写事务重读提案待裁决状态、原证据及剩余字段，重读当前系列/实际库、值、锁和整组作者/标签。终态或已消费字段竞争保存整项 conflict，不写内容、字段行或沿革；SQLite busy/snapshot 等事务故障仍可返回不可用，不带旧授权自动重试。实际元数据、最新来源、字段消费、接受标记、终态和不可变收据同事务。常规写错在08/10同一 savepoint 回滚全部写入再保存 failed；日志/COMMIT 自身不可用不能承诺收据。纯关闭提交也发布 refresh；重放不重复发布。

同键同意图永远返回原 ID/replayed=true/applied=false，包括原 partial 收据在后来关闭后仍是当时 pending；最新状态另查提案。新键重复消费返回持久 conflict。mode、reason 以显式可选命令 V1 结构参加摘要，缺失完全省略；不 hash 整个 Input/State，不改变08/09/10旧原字节、ExpectedState、Undo 摘要或持久键命名空间。

Undo 恢复的是真正改变字段，不是提案状态回退；不会重新生成已消费字段行、重新打开提案或撤销接受历史。新裁决的最新来源引用在仍属于原提案时随实际 Undo 一起移除，不伪造未知的更早来源；完整原来源与理由仍留在不可变审计。没有触碰13材料清理、自动删除、保留期限或封面私有材料。

## 生产验收与测试替身

`internal/bootstrap/mcp_adjudication_test.go` 接入现有 `runProductionAuthorCorrections`，HTTP 与真实编译启动的 mcp-proxy stdio 均运行相同裁决场景。生产 bootstrap、真实 SQLite、真实事务/触发器与管理员 HTTP 均不替换；仅使用原外部 catalogue provider 替身，不调用付费模型。

覆盖：

1. description 部分接受、标签补充后关闭、同键重放/异 mode 或 reason 拒绝、新键终态冲突，原 pending 收据不可变；最后字段 applied，无空 pending 收件箱条目；纯关闭和 reject-after-partial 不撤销内容。
2. 无确认旧提案可读但不可应用，明确拒绝可用；当前用户降级、只读凭据（包括重开后）、系列移至无权库均拒绝读取/应用，拒权无伪造收据。
3. 七类确定性证据屏障竞争：delegated reject、原人工 HTTP reject、另一部分应用、补充后关闭、新锁、角色改变、跨库移动。失败方不写内容/重复消费，先前成功部分应用保留，关闭/拒绝/原收据重放的公开结果明确。
4. 实际作者别名+角色校正、另一个系列共享身份不变、保护非空人工描述、保留个人标签并只追加内容标签；关闭后内容保持，实际 Undo 恢复关联但终态不回退。保留原09更多角色/整组 expected/锁/触发器/其他系列回归。
5. 真实 SQLite provenance INSERT、field DELETE、terminal UPDATE、journal INSERT 故障；标量/集合/字段消费/沿革/接受标记/状态无半提交。可保存的 failed 收据同键不再执行；日志故障返回公开错误而不是虚假持久成功。公开系列 metadata-review 投影验证无孤立来源，无私有诊断泄露。
6. 关闭原生产图和 SQLite store，新连接/新生产图、重新签发合法凭据后公开读终态/审计，再 Undo 原单项变更；状态仍 supplemented_closed，其他操作标签不受影响，不留误导的已撤销字段最新来源。
7. 原08真实 DB 夹具重开后的 HTTP/stdio 永久重放、09冻结摘要、10旧命令原字节及新增裁决意图摘要、10 stale cover 永久 conflict、真实 HTTP/stdio transport 取消及批次前缀等回归保留。`verifyAtomicCovers` 仍调用真实 transport cancellation 验证。

管理页组件新增第10个测试展示三种状态、理由与正确 Undo 入口；组件仅 mock HTTP/auth，不替代后端授权。应用测试的外发 publish 回调守护纯关闭 refresh/重放不重复发布；其原无真实提案的 fixture 现显式创建 pending 提案，没有削弱原重放/Undo/响应预算断言。

## TDD 真实记录

日志在本树 `.tmp/11/`，忽略产物不作为提交内容：

- `red.log` → `green.log`：SDK 尚不接受 adjudication；最小接线后生产补充/关闭通过。
- `red-provenance.log` → `green-provenance.log`：真实来源触发器未被写路径触及，意外 changed；补上实际来源归口后整体 failed/回滚。
- `red-terminal.log` → `green-terminal-fixed.log`：普通非消费补充曾可绕过已拒绝提案；增加最终 pending 守卫。中间 `green-terminal.log` 暴露“原来就无该字段 diff”与“已消费”不可混同，保留普通来源补充语义，不改变09多来源作者例外；显式裁决仍严格检查剩余字段。
- `red-close-only.log` → `green-close-only.log`：补充后的空 fields 关闭初被拒绝，新增明确纯关闭语义。
- `red-undo-source.log` → `green-undo-source.log`：真实 Undo 后仍显示错误的最新来源；只清除该原提案拥有的已恢复字段引用，审计保留。
- `red-last-field.log` → `green-last-field.log`：最终实际字段消费后曾留下空 pending；现在推进 applied。
- `red-closure-refresh.log` → `green-closure-refresh.log`：纯关闭原先不发布 refresh；提交后发布，重放不重复。
- `ui-red.log` → `ui-green-fixed.log`：原管理页无提案状态；新增按服务器实际状态显示。`ui-green.log` 是中间失败，locale 使用双引号且 tsgen 需显式登记 AdjudicationResult，修正后才通过。
- 中间曾有 Python heredoc 长度/UTF8 读取错误、测试 response 类型/变量重名编译错误（`expanded.log`），均修正宿主，不称功能红测。最终两轮定向最初在旧07重开只读凭据正例失败（`targeted-two-rounds.log`）；按本票读权限契约给正例合法 read+metadata_write，并新增重开只读拒绝断言，原证据持久内容断言保留。`final-targeted.log` 原应用 fixture 无真实提案，补齐 pending fixture 后通过，不绕过新真实提案守卫。

## 本轮实际门禁

环境 Go **1.26.10**；guide 指定共享 GOCACHE，本树 GOTMPDIR=`.tmp/11/gotmp`，独立 GOLANGCI_LINT_CACHE=`.tmp/11/lintcache`。Node **26.13.1** / npm **11.20.0**；仅拷10的忽略 node_modules/dist 后自行完整前端测试和重建。

- **两轮最终定向 PASS**：`go test -p 1 ./internal/bootstrap ./internal/application/enrichment ./internal/proposal ./internal/application/delegated ./internal/seriesmeta ./internal/api -run 'TestProductionMCP(AuthorCorrections|Enrichment)|TestEnrichment|Adjudication|Concurrent|TestMCP' -count=2 -timeout=10m`，`targeted-two-rounds-fixed.log`/exit0，bootstrap 495.192s。delegated 此 regex 无匹配，不冒称定向运行其测试；完整后端确实运行该完整包。
- **唯一一次完整 backend FAIL**：`go test -p 1 ./... -count=1 -timeout=10m`，外层1800秒；`backend-full.log`/exit1。唯一显示的失败为 `TestMCPStdioSaturatedEOF`，`mcp_stdio_test.go` 的 build 在30.23s返回 `exit status 1`、没有编译诊断。该未改测试的30s context覆盖了 go build；这说明失败位置，不足以证明是机器负载或 baseline。前述同一 regex 两轮曾包含并通过该测试，不抹掉完整门禁失败。未盲重跑、skip、改测试时限或修改该测试。bootstrap 全包432.091s；其他包均 PASS，包括 api275.231s、enrichment/database/proposal/seriesmeta/scanner。**需父后续诊断与独立完整绿灯，最后门禁验收框不勾。**
- **完整 frontend PASS**：`npm test -- --maxWorkers=2`，**121 files / 1156 tests**，`frontend-tests.log`/exit0；`npm run build`、`npm run lint` 均 exit0，`frontend-{build,lint}.log`。
- 最终 `gofmt -l ./cmd ./internal` 空（`gofmt-final.log`）；`golangci-lint run` **0 issues.**（`lint-final.log`/exit0）。
- 最终 `go run ./cmd/tsgen` **46 types**，生成前后 SHA256 均 `30fd1d6bfac7bb41843816d3a66d651f908ae5a88200d2376c84434002ea4f06`（`tsgen-{before,after}.log`），无漂移。生成新增 AdjudicationResult 和 Operation.adjudication，不手写生成契约。
- `sql/query.sql`、所有 schema/sqlc 模型输入及生成绑定均无变化，`git diff --exit-code` exit0（`sql-unchanged.log`）；因此本票**未调用 sqlc**，不冒称 PowerShell SQL 生成已重跑。新增生产手写 SQL 均 ASCII、参数化；query.sql 仍 ASCII（`sql-and-mockups.log`），不宣称旧 schema.sql 既有中文全文件 ASCII。
- `git diff --check` 空；最终祖先/分支/集成 tip复核如开头。长期契约更新 application/proposal/seriesmeta/api/web SPEC 与 CHANGELOG；有效 ADR0011 不改，ADR0016 仍 draft。

## Mockups 与 remaining

`.thinkrail/context/mockups/11-before.html` / `11-after.html` 是匹配现有深色管理历史的静态概念示意：自包含 inline CSS，无 script、控件或外部资源，不是实际浏览器截图/像素验收。

- 公开裁决功能没有已知上述生产验收缺口；**完整后端门禁尚红，是明确 remaining/blocker，不宣称全部验收或独立批准**。父负责诊断、独立 merger/review、索引和提交。
- 旧无07确认证据提案不能自动应用；无法证明的历史接受不回填。若产品要在旧原提案上原地补身份确认，需要父进一步明确契约，本票不擅自扩批准。当前安全路径为明确拒绝或重新来源匹配。
- 单次仅裁决一个提案、七个现有提案字段；其他来源补充、封面改善继续走原受保护能力。普通人工提案不变。没有实现13清理/自动删除、付费模型或全域身份合并。
- 故障覆盖真实 SQLite语句/收据故障与正常资源关闭后重开，不声称穷尽调度、强杀/断电、硬件损坏或丢失材料恢复。

建议提交标题（仅供父使用）：`feat: adjudicate existing proposals with protected supplementation`。
