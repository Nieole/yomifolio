# 工单 05 验收记录：低负载自动化门禁通过

**状态：resolved（自动化验收完成，独立review待父代理执行）。正式预算仍为2x。**

## 证据保留约定

原始日志、采样、源码补丁和哈希清单按测量时内容保留，不为满足 whitespace 检查改写；其中的行末空白、CRLF及补丁上下文不属于生产源码格式。源码与当前说明文档单独执行 whitespace 检查。记录包含本机工作树路径与运行环境，不包含配置密钥；母库、归档树、二进制和缓存不入库。接回时仅规范说明文档换行，并同步持久 SPEC 与 CHANGELOG 的最终验收结论，不改变被测生产代码。

## idle-final05 最终性能验收（当前）

详见 [report-idle-final05.md](report-idle-final05.md)、[完整逐场景表](measurements-idle-final05.md)。**本轮零代码修改**，772个前轮源文件/驱动/母库/二进制hash全部相同；引用finalperf05同源全量后端PASS/gofmt空/lint0 issues及functional05未变前端119文件1143测试/build/lint/生成无漂移，未冒称本轮重跑。

收到空闲窗口后即串行执行固定旧archive完整矩阵（370.95s）和candidate三轮完整矩阵（446.84/347.20/424.44s），每场景10预热+100请求，原200000/20000冻库、同设置同语义完整oracle，首/中/近末页及全部词/类型矩阵齐。第一轮及最后确认轮**全部正式预算通过**：首批/同期旧版最大1.266571x、1.033759x；深页/同轮首批最大1.155608x、1.092555x，均<=2。历史baseline及所有失败保留并列参考，不换基线。

**中间复测有失败，不能称三轮全绿**：海贼王book近末609.9937ms/首批270.2347ms=2.257274x；该轮末段外部chdman再启动，最高1346%、整机CPU100%，不满足同低负载条件，完整保留raw/log/负载后另做完整确认轮，未删样本/裁尾/只测失败场景。baseline/第一轮/确认轮CPU中位18/16/15%，最大45/42/53%，无chdman高占用观测；275条总记录仅1条CPU100%。两次低负载有效完整测量全部通过，自动化门禁闭合，不外推任意外部负载下稳定延迟。独立review仍交父代理；若不接受负载污染轮单列，应恢复claimed，不改失败为PASS。

正式回填/完整真实文件维护预算引用functional05的fast_scan配置0.01991x/重试0.04731x通过；默认metadata_scan中断、有头人工/浏览器在途身份切换未执行、真实用户漏搜样本缺失继续诚实保留，不新增规格未要求的门禁。新label idle-final05保存raw/日志/hash/patch/负载/测量表。所有前序未提交修改保留，无提交/PR/删树。以下章节全部为历史归档，旧“当前/阻塞/claimed”描述不覆盖本节结论。

## finalperf05 最终性能续办（历史）

详见 [report-finalperf05.md](report-finalperf05.md)、[全部逐场景预算表](measurements-finalperf05.md)。系列改为单索引候选流选择完整最佳字段，书完整候选+有界堆不变。复用原母库和固定旧archive二进制，重新串行旧版完整矩阵及新实现两轮完整矩阵（全部10预热+100次，含中/近末页）；所有旧失败不覆盖。第一轮多个首批及海贼series近末页2.3013x失败；第二轮相对同期旧版/深页通过，但海series62.735ms/原24.1022ms=2.6029x仍失败。720条持续负载记录中639条CPU100%，外部压缩最高1429%/训练1153%。**未稳定闭合，不resolved，不挑成功轮。** 当前全量后端首次无关Windows rename失败保留，串行全量重跑PASS；gofmt空、lint0 issues。无前端修改，引用functional05同源前端/生成门禁，不冒称本轮重跑。源码/hash/补丁和完整未提交状态均另存finalperf05新label。

正式回填/完整维护比较在已执行fast_scan配置下0.01991x/0.04731x通过；默认metadata_scan未完成保留为配置限制，不自设“必须所有档位完成”的新门禁。功能报告其余已完成范围继续有效；有头人工视觉、浏览器在途身份切换与真实漏搜样本仍缺，独立review留父代理。本README后续旧章节的“仍缺”按各自label归档，当前状态以本节/新报告为准。

## functional05 功能续办（前轮）

