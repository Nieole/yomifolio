# finalperf05：性能仍未稳定闭合，保持 claimed

## 本轮结果

唯一工作树 `C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05`。保留全部前序未提交改动、所有失败和原母库/archive；未提交、未修改索引/历史、未停止或调整任何外部任务。没有降低正式2x预算，没有新增快照、截标题、限候选、换数据或另造baseline。

当前性能实现相较perf05只改 `internal/database/series_search.go` 的系列字段选择：把两次FTS候选/投影点查的互斥分支改为一次候选流，以完整 `(精确层级, Unicode位置, 字符长度)` 元组选择最佳完整字段，再算排名、seek/top-N、限额后水合。标题不命中时直接选择名称；名称不命中且标题命中时选标题；相同键名称胜出。两字段都不命中仍过滤。SQL CASE 是按行选择完整字段，不是截断字段。书的完整候选+有界堆保持原样，未改回全排序。API、归一化、权限、cursor和迁移均未改变。

数值facts物化诊断 `diagnostic-finalperf05` 海series p95=49.0446ms；最终内联完整字段方案 `diagnostic-finalperf05-inline`=39.1405ms，对历史24.1022ms约1.624x，距历史2x预算约18.8%。两时段负载不同，**不是严格消融或最终性能结论**，不能用筛选诊断替代完整矩阵。原诊断样本均保留。

## 全矩阵（不是只挑成功轮）

完整数字见 [measurements-finalperf05.md](measurements-finalperf05.md)，生成器 `summarize-finalperf05.py`；全部原始纳秒在同label JSON。冻结母库仍为200000 books/20000 series，hash仍为 `96dc6cc54c52e3c8419b950db1d112ef265b290a9ae18b56e09e37de76cd15d5`。旧版仍为原固定 `86c510d6501c0525c2e66617718522699176ff30` archive及原 `baseline-api.test.exe`。所有完整运行无QUERY/TARGET筛选，所有场景10预热+100独立认证HTTP请求，短/长、稀疏/广泛、首批/中页/近末页及新宽松匹配均执行。新旧各自复制同一母库，SQLite设置与README一致。独立全集合oracle、HTTP指定页排序与稀疏全遍历均通过；广泛全量HTTP有序遍历另有functional05证据，不冒充本轮重新全量遍历。

- `baseline-finalperf05.json/log`：固定旧版同期完整矩阵，528.59s。
- `candidate-finalperf05.json/log`：当前最终实现第一轮完整矩阵，1290.74s。
- `candidate-finalperf05-repeat.json/log`：同一二进制第二轮完整矩阵，911.88s。
- 这三次重型计时串行执行，期间没有运行本票构建/测试/维护。负载采样为唯一轻量并行观察。

|关键预算项|第一轮 p95 ms|第二轮 p95 ms|对照/结论|
|---|---:|---:|---|
|海 series 首批|229.227|62.735|历史24.1022：9.5106x / 2.6029x，均FAIL；同期57.705：3.9724x FAIL / 1.0872x PASS|
|海 book 首批|1777.489|1055.410|同期631.633：2.8141x FAIL / 1.6709x PASS|
|海贼 series 首批|135.342|67.005|同期42.582：3.1784x FAIL / 1.5735x PASS|
|海贼 book 首批|2490.218|973.585|同期597.477：4.1679x FAIL / 1.6295x PASS；历史1184.9：2.1017x FAIL / 0.8217x PASS|
|海贼王 series 首批|838.470|46.454|同期99.202：8.4521x FAIL / 0.4683x PASS|
|星 book 首批|8.456|6.114|历史9.9493：0.8499x / 0.6145x，PASS|
|海贼 series 近末页/本轮首批|311.464 / 135.342|62.438 / 67.005|2.3013x FAIL / 0.9318x PASS|
|海贼 book 中页/本轮首批|2385.712 / 2490.218|1529.422 / 973.585|0.9580x / 1.5709x，PASS|

第一轮最大深页比 **2.3013x FAIL**；第二轮最大深页比 **1.5709x PASS**。第二轮全部首批相对同期固定旧版<=2，但相对原旧版海series仍FAIL。既不能偷偷用膨胀的同期旧版替换历史，也不能把第二轮通过部分当稳定结论。宽松查询不与旧版零结果比：第一/第二轮 series首批6.618/4.102ms，book首批11.891/7.111ms、近末15.290/7.457ms。

## 持续负载证据及测量限制

`monitor-finalperf05.ps1` 只读 CIM，每轮采系统总CPU和>20%进程CPU，sleep5秒（CIM自身有延迟，所以并非精确5秒周期），720条原始记录在 `load-finalperf05.jsonl`；摘要 `load-finalperf05-summary.json`。UTC **2026-11-18 16:14:37—17:27:03**，覆盖诊断、旧版和两轮新实现以及后端检查大部分阶段。CPU min92%、median100%、max100%，**639/720**条=100%。外部chdman观测702项，中位1048.5%、最大1429%（100%=一逻辑核）；python各实例观测930项，中位184.5%、最大1153%。这些是记录中进程观测的统计，不是把多个实例中位数当整机总和。外部训练/压缩可交替占用约10–14逻辑核，未干预。采样器自然完成，无遗留本票后台进程；最后对PID35032只查询/尝试停止本票采样器，已不存在，命令exit1不代表性能或产品失败。

