# functional05 续办（claimed；不覆盖 perf05 或更早失败证据）

本轮优先功能、真实升级与维护；没有修改搜索性能实现或正式预算，也没有重测/宣称性能稳定。所有改动未提交，未新增工作树、未开 PR。原 `report-perf05.md` 的 1.9994x / 外部负载限制继续有效。

## 维护失败传播：已修复，真实 HTTP 红绿

- 已先阅读 TDD skill；沿用正式规格已确认的真实 HTTP + SQLite seam，不引入测试专用生产接口。
- `TestSearchMaintenanceMissingLibraryFailsHTTP` 先创建有效 PNG/CBZ 与真实资料库，再删除资料库根目录，HTTP 发起系统索引重建并观察终态。`functional05-maintenance-red2.log` 真实复现单库摄入报错后运行却 completed。最初夹具误删 DB 根目录导致 Windows 文件占用的无效红灯另保留 `functional05-maintenance-red.log`，不把它当产品红灯。
- `application/scanning.ReindexLibraries` 并发安全汇总库错误（保留 ID/路径及 `%w` 错误链），等待已启动扫描；ctx 取消优先返回 `ctx.Err()`，不被聚合的磁盘错误改写为普通失败。没有扫描重构或改变吞吐策略。
- `functional05-maintenance-green.log` 缺失库正确 failed，有效真实 CBZ 连续两次维护仍成功。
- 原规范化生命周期/SQL 快照夹具使用假路径，只能证明索引先修复、随后全扫描正确失败。将这些测试改为明确要求 failed，不再拿它们冒称完整维护成功。独立真实归档测试仍严格要求 completed。
- 当前契约写入 `internal/application/SPEC.md`，用户可见修复写入 CHANGELOG。

## 独立 archive 安装升级链

外部 artifacts 目录沿用 README：`C:/Users/saber/.thinkrail/search-experience-research/acceptance-05`。冻结原母库只读复制，不覆盖。

1. 原真实 v4 `86c510d6501c0525c2e66617718522699176ff30` 生成的 200000 books / 20000 series 母库。
2. **独立 `git archive 765728ee`** 到 `intermediate-765728ee`；已用只读 git 核实该提交 `currentSchemaVersion=5`，不是手写/模拟的中间版本。数据库测试驱动由 Go `-overlay` 虚拟加入，归档生产源码不修改。`functional05-upgrade-intermediate.log`：首次真实升级到 v5 **5.4285268s**，重复迁移 **0.344836s**，两次数量都完整。
3. 最终 HEAD 独立 archive 到 `final-functional05`（精确提交见 `functional05-final-archive-head.txt`），通过 overlay 加入本树未提交的实际数据库生产变更 `series_search.go` / `search_rank.go` 和共享驱动。计时版 migrate 文件位于外部 artifacts，仅在真实调用前后加时钟/打印，不更改迁移分支或 SQL；生成方法为 `scripts/search-upgrade-attribution-functional05.cjs`。
4. `functional05-upgrade-attributed.log`：真实 v5 安装在 book 派生回填插入触发器中注入可识别失败；Migrate 明确报错且仍为 v5。移除故障后同一安装重试成功到 v6，再重复 Migrate 成功。最终 200000/20000 完整。并从另一份真实 v4 母库直接升级到 v6，重复成功。不是只改 user_version 的快照测试。

| 最终 archive 路径 | 真正派生回填调用区间 | 迁移总计 | 重复迁移 |
|---|---:|---:|---:|
| v5 故障后重试 → v6 | book **28.1365517s**，series 没有重新回填 | **28.2452258s** | **0.0143058s** |
| v4 → v6 | series **0.9506497s** + book **10.8911759s** = **11.8418256s** | **11.9814812s** | **0.0181658s** |

这里“额外搜索回填”精确指两个生产 `rebuild*SearchIndex(db)` 的执行区间，包含其索引及派生文本重建，不将其他迁移或整个 Migrate 冒充搜索回填。直接 v4 总计减这两个区间为 **0.1396556s**，包括触发器安装及其他启动迁移/清理；v5 对应 **0.1086741s**。不同阶段外部负载与本轮其他检查并发，28s 与 10s 的差别不是受控消融或回归结论。

## 升级后 HTTP、协议、资料库和权限

