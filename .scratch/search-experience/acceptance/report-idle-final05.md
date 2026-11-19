# idle-final05：空闲窗口正式性能验收通过（自动化范围）

## 结论与失败处理

本轮**没有修改任何生产代码、测试或生成契约**。在同机低负载窗口串行完成固定旧 archive 全矩阵及当前 candidate 三轮完整矩阵。第一轮及最后确认轮全部正式性能预算通过；中间复测仍有一个失败，原样保留，不宣称三轮全绿。该轮结束前外部 chdman 再次启动，整机 CPU 达100%、进程达1346%（100%=一逻辑核），不满足同低负载比较条件；随后另做完整确认轮，而非只重跑失败场景或删异常值。低负载完整两轮均无 chdman 高占用记录，所有首批、深页含近末页<=2x。正式可自动化验收据此闭合，工单标 resolved；父代理独立 review 尚未执行，非已合入/已发布声明。

**中间失败不是被“修复”或抹去**：`海贼王/book/near_end` p95=609.9937ms / 同轮首批270.2347ms = **2.25727377x FAIL**。该时段有明确外部负载突发，不能用它宣称空闲下稳定失败，也不能证明每一纳秒慢化的因果。若 reviewer 不接受该负载污染轮与有效低负载轮分开判读，应恢复 claimed；不能把失败改记 PASS。规格不承诺主机任意负载下的绝对延迟。

## 身份与测量契约

- 唯一工作树 `C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05`，未 commit/add/reset/clean/stash，全部前序未提交改动完整保留。未停止或调整任何外部任务；只停止了本票自己的两个只读负载采样器（PID43480、30148）。
- 原固定 baseline：`86c510d6501c0525c2e66617718522699176ff30` 独立 archive，仍使用原 `baseline-api.test.exe`，SHA256 `7c4b7c3d59da9e87fb306800acefb370e41a26d65db1d40f58dde2c4a4194cef`。
- 当前原二进制 `candidate-finalperf05.test.exe`，SHA256 `0b2d0d2bf4b29a19e064d58d08c3f3f5e56b84b2534382c6d78c6317105d1d97`。`idle-final05-source-verification.json`逐个重算前轮清单**772个文件全部一致**，含全部Go源码/测试/驱动/工具、母库和两二进制，未重编译混入别的代码。
- 原200000 books / 20000 series冻结母库，SHA256仍为 `96dc6cc54c52e3c8419b950db1d112ef265b290a9ae18b56e09e37de76cd15d5`，未重建或改数据。名称、固定ID/时间/路径及分布见README；广泛19980系列/199980书，稀疏20系列/380书。
- Win11/Ryzen7 8845HS/16逻辑核/约31.33GiB/NVMe，同原硬件与平衡电源计划；Go1.27.4、SQLite3.54.1；WAL/NORMAL、page4096、mmap268435456、cache_size=-128000、temp_store=2；每侧从母库独立复制，连接热缓存，不清OS缓存。
- 全部运行无 QUERY/TARGET 筛选，真实认证HTTP，limit20，同target同语义；每场景10次预热+100次独立请求，保存原始纳秒，最近秩p50/p95。基线12场景，candidate每轮30场景；短/双字/长词、稀疏/广泛、series/book、首/中/近末页及宽松匹配都执行。低命中系列只有一页，不虚构深页。
- 各轮独立完整集合oracle、HTTP首/指定深页完整数值顺序、稀疏全遍历均通过；中/近末页使用服务端真实next_cursor直接定位，不累计拉前页。广泛全量HTTP遍历引用functional05，不冒称本轮重新执行。
- 当前实现仍为系列单索引候选流选择完整最佳字段、书完整候选有界堆；未截字段/候选/排序键，未改归一化、身份、cursor，无新snapshot。

## 精确预算结果

