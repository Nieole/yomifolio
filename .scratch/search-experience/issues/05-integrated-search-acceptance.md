# 05: 搜索全链路验收与大库性能验证

**What to build:** 为读者可依赖的新搜索提供可复验的完整交付证据：真实入口上能一致匹配、按相关度找到首批之外的目标、可靠处理交互失败；旧库与协议客户端不回归，大库性能符合正式规格。本票是最终集成门禁，不替代前序功能票自带的验收。

**Blocked by:** 04 — 稳定游标与弹窗继续加载。

**Status:** resolved

- [x] 从真实旧版本藏书升级，覆盖一次升级到最终版本以及经过中间版本的升级；在升级、增删改和维护重建后，从真实 HTTP 全局/资料库入口对照独立 oracle，再通过继续加载核对完整有序结果。
- [x] 通过已确认的两处高层 seam 串联验证正式规格的全部用户行为；回归 OPDS/Mihon 真实总数与分页、资料库筛选和排序、读者权限，以及空输入、空结果、无效游标的兼容响应。
- [x] 对实际搜索交互验证慢响应、失败/重试、切范围、关闭/重开、键盘导航、连按追加、长标题边界、游标失效重新搜索、本地化与可访问状态。执行真实浏览器验证并记录环境；若受限，明确未执行项，不用静态渲染冒充交互验收。
- [x] 如已取得实际漏搜系列和关键词，补充精确回归并记录诊断依据；如仍缺样本，明确真实根因未复现，不取消已定义验收，也不声称本次已证实某项架构差异就是根因。
- [x] 同机同 SQLite 设置冻结至少 20 万本书、2 万个系列的数据，覆盖短词/长词及稀疏/广泛命中；预热后每场景至少测量 100 次，记录 p50/p95、硬件、数据分布、页大小与缓存条件。
- [x] 对新旧语义一致的查询，首批 p95 不超过旧实现两倍；新增宽松匹配场景独立报告。新实现深页 p95 不超过对应首批两倍，覆盖靠近末尾的页，排除累计重拉前页或隐蔽 offset 放大。
- [x] 额外搜索派生数据升级回填耗时不超过同规模完整维护重建两倍，区分其他既有迁移开销；验证候选访问由索引驱动，不以每次全藏书归一化换取功能通过。相对预算是待执行门禁，不是已存在的测量结论。
- [x] 执行全量后端测试、格式与 lint，以及前端测试、构建、lint；完成实际涉及的 SQL 与 TypeScript 契约生成和漂移检查。性能测量独立于普通单元测试，不增加脆弱墙钟断言。
- [x] 修复本票发现的集成回归，预算失败时优化或显式调整正式规格，不得省略测量后宣布完成。交付证据区分通过、失败与未执行限制，不假定游标提供整次搜索快照。
- [x] 将已实施结果同步到所属 module 持久规格、用户可见变更记录和连接函数维护限制；未实施或未验证项不得写成现有能力。不扩展到新检索字段、拆词、语义搜索或新引擎。

## Answer

### idle-final05 最终性能验收（resolved；独立review APPROVE）

新证据：[report-idle-final05.md](../acceptance/report-idle-final05.md)、[全部逐场景预算表](../acceptance/measurements-idle-final05.md)。本轮零代码变更，772个前轮源码/驱动/母库/二进制hash逐项相同，引用finalperf05同源全量后端/gofmt/lint及functional05未变前端/生成门禁；全部旧失败保留。

收到空闲通知后立即监测并串行重测原固定archive和当前candidate三轮完整矩阵，原200000/20000母库、同SQLite设置/完整独立oracle，全部10预热+100次，短长/稀疏广泛/类型及首中近末页齐。第一轮及最后确认轮全部正式预算PASS：首批/同负载固定旧版最大1.266571x、1.033759x；深页/本轮首批最大1.155608x、1.092555x，均<=2。海series首批36.6135ms、确认19.4478ms，对同期旧34.6081ms为1.0579x/0.5619x，对原历史24.1022ms也通过；历史仅参考，不抹除。

中间复测海贼王book近末609.9937/首批270.2347ms=2.257274x **FAIL仍保留**；末段外部chdman1346%、整机CPU100%，不满足同低负载比较条件，所以另做完整确认轮，不删异常值/不挑场景。有效baseline/第一轮/确认轮CPU中位18/16/15%，最大45/42/53%，无chdman高占用观测；总275条负载仅1条100%。结论是两个低负载完整矩阵闭合预算，不是三轮全绿或任意系统负载下保证。父代理独立 review 已 APPROVE，复核102场景/10200样本及本地源码hash一致；污染轮FAIL仍原样保留。