- `functional05-upgrade-http-matrix.log` 保存初轮（管理员）通过；`functional05-final-http-recheck.log` 补全普通读者组合，不覆盖初轮。
- 对**直接升级安装**和**经过 v5 且失败重试的安装**分别运行：星/孤星/孤星Atlas/宽松 `孤—星 Ａｔｌａｓ`；all/series/book/title 四个 target；只用真实服务端 next_cursor 直到末页，逐项与独立生成事实 + 字面子串数值键 oracle 比较，不调用生产归一化或排名函数。
- 两种角色 admin/regular；每种查询 × library=1/不存在的999 × name_asc/name_desc，资料库完整预期与 total；Mihon limit=7，首/中/末/越界四页、实际 total 与 has_next；OPDS 四页身份与 next 导航。另验证 status=completed 筛出明确10项、未登录401、角色变化旧游标400 invalid_search_cursor、强制改密403。
- 普通读者权限是项目当前 regular 角色契约，没有凭空引入资料库 ACL。既有空输入/标点/无效游标/长字段等组合由全量测试回归，不把这些 opt-in 查询称作穷尽所有参数笛卡尔积。

## 广泛结果完整 HTTP 遍历

- `TestSearchBroadCompleteOrderedHTTP`：跨两个库的150系列/3000书，独立三个排序组（自身精确书、精确系列、继承书），all=3150、series=150、book=3000，全部真实 HTTP limit=100 遍历并严格逐项有序比较。初轮 PASS 5.22s，最终扩展矩阵轮再跑 PASS 132.91s（严重负载），两份完整日志保留。
- 冻结实际规模、经过 v5 升级的安装，海查询只用服务端 next_cursor，**series=19980 和 book=199980 均逐项完整有序比较 PASS**；不是合法定位游标跳页。`functional05-broad-http.log` 完成 **2356.00s**。该轮只验证功能，无预热/100次性能预算采样，不修改原性能证据。
- 200000/20000 规模 **all=219960** 的完整遍历 **PASS，2555.40s**：`functional05-final-http-recheck.log`。series/book/all 三种范围现在都有实际完整有序 HTTP 证据，不再是只验证首批或猜游标。2356.00s/2555.40s 是各自完整功能驱动墙钟（含复制、迁移、独立 oracle 和末尾派生重建），不是纯 HTTP 请求计时或 p95。

## 同规模真实文件维护

- `search_large_maintenance_acceptance_test.go` 从只读母库副本升级，为每个持久书/系列身份设置实际路径；真实 20000 个系列目录、200000 个有效 CBZ（每份236 bytes，含一张有效32×32 PNG），路径唯一，硬链接只共享内容，不共享藏书身份。所有文件均实际可访问。母库原合成路径未改写。
- 首次单 inode 超过 NTFS 硬链接上限，在第1024本失败，保留 `functional05-large-maintenance.log`。改为每1000本一个独立模板 inode 后，完整准备200000路径成功（**5m58.8591825s**），再实际 HTTP 启动默认 metadata_scan 完整维护。
- 默认 metadata_scan 在严重主机负载与本轮检查并发下速度极低，约32分钟后由本代理主动中断，日志 `functional05-large-maintenance-recheck.log` 以 EXIT=143 保留。**不是 completed，不是维护通过**。封面生成、读取归档及 SSE 队列警告均保留；该轮测试夹具未启动 SSE broker 循环，也有额外丢事件日志成本。
- 新一轮明确选择产品支持的 **fast_scan** 配置、复用上述200000真实有效文件、独立母库副本/独立 DB，逐个 os.Stat 验证后才准入维护；启动实际 broker 循环避免夹具人为日志洪泛。fast_scan 强制遍历/摄入全部文件，但按产品契约不读取归档元数据、不重建封面。**不能把它说成默认 metadata_scan 或所有扫描档位的完整验收**。`functional05-large-maintenance-fast.log` **PASS**：准备/逐文件检查 **1m42.9172323s**（不计入维护），HTTP 完整任务 **594.7720135s**；扫描自身 **580.557s**，discovered=processed=200000、skipped/opened/queued_covers/failed/rehomed 均0，series=20000/books=200000 未变；全部实际路径检查通过，升级后全局独立 sparse oracle **400** 项、资料库 oracle **20** 项。整个 opt-in 测试887.35s含准备/后置校验，不能拿总测试时间代替维护区间。
- 后续独立身份审计 `TestSearchRealArchiveIdentityAudit` 还核对所有书/系列连续持久 ID、library_id、精确磁盘路径和全部书的 series_id，对路径逐个 os.Stat；不仅核对聚合数量。`functional05-real-identity-audit.log` **PASS，247.17s**：20000系列/200000书的ID、库与精确路径全部保留，全部200000书的 series_id 关系保留，无不可访问路径。
- 在本次实际 **fast_scan 完整维护** 下，直接 v4 的精确额外搜索回填 / 维护 = **11.8418256/594.7720135 = 0.01991x**，v5故障后重试 = **28.1365517/594.7720135 = 0.04731x**，均小于正式2x预算。但默认 metadata_scan 未完成，不能将这份配置不同的成功当作默认档位或所有部署的完整维护预算已闭合；该限制明确保留，正式预算未修改。