详见 [report-functional05.md](report-functional05.md)。本轮修复维护单库失败误报 completed（真实维护 HTTP 故障注入红绿）；补真实 v4→独立 archive `765728ee` v5→最终 archive+当前生产 overlay v6、失败重试与重复迁移，精确记录派生回填区间；两条真实升级路径均执行 admin/regular 的全局完整游标、资料库筛选排序、OPDS/Mihon 分页矩阵。英语真实 Edge、长标题边界、普通读者系列/阅读导航及身份切换通过。实际规模 series=19980/book=199980（2356.00s）、all=219960（2555.40s）完整 HTTP 有序遍历均已完成。

**仍不标 resolved，不宣称稳定性能通过。** 200000/20000 的默认 metadata_scan 实际运行后因主机负载/夹具日志成本中断，不能算成功；复用200000真实文件的 fast_scan 完整维护594.7720135s通过、20000系列/200000书全部身份/路径/归属独立审计通过，但不冒充默认metadata扫描；全量后端最终重跑、lint/gofmt、前端119文件1143测试/build/lint及生成漂移均通过。详情与保留的失败见新报告。原所有失败记录、预算、冻库与 perf05 证据均保留。以下 perf05/前轮内容按各自 label 归档，不代表本轮新增维护修复已经包含在旧二进制中。

## 前轮 perf05（搜索生产查询保持不变）

**整票未过最终验收。** 本轮只优化查询与复验性能，未修维护错误传播、未补浏览器矩阵。当前详情见 [report-perf05.md](report-perf05.md)、完整逐场景表 [measurements-perf05.md](measurements-perf05.md)。以下原 `candidate-final` 全部资料作为前轮归档保留，不能当作当前代码的最终验证。

- 书使用互斥自身/继承索引候选流，在数据库 module 内用容量 N 的完整数值键最大堆取 keyset 后继，然后水合；系列用互斥最佳字段分支排除次佳字段，避免字段物化。没有全库原文归一化/截断/服务器快照。
- 先分析原计划与CPU profile，再优化；补HTTP来源去重/字段排序/游标与不同limit回归。计划、优化前后profile原文件、top报告均保留。
- 复用原200000/20000母库及原baseline二进制，同语义完整oracle和真实HTTP顺序校验仍执行；原旧版同期完整重测为 `baseline-perf05.json/log`，不替换原 `baseline.json`。
- **两轮全矩阵**均10预热+100请求、首/中/近末页全测且生产代码一致：`candidate-perf05` 第一轮海series首批134.088ms仍失败；`candidate-perf05-recheck` 第二轮所有相对项通过本次采样。三项原失败分别为48.191ms/旧24.1022ms=1.9994、13.6272ms/旧9.9493ms=1.3697、海贼book中页517.754ms/首批540.611ms=0.9577；最大深页比1.5839。
- **不能宣称稳定闭合性能门禁**：海series对原预算仅余0.0134ms；实际外部python训练/chdman压缩占用约10–13核，CPU100%，原旧版海series诊断自身也升至62.854ms。环境证据及全部失败样本保留，不终止其他进程。需在外部负载可控时复验，不以第二轮通过掩盖第一轮失败。
- 当前代码/驱动/二进制hash及补丁：`perf05-source.sha256`、`perf05-source.patch`。后续只补报告与计划测试，被测生产代码未变；无新迁移/生成契约。
- 相关搜索/游标/排名/gram测试PASS，gofmt空，lint0 issues。更广api/database包级执行database PASS、api因无关relocation目录rename Access is denied失败，单独3次重试PASS；日志全保留。**当前代码未跑全量后端/前端/浏览器，不沿用旧全量PASS冒称当前全量通过。**

## 前轮归档说明

下文的“最终/candidate-final/最终SQL/最终检查”均指前轮，不是perf05当前代码。冻结库、原基线、工具及原复验命令继续有效；本轮新label复验命令见report-perf05。

## 身份、数据与测量

