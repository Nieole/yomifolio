---
id: mm-runtime
type: submodule-design
status: active
title: 进程能力装配与生命周期
parent: mm-application
references: [mm-adr-0014]
---

## 责任与顺序

`New` 接收共享的 `config.Manager` 与 `database.Store`，不创建第二份配置或数据库连接；在它们之上装配任务存储、任务引擎、IO 执行器、功能服务和重启注册表，先于任何恢复或发起输入。传入的 `Effects` 仅提供发布、缓存和身份投影叶操作，不把生命周期配方交回 API。

`Start` 依次恢复资料库迁移一致性（`Recover`）、应用当前配置的进程资源效果、恢复运行（`RecoverRuns`），最后开放配置监听、事件和其他后台输入；成功后 HTTP host 才可开放请求。`RecoverRuns` 将遗留运行转成中断历史，再通过各能力真实重启入口和已登记策略发起安全续跑，不把旧行改成新运行。配置变更可重新释放排队工作，但不替换共享的 `config.Manager`。

`Shutdown` 先停止配置监听和 SSE 长连接，再调用普通 HTTP 请求的 drain，最后执行 `Close`；drain 的错误仍返回给调用方，失败不意味着全部请求已经退出。

`Close` 停止配置监听和事件输入、通知后台循环，先关闭任务准入并请求取消，再停止 watcher；等待已登记工作真实退出后才关闭 scanner 和退休运行资源。关闭必须幂等，并发关闭调用也须等到关闭完成；仍可能登记工作的阶段不能提前封闭后台等待组。取消只是协作请求，不保证无法取消或不响应取消的工作能及时退出。

## 边界

生产组合根 `bootstrap.Open` 负责连接共享依赖、应用能力与 HTTP 适配器，绑定叶效果后调用 `Start`；本包负责该应用图的启动恢复和资源寿命，不接管生产 HTTP 装配，也不关闭调用方持有的数据库连接或进程日志文件。不拥有阅读规则、任务状态机、数据库事务或 HTTP 解释。能力通过窄端口注入，测试可替换时钟、调度和叶效果而不建立第二套实现。

核对入口：`New`、`Start`、`Recover`、`RecoverRuns`、`Shutdown`、`Close`、`Registry` 与本目录生命周期测试。