## 真实 Edge + Playwright 补充

`functional05-browser-server.log`、`functional05-browser-run.log`、`functional05-browser.json` 与两张 PNG；Edge **157.0.4341.60**，headless、en-US、1280×900、重建 web/dist + 实际 SQLite/HTTP/认证，不拦截任何成功或失败响应。

已执行 PASS：英语搜索文案；3000字符元数据标题的弹窗 viewport 边界/无水平溢出自动检测与截图；管理员系列结果真实导航；同一浏览器上下文从管理员切换成 regular 后重新 bootstrap，搜索并进入真实系列；普通读者书搜索进入真实 reader 路由（有效可访问 CBZ）。无 pageerror。截图为 `functional05-browser-english-long.png` / `functional05-browser-reader.png`。

未执行：有头人工视觉审阅；身份切换恰逢旧响应仍在途的真实浏览器动作（现有会话组合测试覆盖不取消/晚到等竞争，但不冒充浏览器证据）；仍缺真实用户漏搜样本。早先 zh-CN 的慢请求/失败/追加等浏览器证据继续独立保留。

## 当前检查与资源约束

- 前端 `functional05-frontend-tests.log`：**119 files / 1143 tests PASS**，261.12s；相同真实 node 工具入口 tsc + vite build、eslint **EXIT=0**，见 `functional05-frontend-build-lint.log`。无前端生产变更。
- `functional05-generated.log`：tsgen、PowerShell sqlc generate 都退出0；before/after SHA256 无漂移。
- 早一轮 gofmt 空、lint0 issues：`functional05-gofmt.log` / `functional05-lint.log`。新增夹具后的当前代码检查另以最终串行 label 保存，不沿用旧轮冒称当前全部通过。
- 全量后端初轮捕捉到本次修复要求的假路径测试终态变化（已修夹具预期，不放宽成功条件）及无关 scanner cover 队列10s超时；该 scanner 测试独立重跑 PASS，保留两个结果。
- 第二轮全量 `functional05-backend-recheck.log`：上述维护/扫描测试通过，但三个无关 relocation 测试 Windows rename 失败，记录保留。
- 随后并发启动多个 Go 检查导致主机严重迟缓；本代理终止自己的重复 Go/lint 启动以串行化（EXIT143，不是 PASS；见 `functional05-serialization.txt`）。未停止/修改任何外部训练或压缩任务。环境诊断 PowerShell 自身超时，不能声称取得了新的可靠 CPU/温度测量。
- 串行全量 `functional05-backend-serialized.log`：维护相关及其余包通过，api 仍在另一无关 `TestRelocationCommitUsesPreviewWithoutReadingTargetFiles` 的 Windows rename 失败。随后四个涉及的 relocation 测试独立3次执行，第一次 before_rename 又失败，其余重复通过，见 `functional05-relocation-isolated.log`；不能把这一失败独立执行称作PASS。完整维护结束后的再次**全量后端 `go test ./... -p 1 -count=1 -timeout 20m` 全部 PASS，exit0**，见 `functional05-backend-post-maintenance.log/.exit`（api563.012s）。未改无关 relocation 代码、未放宽断言，所有先前失败保留。
- 最终 lint **0 issues / exit0**，gofmt输出0 bytes：`functional05-lint-serialized.*` / `functional05-gofmt-serialized.log`。

## 复验入口（另取新 label / 新输出路径）

