---
id: mm-proposal
type: module-design
status: active
title: 元数据提案生命周期
parent: mm-architecture
references: [mm-adr-0011]
---

## Responsibility

持有元数据提案的入队、去重、读取、应用与拒绝生命周期，以及裁决写入的事务；把候选转为可审阅字段差异，而非直接接受整个候选对象。

## Contract

- `Queue` 要求非 nil 候选，接收调用方的系列快照；按锁过滤差异，返回新建、复用、无变化、全锁或曾拒绝等 `QueueStatus`，error 仅表示故障。
- 去重限定同一系列，比较字段当前值与建议值及 `SourceID`；文本只裁首尾空白、归一 CRLF，历史字段按当前锁过滤。先查待裁决，再查最近 20 条拒绝记录；`IgnoreRejected` 只跳过拒绝去重。签名并不额外包含 provider 名称。
- `Apply` 支持全部应用和只补空；在 `ExecTx` 内重读 pending 状态、剩余字段、系列存值、当前锁及集合成员，再选择本次字段，不用入队快照或目录名展示回落。全部跳过或无变化时保留待裁决状态。
- 真正写入的字段交给系列写入器；元数据、沿革、统计与提案处理处于本包发起的事务。部分应用删除已写字段行、保留剩余字段待裁决；全部完成才以 pending CAS 关单，抢先时回滚并返回 conflict。拒绝也只推进 pending，不刷新既有终态时间。
- `Get`、`ListBySeries`、`Inbox` 与 `PendingCount` 提供读取；字段展示重算当前锁和系列当前值，不相信字段行的旧锁/旧值快照。字段行按批查询，分页总数不随当前页缩减。

- 受限匹配调用方可指定 `QueueOptions.RefreshSeries`，在实际入队事务内重读当前系列值及锁；事务授权仍由调用方的持久端口执行，不能用事务外预检代替。普通网页入队未指定时保持原快照与去重语义。
- 确认匹配的 `SeriesMetadata.MatchEvidence` 随 raw_payload 原子保存；`QueueOptions.MatchEvidence` 为字段行提供来源及 URL。复用既有提案不更新原证据、不声称新建；无差异、全锁、曾拒绝不额外持久化证据。原始 cover/link 仍不是新增裁决字段，匹配记录不是变更审计。

## Boundary

本包只认字段名，不拥有 HTTP 编解码、展示标签、候选请求或 AI 合集成员裁决。系列来源写入策略不是提案生命周期；共享边归根 `architecture.md`。

事务外的状态检查只提供快速冲突结论，不承担写入防线。部分应用也必须在写事务内确认 pending 并基于最新剩余字段决策；并发拒绝若先提交，则返回 conflict 且不写元数据、字段或沿革，先前部分应用消费过的字段不得被后续请求重放。元数据写入、字段删除或沿革失败时整体回滚，不能留下部分裁决结果。

## Evidence

- `queue.go`、`signature.go`、`resolve.go`、`read.go`：业务状态、去重、事务与读出契约。
- `queue_test.go`、`read_test.go`：去重范围、当前锁/当前值、分页及批量读字段。
- `resolve_test.go`、`fill_empty_test.go`、`replace_test.go`：部分应用、终态抢先、按存值补空、集合替换及关联失败整体回滚。
- `concurrency_test.go`：事务前拒绝导致部分应用冲突、并发部分应用不重用已消费字段、事务内当前值及锁决策、部分应用失败整体回滚。

## 授权调用方的事务内裁决

`Read` 允许调用方使用同一个事务快照投影当前字段/锁；授权由应用提供。`AdjudicationPending` 在调用方实际写事务重读系列归属、pending 及所选剩余字段；未知/错系列与终态竞争不混为成功。`ConsumeAdjudication` 仅删除实际写入字段行，pending 期间以 applied_at 保存已接受标记，继续部分应用及关闭不重写此时间；最后剩余为空时推进 applied，终态时间沿用既有 CAS 约定；明确关闭剩余差异且有当前/既往接受时推进 supplemented_closed，无接受则 rejected。`Accepted` 在调用方已授权的同一事务内统一结合 applied_at、applied / supplemented_closed 终态及现存 review_id provenance 投影接受事实，供公开提案读取、确认入队的新建/复用响应、所有裁决收据和关闭判断共享；入队调用方在实际授权入队事务内投影，不把 reused_existing 当作未接受。旧人工部分应用仍有当前 provenance 指向该提案时也能识别已接受，不猜测已丢失的历史；当前值恰好相同、锁定、保护或 noop 均不是接受证据。清理操作快照不删除最小裁决标记或提案 provenance；重放仍是原收据，不按最新提案重算。

`superseded` 是系列被识别后结案的终态，由 `SupersedePending` 在调用方写事务内以同一 pending CAS 推进，已被其他裁决结束的提案保持原终态；它既不是驳回也不是接受，不产生排除候选，剩余差异不再可裁决，此前已接受的字段仍算接受。supplemented_closed 是终态，不是 rejected 的别名；关闭保留未消费差异及已接受内容，Undo 不重新入队或恢复字段行。裁决状态推进采用同一 pending CAS，字段消费、标记、来源写入和收据失败必须由调用方整体回滚。来源保护规则与权限归 mm-application/mm-seriesmeta；普通人工 Apply/Reject 不改用新模式。
