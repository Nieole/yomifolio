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
- 系列搜索由 `NormalizeSearchText` 集中定义：宽度折叠、NFC、无语言环境 Unicode case folding、删除 Unicode 空白和 Punctuation、再次 NFC；不以 NFKC 替代，保留符号。`series_search_text` 独立存储名称与标题，查询按归一化后的字符长度选 2-gram 或 trigram 候选，再按独立字段连续子串精确过滤；归一化为空不命中。不拼字段、不改源名称、路径或身份，不在每次查询归一化全库。书自身索引保留原有语义，所属系列带书共享系列匹配。
- 系列插入、修改、删除触发器同时维护投影和索引。`RebuildSearchIndexes` 的系列重建在事务内替换投影与双索引，失败保留完整系列旧索引；不意味着书重建或整个维护任务原子。迁移按版本显式替换触发器、回填成功才记版本；失败重试不依赖表非空判断。
- 分页提供 offset 与游标方法；协议搜索保留真实总数及各自排序/分页。用户进度、账号会话、书签及统计由各自查询明确用户作用域。用户名身份按字节精确匹配，不在持久层做大小写折叠。
- `Migrate` 重放必要 DDL，昂贵回填成功后才更新 `user_version`；它不是包住所有升级步骤的全库原子事务，不应承诺中途失败完全无变更。

`GetReadingPosition` 以一致查询读取书、用户进度与位置依据，返回读态、实际胜出事实及纳秒时间；清除通过保留的依据与缺失的位置行区分于从未阅读。位置事务在同一事务内读取提交后快照，拒绝或重复时返回当前胜者；投影刷新失败与位置、依据、收据共同回滚，不另建协议专用位置真相。

## 边界与限制

默认 modernc SQLite 驱动在包初始化时注册确定性 `search_normalize(TEXT)`，覆盖迁移、运行期连接池、维护与测试连接。直接外部写入系列的连接必须注册与 `NormalizeSearchText` 同规则的函数；普通 SQLite CLI 不具备该函数，不能作为直接写库工具。缺函数应显式失败，不能吞错或返回空结果；工具约束见 `docs/development.md`。

不执行藏书文件遍历、归档解码、网络抓取或任务调度；持久化操作可以维护派生数据，但不接管阅读与编排用例裁决。任务表的专属 DDL 不并入 sqlc 模型；跨包迁移安装关系归架构节点。旧 `doc.go` 中任务引擎和作品群计算的 API 归属已漂移，不能据此扩张本包责任。

## 证据

系列索引升级与重建证据：`series_search_migration_test.go`、`search_normalize_test.go`、`series_search_readers_test.go`；跨入口实际行为由 `internal/api/series_normalized_search_test.go` 验证。

`Store`、`SqlStore`、`ExecTx`、`Migrate`、`schemaSQL`；`store_exectx_test.go` 的 `TestExecTxAlwaysFinishesTransaction`，`migration_test.go` 的 `TestMigrateSetsSchemaVersionAndSkipsRebackfill`，以及 `users_case_test.go`、`pagination_total_order_test.go`、`store_search_dualpath_test.go`。
