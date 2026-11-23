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

## Fresh 独立 merger / 诊断门禁（HEAD 7a082c16，未提交增量）

**结论：BLOCKED，交父 freshfix；不是 merge-ready。** 下列结果补充并取代上文“无已知功能缺口 / 后端尚未绿”的当前判断，历史失败记录仍保留。唯一树为本票树；HEAD=7a082c16，父分支 tip 与 merge-base 均为13ea7e66。未触碰父树/13/TODO，未执行 index/history 变更。

### 可复现授权缺陷（需父修复后再审）

`internal/application/enrichment/service.go` 的 `Apply → existing` 在证据和提案终态检查之前取永久收据，顺序正确，但 `existing` 只重查 metadata_write，没有裁决当前必需的 read。最终新写事务虽然检查两者，早返回的重放路径却绕过 read 撤权。影响：保留 metadata_write、撤掉 read 的凭据仍能通过同键裁决重放读取完整旧收据（包括 before/after、来源及理由）；未观察到新的 metadata 写入。完全撤销凭据、用户降级等仍由原守卫拒绝，不能将此漏洞泛化为全部撤权无效。

真实复现：在 `verifyProposalAdjudication` 首次 partial 成功后，临时加入子测试：真实 SQLite `UPDATE mcp_credentials SET capabilities='["metadata_write"]'`，用原 session 再发 `partialInput`，断言 `IsError`；defer 恢复原 read+metadata_write。调用真实生产 bootstrap/SQLite/SDK，HTTP 与真实编译 stdio 都失败，均返回完整收据，而不是拒权。命令：

`go test -p 1 ./internal/bootstrap -run '^TestProductionMCPAuthorCorrections(Stdio)?$' -count=1 -timeout=10m`

证据 `.tmp/11/fresh/read-revocation-red.{log,exit}`（exit1，15.724s），精确插入代码 `.tmp/11/fresh/repro-insertion.txt`。复现插入已移除，原测试文件恢复 HEAD 原字节，未留下失败测试或功能修复。建议父 freshfix：让 early receipt transaction 对 adjudication 同时检查当前 read，且保留在终态预检之前重放的正确顺序；不可全局改变08/09/10既有请求字节或普通补充授权契约。补两种 transport 的 read-only 撤权重放红绿回归，再验证 receipt replay/终态及旧兼容。

### 30秒构建失败诊断（不冒称已证明 CPU 根因）

已读取保存的完整 `backend-full.log` 与 helper。失败位置确为 `TestMCPStdioSaturatedEOF` 内的 `go build -o <test TempDir>/mcp-proxy.exe ../../cmd/mcp-proxy`，不是已启动代理 hang：同一个30秒 context 从测试开头覆盖编译及后续 saturation。此 helper 每次独立 go build；没有使用 cached agentPath/prebuilt fixture。Go 内容缓存复用不等于复用已生成 exe。原日志30.23s/exit1/空输出**符合** deadline 杀编译，但没有 ctx.Err 或进程诊断，无法追溯证明 CPU 并发是根因；也不能认定 baseline。

假设按可检验性排序：① context deadline 杀编译（应见 ctx.Err=deadline exceeded）；②真实 go build/toolchain 错误（应见未过期context/编译输出）；③代理 hang（应先看到构建成功、失败落在启动或EOF等待）。本轮仅做必要单次同 cmd/env 构建测量：Go1.26.10、指定共享缓存、本树独立 fresh/gotmp，3.407s exit0/无编译输出（`fresh/build-probe.log`）。这不重现旧负载，不能排除①，也无证据指向自己的 Go/tmp 错配。

唯一代码增量是 `internal/bootstrap/mcp_stdio_test.go` 在原 build.CombinedOutput 周围记录实际 elapsed、ctx.Err、真实 err、完整 quoted CombinedOutput；失败仍 FailNow，30秒总预算、8并发+第9个过载、3秒EOF退出、transport cancellation 和业务隔离条件全部未改。仅诊断可观测性，不改fixture、不预建、不放宽、不skip；没有伪造一次deadline红测来声称修复了旧故障。额外可观测性检查 `go test -p 1 ./internal/bootstrap -run '^TestMCPStdioSaturatedEOF$' -count=1 -v -timeout=10m` PASS，真实 build elapsed=2.0071856s/context=nil/err=nil/output=""，总3.30s（`fresh/saturation-diagnostic.log`）。如再失败，测试日志即可区分①②③；旧失败的确切根因仍未证实。

