---
id: mm-bootstrap
type: module-design
status: draft
title: 生产装配根与请求开放屏障
parent: mm-architecture
references: [mm-adr-0014, mm-runtime]
---

## 责任与边界

`Open` 是生产装配根：接收调用方持有的数据库与共享配置，创建一份内容访问、缓存、统计和推荐等资源，装配应用及窄 HTTP 家庭能力。HTTP 宿主只消费已绑定能力，不构造应用，也不复制完整运行时服务图；取舍归 `mm-adr-0014`。

## 外部来源接缝

`Options.Sources` 仅替换外部客户端工厂，在恢复与开放输入之前固定到生产图。`Metadata` 覆盖按来源键获取和分页搜索（含 LLM 元数据别名），HTTP 与应用用例（含证据包预取）使用同一工厂；`AI` 覆盖分组、推荐和连接测试。工厂接收调用时的当前配置或已解析连接参数，须支持并发调用；返回的来源可通过 context 协作取消。未提供的工厂保留现有生产来源选择，不替换应用用例、数据库、任务引擎或生命周期，也不提供运行期间更换工厂的入口。

## 开放与关闭

先完成能力和效果绑定，再经应用 `Start` 恢复一致性、应用资源、处理遗留运行并开放后台输入；只有成功后才返回可服务的 HTTP 宿主。启动失败不返回宿主，并关闭已创建的进程资源，不把清理责任留给无法取得 Process 的调用方。

`Process.Shutdown` 交由 `mm-runtime` 执行长连接结束、普通请求排空及后台资源退休的顺序；`Process.Close` 仅关闭应用资源，不等于 HTTP 排空。数据库连接和进程日志仍由传入方负责关闭。

## 核对入口

`Open`、`Process.Shutdown`；`bootstrap_test.go`、`recovery_test.go`、各 HTTP 家庭集成测试及 `sse_shutdown_test.go` 验证真实生产组合，而非另一套测试专用装配。

## MCP 能力绑定

`Open` 用同一数据库构建 `delegated.SQLStore` 与 `delegated.Service`，并注入共享内容服务及同一 Runtime 的 Engine（内容准入）、Scans 和资料库窄查询能力，并绑定 `MatchingAccess` 到同一 Runs、数据库及调用时配置的来源工厂；经 `AdministrationDependencies.Delegated` 显式注入管理员 HTTP 及 MCP 协议适配。协议适配器不获得数据库、运行时或独立资源关闭权；请求开放与排空沿原有 Host 生命周期，不启动第二个应用运行时。


`Open` 同时从同一数据库与 delegated 实例装配 `enrichment.Service`，经 `AdministrationDependencies.Enrichment` 显式绑定网页和 MCP；提交后通过现有效果端口失效统计及发布刷新，不创建独立运行时或后台维护任务。

内部模型额度操作另绑定 `ModelBudgetAccess` 到同一数据库、配置快照、`Runs.Start` 和 `ExternalSources.ConfiguredAI`（01 的生产 AI 工厂）；未注入替身时沿用既有 OpenAI Responses、Chat Completions 或 Ollama 来源，不新建付费客户端体系或运行时。AI 工厂替身属于可信外部边界，须遵守单次调用不隐藏重试的契约并响应 context 取消；生产 HTTP 来源额外执行每请求守卫。额度、不可变操作与故障语义归 `mm-application`。


`enrichment.WithCovers` 绑定同一 delegated 实例及配置快照，复用现有图片/内容资格，不注入第二份图片宿主、运行时或网络客户端。不可变材料先准备/事务引用切换没有启动期文件重发或发布队列，故障与重开策略归 mm-application。


`enrichment.WithCleanup` 绑定同一 delegated 实例和 app.Runs.Start；清理运行使用唯一引擎，不启动额外 goroutine 宿主、常驻材料 janitor 或自动恢复。持久隔离与有限文件回收由应用用例负责。
