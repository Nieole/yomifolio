# 09 目标系列作者校正验收

## 树、范围和接口

唯一编辑树 `C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-09`，分支 `mcp-series-authors`，HEAD 为 `c4540ce4`；开始时 `git merge-base --is-ancestor c4540ce4 HEAD` 退出 0，交付时集成分支仍指向该 commit。全部变化未 stage/commit/merge/reset；未修改票10、父 TODO 或其他树。09 保持 ready-for-agent，勾选代表本树实际核验，不代表父级独立 review/集成已完成。

复用已批准的测试接缝：生产 `bootstrap.Open` + 真实临时 SQLite + 通用 Go MCP SDK（HTTP 与真实 mcp-proxy 子进程 stdio）及管理员管理 HTTP。只替换外部 metadata provider，不替换应用服务或事务，不访问收费模型。

- `enrichment.Input.fields` 增加 authors；`State.authors` 是小写 name/role 精确身份对数组。
- 可选 `author_corrections: [{from:{name,role},to:{name,role},reason}]`。from 必须属于 expected 作者组，to 必须在本次已确认提案 authors 来源证据内，reason 必须非空且 <=1000 字节，最多 50 条。无此列表时仅追加来源作者；未显式映射的旧成员全部保留。重复 from、自映射和目标同时被移除的矛盾链拒绝；多个别名允许映射到同一已有目标。
- 角色属于 authors 身份行：真实 schema 是 UNIQUE(name,role)，series_authors 只有系列/作者 ID。校正只重建目标系列关联；新 `Queries.EnsureAuthor` 采用 INSERT ... DO NOTHING + 精确读取，绝不 UPDATE 或 DELETE 身份。旧行、其他系列及同名不同角色保留。别名即使再次出现在另一来源也不会被自动加回。
- 作者是成员保留规则唯一明确例外，不开放 description/summary、rating 等既有值覆盖。共用 08 的单系列事务、当前实际权限/库/字段锁、持久请求键、收据及 Undo；没有改 Undo 恢复策略或降低 08 保护；本轮仅将 Undo 的摘要调用改为独立冻结入口。网页与 MCP 都通过同一收据查询前后身份、来源、短理由和结果。
- `AuthorSet` 对精确 name/role 去重排序，nil/空组同义；无隐式大小写、空白或角色归一化。整组 expected 检查与整组 after-state Undo 检查，不用全局版本阻塞无关编辑。
- 永久契约归 application、seriesmeta、database、api、web 所属 SPEC；CHANGELOG 记录用户变化。没有提升 ADR0016 draft。

## 验收指针

`internal/bootstrap/mcp_authors_test.go`：`TestProductionMCPAuthorCorrections` 和 `TestProductionMCPAuthorCorrectionsStdio` 使用相同生产场景分别覆盖真实 HTTP 与 stdio：

1. 两系列共享同一 Alias/Artist 身份，目标别名与角色校正为 Canonical/Writer，追加 Artist/Artist；另一系列仍为 Alias/Artist。来源外 Invented identity 被拒绝。公开 HTTP 操作收据包含校正前后、来源与具体短理由。
2. 两来源分别仍使用旧别名和正式名，显式校正优先于追加；多个别名归并到已有正式身份，同名 Artist 与 Writer 两角色均保留。实际姓名/角色数组与独立字面预期完全相等，而非只比较长度。
3. expected 重复及排列不产生冲突；新增任一非校正作者导致提交和撤销冲突。作者锁拒绝 Undo；无关 publisher 修改不阻止作者+summary 撤销且被保留。
4. GetMetadataReview 确定性屏障停在证据预检之后，分别新增作者锁、整组任一成员、把系列移到无权资料库、收紧凭据为 read；放行后当前事务返回 conflict 或拒绝，不使用旧授权。read-only 凭据 Undo 被拒绝。正常授权、账号降权/撤权及其他锁守卫由 08 生产用例及本次全量后端共同保留。
5. 真实 SQLite BEFORE INSERT ON series_authors 触发器在标量更新后失败，summary 与全部作者关联整体回滚，failed 收据可查询。真实 BEFORE DELETE ON series_authors 令 Undo 失败，完整 after 组和 summary 保留，failed-undo 收据关联原操作，未误标已撤销；去掉故障后可合法撤销。公开历史不包含私有 trigger 诊断。
6. BEFORE UPDATE ON authors 触发器禁止身份更新；关闭 helper、HTTP、生产图及原 SQLite store，打开新连接和新生产图，再由管理员 HTTP Undo 成功恢复精确旧身份组（含原角色），证明不依赖进程内材料、不修改共享身份。旧普通编辑、扫描、提案及共享关联回归由完整后端门禁执行。

