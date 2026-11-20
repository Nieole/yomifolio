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

`Options.Sources` 仅替换外部客户端工厂，在恢复与开放输入之前固定到生产图。`Metadata` 覆盖按来源键获取和分页搜索（含 LLM 元数据别名），HTTP 与应用刮削使用同一工厂；`AI` 覆盖分组、推荐和连接测试。工厂接收调用时的当前配置或已解析连接参数，须支持并发调用；返回的来源可通过 context 协作取消。未提供的工厂保留现有生产来源选择，不替换应用用例、数据库、任务引擎或生命周期，也不提供运行期间更换工厂的入口。

## 开放与关闭

先完成能力和效果绑定，再经应用 `Start` 恢复一致性、应用资源、处理遗留运行并开放后台输入；只有成功后才返回可服务的 HTTP 宿主。启动失败不返回宿主，并关闭已创建的进程资源，不把清理责任留给无法取得 Process 的调用方。

`Process.Shutdown` 交由 `mm-runtime` 执行长连接结束、普通请求排空及后台资源退休的顺序；`Process.Close` 仅关闭应用资源，不等于 HTTP 排空。数据库连接和进程日志仍由传入方负责关闭。

## 核对入口

`Open`、`Process.Shutdown`；`bootstrap_test.go`、`recovery_test.go`、各 HTTP 家庭集成测试及 `sse_shutdown_test.go` 验证真实生产组合，而非另一套测试专用装配。
