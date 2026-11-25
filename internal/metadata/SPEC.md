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

- `SourceFanout.Search` 按 `CatalogueSources` 的顺序逐个搜索当前配置启用的目录型来源：Bangumi、AniList、MangaDex 恒在，MyAnimeList、Comic Vine 仅在配置了对应凭据时加入；LLM 别名不是目录型来源。网页刮削来源列表也由 `CatalogueSources` 派生，再附加 LLM。每次访问来源前后都调用调用方给的检查点，检查点出错即停止扇出、丢弃在途页并原样返回该错误。每个来源独立报告 available、failed（不可达、结果多于请求数或总数为负）、invalid_response（任一结果未通过快照校验，整页丢弃）或 too_large（结果有效但调用方预算已满，由调用方经 `SourcePage.TooLarge` 判定）；一个来源不可用不隐藏其他来源。快照校验要求：结果非空、不携带 `MatchEvidence`、来源 ID 与册数非负、十分制评分在 0–10 内（0 表示未提供）、可 JSON 编码（非有限数值拒绝）、单条编码不超过 32 KiB、标签与作者各不超过 100 项；任一不满足即整页 invalid_response，不截断字段。通过的结果经 JSON 深拷贝脱离来源对象，`Provider` 盖为来源名。结果预算、候选编号、分页与运行结局归调用方。
- `MatchEvidence` 是由应用在显式确认后附加的短摘要和字段来源快照，便于提案持久化与后续审计取证；`SourceFacts` 不递归嵌入决策记录。外部来源不生成这个记录。确认响应的 accepted 是应用从持久提案事实投影的结果，不来自候选、确认摘要或 MatchEvidence 的身份确认布尔值；复用不修改原证据。来源 confidence 不是身份或版本匹配概率，搜索中的首项不是确认结果；源数据中的 URL 只是证据而非任意获取授权。

## Boundary

不写数据库、不落盘、不下载封面，也不拥有提案生命周期、裁决或任务调度。共享依赖归属见根 `architecture.md`。`doc.go` 仍将提案裁决和刮削调度归 API，存在归属漂移；本规格按当前实现区分候选生成、提案裁决与应用编排。

实现观察：并发闸门只覆盖表中数据源，未列出的 LLM 不受此闸门限制；它不是每小时配额保证。

## Evidence

- `provider.go`、`locale.go`、`provider_budget.go`、`errors.go`：公开结果、状态出口、请求预算及错误处理。
- `fanout.go`：多来源搜索扇出；经 MCP 来源搜索由 `TestMCPSourceSearchReportsEachCatalogueStatus`、`TestMCPMatchingConfirmedProposal` 守卫。
- `llm_status_test.go`、`prompts_locale_test.go`、`provider_routing_test.go`：状态缺省、语言与 LLM 路由。
- `provider_budget_test.go`、`retry_loop_test.go`、`errors_test.go`：跨实例并发、重试取消、请求体重建与脱敏截断。


`SiteRating` 保留公共站点原始分数和零基尺度，`SourceFacts` 随确认提案保留它。AniList averageScore 为 100 分制，Bangumi score 与 MyAnimeList mean 为 10 分制；来源的 0/缺失表示未提供，不伪造有效评分。已有 `Rating` 十分制转换不变，MangaDex、ComicVine 与 LLM 不新增推测站点评分。综合评分策略不在来源适配器内执行。

委托额度路径通过 `WithSingleModelRequest` 限定一次准入上下文只能发送一次实际 HTTP 模型请求；三个生产模型来源在 client.Do 前执行实时检查，拒绝同上下文第二次发送并禁用重定向，且仅在该路径移除请求体重放配方，阻止 Go HTTP/2 在 REFUSED_STREAM/GOAWAY 后透明重发已经发送的 POST；尚未发送的连接选择不消耗额外次数，避免隐藏重复付费 POST。没有该上下文的网页/既有智能用例保持原行为。来源不拥有额度数据库、不退款、不恢复或重试委托操作；额度准入及证据规则归 `mm-application`。
