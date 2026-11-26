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

- `Catalogue`（`catalogue.go`）是目录型来源的唯一描述：键、显示名（与 provider 的 `Name()` 相同，提案以它记来源、命名链接）、网页来源列表的说明、历史提案里的其它写法、条目 URL 的形状与规范地址、封面所在的图片域名，以及是否要凭据。顺序与 `config.DefaultSourcePriority` 一致。识别的来源键与条目解析、候选封面的域名白名单、补充追加的来源链接名与网页来源列表都从它取，不另抄一份；`TestCatalogueFollowsTheDefaultPriorityAndTheProvidersNames` 守卫这几处对齐。
- `SourceFanout.Search` 按 `CatalogueSources` 的顺序逐个搜索当前配置启用的目录型来源：Bangumi、AniList、MangaDex 恒在，MyAnimeList、Comic Vine 仅在配置了对应凭据时加入；LLM 别名不是目录型来源。网页刮削来源列表也由 `CatalogueSources` 派生，再附加 LLM。每次访问来源前后都调用调用方给的检查点，检查点出错即停止扇出、丢弃在途页并原样返回该错误。每个来源独立报告 available、failed（不可达、结果多于请求数或总数为负）、invalid_response（任一结果未通过快照校验，整页丢弃）或 too_large（结果有效但调用方预算已满，由调用方经 `SourcePage.TooLarge` 判定）；一个来源不可用不隐藏其他来源。快照校验要求：结果非空、不携带 `MatchEvidence`、来源 ID 与册数非负、十分制评分在 0–10 内（0 表示未提供）、可 JSON 编码（非有限数值拒绝）、单条编码不超过 32 KiB、标签、标签票数、作者与别名各不超过 100 项、票数非负；任一不满足即整页 invalid_response，不截断字段。通过的结果经 JSON 深拷贝脱离来源对象，`Provider` 盖为来源名。结果预算、候选编号、分页与运行结局归调用方。
- `SourceFanout.Only` 非空时只问其中列出的已启用来源，供增量预取只问重开系列的新来源。`SourcePacer` 让共用它的调用方共享每个来源的请求节拍：`Wait` 预约该来源的下一个时隙，与上一个时隙至少相隔给定间隔，非正间隔不限速；并发运行因此不会叠加出更快的速率。它补充上述并发闸门（只限并行数、不限速率），间隔取值由调用方给出。
- `Snapshot` 对单个来源结果执行与扇出相同的快照校验并深拷贝，供所有者从手动搜索挑中的结果成为识别证据前使用。
- `MatchEvidence` 是附在提案原始结果上的短摘要和字段来源快照，库里委托匹配产生的提案带着它；`SourceFacts` 不递归嵌入决策记录。外部来源不生成这个记录，快照校验拒绝携带它的来源结果。来源 confidence 不是身份或版本匹配概率，搜索中的首项不是识别结论；源数据中的 URL 只是证据而非任意获取授权。

## Boundary

不写数据库、不落盘、不下载封面，也不拥有提案生命周期、裁决或任务调度。共享依赖归属见根 `architecture.md`。`doc.go` 仍将提案裁决和刮削调度归 API，存在归属漂移；本规格按当前实现区分候选生成、提案裁决与应用编排。

实现观察：并发闸门只覆盖表中数据源，未列出的 LLM 不受此闸门限制；它不是每小时配额保证。

## Evidence

- `provider.go`、`locale.go`、`provider_budget.go`、`errors.go`：公开结果、状态出口、请求预算及错误处理。
- `fanout.go`、`pacing.go`：多来源搜索扇出与来源节拍；`fanout_test.go` 的 `TestEachCatalogueSourceKeepsItsOwnStatus`、`TestAnInvalidAnswerMakesOnlyItsSourceUnavailable` 守卫逐源状态与快照校验。
- `bangumi.go`：Bangumi 搜索结果保留标签票数，由 `TestBangumiSearchKeepsTagVotes` 守卫。
- `aliases_test.go` 的 `TestSourcesNameTheEntrysOriginalTitleAndAliases` 守卫各来源的原名与别名。
- `llm_status_test.go`、`prompts_locale_test.go`、`provider_routing_test.go`：状态缺省、语言与 LLM 路由。
- `provider_budget_test.go`、`retry_loop_test.go`、`errors_test.go`：跨实例并发、重试取消、请求体重建与脱敏截断。


`SiteRating` 保留公共站点原始分数和零基尺度，`SourceFacts` 随确认提案保留它。AniList averageScore 为 100 分制，Bangumi score 与 MyAnimeList mean 为 10 分制；来源的 0/缺失表示未提供，不伪造有效评分。已有 `Rating` 十分制转换不变，MangaDex、ComicVine 与 LLM 不新增推测站点评分。综合评分策略不在来源适配器内执行。

`OriginalTitle` 是条目的原名，`Aliases` 是来源给出的其它名称：去掉与标题、原名重复的（忽略大小写），按来源给出的顺序至多 20 个。Bangumi 取 infobox「别名」；AniList 取没当标题用的 english、romaji、native 与 synonyms；MangaDex 取各语言标题与 altTitles；MyAnimeList 的原名取 `alternative_titles.ja`，别名取英文名与 synonyms；Comic Vine 取换行分隔的 aliases，没有原名。识别结论保存它们，复核判据拿它们比对（`mm-identification`）。

`TagVotes` 保留投票类来源每个标签的票数，与 `Tags` 同序；投票类来源由 `TagsAreVoted` 判定，目前只有 Bangumi（用户自由标注，票数即标注人数）。AniList、MangaDex、MyAnimeList 的标签是来源自己维护的分类，不带票数，Comic Vine 不给标签。标签过滤策略不在来源适配器内执行。