### 本轮实际 checks

全部新证据在本树 `.tmp/11/fresh/`。Go1.26.10，共享指定 GOCACHE、本树 GOTMPDIR 与独立 lintcache；Node26.13.1。

- 定向两轮 PASS：与上轮相同包/regex 的 `go test -p 1 ... -count=2 -timeout=10m`；`targeted.{log,exit}` exit0，bootstrap241.438s。包含实际裁决/故障/并发/持久重开/HTTP+stdio、摘要兼容和stdio EOF回归；delegated regex 无匹配，不冒称该包定向已覆盖。
- 本轮完整 backend **仅运行一次，PASS**：`go test -p 1 ./... -count=1 -timeout=10m`，外层1800s；`backend-full.{log,exit}` exit0，bootstrap104.039s/api161.144s。此绿灯不抵消另行构造的真实撤权红测；后者是门禁原覆盖缺口。
- 完整 frontend PASS：121 files /1156 tests，241.97s；build/lint exit0（`frontend-*`）。未修改 UI；mockup仍为 `.thinkrail/context/mockups/11-before.html` 与 `11-after.html`。
- gofmt最终空、golangci-lint **0 issues.**。首次lint因临时复现备份恢复使用Python文本写入导致原测试换行变成CRLF而失败；已按 HEAD 原字节恢复，重新核对git diff无该文件增量后检查通过，没有功能改动或忽略规则。
- tsgen46 types，前后SHA256同为30fd1d6bfac7bb41843816d3a66d651f908ae5a88200d2376c84434002ea4f06；无漂移。SQL/schema输入与base无差异（`sql-input-diff.log`空），无需且未重跑PowerShell sqlc。git diff --check通过。

除上述read撤权重放外，独立审查未发现新的确定性裁决/事务/旧摘要/普通人工流程回归；不是穷尽安全证明。13共享修改尚未集成，本报告不代表其门禁。请父先安排freshfix并做独立request_review；本票不标done。

## Freshfix 撤销 read 后的裁决收据授权（HEAD 7a082c16，未提交）

**当前结论：授权缺陷已修复并实测；完整 backend 本轮失败，仍非 merge-ready，待父独立 review。** 本节取代上节“交父 freshfix”的当前待办判断；历史红测/门禁记录不删除。只修改本票树，未 stage/commit/merge/reset、未碰父 TODO/其他树。

### 窄修与公开回归

- `enrichment.retry` 在同一持久收据事务中，先按当前 user+credential 的永久请求键与摘要定位原收据，再依据**原持久收据的 adjudication** 对其系列实际当前资料库重新 `AuthorizeTx(Read)`。调用方原有 MetadataWrite 检查保留；当前凭据的持久 library_ids/capabilities、撤销状态及当前用户角色仍由原授权实现检查，不借输入/上下文授权快照。错误返回空 Operation，不带旧收据。
- 保留 early existing → 原收据 → 新意图证据/终态守卫的顺序；成功 partial 在后来关闭后仍重放原 pending 快照，不重新消费字段。集中 retry 策略也覆盖最终事务内命中收据的路径。缺少 adjudication 的同键异载荷仍被摘要拒绝，不能借可选输入标志取旧收据；没有修改任何命令摘要、08/09/10 字节或普通补充/作者/封面授权。
- 永久生产测试 `verifyAdjudicationReceiptAuthority` 加入原 HTTP 与真实编译 stdio 场景：成功 partial 后用 SQLite 精确更新该凭据行，分别撤 Read、撤 MetadataWrite、撤库；原键重放、新键 Apply、删除 adjudication 的同键载荷均拒绝。Read/库撤销还验证公开 Get/List receipt 拒绝且错误中无 receipt ID、内容、理由或 credential ID。真实用户降权仍拒绝 Apply。
- 恢复真实持久权限后验证相同 ID、replayed=true/applied=false、before/after/fields/adjudication 原快照，以及 pending 的三个剩余字段未再次消费；既有关闭后旧 partial 重放检查保留。真实 DB/生产图重开后，新签发的合法凭据使用原键只得到自身的终态 conflict，不借用旧凭据的 receipt/命名空间。该跨凭据重开场景沿用原 HTTP session（两种主场景均调用），不冒称重开阶段使用 stdio。
- Get/List 的历史查询仍遵守已有当前目标 Read 契约：仅撤 MetadataWrite 或降为普通用户不是本票新增的历史查询禁令；Apply 则仍须 MetadataWrite/管理权限。未降低或全局重定义08/09/10历史查询契约。
- owning `internal/application/SPEC.md` 与 CHANGELOG 已同步。merger 的饱和 EOF build elapsed/context/err/output 诊断原样保留；30秒总预算、饱和条件、取消与 EOF 断言均未改。