工具绝对路径与 TEMP/TMP/GOTMPDIR 设置沿用 README；所有 Bash 先显式 cd 本树。以下 `$G`/`$N` 沿用 README 的真实 Go/Node bin 目录，不通过 PATH shim。

```bash
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
"$G/go.exe" test ./internal/api -run '^TestSearch(MaintenanceMissingLibraryFailsHTTP|FullMaintenanceWithRealArchiveHTTP|BroadCompleteOrderedHTTP)$' -count=1 -v
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
SEARCH_UPGRADE_HTTP_INPUT=C:/Users/saber/.thinkrail/search-experience-research/acceptance-05/functional05-archive-retry-v6.db "$G/go.exe" test ./internal/api -run '^TestSearchArchivedUpgradeHTTPMatrix$' -count=1 -v
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
SEARCH_REAL_INPUT=C:/Users/saber/.thinkrail/search-experience-research/acceptance-05/frozen-v4.db SEARCH_REAL_ROOT=C:/tmp/search-functional05-replay-NEW SEARCH_REAL_FILES=C:/tmp/search-functional05-real-recheck/library SEARCH_REAL_PROFILE=fast_scan "$G/go.exe" test ./internal/api -run '^TestSearchLargeRealMaintenanceHTTP$' -count=1 -v -timeout 60m
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
SEARCH_REAL_VERIFY_DB=C:/tmp/search-functional05-real-fast/catalogue.db SEARCH_REAL_VERIFY_FILES=C:/tmp/search-functional05-real-recheck/library "$G/go.exe" test ./internal/api -run '^TestSearchRealArchiveIdentityAudit$' -count=1 -v -timeout 10m
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
SEARCH_ACCEPTANCE_FULL_WALK=1 SEARCH_ACCEPTANCE_QUERY=海 SEARCH_ACCEPTANCE_TARGET=all SEARCH_ACCEPTANCE_INPUT=C:/Users/saber/.thinkrail/search-experience-research/acceptance-05/functional05-archive-retry-v6.db SEARCH_ACCEPTANCE_OUTPUT=C:/tmp/search-functional05-walk-NEW.json SEARCH_ACCEPTANCE_LABEL=functional05-walk-NEW "$G/go.exe" test ./internal/api -run '^TestSearchAcceptanceMeasure$' -count=1 -v -timeout 90m
```

创建新文件树时省略 SEARCH_REAL_FILES；要复验默认归档元数据扫描则省略 SEARCH_REAL_PROFILE（保留新 ROOT，不能覆盖旧失败）。真实磁盘文件在 C:/tmp，不纳入 Git；受临时目录清理影响时须重新物化，不允许只复用 DB 后宣称文件存在。

真实浏览器复用 README 的 fixture 启动方法，但使用**新 ready 文件名**，运行 `scripts/search-browser-functional05.cjs`，PLAYWRIGHT_MODULE/SEARCH_BROWSER_EXE 等与 README 相同。冻结母库 SHA256 仍为 `96dc6cc54c52e3c8419b950db1d112ef265b290a9ae18b56e09e37de76cd15d5`；升级产物 hash 在 `functional05-installations.sha256`，本轮生产/测试驱动与 archive 二进制 hash 在 `functional05-source.sha256`。此前 perf05 二进制/hash/原始样本没有被替换。

## 交付闭合状态

1. 真实直接/经02升级链、失败重试、幂等与升级后协议/角色/资料库组合：**已闭合本轮自动化范围**。
2. 精确搜索回填归因、200000/20000真实文件维护和全部身份保留：**fast_scan 已实测通过**；**默认 metadata_scan 未闭合**，不能泛化配置。
3. 维护吞错假 completed：**已修复，红绿闭合**；ctx 取消语义保留。
4. 实际规模 series/book/all 全量有序 HTTP 与独立 oracle：**已闭合**。
5. 英语/长标题边界/普通读者导航/同浏览器身份切换：**已执行**；有头人工视觉审阅、浏览器在途身份切换仍未执行。
6. 当前全量后端、gofmt/lint、前端 test/build/lint、生成漂移：**已闭合**，早先失败和资源中断不抹除。

本票保持 claimed。仍需可控负载稳定性能复验（perf05仅1.9994x极小余量，未宣称稳定通过）、默认 metadata_scan 完整维护证据，以及上述人工/浏览器限制；真实用户漏搜样本仍缺。所有本代理启动的测量/检查进程已结束，没有遗留后台验收任务。
