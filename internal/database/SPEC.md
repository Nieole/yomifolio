---
id: mm-database
type: module-design
status: active
title: 藏书数据访问与统一迁移
parent: mm-architecture
references: [mm-adr-0003]
---

## 职责

持有藏书与用户数据的 SQLite 形状、生成查询、手写事务、搜索索引及聚合读模型；统一 `Migrate` 负责建表、存量升级与按版本门控的一次性回填。

## 公开契约

- `NewStore` 打开连接并返回组合 `Querier` 与手写操作的 `Store`；迁移由独立 `Migrate(dbPath)` 入口执行。`DB` 暴露底层连接用于装配，`Close`、`PingContext` 管连接生命周期与探测。
- `SqlStore.ExecTx` 在事务绑定的 `Queries` 上执行回调，失败或 panic 后回滚，panic 继续传播；不是所有 Store 调用自动共享事务。一般 SQL 查询归生成绑定，跨表操作由手写方法收口。
- 系列与书搜索由 `NormalizeSearchText` 集中定义：宽度折叠、NFC、无语言环境 Unicode case folding、删除 Unicode 空白和 Punctuation、再次 NFC；不以 NFKC 替代，保留符号。`series_search_text` 与 `book_search_text` 独立存储各自名称与标题，查询按归一化后的字符长度选 2-gram 或 trigram 候选，再按独立字段连续子串精确过滤；归一化为空不命中。不拼字段、不改源名称、路径或身份，不在每次查询归一化全库。书自身字段与所属系列带书使用相同语义；全局最佳字段排名投影供 `mm-catalogsearch` 混排，不修改资料库和协议排序。
- 系列和书插入、修改、删除触发器分别维护自身投影和双索引。`RebuildSeriesSearchIndex` 与 `RebuildBookSearchIndex` 各自在事务内替换投影与双索引，失败保留对应旧派生数据；整个维护任务与全库迁移并非原子操作。版本 5 升级系列，版本 6 升级书，从旧版本或版本 5 均回填书派生数据。迁移按版本显式替换触发器、回填成功才记版本；失败重试不依赖表非空判断。
- `SearchGlobalBatch` 通过一个只读事务读取书、系列的完整键后继。系列单次索引候选流按完整字段数值元组选择名称/标题中的最佳完整字段，再计算排名并seek/limit；书以自身字段命中和仅所属系列命中的互斥索引候选流避免候选UNION去重排序，在数据库module内读取候选完整归一化文本计算Unicode字符排名，以容量N的最大堆保留完整键后继。每个候选都经过排名后才可能被堆淘汰，不预先截断字段来源或命中集合、不排序全部书候选；请求结束即释放，不新增服务器搜索快照。最终身份才装载完整藏书与封面投影，水合与候选在同一读事务内；内部大limit读取按1000身份分块水合以避免SQLite绑定参数上限。seek不读取上一条记录、不用offset，不归一化全库原文。全局不COUNT。其他分页提供offset与游标方法；协议搜索保留真实总数及各自排序/分页。用户进度、账号会话、书签及统计由各自查询明确用户作用域。用户名身份按字节精确匹配，不在持久层做大小写折叠。
- `Migrate` 重放必要 DDL，昂贵回填成功后才更新 `user_version`；它不是包住所有升级步骤的全库原子事务，不应承诺中途失败完全无变更。

`GetReadingPosition` 以一致查询读取书、用户进度与位置依据，返回读态、实际胜出事实及纳秒时间；清除通过保留的依据与缺失的位置行区分于从未阅读。位置事务在同一事务内读取提交后快照，拒绝或重复时返回当前胜者；投影刷新失败与位置、依据、收据共同回滚，不另建协议专用位置真相。

## 边界与限制

默认 modernc SQLite 驱动在包初始化时注册确定性 `search_normalize(TEXT)`，覆盖迁移、运行期连接池、维护与测试连接。直接外部写入系列或书的连接必须注册与 `NormalizeSearchText` 同规则的函数；普通 SQLite CLI 不具备该函数，不能作为直接写库工具。缺函数应显式失败，不能吞错或返回空结果；工具约束见 `docs/development.md`。

不执行藏书文件遍历、归档解码、网络抓取或任务调度；持久化操作可以维护派生数据，但不接管阅读与编排用例裁决。任务表的专属 DDL 不并入 sqlc 模型；跨包迁移安装关系归架构节点。旧 `doc.go` 中任务引擎和作品群计算的 API 归属已漂移，不能据此扩张本包责任。

## 证据

固定旧版与200000本书/20000系列母库的低负载完整矩阵已通过相对性能预算：每场景10次预热、100次采样，两轮首批最大比值为1.266571x/1.033759x，深页最大比值为1.155608x/1.092555x，均不超过2x。完整oracle、HTTP遍历、实际候选/水合计划、profile、源码hash及包含污染轮失败的原始证据见 `.scratch/search-experience/acceptance/README.md`；结果不保证任意主机负载下的延迟。书候选由MATCH、主键和series_id索引驱动，无广泛候选排名临时排序；系列仍有top-N排序。真实升级回填精确归因及同规模200000真实归档fast_scan完整维护与身份审计已验证，配置及未执行限制见验收报告，不泛化为默认metadata_scan已完成。

系列索引升级与重建证据：`book_search_migration_test.go`、`series_search_migration_test.go`、`search_normalize_test.go`、`series_search_readers_test.go`；跨入口实际行为由 `internal/api/series_normalized_search_test.go` 与 `internal/api/book_rank_search_test.go` 验证。

`Store`、`SqlStore`、`ExecTx`、`Migrate`、`schemaSQL`；`store_exectx_test.go` 的 `TestExecTxAlwaysFinishesTransaction`，`migration_test.go` 的 `TestMigrateSetsSchemaVersionAndSkipsRebackfill`，以及 `users_case_test.go`、`pagination_total_order_test.go`、`store_search_dualpath_test.go`。