### TDD / 实际 checks

证据统一 `.tmp/11/freshfix/`。Go **1.26.10**，GOCACHE=`C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-01/.tmp/merger01/gocache126`，本树 GOTMPDIR=`.tmp/11/freshfix/gotmp`、独立 lintcache。Node **26.13.1** / npm **11.20.0**。

- `red-final.{log,exit}`：永久新测试在移除窄修时实际 **FAIL/exit1**，HTTP 与真实 stdio 的 read-revoked 子项均显示 Apply 返回数据；恢复修复后再执行下述最终两轮。更早 `red.log` 同样触发缺陷，但 helper 当时捕获父 t 导致附带 Goexit 诊断，已改为显式子 t；不把该诊断当业务失败。`green.log` 是中间测试设计错误：完全撤销 credential 会关闭 HTTP session，恢复 DB 无法复用已关闭 session；未改业务契约，移除不在本窄修范围的该新增项。原凭据撤销/改密覆盖未动。`green-fixed.log` 修正后 PASS。
- **最终定向两轮 PASS**：`go test -p 1 ./internal/bootstrap ./internal/application/enrichment ./internal/proposal ./internal/application/delegated ./internal/seriesmeta ./internal/api -run 'TestProductionMCP(AuthorCorrections|Enrichment)|TestEnrichment|Adjudication|Concurrent|TestMCP|TestCurrentAuthority' -count=2 -timeout=10m`；`targeted-final.{log,exit}` exit0，bootstrap196.946s。包括新授权回归、HTTP/stdio、作者/封面/裁决/receipt/digest/08重开及stdio饱和/取消；本轮额外匹配 `TestCurrentAuthority`，delegated 不再是无匹配。
- **相关六包完整 PASS**：相同六包去掉 -run，`-count=1 -timeout=10m`；`related-full.{log,exit}` exit0，bootstrap107.323s、api167.071s。
- **本轮唯一一次完整 backend FAIL**：`go test -p 1 ./... -count=1 -timeout=10m`，外层 **1800s**；`backend-full.{log,exit}` exit1。唯一失败为 `internal/api/TestConfigFailedUpdatePreservesConcurrentExternalEditor/save`，`config_external_compensation_test.go:46` 期望 runtime failure，但实际两次 `.config.yaml.tmp-* → config.yaml` rename 返回 **Access is denied**。bootstrap92.151s PASS（包含饱和EOF与新裁决测试），其他包全部 PASS。本轮相关包全测 api 曾通过，但这不证明本次失败为 baseline 或确定其根因；未改配置代码/测试、未盲重跑全套或放宽条件。完整门禁验收框继续不勾，交父诊断/独立门禁。
- **frontend 仅历史页定向 PASS**：`npm test -- src/pages/settings/metadataOperations.test.tsx --maxWorkers=2`，1 file /10 tests，40.47s（`frontend-targeted`）。UI 无修改，**本次未重跑完整 frontend/build/frontend lint**；merger 历史121 files/1156 tests及build/lint通过的证据仅引用上节，不冒称本次结果。
- `gofmt -l ./cmd ./internal` 空（`gofmt.log`）；`golangci-lint run` **0 issues.**/exit0（`lint`）。
- `go run ./cmd/tsgen` exit0、46 types；前后SHA256均 `30fd1d6bfac7bb41843816d3a66d651f908ae5a88200d2376c84434002ea4f06`，无生成物漂移（`tsgen*`）。SQL/schema/sqlc与本轮起点无差异，无需且未调用sqlc。
- `git diff --check` 通过；未提交、未改变 HEAD。最终文件清单以未提交 diff 为准。