`web/src/pages/settings/metadataOperations.test.tsx` 新增作者姓名/角色、来源理由与撤销冲突交互，保留其他 5 个审计/授权/分页/失败重试守卫。界面按原有深色设置风格逐行呈现“姓名 · 角色”，不让用户阅读原始 JSON；沿用确认与服务器权威结果，conflict 不显示 Undone。

## TDD 与真实失败记录

日志均在本树 `.tmp/09/`（忽略产物，不作为提交内容）。

- 初始测试宿主多语句 SQL 的绑定在每条语句重新使用，导致共享关联重复插入；拆成逐语句参数化真实 SQL，修正宿主，不改生产逻辑。
- `red.log`：公开 library_enrich_metadata 尚不接受作者字段/映射，返回 IsError。最小作者接线后 `green.log` 通过；逐步扩展后的 `expanded.log` 与 `http-stdio.log` 通过。
- `ui-red.log`：找不到人读的 Alias · Artist；增加作者展示与双语标签。首次实现误将字符串换行写成实际换行，`ui-green.log` 是真实语法失败；修正后 `ui-green-fixed.log` 6/6 通过。
- `races.log`：测试把收紧凭据的 name 从 enrich 改成 read-only，违反凭据名称不可变契约，web helper Fatal 时屏障未释放，deferred Close 等待造成 3 分钟 timeout。改为原名称并用局部 defer 保证故障也释放屏障；`races-fixed.log` 通过。没有弱化安全断言或把失败称为 baseline。
- `alias-red.log`：新多来源用例证实追加会把明确归并的旧别名加回，实测得到 3 人而非 2 人。修正追加排除显式 from，并拒绝矛盾链；`alias-green-final.log` 三包定向通过。

## 实际门禁

环境：Go1.26.10；共享 GOCACHE=`C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-01/.tmp/merger01/gocache126`；本树 GOTMPDIR=`.tmp/09/gotmp`、独立 GOLANGCI_LINT_CACHE=`.tmp/09/lintcache`。前端使用 fnm Node26.13.1 / npm11.20.0；拷贝 08 忽略的 node_modules/dist 后，实际自行执行全部前端检查并重建 dist。

- `go test -p 1 ./... -count=1 -timeout=10m`，外层 1800s：**完整后端一次全部通过**，`backend-full.log`。涵盖所有生产实现与多来源修复，不跳过真实存储/stdio/扫描/普通提案回归。之后只将作者组断言从长度进一步加强为完整字面身份数组，未改生产代码；`go test -p 1 ./internal/bootstrap -run TestProductionMCPAuthorCorrections -count=1 -timeout=3m` 再通过（`exact-identities.log`）。不冒称在加强测试后又跑过全量。
- `npm test -- --maxWorkers=2`：**121 文件 / 1152 测试通过**，`frontend-tests.log`。`npm run build` 和 `npm run lint` 全部通过，`frontend-build.log` / `frontend-lint.log`。
- `gofmt -l ./cmd ./internal` 输出为空，`gofmt.log`；`golangci-lint run` **0 issues.**，`lint.log`。最终严格断言后格式仍空且 lint 再通过，`lint-final.log`。
- `go run ./cmd/tsgen`：43 类型，新增 Author 与 State.authors 正确生成；再次生成前后 SHA-256 同为 `fb04c17266ec28033b735ed70d1b5e0bee24e93a9a5189f01cbb8fcfe9f16782`（`tsgen{,-before,-after}.log`），无漂移。
- 未改 `sql/query.sql`、`schema.sql`、`schema_handwritten.sql` 或任何 sqlc 模型输入/生成绑定，因此**未运行 sqlc**，不冒称 SQL 生成已重跑；新手写 Go 查询是 ASCII，query.sql 与 schema_handwritten.sql 仍 ASCII。schema.sql 自带已有中文注释，未改动，不冒称全文件 ASCII。真实迁移、数据库与新事务回归均通过。
- 最终 `git diff --check` 通过。无额外依赖、付费调用、commit 或 index 变更。

## Mockups 与 remaining

