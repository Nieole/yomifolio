---
id: mm-task
type: module-design
status: active
title: 任务身份、运行与统一准入
parent: mm-architecture
references: [mm-adr-0004, mm-adr-0005, mm-adr-0006, mm-adr-0007, mm-adr-0008, mm-adr-0013]
---

## 职责

持有任务四要素身份与长期属性、单次运行状态机、统一准入、全局运行槽位、排队与合并，以及进度、事件、采样和历史保留策略。

## 公开契约

- `Engine.Start(RunSpec, Body)` 校验身份与发起方；槽位满、同任务已有活动运行或全部暂停时排队。已有排队运行时返回 `Launched.Coalesced`，后来交出的声明与任务体不执行。定时、监听发起受禁用与退避约束，手动发起复位连败。
- 控制按运行 ID，长期禁用按任务 ID；`RunSpec.Key` 仅供日志，不落盘、不反解身份。任务体经运行句柄上报，以返回错误裁决完成、取消或失败，panic 转失败；重启的 `MarkInterrupted` 转写活动与排队运行，仅交出本批符合白名单的恢复候选，不重建任务体。
- 排队不占运行槽位，暂停与取消中仍属活动态。`PauseAll` 同时拦队列放行；单次暂停是协作式的，要等任务体到可中断点，不表示磁盘操作已立即停止。`Shutdown` 等准入启动交接，任务体退出仍由后台所有者等待。

## 资料库访问与交接

`RunSpec.LibraryIDs` 必须声明运行可能访问的全部资料库，`AllLibraries` 包含准入后才发现的资料库。覆盖声明约束任务准入及队列放行，不等于同库任务彼此互斥。排队闭包也可能保留旧根路径，属于占用；运行进入终态不证明任务体已经退出，覆盖占用须保留到真实退出。`AwaitBodyExit` 等待已准入回调退出，不能代替撤回或保护尚未执行的排队工作。

同步访问通过 `AcquireLibraryRead` 持有读租约直到使用结束，释放幂等。路径快照只有在取得读租约并确认 `LibraryAccessVersion` 自查找前未变化后才可缓存。`ReserveLibrary` 在同一访问边界封闭该库的新读、任务准入与队列放行，让已有占用退出；预留所有结果都必须释放。

`LibraryReservation.Drain` 仅请求取消身份和实际覆盖均合资格的局部工作，不为删除单库取消系统级或跨库工作。取消请求、取消失败、已观察退出与剩余冲突分别返回；读租约释放、排队占用解除和任务体真实退出才是完成条件，终态快照不是。调用方须提供有界上下文；不能取消的工作继续构成冲突，不承诺其安全退出或在期限内退出。

`Upgrade` 在同一锁边界复核真实占用并升级独占，不打开准入窗口。独占删除后先以 `Retire` 使已删除稳定身份失效，再释放预留，不能让旧身份恢复访问；全局工作仍可发现剩余资料库。

## 边界与限制

不持有 SQL、HTTP、具体任务类型词汇或文案翻译；落盘经 `Store`，快照与实况经注入函数。`taskcontrol` 只实现每次运行的上下文暂停闸门，不承担运行槽位或卷 IO 许可。落盘端口与临界区内回调不得回调引擎。

`Args` 只承载行为输入，环境描述归 `Labels` 或已有 `Limits`，IO 累计量归指标。合并保留原排队声明，比较落盘与本次提交的 `Args`，差异每次记录有损合并事件，无损重复仅首次记录；每次仍累计合并次数。事件明细与兼容规则归 `mm-adr-0008`。

进度等写入失败存在只告警路径，不承诺所有快照与侧数据原子持久化。

## 证据

`Identity.Validate`、`Engine.Start`、`settle`、`MarkInterrupted`、`Store`；`slots_test.go` 的 `TestCoalescedRunOnlyRunsOnce`、`TestCoalescingIsDecidedByTheStore`，`events_test.go` 的 `TestRepeatedCoalescingLandsOneEvent`、`TestEveryLossyCoalesceRetainsIncomingDeclaration`，以及 `state_machine_test.go`、`terminal_test.go`、`resume_test.go`、`library_reservation_test.go`、`library_drain_test.go` 与 `await_test.go`。