### Remaining / 父 review 输入

已知 read 撤权重放缺陷没有剩余复现；这不是穷尽安全证明，也不替代父独立 review。当前唯一实测门禁阻塞为上述完整backend的配置并发保存 Windows rename 拒绝；不将原先的30秒stdio编译失败与这次不同的失败混为一谈。工单保持 ready-for-agent，最后门禁框未勾；父负责审查、诊断与提交。


## Final fresh merger：11 freshfix + 13（本轮当前结论）

**结论：merge-ready（仅本轮组合），保持 ready-for-agent，交父 canonical request_review；不是 done/独立批准。** 唯一编辑树 `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-11`，分支 `mcp-proposal-adjudication`，HEAD=`6311bca3d6ac36fae264f8651ad43a5fcf885dbf`（含7a082c16实现及read freshfix），父预先启动的 MERGE_HEAD=`5e7dfaf05fbed0d53507c571316653b35d7a657c`。本轮未 stage/commit/merge/reset/abort、未动父TODO或其他树。索引仍有父原先的9项UU，**正文冲突均已解决且无冲突标记**；由父检查并stage完成合并。没有整文件ours/theirs替换。

### 合并与独立核对

- `CHANGELOG.md`、application/api/web SPEC 保留两票新增契约；修正同段“未提供清理/裁决”的过时互斥说明，两者现在并存。有效 ADR0011 不变，ADR0016仍draft。
- `cmd/tsgen/main.go` 同时登记 AdjudicationResult、CleanupResult、MaterialResult；`service.go` 同时保留 Input.Adjudication、Operation.adjudication、undo_unavailable/cleaned_by 与共享 cleanup runtime 接线。其 `validKey` 以下所有方法与11 freshfix HEAD逐字相同，未在合并时重写授权或业务流程（`service-preservation.log`）。13非冲突接线/清理/测试增量保留。
- 独立核对 early existing：当前 MetadataWrite授权与 `retry` 均在同一事务；`retry` 先按持久 user+credential+key找收据并核对摘要，再按**持久收据 Adjudication**对收据系列实际当前库检查Read，不依赖本次可篡改输入。错误不带旧Operation。原收据在提案终态/证据预检之前，恢复权限可重放原pending快照而不二次消费。cleanup裁剪也不删该持久裁决标记，不改变此策略。
- 原HTTP/真实stdio生产回归继续覆盖撤Read、撤MetadataWrite、撤库、降用户角色，同键重放/新键/删adjudication载荷拒绝；Read/库撤销时Get/List不泄露收据，恢复真实持久权限后原key/ID/快照重放；重开后跨凭据不能借用旧命名空间。旧缺证据提案只读/拒绝、作者关联例外与共享身份保护、终态/消费竞争、SQLite savepoint/日志故障回滚、close/reject不撤销已应用内容及Undo不重开提案均保留并过组合门禁。
- `digest.go`、冻结digest测试、mcp_adjudication/mcp_authors/Base08Replay及proposal并发测试与11 HEAD原字节完全一致（`preserved-11.log`）。普通08/09/10摘要字节/缺省扩展与普通权限未被合并扩大；真实08数据库重开及09/10冻结摘要回归通过。
- 网页保留服务器 adjudication.status+accepted 的pending partial / supplemented_closed / rejected展示，同时保留13 cleanup警告、固定重试键、partial/pending反馈和清理后不显示恢复值/Undo。两边新增UI测试逐段完整保留。**13另树正在修复的cleanup pending离页恢复不在本次MERGE_HEAD内，本轮没有抢做/宣称该后续修复已验收，父须随后叠加并复验。**

### Actual checks（全部新证据 `.tmp/11/finalmerge/`）