全部逐场景p50/p95、历史参考与同期比较见 [measurements-idle-final05.md](measurements-idle-final05.md)，生成器 `summarize-idle-final05.py`。本轮使用**相同固定旧archive在当前低负载下的重测**作有效比较，不把历史受不同负载影响的数字当同期基线；历史 `baseline.json` 以及所有旧失败完整保留、同时列参考比值。

|正式预算|第一轮低负载|最后确认轮低负载|要求|
|---|---:|---:|---:|
|首批/同期固定旧版，全部同语义场景最大比值|1.266571x|1.033759x|<=2x|
|中/近末页/同轮首批，全部场景最大比值|1.155608x|1.092555x|<=2x|
|海 series首批 / 同期旧34.6081ms|36.6135ms / 1.0579x|19.4478ms / 0.5619x|<=69.2162ms|
|星 book首批 / 同期旧4.5531ms|3.0866ms / 0.6779x|3.5182ms / 0.7727x|<=9.1062ms|
|海贼 book中页 / 同轮首批|378.9111 / 371.8174ms = 1.0191x|373.8724 / 393.1657ms = 0.9509x|<=2x|
|额外回填 / 同规模完整真实文件维护（前轮同源证据）|11.8418256 / 594.7720135s = 0.01991x|失败重试28.1365517 / 594.7720135s = 0.04731x|<=2x|

表中细节以raw JSON计算值为准，显示位数有舍入。第一轮最紧首批仍有约36.7%预算余量，最后确认轮约48.3%；不再是此前0.0134ms余量。历史海series24.1022ms作为参考：本轮36.6135ms=1.5191x，确认19.4478ms=0.8069x，也通过；不能据此抹去历史失败。

宽松查询不拿旧版零结果比：第一轮series/book首批p95=2.523/3.999ms，book近末4.031ms；确认轮2.519/3.995ms、近末4.016ms，独立报告。原始JSON同时保留迁移/搜索重建诊断耗时，不拿这些替代正式额外回填/完整维护归因。

## 持续负载与全部运行

收到“资源释放”后立即读负载，09:27:27本地首采CPU22%，下一条12%，空闲窗口确认后立即开始旧版。全程只有轻量CIM采样与测量串行主任务；没有并行构建/测试/维护。约6秒一次（CIM开销使其不是严格5秒），共275条，UTC01:27:27—01:56:11，整机CPU min1/median16/p9536/max100%，仅1条100%，最低可用内存5321212KiB。采样器自身有开销，未测温度，不宣称严格恒定/隔离主机。

|运行|UTC起止（由日志结束mtime减测试duration，近似）|耗时s|CPU median/p95/max|结论|
|---|---|---:|---|---|
|baseline-idle-final05|01:27:34—01:33:45|370.95|18/38/45%|原固定archive，12场景PASS|
|candidate-idle-final05|01:33:57—01:41:24|446.84|16/33/42%|30场景，所有预算PASS|
|candidate-idle-final05-repeat|01:41:40—01:47:27|347.20|18/38/100%|30场景；近末页一项预算FAIL，保留|
|candidate-idle-final05-confirm|01:48:51—01:55:55|424.44|15/34/53%|30场景，所有预算PASS|

四次驱动行为断言均PASS/exit0，不等于中间轮性能PASS，预算由独立汇总计算。中间轮01:46:53—01:47:05观测chdman100/1346/1145%，CPU38/54/100%；两轮有效低负载candidate和baseline没有此类chdman高占用观测。证据 `load-idle-final05{,-confirm}.jsonl`、`load-idle-final05-windows.json`、`summary-idle-final05.json`全部保留，不删高负载采样、不裁尾、不换baseline、不降低2x。原始样本不是置信区间，不能外推所有硬件/后台任务。

## 正式spec与工单逐项核对