自包含、inline CSS、无脚本或外链的概念对比：`.thinkrail/context/mockups/09-before.html`、`.thinkrail/context/mockups/09-after.html`。不是实际浏览器截图或像素回归证据。

无已知 09 验收缺口；未穷尽所有调度、硬件/进程崩溃及未来清理后的材料存活，不对此作保证。10 封面、11 提案终态、13 清理不在本票范围。父级仍须独立 review/集成；合并 10 时保留双方的 Input/State/Apply 分支、生成类型、工具说明与规格增量，作者例外不得扩展为已有描述/评分覆盖。本轮 undo.go 仅改摘要调用一行（hash → undoDigest），父合并 10 时须保留该入口，不能用整个文件覆盖。

建议 commit subject：`feat: add series-local author corrections with audited undo`


## 持久幂等 blocking 修复（本轮独立复验）

### 发现、最窄修复与稳定接点

独立 reviewer 指出的 blocker 属实：08 原有 Input 的 expected 无 authors；09 把非 omitempty 的 State.Authors 加入响应后，原 hash(in) 会额外编码 authors:null，旧 request_hash 不再匹配。实际 MCP 还有一道同源兼容问题：SDK 从响应 State 推导 expected 输入 schema 时把 authors 变成必填，原始缺字段请求甚至在服务入口前就会失败。

- 新增 `internal/application/enrichment/digest.go`，使用内部 `applyCommandV1` / `expectedV1` / `undoCommandV1` 固定结构和显式映射；基本字段名、顺序、null/空数组编码完全保留 08，撤销 request_key/operation_id 的字节形状不变。Apply 的预查、事务内重放、正常/失败收据全部使用同一 `applyDigest`；Undo 只替换一行调用为 `undoDigest`。
- `Input.Expected` 改为独立 `ExpectedState`，authors 可选，response `State.Authors` 仍是原有非 omitempty 契约。旧单项/批量 MCP 请求无需补新字段。响应快照扩展不再隐式改变命令 schema 或持久 SHA。
- 作者扩展仅缺失/null/空集同义并省略；非空 expected 身份按精确 name/role 集合去重排序后纳入摘要，校正列表每个 from/to/reason 都参与。没有旧 hash 模糊回退、没有忽略非空作者或任何校正、没有迁移/猜旧操作语义。旧 tags/links 的 null/空数组差异保持，不借修复广泛归一化其他旧字段。
- 旧审计缺少 authors 解码为未记录，不伪造历史作者；旧 Undo 仍只恢复原收据实际改变字段。回归在旧操作后人为新增作者，实际撤销旧补充时保留该作者。
- 所属 application/seriesmeta/api SPEC、CHANGELOG 及工单同步说明；ADR0016 仍 draft。未改票10树、父 TODO、SQL 或生成物类型形状，本轮没有改前端。

**父须传达票10：**稳定摘要接点是 `internal/application/enrichment/digest.go: applyDigest / undoDigest`，输入快照是 `service.go: ExpectedState`。封面新命令语义必须显式映射为“旧意图不编码”的可选摘要扩展（或另定带判别符版本），保持 V1 旧字段顺序和字节；不得恢复 hash(Input/State)、新增无条件 version 字段，或忘记将封面意图纳入摘要。新 response State 字段本身不修改摘要。合并 undo.go 只需保留 `digest := undoDigest(in)`。

### 真实 base08 夹具、红绿与保护

`internal/bootstrap/testdata/enrichment-base08.{db.gz,json,md}` 保存由 **c4540ce4 原生产代码**、真实 bootstrap/SQLite/MCP 生成的关闭数据库、原请求及原收据，包含真实 enrich、成功 undo 与尚可撤销 enrich。使用 Go overlay 装载 base tracked Go 代码，临时排除新增 authors 实现，不移动 HEAD/index，不修改其他树。没有用 09 的摘要生成“预期值”，没有手写/改写 journal hash。生成步骤、原 SHA 与夹具校验值在相邻 md；合成凭据只属于临时测试数据库，不是部署密钥。生成日志 `.tmp/09/compat/base08-generate.log` 通过；初次裁剪旧测试的 generator 曾因 b/c 变量未使用编译失败，修正测试生成宿主后才生成成功。

`TestProductionMCPEnrichmentBase08Replay` 将旧数据库解压到新的临时路径，经真实 `database.Migrate` / 新连接 / `bootstrap.Open`，使用同一固定请求与真实持久键，在 HTTP 与实际 mcp-proxy stdio 子进程分别核验：