Go **1.26.10**；guide共享GOCACHE，本树独立 `gotmp` / `lintcache`。fnm Node **26.13.1**、npm **11.20.0**。runner=`checks.sh`，各命令独立 `.start/.end/.log/.exit`，未失败后循环重试。

1. **定向两轮 PASS**：`go test -p 1 ./internal/bootstrap ./internal/application/enrichment ./internal/proposal ./internal/application/delegated ./internal/seriesmeta ./internal/api ./internal/database ./internal/covermaterial -run 'TestProductionMCP(AuthorCorrections|Enrichment)|TestEnrichment|Adjudication|Concurrent|Concurrency|TestMCP|TestCurrentAuthority|Cleanup|Cover|Undo' -count=2 -timeout=10m`；`targeted.exit=0`，bootstrap292.166s、api14.580s；含新撤权/裁决/cover/cleanup/stdio，delegated有实际匹配；covermaterial无独立测试，其文件行为由真实bootstrap公开测试验证。
2. **本组合唯一一次完整backend PASS**：`timeout 1800 go test -json -p 1 ./... -count=1 -timeout=10m`，15:33:19→15:43:49（+08:00），`backend-full.exit=0`；38有测试包PASS、0 fail事件，api206.582s/bootstrap168.027s。原配置并发保存save、relocation crash及stdio饱和EOF在本轮通过；HTTP/stdio的read/write/library撤权子项全部通过。事件摘要=`backend-summary.json`。
3. **完整frontend PASS**：`npm test -- --maxWorkers=2`，**121 files /1158 tests**，270.12s；`npm run build`、`npm run lint`均exit0。本轮实际重建，不将复制dist当build。
4. `gofmt -l ./cmd ./internal` 输出空，exit0；`golangci-lint run --allow-parallel-runners` **0 issues.**/exit0，使用本树独立cache，没有豁免新规则。
5. `go run ./cmd/tsgen` **48 types**/exit0，`tsgen-before.log`/`tsgen-after.log`的SHA256相同，无漂移；generated.ts由生成器重建，非手写冲突合成。
6. `sql/query.sql`、sqlc schema/config与生成bindings对HEAD及MERGE_HEAD均无差异，两个 `sql-input*-diff.log`为空；新增13 handwritten `schema_cleanup.sql`保留，非sqlc输入，本轮没有再改SQL，**无需且未调用sqlc**。不冒称PowerShell生成已重跑。
7. `git diff --check` exit0；9项正文已无冲突标记。最后仅追加本验收/issue记录，不改已验证源码。

### 历史诚实边界 / remaining

11初次完整backend已绿；freshfix完整backend曾唯一失败于 `TestConfigFailedUpdatePreservesConcurrentExternalEditor/save` 的Windows rename Access denied。13历史两次不同rename失败及其顺序baseline/13完整green证据仍按13acceptance原样保留。**本轮是新的有效11+13组合验证，不是已定位holder、已修复rename、或已证明baseline不稳；不抹去历史红灯。** 未添加retry/sleep/skip/timeout放宽，也未改配置或relocation实现。

本轮未发现新的已证实非合并业务缺陷，不代表穷尽安全/崩溃调度证明。当前组合门禁无实测blocker；父负责索引解决、提交、独立canonical request_review及后续13pending修复整合。现有11/13静态mockups保留，本轮无新UI功能/浏览器截图主张。

## 最终小增量 merger：parent ef44f9b6 / 13 canonical freshfix

**当前结论：merge-ready，11 保持 ready-for-agent，交父提交并 canonical request_review；不代表独立批准或 done。** 本节更新上一轮“13 pending 后续修复未纳入”的组合边界。唯一编辑树 `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-11`，分支 `mcp-proposal-adjudication`，HEAD=`0a634bb9`，父预置 MERGE_HEAD=`ef44f9b6d7a8aa88c1702c740737dd2f3edd4c1e`（含13批准修复 `727820e9` 及关闭文档）。未 stage/commit/merge/reset、未动父 TODO；13 done 状态及关闭文档原样保留。

### 纯合并与组合核查

