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
- `SearchGlobalBatch` 对完整命中候选逐一计算最佳字段排名，按包含排名、类型与身份的完整键读取严格后继；不预先截断字段来源或命中集合，不全量排序书候选，不靠上一条记录或 offset 定位。候选选择与最终藏书、封面投影水合处于同一只读事务，保证本批一致；水合须控制绑定参数数量以适应 SQLite 限制，查询值使用参数绑定。请求结束即释放事务，不新增跨请求的服务器搜索快照，不在查询时归一化全库原文，也不为全局搜索 COUNT。
- 其他分页提供 offset 与游标方法；协议搜索保留真实总数及各自排序/分页。用户进度、账号会话、书签及统计由各自查询明确用户作用域。用户名身份按字节精确匹配，不在持久层做大小写折叠。
- `Migrate` 重放必要 DDL，昂贵回填成功后才更新 `user_version`；它不是包住所有升级步骤的全库原子事务，不应承诺中途失败完全无变更。

`GetReadingPosition` 以一致查询读取书、用户进度与位置依据，返回读态、实际胜出事实及纳秒时间；清除通过保留的依据与缺失的位置行区分于从未阅读。位置事务在同一事务内读取提交后快照，拒绝或重复时返回当前胜者；投影刷新失败与位置、依据、收据共同回滚，不另建协议专用位置真相。

## 边界与限制

默认 modernc SQLite 驱动在包初始化时注册确定性 `search_normalize(TEXT)`，覆盖迁移、运行期连接池、维护与测试连接。直接外部写入系列或书的连接必须注册与 `NormalizeSearchText` 同规则的函数；普通 SQLite CLI 不具备该函数，不能作为直接写库工具。缺函数应显式失败，不能吞错或返回空结果；工具约束见 `docs/development.md`。

不执行藏书文件遍历、归档解码、网络抓取或任务调度；持久化操作可以维护派生数据，但不接管阅读与编排用例裁决。任务表的专属 DDL 不并入 sqlc 模型；跨包迁移安装关系归架构节点。任务活动态与作品群计算不归本包，不能因保存其结果而接管规则或生命周期。

## 证据

完整候选排名 oracle、HTTP 遍历、候选与水合查询计划及性能预算验收见 `.scratch/search-experience/acceptance/README.md`，原始证据与未执行限制以该报告为准。性能验证针对报告记录的数据集、配置和负载，不保证任意主机负载下的延迟；升级回填与真实归档 fast_scan 维护、身份审计的验证不能泛化为默认 metadata_scan 已完成。

系列索引升级与重建证据：`book_search_migration_test.go`、`series_search_migration_test.go`、`search_normalize_test.go`、`series_search_readers_test.go`；跨入口实际行为由 `internal/api/series_normalized_search_test.go` 与 `internal/api/book_rank_search_test.go` 验证。

`Store`、`SqlStore`、`ExecTx`、`Migrate`、`schemaSQL`；`store_exectx_test.go` 的 `TestExecTxAlwaysFinishesTransaction`，`migration_test.go` 的 `TestMigrateSetsSchemaVersionAndSkipsRebackfill`，以及 `users_case_test.go`、`pagination_total_order_test.go`、`store_search_dualpath_test.go`。

## 委托凭据持久化与密码事务

`schema_handwritten.sql` 的 mcp_credentials 由统一迁移安装，外键绑定真实 users 并在删除账户时级联清除；凭据授权裁决归 `mm-application`。它不进入 sqlc 的模型输入，SQL 与 JSON 列只保存摘要和授权限制，不保存原始 Bearer 密钥。

`accountPasswordWrite.RevokeSessions` 在同一个 `WithAccountPassword` 事务中删除浏览器会话并撤销该用户全部 MCP 凭据，与 ReplacePassword 不可分割。任一步失败都回滚密码、网页会话与委托撤销；提交后 Basic 缓存失效仍沿原有路径。改密或管理员重置后的成功响应不能早于这次事务提交，不能用提交后内存回调代替持久撤销。


## 元数据操作日志

`schema_handwritten.sql` 的 metadata_operations 与 `MetadataJournal` 保存版本化完整应用收据，读取和插入绑定实际元数据事务。它独立于最新字段来源，不自动过期，不用级联删除抹去历史；身份 ID 保留，不保存 Bearer 密钥。用户/凭据/请求键唯一，成功撤销父操作唯一；sequence 支持稳定降序 keyset 查询。授权、补充与撤销裁决归 `mm-application`，持久层不另造工作流。

`MetadataJournal.TryChange` 用同一外层事务内的 savepoint 包住元数据写入；仅当失败写入已完整回滚且 savepoint 释放成功时报告 failed，供应用保存脱敏失败收据。事务/savepoint 错误要求外层中止，不把已 rollback 的数据记为 changed；不使用另一个事务补记撤销失败来假装原子性。

`schema_model_budget.sql` 由统一 Migrate 安装，不进入 sqlc 输入。mcp_model_operations 与 mcp_model_calls 只保存独立命名空间的模型操作归属、冻结来源证据、不可变额度、准入消耗和白名单结果标签；唯一凭据/搜索及操作/item 约束防止重复额度/消费，used 受 allowance CHECK 约束。不与元数据变更审计共用表，不存模型内部推理；应用在实际事务内执行当前权限护栏。记录不随临时搜索缓存、运行历史裁剪或重启删除，用户/凭据失效仍由当前授权拒绝访问；不是通用审计或清理功能。
