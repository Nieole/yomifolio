---
id: mm-metadata
type: module-design
status: active
title: 元数据源与候选生成
parent: mm-architecture
---

## Responsibility

将外部漫画数据源与 LLM 响应转换为内存候选 `SeriesMetadata`，并生成推荐、合集分组建议与测试响应；不把候选当成已接受的系列事实。

## Contract

- `Provider` 提供按标题获取与分页搜索；`AIProvider` 扩展推荐、分组和 `TestLLM`。结果携带来源、置信度及封面 URL，分页与无结果口径由各适配器实现。
- `WithLocale` 决定提示词语言：英文前缀归 `en-US`，其余及缺省归 `zh-CN`。源侧状态只报告已知事实，缺失、无法识别及源侧 `unknown` 留空；写入端专用的 `StatusCodeOrUnknown` 不用于候选出口。
- 五个公共数据源的真实 HTTP 请求共享进程级并发闸门；重试识别 `Retry-After`，退避和等令牌可取消，POST 重试重建请求体。上游错误处理脱敏密钥、截断响应体，不能将完整错误页直接写日志。

- `MatchEvidence` 是由应用在显式确认后附加的短摘要和字段来源快照，便于提案持久化与后续审计取证；`SourceFacts` 不递归嵌入决策记录。外部来源不生成这个记录。来源 confidence 不是身份或版本匹配概率，搜索中的首项不是确认结果；源数据中的 URL 只是证据而非任意获取授权。

## Boundary

不写数据库、不落盘、不下载封面，也不拥有提案生命周期、裁决或任务调度。共享依赖归属见根 `architecture.md`。`doc.go` 仍将提案裁决和刮削调度归 API，存在归属漂移；本规格按当前实现区分候选生成、提案裁决与应用编排。

实现观察：并发闸门只覆盖表中数据源，未列出的 LLM 不受此闸门限制；它不是每小时配额保证。

## Evidence

- `provider.go`、`locale.go`、`provider_budget.go`、`errors.go`：公开结果、状态出口、请求预算及错误处理。
- `llm_status_test.go`、`prompts_locale_test.go`、`provider_routing_test.go`：状态缺省、语言与 LLM 路由。
- `provider_budget_test.go`、`retry_loop_test.go`、`errors_test.go`：跨实例并发、重试取消、请求体重建与脱敏截断。


`SiteRating` 保留公共站点原始分数和零基尺度，`SourceFacts` 随确认提案保留它。AniList averageScore 为 100 分制，Bangumi score 与 MyAnimeList mean 为 10 分制；来源的 0/缺失表示未提供，不伪造有效评分。已有 `Rating` 十分制转换不变，MangaDex、ComicVine 与 LLM 不新增推测站点评分。综合评分策略不在来源适配器内执行。