其余checkbox按正式spec逐项证据核对见report-idle-final05：真实archive v4→02v5→final/失败重试、完整HTTP有序遍历、协议/筛选/权限、真实浏览器及前后端门禁均有functional05/前轮证据；fast_scan真实200000CBZ完整维护594.772s，新增回填11.842s/重试28.137s=0.01991x/0.04731x。默认metadata_scan中断、有头人工/浏览器在途身份切换未执行及真实用户漏搜样本仍缺均明确记录，按规格的条件性/限制记录要求勾选，不冒称已执行或已复现用户漏搜，不强加额外扫描档位门禁。无新快照/截断/改语义。全部未提交，无PR/删树，无本票遗留后台进程。

以下Answer为历史归档，旧claimed/阻塞不覆盖本节。

### finalperf05 最终性能续办（历史；当时claimed）

新证据：[report-finalperf05.md](../acceptance/report-finalperf05.md)、[全部预算数字](../acceptance/measurements-finalperf05.md)。仅系列SQL改为单次索引候选流选择完整最佳字段；书完整候选+有界堆不变，无快照/截断/限候选。原母库/固定旧archive二进制不换，串行同期旧版完整矩阵及当前新实现两轮完整矩阵均10预热+100次（短长稀疏广泛及中/近末页）。第一轮首批及海贼series近末页2.3013x失败；第二轮相对同期旧版和深页通过，但海series62.735ms/原24.1022ms=2.6029x仍FAIL，不能挑一轮通过。持续720条负载采样639条CPU100%，外部压缩/训练显著；保留全部原始失败、源码hash/补丁，不擅调预算、不停外部任务。当前全量后端首次无关rename失败保留，串行完整重跑PASS；gofmt空、lint0 issues。无前端变动，引用functional05同源前端与生成门禁。

正式回填/完整维护比较在已完成fast_scan配置下0.01991x/0.04731x通过；默认metadata_scan中断保留为配置限制，不将“必须所有档位完成”加成正式门禁。当前阻塞仍是不可控负载下性能未稳定闭合；独立review由父代理后续执行。有头人工视觉、浏览器在途身份切换未执行，真实漏搜样本仍缺。全部未提交，完整状态见finalperf05-status.txt，无遗留本票后台进程，不resolved。

### functional05 功能续办（前轮；claimed）

新证据入口 [report-functional05.md](../acceptance/report-functional05.md)，保留全部 perf05/此前失败记录。本轮真实维护 HTTP 红绿修复 `ReindexLibraries` 吞错，ctx 取消优先语义保留；假路径索引修复夹具改为明确要求维护 failed，不冒称 completed。独立真实 archive 中间提交 `765728ee`（v5），最终 archive + 实际未提交生产 overlay（v6），完成真实 v4→v5→v6、故障后重试和重复迁移，派生回填精确区间直接升级11.8418256s、v5重试book28.1365517s，区别于总迁移。两条升级路径的 admin/regular × 四查询 × all/series/book/title 完整游标 oracle、资料库范围/排序/status、OPDS/Mihon 四页/总数、角色游标/鉴权通过。广泛实际规模 series19980/book199980 完整有序 HTTP 遍历通过（2356.00s），all219960也完整通过（2555.40s），3150条混排独立全量 oracle 也通过。

真实 Edge en-US、3000字符标题边界截图、普通读者系列/阅读导航及同浏览器身份切换通过；有头人工视觉审阅与浏览器在途身份切换未执行。200000有效CBZ/20000真实目录已完整物化；默认metadata_scan约32分钟后主动中断（绝不算通过）。明确配置fast_scan的完整真实文件维护594.7720135s通过，全部20000系列/200000书ID、库、精确路径和书系列归属独立审计通过；额外回填/此配置维护比为0.01991x（v4）/0.04731x（v5重试），不冒充默认metadata档位闭合。前端119文件1143测试、build/lint、sqlc/tsgen无漂移通过；早先多轮无关Windows relocation rename失败与单独重跑均保留，最终维护结束后 `go test ./... -p 1 -count=1 -timeout 20m` 全量PASS，最终lint0 issues、gofmt空。详见新报告。仍缺真实漏搜样本，仍须可控负载性能复验及默认metadata完整维护证据，不降低预算，不勾整票完成。

### perf05续办（前轮）

只优化性能，维护错误传播/浏览器项目留下一fresh代理。`series_search.go`采用互斥最佳字段/来源，`search_rank.go`在数据库module以内存O(N)堆计算完整书候选后继，未截断字段或集合、未新增快照。查询计划与profile前后均保存，新增真实HTTP来源/字段/游标/批量回归。