- 集成基点：`0302810823672c47e4a000f86c8613407aec135d`，最终性能针对该基点加 `measured-source.patch`。生产 SQL 与驱动 SHA256 见 `measured-source.sha256`；之后仅补充测试/报告和浏览器夹具清理，没有再修改被测生产 SQL。
- 基线固定 `86c510d6501c0525c2e66617718522699176ff30`，独立 `git archive` 导出到仓库外 `C:/Users/saber/.thinkrail/search-experience-research/acceptance-05/baseline`。没有借用任何活动实施树源码作 baseline，没有编辑基线；共享测量测试用 Go `-overlay` 虚拟加入，二进制在 archive 外。
- 母库由**真实旧版** `Migrate` 和正常触发器写入创建，关闭/清空 WAL 后只读复制。精确 200000 books、20000 series、每系列 10 本；名称、ID、路径、UTC 时间固定。母库 SHA256：`96dc6cc54c52e3c8419b950db1d112ef265b290a9ae18b56e09e37de76cd15d5`。生成器在 `internal/api/search_acceptance_test.go`。ID 能被 1000 整除时使用稀疏前缀 `孤星Atlas`，其他使用 `海贼王`，名称附定长类型/编号；标题为空。合成路径**不可供文件扫描**。
- 比较前以不调用生产归一化/排名函数的字面子串 oracle 校验旧/新**完整**数据库命中集合，不拿不同集合比较速度。广泛系列 19980、广泛书 199980；稀疏系列 20、稀疏书 380（自身或所属系列命中）。宽松查询 `孤—星 Ａｔｌａｓ` 单列，仅新实现，不与旧版零结果比较。
- 真实监听 HTTP 路由、SQLite、管理员登录 Cookie 和每请求认证；首批 limit=20、同一 target 比较，不把旧 all 的 40 条和新 all 的 20 条比较。每场景 **10 次预热 + 100 次独立请求**，原始纳秒保存在 JSON；最近秩 p50 为排序后第 50 项，p95 为第 95 项。无单元测试墙钟断言。
- 深页不累计重拉前页。广泛场景由独立 oracle 的完整排序键构造合法定位游标，先请求其 20 个后继并验证顺序，再使用**服务端返回的真实 next_cursor**测中页/近末页；定位和验证不计时。稀疏书逐页完整 HTTP 遍历并保存末页游标。只有一页的 20 条系列不伪装成深页。广泛集合尚未逐页全量 HTTP 遍历，数据库全集合与 HTTP 首批/指定深页是两份证据。
- Windows 11 10.0.26300、Ryzen 7 8845HS（8C/16T）、约 31.33 GiB、WD_BLACK SN7100 NVMe SSD、平衡电源计划，详见 `hardware.json`。Go1.27.4、SQLite3.54.1；两侧实测 WAL/NORMAL、4096 page、mmap 268435456、cache_size -128000、temp_store 2。每次独立复制 DB；连接内热缓存，不清操作系统缓存。测量时没有同时运行本任务的构建/测试；温度与其他系统后台负载未持续监测，因此仍不能把单次预算通过外推为全部环境通过。

## 性能结果与优化（仍有失败）

全部 p50/p95、同语义旧版比值和对应首批比值见 [measurements.md](measurements.md)。

| 失败门禁 | p95 | 对照 p95 | 比值 | 预算 |
|---|---:|---:|---:|---:|
| `海` series 首批 | 72.013ms | 旧版 24.102ms | 2.988 | <=2 |
| `星` book 首批 | 23.212ms | 旧版 9.949ms | 2.333 | <=2 |
| `海贼` book 中页 | 2220.027ms | 新首批 910.482ms | 2.438 | <=2 |

其他已测相对项通过**本次采样**；宽松匹配独立报告。失败不能解释成已满足规格，也没有删除失败样本或改预算。

红—优化—回归过程：

1. 原集成 SQL 测得 `海` series 首批 p95 170.653ms（旧版 24.102ms）；随后 broad book 全集合/首批阶段 1800 秒进程上限中断，不能提供其 100 样本，见 `candidate-before.*`。这就是最先复现的性能红灯，不是一个完成的性能报告。
2. 移除按每个身份扩展 2/4 字段行再做窗口排序的查询；索引候选去重后选择自身最佳字段，无自身命中才选所属系列最佳字段；系列物化有限数值事实；先 keyset/limit 再水合结果。没有截断字段、候选或命中集合。初步运行 `candidate.json` 用于诊断，**最终结论只使用 candidate-final.json**。
3. 新增 HTTP 数值字段选择回归（exact、位置、长度、Unicode 字符、同分持久 ID、不同批量、书/系列），旧有排名/游标/长标题/归一化回归和最终全量后端通过。
4. 对最终 SQL 重新执行所有短/长、稀疏/广泛首批、中页与近末页及宽松场景，每项预热+100次。最终过程 1454.28 秒完成，见 `candidate-final.log/json`。优化后仍存在上表失败，**没有因为失败而省略测量或标 resolved**。这里是测量驱动的性能红绿回归；没有声称新增行为测试在原集成版本出现了一个不存在的功能红灯。

`plan-final.log` 对 `globalSeriesStatement` / `globalBooksStatement` 的**实际生产语句**执行 EXPLAIN，包括短/长词、首批/seek。候选经 FTS MATCH，投影/藏书经主键点查，继承书经 `idx_books_series_id`；SCAN 的是 MATCH 虚表与物化候选集，不是每次全库归一化或无索引 books 扫描。仍需排序/处理广泛候选，索引驱动本身不能证明预算通过。

