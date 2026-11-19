# perf05 优化复验：本次完整采样相对项通过，整票仍 claimed

## 生产变更与依据

`series_search.go`：系列的名称/标题分成互斥分支，以完整字段数值元组排除较差字段后 seek/top-N；等分由名称分支拥有。没有先限额来源再去重，没有把次佳字段放到旧游标之后重复返回。书的自身来源与所属系列来源改为精确匹配后互斥的 UNION ALL：仅自身两个字段均不匹配时才从所属系列带书，消除大候选 UNION 去重排序和对所有书重复装载所属系列全文。

`search_rank.go`：数据库 module 内对书的索引候选流读取完整归一化文本，按 Unicode 字符计算完整数值键；在无服务器状态的请求内以容量 N 的最大堆保留最好的 N 个完整键后继。所有候选都经过排名后才能被堆淘汰，内存不保留全命中集合/跨请求快照；keyset 不依赖边界身份仍存在，不执行 offset。之后只按最终 ID 水合藏书。HTTP 的 N 至多 101；旧内部接口的完整 oracle 读取仍支持大 limit，水合按1000身份分块避免 SQLite 绑定参数上限。全局 batch 仍在原有同一只读事务内完成候选与水合。

未增加迁移、派生表、查询快照或新归一化函数。没有每请求对全藏书原文归一化，没有截断字段/标题/命中集合。索引候选广泛时仍需线性处理，但不对所有书候选做 SQLite 排名临时排序。

## 红灯、尝试与 profile

性能红灯沿用 README 和 candidate-final 的真实失败样本；没有伪造行为失败。`profile-before.cpu/-top.txt/.json/.log` 使用原候选二进制，限定海贼/book诊断：663.55s，SQLite VM累计616.25s、排序器整数比较9.28s，instr累计28.73s。原计划含候选 UNION 排序、宽文本 fields物化以及排名排序。

尝试了固定宽度 rank 标量函数、空标题快路径、字段物化、Go候选堆和字段互斥分支；诊断 JSON/log 都保留，不以筛选场景代替最终矩阵。最终方案以计划消除书的广泛候选去重排序/排名物化；`plan-perf05.log` 对实际候选与水合语句 EXPLAIN：MATCH + 投影主键点查，继承经 idx_books_series_id；书候选无 ORDER BY/UNION 去重临时树，水合只排序限额后的常量行。系列仍需两路候选 top-N 排序，不声称消除了所有排序。

相同海贼/book诊断 `profile-perf05.cpu/-top.txt/.json/.log`：250.05s，SQLite VM累计139.01s。profile含迁移、oracle、水合和重建，且两时段负载不同，因此这些累计数不是单请求严格消融归因。优化后的热点转为候选读取/Rows转换；没有把 profile 有开销的结果作为最终预算采样。

## 全矩阵与并排旧版

- 母库和 baseline源码/二进制均复用 README 所述固定版本，没有换数据或另建基线。`baseline-perf05.json/log` 是原 baseline 二进制的同期全场景重测，不覆盖 `baseline.json`。
- 最终代码完整跑两轮：`candidate-perf05.json/log` 632.53s，`candidate-perf05-recheck.json/log` 567.72s。每轮每场景10预热+100请求，保留每个纳秒原始样本。两轮之间没有修改生产代码、驱动、数据或预算。
- 第二轮全部首批相对原 baseline、同期 baseline 均 <=2；所有中页/近末页相对本轮对应首批均 <=2。全表见 `measurements-perf05.md`，不是只报告三个旧失败场景。
- 三个旧失败项：海/series首批48.1910ms /原24.1022ms=1.9994；星/book首批13.6272ms /原9.9493ms=1.3697；海贼/book中页517.754ms /本轮首批540.611ms=0.9577。最大深页比1.5839。
- 第一轮海/series首批134.088ms仍失败，原 candidate-final 的失败也完整保留。不能删掉这轮后称所有重复运行稳定通过。