- 仅 `CHANGELOG.md` 有正文冲突，逐块保留11裁决/read撤权修复和13刷新后 pending 续办条目，未整文件 ours/theirs。正文无冲突标记；索引仍为父预置的 **UU CHANGELOG.md**，由父解决索引并提交。
- 自动合并的 application/api/web SPEC、generated TS、双语 locale、SettingsMetadataOperationsPage 及测试没有发现语义错配。48 types 同时包含 AdjudicationResult、CleanupResult、MaterialResult；CleanupResult 新增可选 retry_request_key，不进入命令摘要。页面保留服务器 status+accepted 的裁决三态；13 hook 与 cleaned_by 去重查询、原键续办、目标绑定、失败重读和完整卸载/挂载测试均保留。
- `service.go`、冻结 digest 实现/测试、mcp_adjudication/mcp_authors 与 proposal concurrency 测试与11 HEAD原字节一致；cleanup.go、cleanup_runs.go、mcp_cleanup_test.go 和 useOperationCleanups.ts 与父 MERGE_HEAD原字节一致。历史页两侧原测试块全部保留（仅空白分隔行不作身份判断）：11 HEAD 12项、父15项，合并16项。证据 `preservation.log`，非以旧1158总数代替13新增覆盖。
- 重查11 early existing/retry：先当前 MetadataWrite，同事务按 user+credential+key/hash 找原收据，再按持久 Adjudication 对系列实际当前库重验 Read；拒权不返回旧Operation，不以本次可省略的 adjudication 输入决策。终态守卫前重放、恢复权限后原pending快照及不二次消费规则保留。
- 重查13 pending recovery：仅原网页 user、空credential命名空间、合法原键/V1摘要及匹配 operation_id、当前管理员/实际库 cleanupAuthority 通过才投影 retry identity；POST仍重新授权并拒绝同键异目标。agent/其他执行者只可被管理员检查，不转移重试命名空间。cleanup裁剪不删除原 Adjudication 标记，不能借 recovery 扩大11重放权限。旧08/09/10摘要、普通人工提案及终态规则未改。

### Actual checks（全部本组合新证据 `.tmp/11/parentef44/`）

环境 Go **1.26.10** windows/amd64；guide共享GOCACHE，本树独立 gotmp/lintcache；fnm Node **26.13.1** / npm **11.20.0**。runner=`checks.sh`，各命令有独立 start/end/log/exit；没有失败后循环重试。

1. **相关定向两轮 PASS**：`go test -p 1 ./internal/bootstrap ./internal/application/enrichment ./internal/proposal ./internal/application/delegated ./internal/seriesmeta ./internal/api ./internal/database ./internal/covermaterial -run 'TestProductionMCP(AuthorCorrections|Enrichment)|TestEnrichment|Adjudication|Concurrent|Concurrency|TestMCP|TestCurrentAuthority|Cleanup|Cover|Undo' -count=2 -timeout=10m`，exit0；bootstrap465.685s、api15.703s。包含真实HTTP/编译stdio、read/write/library撤权和用户降权、裁决与终态/摘要兼容、cleanup故障重开原key恢复/身份限制、cover/Undo/运行中撤权/junction。delegated有实际匹配；covermaterial无独立测试，其文件保护经bootstrap公开测试验证。
2. **本组合唯一一次完整 backend PASS**：`timeout 1800 go test -json -p 1 ./... -count=1 -timeout=10m`，16:06:57→16:18:22 +08:00，exit0，**38个有测试包PASS / 0 fail事件**；api153.046s/bootstrap286.112s。JSON明确记录HTTP与stdio read/write/library撤权及user-demoted子项通过；配置并发保存/relocation crash/stdio饱和EOF均通过。摘要 `backend-summary.json`。
3. **完整 frontend PASS：121 files /1162 tests**，130.44s；`npm run build`、`npm run lint`均exit0。另实际运行历史页定向，**1 file /16 tests PASS**，2.97s（`frontend-history`），包括11三态及13四项freshfix/remount增量。build只出现非失败PLUGIN_TIMINGS提示。
4. `gofmt -l ./cmd ./internal` 输出空；独立cache `golangci-lint run --allow-parallel-runners` **0 issues.**，exit0；无规则豁免。
5. `go run ./cmd/tsgen` **48 types**，exit0；前后 generated.ts SHA256均 `109ab5217b37c3aab46ee9b43bfe71cbf94641263e7d109f6dca02eba325a7d7`，无漂移。
6. SQL/query/schema/sqlc配置及生成bindings相对HEAD/MERGE_HEAD均无输入差异（`sql-input-vs11.log`、`sql-input-vs13.log`均空），无需且未跑sqlc。`git diff --check`通过；最后只追加本验收/issue记录，不修改已验证源码。