1. **真实旧版升级/重试/写入/维护后HTTP oracle**：functional05已完成v4→final、真实独立archive v4→02v5→final、故障重试/重复迁移、增删改/索引缺项检测；完整series19980/book199980/all219960有序HTTP遍历通过。
2. **全部用户行为与协议/权限兼容**：真实HTTP+SQLite与现有前端session/modal两seam，OPDS/Mihon真实总数/分页、资料库筛选/排序、权限、空/无效输入/cursor及有界长标题cursor回归已通过，详见functional05及前轮行为门禁。
3. **真实交互**：Edge157、zh-CN/en-US、慢/失败/重试/切范围/关闭重开/键盘/连按/长标题/无效cursor及普通读者导航已执行；组合竞争/晚到/取消无效/身份切换前端自动化通过。有头人工视觉及真实浏览器在途身份切换未执行，按规格明确限制，不新增强制门禁。
4. **真实漏搜样本**：仍缺，真实根因未复现。该项按正式条件性要求记录缺失，不宣称已复现用户漏搜，不取消已定义验收。
5. **规模/设置/采样**：原冻结200000/20000母库、相同SQLite设置、短长/高低命中、全部10+100样本与硬件/缓存条件齐备。
6. **首批/深页性能**：上述两个低负载完整矩阵全部<=2x；中间受外部突发负载污染轮一项FAIL完整保留并明确，未挑场景。历史baseline不抹去。
7. **回填/维护/索引**：同源functional05真实200000CBZ fast_scan完整维护594.772s、身份/路径/归属独立审计通过，新增派生回填11.842s/重试28.137s均<2x。默认metadata_scan曾中断，仍是配置限制，不冒称完成；正式spec没有额外要求所有扫描档位都跑。finalperf05实际生产EXPLAIN证明索引候选、主键点查和series_id索引，无全库请求归一化。
8. **工程门禁**：本轮无代码变化；772源文件hash逐项相同，引用finalperf05同源后端完整串行重跑PASS、gofmt空、lint0 issues。最初无关Windows relocation rename失败仍留档。前端未变，引用functional05 119文件/1143测试PASS/build/lint、sqlc/tsgen无漂移；不冒称本轮重跑。另存369个前端tracked文件hash，`idle-final05-frontend-diff.patch`为空；functional05源清单12项仅此前finalperf05已变更的series SQL不一致，其余一致，见`idle-final05-functional-source-verification.json`。
9. **集成修复/证据边界**：前轮维护吞错修复真实HTTP红绿已通过；本轮先测后判定，无需再优化，不引入额外范围。失败、未执行与通过明确区分，游标不承诺整次快照。
10. **持久规格/变更记录/连接限制**：此前database/catalogsearch/application所属SPEC、CHANGELOG、外部SQLite连接函数限制已同步。本轮未改变边界/行为/架构，不需要改正式预算或持久规格。

## 证据与复验

新label不覆盖任何前序文件：`baseline-idle-final05`、`candidate-idle-final05{,-repeat,-confirm}`各有JSON/log/exit；`idle-final05-source.sha256`、`idle-final05-source-verification.json`、`idle-final05-source.patch`、`idle-final05-status.txt`；采样和汇总脚本同目录。`verify-idle-final05.log`核对102场景/10200计时样本、4次exit0、两个有效轮零预算失败及源/证据hash，PASS；首次辅助核对脚本因Windows默认GBK读UTF-8 JSON失败，显式UTF-8重跑通过，未涉及产品代码或门禁。详细环境及原archive来源见README。复验沿用report-finalperf05命令，NEW换成不存在label；INPUT原frozen-v4.db，旧版LABEL必须baseline，candidate使用新label，禁用QUERY/TARGET，`-test.run '^TestSearchAcceptanceMeasure$' -test.v -test.timeout 90m`。所有Bash显式cd唯一树，读写绝对路径。

仍开放但非本轮正式自动化阻塞：父代理独立review；有头人工/浏览器在途身份切换；默认metadata_scan未完成；真实用户漏搜样本缺失。没有PR/提交/删树，没有遗留本票后台进程。