相同生产二进制两轮海series首批p95 **229.227→62.735ms（3.654x）**，海贼王series **838.470→46.454ms（18.049x）**；稀疏项同样抖动。固定旧版海series同期57.705ms比历史24.1022ms **2.394x**，说明基线也受时段影响。原始样本无删异常值/裁尾；这些是负载与波动证据，**不是统计置信区间，也不能定量证明全部慢化由外部负载造成**。当前无法控制主机负载，继续盲跑挑一轮通过没有验收价值，故停止扩大测量，不宣称稳定或预算闭合。

## 当前门禁

- `related-finalperf05.log`：搜索/排名/游标/gram的api/database/catalogsearch相关测试PASS；已有最佳字段、Unicode、长标题、不同limit、来源去重回归均覆盖本次优化。
- `plan-finalperf05.log`：实际生产语句短/长首批/seek EXPLAIN PASS；系列单FTS候选流+投影主键点查，有限top-N排序，书原MATCH/主键/series_id索引候选及限额后水合保持不变。无每请求全藏书归一化。
- 当前全量 `backend-finalperf05.log` 首次FAIL（exit1），仅三个无关relocation测试Windows rename Access is denied，未改其断言/生产代码、未删除失败。
- 随后串行完整重跑 `go test ./... -p 1 -count=1 -timeout 20m`，`backend-finalperf05-retry.log/.exit` **全量PASS exit0**。
- `gofmt-finalperf05.log` **0 bytes**；`lint-finalperf05.log/.exit` **0 issues / exit0**。
- 无前端生产/测试改动，引用上一代理当前同一前端源的 `functional05-frontend-tests.log` **119文件/1143测试PASS** 及 `functional05-frontend-build-lint.log` build/lint exit0，不冒称本轮重跑。无SQL源/schema/契约变动，引用functional05的sqlc/tsgen无漂移；手写SQL无生成绑定。
- 本轮未重新跑浏览器或200000文件维护；不新增“默认metadata_scan必须完成”的自设门禁。正式回填/完整维护预算在已实测fast_scan配置下为11.8418256/594.7720135=**0.01991x**、失败重试28.1365517/594.7720135=**0.04731x**，均<2。默认metadata_scan中断不是功能失败，也不是已完成；配置限制继续保留。

## 身份、补丁与复验

`finalperf05-source.sha256`记录当前Go源码/测试驱动/脚本、固定母库、原旧版与当前二进制；`finalperf05-source.patch`包含HEAD之上所有既有及本轮tracked改动和未跟踪Go/工具文件（不把之前改动占为本轮）。`finalperf05-status.txt`为完整未提交状态。本轮仅改系列SQL、对应module/CHANGELOG状态描述及新增证据，其他全部前序改动保留。

编译及完整重测（**将NEW替换成未存在label，不覆盖本轮或任何旧失败**）：

```bash
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
export TEMP=C:/tmp TMP=C:/tmp TMPDIR=C:/tmp GOTMPDIR=C:/tmp GOCACHE=C:/Users/saber/AppData/Local/go-build
C:/Users/saber/scoop/apps/go/current/bin/go.exe test -c -o C:/Users/saber/.thinkrail/search-experience-research/acceptance-05/candidate-NEW.test.exe ./internal/api
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
SEARCH_ACCEPTANCE_INPUT=C:/Users/saber/.thinkrail/search-experience-research/acceptance-05/frozen-v4.db SEARCH_ACCEPTANCE_OUTPUT=C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05/.scratch/search-experience/acceptance/baseline-NEW.json SEARCH_ACCEPTANCE_LABEL=baseline C:/Users/saber/.thinkrail/search-experience-research/acceptance-05/baseline-api.test.exe -test.run '^TestSearchAcceptanceMeasure$' -test.v -test.timeout 90m
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
SEARCH_ACCEPTANCE_INPUT=C:/Users/saber/.thinkrail/search-experience-research/acceptance-05/frozen-v4.db SEARCH_ACCEPTANCE_OUTPUT=C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05/.scratch/search-experience/acceptance/candidate-NEW.json SEARCH_ACCEPTANCE_LABEL=NEW C:/Users/saber/.thinkrail/search-experience-research/acceptance-05/candidate-NEW.test.exe -test.run '^TestSearchAcceptanceMeasure$' -test.v -test.timeout 90m
```

最终矩阵不能设置QUERY/TARGET；诊断才允许筛选。所有环境、原archive制备和母库来源仍见README，不需Prepare/Seed。采样脚本输出同样须另取新label。

## 仍开放

1. **阻塞：正式性能预算尚未稳定闭合**。需要外部负载自然降低/可控时再次同机同设置完整旧/新重复测量，不能擅调预算或停外部任务。
2. 独立review由父代理合入后执行，本票保持claimed，不能resolved。
3. 有头人工视觉、真实浏览器在途身份切换未执行；真实用户漏搜样本仍缺，根因未复现。已有自动化/真实Edge功能范围见functional05，不冒充上述项目。
4. 默认metadata_scan未完成是已记录配置限制，不新增所有扫描档位必须满足预算的门禁；本轮不再无限扩大维护范围。