### Remaining / 诚实边界

本轮未发现具体功能缺陷或实测门禁blocker；没有扩修实现。此前Windows rename Access denied本轮未复现，历史根因/holder仍未定位，**不宣称已修复或已证明baseline不稳**，没有retry/sleep/skip/预算放宽。两票安全回归在同一组合生产图执行并经静态边界交叉核查；不冒称穷尽所有取消/崩溃调度或新增浏览器端到端测试。13仍done；11仍ready-for-agent，父负责索引、提交及独立canonical review。


## Fresh narrow fix：普通人工 partial 的 provenance 接受事实（HEAD ab462e9c）

本节取代前轮“无已知功能缺口”的当前结论。仅本票树 `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-11` / `mcp-proposal-adjudication`，起点 HEAD 与父 merge-base 均为 `ab462e9c71b97bd2be1ced1df6e82dfb52a3485e`，起点干净；未 stage/commit/merge/reset、未改父 TODO 或其他树。11 保持 ready-for-agent，待父 fresh 独立 review，不标 done。

### Fix / 真实生产 seam

- `proposal.Accepted` 集中在调用方已授权的同一事务里组合 applied_at、applied / supplemented_closed 终态和现存 review_id provenance。GetMatchProposal、初始/冲突收据、完成裁决收据及关闭判断共用，不再由三个调用点漏掉人工 partial 的来源事实。无需迁移或改变普通人工 Apply/Reject/fill_empty，也不补 applied_at 或伪造 confirmation；保护、锁、noop、当前值相等都不是接受证据。
- 保留实际写事务的当前授权/剩余字段/CAS、防终态竞争及同键 early receipt 重放顺序；read 撤权重验逻辑、摘要、13 清理实现均未改。收据仍记录当时事实，不重算或回填已持久的旧收据。
- 生产 `verifyManualPartialAdjudication` 同体由 HTTP 和真实编译 stdio 调用：真实确认 title+summary 提案，已有 title/空 summary，网页 POST `/api/metadata/reviews/bulk-apply` fill_empty 返回 partial；来源为唯一接受依据（实查 applied_at 为空），MCP 读为 pending/accepted=true；对受保护 title 执行无实际变更 partial，真实持久收据及网页 GET 皆 pending/accepted=true，原键重放 ID 不变；网页真实清理该收据后仍保留裁决及来源事实，原键仍重放正确。
- 关闭生产图与 SQLite 并重开后，合法新凭据读回该 pending/accepted=true 提案和原 immutable partial 收据；随后 reject 返回 supplemented_closed/accepted=true，title 与人工填入 summary 不撤销，原 pending 收据仍不变。原凭据的同键重放在重开前验证；重开为新凭据，不冒称跨凭据借用原键。
- 增补无人工接受、现值恰等于提案的 noop partial -> pending/accepted=false -> reject/rejected/false，以及 applied 终态 conflict receipt accepted=true。原纯 reject false、部分后拒绝、事务故障、read/write/库撤权及13 cleanup 守卫全部保留。
- owning proposal/application SPEC 与 CHANGELOG 同步；API/网页响应结构及三态规则不变，无 frontend 源码或 schema 扩张。

### TDD / 检查证据

本轮所有新日志 `.tmp/11/acceptedfix/`；Go1.26.10、guide 共享 GOCACHE，本树独立 gotmp/lintcache；fnm Node26.13.1 / npm11.20.0。