## 升级与维护

- 真实 v4 母库副本经最终 `database.Migrate` 升到 user_version=6；随后独立集合 oracle 和真实 HTTP 首批/继续加载验证通过。旧版重复 Migrate=0.0838866s；最终升级 **总计**14.0983935s。未把总耗时冒称为精确新增派生回填时间。
- 同规模 `RebuildSeriesSearchIndex` + `RebuildBookSearchIndex` 共44.9468578s，记录的是**搜索派生数据重建**，不是包含磁盘扫描的完整维护任务。总迁移/搜索重建比约0.314仅为保守诊断，不宣布正式“额外回填/完整维护”归因门禁完成。
- `TestSearchFullMaintenanceWithRealArchiveHTTP` 新增真实可访问 CBZ、有效 PNG 页面、HTTP 发起维护、等待真实 completed、强制扫描、随后全局与资料库 oracle、数量不变，并重复两次通过。不能拿这个一书夹具充当 20 万本书全文件维护计时。
- **仍缺**真实 v4→中间发布版→最终版的独立 archive 连续升级；已有 v4/v5 SQL 快照测试全量通过，但不是该真实安装链的替代。
- **额外已发现限制**：`application/scanning.ReindexLibraries` 记录单库扫描失败但只返回 `ctx.Err()`；因此假路径重建可能被报告 completed（首轮全量日志含 `/one` 失败后 completed 的测试）。该行为尚未修复，不以这些旧假路径夹具声称完整维护健康。新增真实归档夹具避免了这一假阳性，但不能证明错误传播正确。

## 真实浏览器与行为覆盖

实际运行本机 Playwright + Edge **157.0.4341.60**，headless 1280×900、zh-CN；加载本树重新 build 的 `web/dist`，后端为真实 SQLite+认证+HTTP fixture，不是静态 mockup。只有慢请求、503、invalid_search_cursor 在网络 seam 注入，成功响应仍来自真实后端。

通过：真实混排首批；追加失败保留20条及重试40条；游标失效显式重新搜索；慢查询立即禁用旧条目；首批失败与重试；切仅系列；连按追加不重复；45条完整系列末页；键盘有效选择；关闭/重开重新查询。`browser.json`、`browser.png`、`browser-initial.png` 与 `browser-server.log`（PASS）保存证据。修正了验收夹具关闭时 SSE 未关导致的等待，没有据此修改产品会话。

未执行浏览器项目：有头人工视觉复核、英文 locale、超长标题视觉边界、普通读者真实导航；取消无效/晚到响应/重置/连接身份切换/长标题及本地化的自动化组合回归通过，但不能冒充这些浏览器动作已执行。

原有 HTTP/SQLite 测试覆盖资料库筛选/排序、OPDS/Mihon 页码与真实总数、鉴权/角色变化、空输入/仅标点/无效游标、写入/删除/元数据/索引缺项修复等；新增字段选择和完整真实归档维护测试通过。**没有实际用户漏搜系列与关键词样本，真实漏搜根因仍未复现。**

## 最终检查

- 后端：`backend-final.log`，`go test ./... -p 2 -count=1 -timeout 20m` **PASS**；`gofmt-final.log` 空；golangci-lint2.15 `backend-lint-final.log` **0 issues**。
- 首次未限并发全量出现 relocation generation 的非搜索失败，`backend-full.log`保留；独立固定旧版同一测试通过，不能断言是既有确定性失败。最终限并发完整重跑通过，不删除首轮结果。
- 前端最终 `vitest run --pool=forks --maxWorkers=4`：**119 files / 1143 tests PASS**。threads 首轮热力图 TZ worker 差异和 ApplicationRoutes 超时、无限 forks 后的 adminActions 超时都留档；未改无关产品代码或放宽断言。
- 直接 Node 执行 tsc/vite build、eslint：**PASS**。npm 脚本的 Windows 子进程 PATH 找不到 node，改用真实 node 执行同一工具入口，不把 shim 失败当产品失败。
- PowerShell `sqlc generate` 退出0；`go run ./cmd/tsgen` 退出0；前后四个生成文件 SHA256 无漂移，见 `generated-{before,after}.sha256`。SQL 源/契约未变更。

## 精确复验命令

以下 Bash 均先显式 cd 本树；`G`、`N` 只为排版缩写，工具均使用真实二进制。