环境限制有实际证据：诊断时系统CPU100%，外部训练 python PID39108约990% CPU，子进程约93%，chdman约79%；Win32_Process确认训练命令在仓库外 `benchmark/geoai-platform/pilot/train.py --threads 12 --workers 4`。之后 `perf05-environment-after.txt` 实际保存了 chdman PID13816约1270% CPU、python仍运行的采样。未停止、降优先级、改亲和性或修改其他任务。固定旧版海/series诊断自身也从历史24.102ms升至62.854ms，见 baseline-environment-check，说明不同阶段显著受外部负载污染。同期原旧版全矩阵也已保存，不把历史与同期数据偷偷替换。

**海/series对原基线预算仅余0.0134ms；不能把第二轮采样通过当作有稳健余量的最终验收。需要在外部训练/压缩负载可控时复验，不能自行终止外部进程。** 整票保持claimed，后续维护/浏览器/升级链等项目仍按README未完成。

## 行为与检查

沿用驱动独立字面子串的完整数据库集合 oracle，以及真实监听HTTP首批/中页/近末页顺序验证；稀疏集合完整HTTP遍历。没有声称广泛集合已经完整逐页HTTP遍历。

补充 `TestSearchBestFieldDisjointSourcesHTTP`：实际SQLite+认证HTTP，覆盖自身/继承来源、同一身份多个匹配字段、游标后次佳字段不能重复、Unicode位置、不同limit（1/2/3/7/100）、堆替换及全程有序去重。已有长标题/长关键词、权限/身份、keyset、归一化、迁移和协议搜索相关测试一起通过。新增行为回归在旧代码可能同样通过；它们保护优化，没有冒称新的功能红灯。

- `go test ./internal/api ./internal/database ./internal/catalogsearch -run 'Search|Rank|Cursor|Gram' -count=1 -timeout 10m` PASS，`backend-perf05-related.log`。
- 更广包级执行 `go test ./internal/api ./internal/database ./internal/catalogsearch -p 2 -count=1 -timeout 15m`：database PASS；api 在无关 relocation 测试 before_rename 遇到Windows目录rename Access is denied而FAIL，日志 `backend-perf05-packages.log`保留。该单测单独连续3次PASS（backend-perf05-relocation-retry.log），不能据此声称本次包级或全量已通过；未修改无关代码。
- gofmt全范围输出为空（gofmt-perf05.log）；golangci-lint2.15独立cache `0 issues.`（backend-lint-perf05.log）。
- 本轮未重跑全量后端/前端/浏览器。旧全量PASS只适用于旧候选，不冒称最终代码全量通过；未修改SQL源/schema/生成契约，无需再生成。

## 源码与复验

两轮完整采样及优化后profile均使用 `perf05-source.sha256` / `perf05-source.patch` 的生产代码、共享驱动和二进制。补丁基于当前HEAD，额外含未跟踪search_rank.go；全部未提交。后续只补计划测试与报告，没有修改被测生产源码。

复验不需要Prepare或Seed；复用 `C:/Users/saber/.thinkrail/search-experience-research/acceptance-05`，详见README原环境设置。每个Bash命令显式cd本树，环境设置相同：

```bash
cd C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05
export TEMP=C:/tmp TMP=C:/tmp TMPDIR=C:/tmp GOTMPDIR=C:/tmp GOCACHE=C:/Users/saber/AppData/Local/go-build
SEARCH_ACCEPTANCE_INPUT=C:/Users/saber/.thinkrail/search-experience-research/acceptance-05/frozen-v4.db SEARCH_ACCEPTANCE_OUTPUT=C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05/.scratch/search-experience/acceptance/<NEW-LABEL>.json SEARCH_ACCEPTANCE_LABEL=<NEW-LABEL> C:/Users/saber/.thinkrail/search-experience-research/acceptance-05/candidate-perf05.test.exe -test.run '^TestSearchAcceptanceMeasure$' -test.v -test.timeout 90m
```

旧版使用同一环境、`baseline-api.test.exe`，LABEL必须是 `baseline`（共享驱动以此识别旧契约），OUTPUT另取新文件名。不要覆盖任何现有结果。诊断才设置QUERY/TARGET；两轮完整复验均未设置筛选。