- `red.log` exit1：新增真实 HTTP 与 stdio 均在普通网页 partial 后读出 accepted=false，精确击中缺陷（非 mock provenance）。首次修后 `green.log` 是漏 import proposal 的编译失败，补齐后 `green-fixed.log` PASS（21.931s），不把编译失败算业务红测。
- 最终新增清理/重开场景专门双轮 `final-transport.log` / exit0：`go test -p 1 ./internal/bootstrap -run '^TestProductionMCPAuthorCorrections(Stdio)?$' -count=2 -timeout=10m`，104.003s。
- 相关定向两轮 `targeted.log` / exit0：八包 `bootstrap application/enrichment proposal application/delegated seriesmeta api database covermaterial`，regex `TestProductionMCP(AuthorCorrections|Enrichment)|TestEnrichment|Adjudication|Concurrent|Concurrency|TestMCP|TestCurrentAuthority|Cleanup|Cover|Undo`，count2；bootstrap337.699s。此轮启动后追加了清理/重开断言，最终 transport 双轮及随后相关全包/完整 backend 对最终版本覆盖；不把较早轮次冒称最终字节。
- 相关八包完整 `related-full.log` / exit0，count1：bootstrap117.115s/api155.698s，covermaterial 无独立测试，其生产行为由 bootstrap 覆盖。

- **本轮唯一一次完整 backend FAIL**：`timeout 1800 go test -json -p 1 ./... -count=1 -timeout=10m`，16:49:47→16:59:24 +08:00，`backend-full.exit=1`。37 个有测试包 PASS、仅 api FAIL；bootstrap116.080s PASS。两个实际失败为 `TestRelocationCommitRejectsChangedConfirmationFacts/occupied`（relocation_commit_test.go:496）和 `TestRelocationPolicyCrashRecoversFromActualDurabilityBoundaries/derived_effects`（relocation_policy_crash_test.go:28），均在本树独立 GOTMPDIR 内 source→target 的 Windows rename 返回 **Access is denied**。不是本票裁决断言失败，也不是历史配置 save/stdio 编译失败；相关全包 api 此前通过不证明这次是 baseline。未盲重跑完整套件、未 retry/sleep/skip 或放宽测试、未碰 relocation/配置实现，holder/根因仍未知。`backend-summary.json` 保存事件摘要。**完整门禁项据实重新取消勾选，不能称 merge-ready。**
- **完整 frontend PASS**：121 files /1162 tests，128.78s；`npm run build` 与 `npm run lint` 均 exit0。本轮真实执行，不借用旧产物/历史结果；无 UI 源码变更，不另造 mockup。
- `gofmt -l ./cmd ./internal` 输出空；独立缓存 `golangci-lint run --allow-parallel-runners` **0 issues.** /exit0。
- `go run ./cmd/tsgen` **48 types** /exit0；generated.ts 前后 SHA256 均 `109ab5217b37c3aab46ee9b43bfe71cbf94641263e7d109f6dca02eba325a7d7`，无漂移。response 结构未改。
- `stable-inputs.log` 空/exit0：冻结 digest 实现/测试、SQL/query/schema/sqlc 生成绑定及 generated.ts 相对本轮 HEAD 不变。仅移动既有参数化 ASCII provenance SQL 至 proposal 共享函数，无 sqlc 输入变更，**无需且未运行 sqlc**，不声称 PowerShell 生成已重跑。
- 最终 `git diff --check` 通过；文档编辑中间曾由 Windows 文本写入变成 CRLF，已仅规范本轮修改文档回 HEAD 原 LF，最终 diff 为窄行变更，非整页换行 churn。最后仅追加本验收/issue 结果，源码不再修改。

### 当前 remaining / 父 fresh review

功能缺陷已修复，真实生产 provenance-only、noop 收据、清理、重开、拒绝内容保留与反例全部实测。**当前实测 blocker 是上述唯一完整 backend 的两处 Windows rename 拒绝，未定位、未声称修复或 baseline；不是独立批准/merge-ready。** 旧已持久收据按原不可变语义不回填，丢失 provenance 的历史不猜测。父负责新的 fresh 独立 review、Windows 故障诊断/独立完整门禁及提交。11 保持 ready-for-agent，所有改动未 stage/commit，HEAD 不变；其他票/父 TODO 未动。