复用原母库、原baseline，另保存旧版同期全场景重测。新代码完整运行两轮，每场景10预热+100请求且包含全部首/中/近末页：第一轮海series首批失败；第二轮所有相对项通过，海series48.191/原旧24.1022=1.9994、星book13.6272/旧9.9493=1.3697、海贼book中页517.754/首批540.611=0.9577，最大深页比1.5839。外部训练/压缩任务占用10–13核，原旧版也显著抖动；海series仅余0.0134ms，不宣称稳定闭合。全部旧失败报告未覆盖。证据入口[report-perf05.md](../acceptance/report-perf05.md)、[measurements-perf05.md](../acceptance/measurements-perf05.md)，源码hash/补丁为perf05-source。

相关搜索测试PASS、gofmt空、lint0 issues；更广api包级在无关relocation rename Access is denied处FAIL，单独3次重试PASS，未删日志。当前代码全量后端/前端/浏览器未重跑。维持claimed，复验环境、升级维护归因/连续升级/错误传播/完整HTTP遍历/浏览器矩阵等继续未勾选。

### 前轮归档（下述最终均指candidate-final）

**未最终验收，不标 resolved。** 所有改动未提交。完整证据、逐场景 p50/p95/预算表、原始100次样本、硬件/SQLite设置、母库和被测源码哈希、可复验精确命令及限制见 [acceptance/README.md](../acceptance/README.md)。

- 固定旧版 `86c510d6501c0525c2e66617718522699176ff30` 独立 archive 到 repo 外；不改基线，Go overlay 加入共享驱动。真实旧版生成 200000 books / 20000 series、固定事实/时间/路径母库；旧/新先核验完整同语义 oracle，再每场景预热10次+100次真实认证HTTP采样。没有拿不同命中集合比较。
- 性能红灯后优化 `series_search.go`：按身份先选择最佳独立字段，避免字段扩展/window排序，先 seek/limit 后水合；添加 HTTP 字段数值选择回归。最终全矩阵重新测量而非跳过失败。当前仍失败：`海` series首批 p95 72.013ms /旧24.102ms=2.988x；`星` book首批23.212ms /旧9.949ms=2.333x；`海贼` book中页2220.027ms /首批910.482ms=2.438x。近末页也实际采样，详见表；未擅改两倍预算。
- 最终真实v4→v6迁移总耗时14.0984s；搜索派生数据重建44.9469s。没有把总迁移冒称精确新增回填、也没有把搜索重建冒称全文件维护。中间真实版本连续升级与全规模真实归档维护/归因仍未完成。
- 新增真实CBZ+PNG、HTTP启动并等待异步维护完成、强制扫描、全局/资料库oracle及数量保持回归，重复两次通过。发现现有 `ReindexLibraries` 单库扫描错误只记录日志、任务仍可能completed，尚未修复；旧假路径夹具不能证明完整维护健康。
- 真正运行本机 Playwright+Edge157、重建UI和真实SQLite HTTP服务，网络seam注入慢/失败/游标失效；追加/重试/切范围/末页/连按/键盘/关闭重开等通过，截图与JSON已保存。英文/长标题视觉/有头检查/普通读者浏览器导航尚未执行。没有实际漏搜样本，未宣称根因复现。
- 最终验证：`go test ./... -p 2 -count=1 -timeout 20m` PASS；`gofmt -l ./cmd ./internal` 空；golangci-lint2.15独立cache `0 issues.`；真实node跑 `vitest run --pool=forks --maxWorkers=4` 119文件/1143测试PASS，tsc+vite build、eslint PASS；PowerShell sqlc generate和go run ./cmd/tsgen退出0且哈希无漂移。初轮并发/TZ失败日志保留，不隐瞒失败重跑。

复验入口（每条 Bash 先显式 cd `C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05`）：

```text
C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -NoProfile -File scripts/search-acceptance.ps1 -Stage Prepare
C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -NoProfile -File scripts/search-acceptance.ps1 -Stage Baseline
C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -NoProfile -File scripts/search-acceptance.ps1 -Stage Candidate
C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe -NoProfile -File scripts/search-acceptance.ps1 -Stage Plans
```

Seed只在新Artifacts目录执行一次，不覆盖冻结母库。全部实际执行命令、环境变量、二进制路径、浏览器夹具启动命令见报告“精确复验命令”。阻塞项是上面的性能失败、完整真实升级/维护链与错误传播、尚未执行的浏览器矩阵；不因时间或预算失败豁免。