1. 原请求返回同一整张收据，允许新增缺失作者投影及实时 undone_by，replayed=true、applied=false；原成功 Undo 也返回其原收据。
2. 原请求 expected.authors 缺失、null、[] 均兼容，批量嵌套 schema 也可重放；非空作者组、不同 proposal_ids 与不同 undo operation_id 均拒绝。
3. 实际撤销旧 before/after 没有 authors 的另一条原补充，成功恢复标量并保留后加作者。

TDD 证据 `.tmp/09/compat/`：

- `red.log`：修复前真实 HTTP/stdio 原请求均 IsError；不是 JSON 字符串单测。
- `green.log` 是中途真实失败：只解耦 hash 后仍被旧请求缺 authors 的 schema 拒绝；进一步独立 ExpectedState 后 `green-schema.log` 通过。
- `red-digest.log`：在 overlay 恢复修复前完整 Input/State 摘要绑定，显式 authors:null 通过旧 schema 后仍因旧 SHA 不匹配而失败；`green-final.log` 同用例在新实现通过。此额外红跑明确区分摘要问题与必填 schema 问题。
- 作者主生产场景新增同键重放及异姓名/角色/理由/expected 作者角色拒绝。扩展时首次与后文已有 corrections 局部变量重名，出现两次编译失败；修正测试变量名后通过，没有改生产逻辑来绕测试。

### 本轮自己实际运行的门禁

以下不借用前一 worker 的 fullUI/1152/build 成绩。环境 Go1.26.10，共享 GOCACHE 同前，本轮独立 GOTMPDIR=`.tmp/09/compat/gotmp`、GOLANGCI_LINT_CACHE=`.tmp/09/compat/lintcache`；前端 Node26.13.1/npm11.20.0。

- `go test -p 1 ./internal/bootstrap ./internal/application/enrichment ./internal/seriesmeta ./internal/database ./internal/api -run 'TestProductionMCP(AuthorCorrections|Enrichment)|TestEnrichment' -count=2 -timeout=5m` 通过，`targeted-two-rounds.log`（database/api 没有匹配测试，不冒称此命令跑了它们全包）。包含正常作者、补充、Undo、并发/当前权限屏障、异载荷、HTTP+真实 stdio 两轮。最终补真实旧 DB Migrate 路径后，`go test -p 1 ./internal/bootstrap -run 'TestProductionMCP(AuthorCorrections|Enrichment)' -count=2 -timeout=5m` 再通过，`targeted-final-two-rounds.log`。
- `go test -p 1 ./... -count=1 -timeout=10m`，外层 1800s：本轮完整 backend 通过（`backend-full.log`）；最终加上旧数据库实际迁移测试后完整 backend **再次全部通过**（`backend-final-full.log`）。相关 api/database/application/enrichment/seriesmeta/bootstrap 全包均实际通过，无跳过收费替身以外的生产实现，无 baseline 失败或盲重跑掩盖。
- `gofmt -l ./cmd ./internal` 输出为空（`gofmt-final.log`）；`golangci-lint run` 为 **0 issues.**（`lint-final.log`）。
- `go run ./cmd/tsgen` 43 类型，生成前后 SHA 同为 `fb04c17266ec28033b735ed70d1b5e0bee24e93a9a5189f01cbb8fcfe9f16782`，`tsgen-final{,-before,-after}.log`；相对原 09 生成物无漂移。
- `npm test -- src/pages/settings/metadataOperations.test.tsx --maxWorkers=2`：1 文件 / 6 测试通过，`frontend-history.log`。**本轮未重跑 fullUI、frontend build/lint**；未改前端，不把前一 worker 的 1152 测试/build/lint 当作本轮结果。
- `git diff --check` 最终通过。写文档曾由 Windows Python 默认 CRLF 引入整文件换行噪音，已恢复 LF，最终 diff 仅保留原 09 delta 与本轮内容增量。未改 SQL，未跑 sqlc，无依赖变更。

### Remaining

无已知此 blocking 残留；未实现票10封面，也未用空壳封面字段伪造未来功能覆盖。未来命令扩展仍须维护显式摘要映射，并保留本 base08 兼容回归。当前09仍 ready-for-agent、ADR0016 draft；全部未提交，等待父新一轮独立 review。建议本次修复 subject：`fix: preserve durable enrichment replay across author extension`。