```bash
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
export TEMP=C:/tmp TMP=C:/tmp TMPDIR=C:/tmp GOTMPDIR=C:/tmp GOCACHE=C:/Users/saber/AppData/Local/go-build
G=C:/Users/saber/scoop/apps/go/current/bin
N=C:/Users/saber/scoop/apps/fnm/current/node-versions/v26.13.1/installation
```

可重复准备/编译/测量驱动（PowerShell 内也显式 Set-Location；先在新 artifacts 目录 Seed 一次，不覆盖已冻结母库）：

```bash
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -NoProfile -File scripts/search-acceptance.ps1 -Stage Prepare
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -NoProfile -File scripts/search-acceptance.ps1 -Stage Baseline
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -NoProfile -File scripts/search-acceptance.ps1 -Stage Candidate
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -NoProfile -File scripts/search-acceptance.ps1 -Stage Plans
```

本次实际执行：`git archive 86c510d6501c0525c2e66617718522699176ff30 | tar -x -C C:/Users/saber/.thinkrail/search-experience-research/acceptance-05/baseline`；在 archive 目录 `go test -overlay ../overlay.json -c -o ../baseline-api.test.exe ./internal/api`；在本树 `SEARCH_ACCEPTANCE_SEED=.../frozen-v4.db baseline-api.test.exe -test.run '^TestSearchAcceptanceSeed$' -test.v -test.timeout 45m`。随后分别运行 baseline/candidate 二进制，环境 `SEARCH_ACCEPTANCE_INPUT=.../frozen-v4.db SEARCH_ACCEPTANCE_OUTPUT=<本树证据目录>/<label>.json SEARCH_ACCEPTANCE_LABEL=<label>`，参数 `-test.run '^TestSearchAcceptanceMeasure$' -test.v -test.timeout 90m`。最终 candidate 输出名称为 `candidate-final.json`；baseline 没有重命名。驱动支持 QUERY/TARGET 环境筛选仅用于诊断；最终报告未启用筛选。

```bash
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
"$G/go.exe" test ./... -p 2 -count=1 -timeout 20m
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
"$G/gofmt.exe" -l ./cmd ./internal
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
GOLANGCI_LINT_CACHE=C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05/.tmp/lint-cache C:/Users/saber/.thinkrail/worktrees/manga-manager/search-book-ranking-03/.tmp/lint/golangci-lint-2.15.0-windows-amd64/golangci-lint.exe run
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05/web
"$N/node.exe" node_modules/vitest/vitest.mjs run --pool=forks --maxWorkers=4
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05/web
"$N/node.exe" node_modules/typescript/bin/tsc -b && "$N/node.exe" node_modules/vite/bin/vite.js build
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05/web
"$N/node.exe" node_modules/eslint/bin/eslint.js .
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
"$G/go.exe" run ./cmd/tsgen
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -NoProfile -Command 'Set-Location "C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05"; sqlc generate; exit $LASTEXITCODE'
```

浏览器复验：外部安装 `node npm-cli.js install --prefix C:/tmp/search-acceptance-browser playwright --ignore-scripts`；编译 `go test -c -o .tmp/browser-api.test.exe ./internal/api`；以 `SEARCH_BROWSER_READY=C:/tmp/search-acceptance-browser-ready.json SEARCH_BROWSER_DIST=<本树>/web/dist` 启动 `.tmp/browser-api.test.exe -test.run=^TestSearchAcceptanceBrowser$ -test.v -test.timeout=12m`，保留前台或 PowerShell Start-Process 后等待 ready 文件。删除上次 `.done` 再启动。然后：

```bash
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
PLAYWRIGHT_MODULE=C:/tmp/search-acceptance-browser/node_modules/playwright SEARCH_BROWSER_READY=C:/tmp/search-acceptance-browser-ready.json SEARCH_BROWSER_OUTPUT=C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05/.scratch/search-experience/acceptance/browser SEARCH_BROWSER_EXE='C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe' "$N/node.exe" scripts/search-browser-acceptance.cjs
```

## 留给后续实施的阻塞项

1. perf05第二轮完整矩阵所有相对项通过，但第一轮仍失败且外部负载显著、海series预算余量仅0.0134ms；需可控负载复验后才能稳定闭合性能门禁，不降低正式规格。
2. 精确升级新增搜索回填归因、同规模完整真实文件维护（非假路径），以及维护单库失败的正确传播。
3. 独立真实中间版本连续升级链、广泛结果完整 HTTP 有序遍历与升级后协议/筛选组合的完整矩阵。
4. 未执行的浏览器视觉/英文/普通读者导航项目。
5. 仍未取得真实用户漏搜样本。不得宣称根因已复现。

本树全部改动未提交，没有 PR，不删除工作树。
