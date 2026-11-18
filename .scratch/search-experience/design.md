# 搜索体验：调查依据

## 文档分工

本文件保存调查事实、证据局限与选型理由。已确认的产品行为、实施约束、测试 seam 及验收标准由 [正式规格](spec.md) 唯一维护，状态为 ready-for-agent。

下一步由用户执行 `to-tickets`。本轮只发布规格，不修改产品代码、不拆工单；正式规格不代表搜索改进已实现或已验收。

## 用户问题与证据局限

用户反馈：确定存在的系列在全局搜索找不到，而在对应资料库的名称搜索可以找到。目前没有实际系列、关键词与响应样本，真实故障根因尚未复现。

以下是代码调查所得，不等于真实藏书故障根因。实际样本取得后应增加回归，但不妨碍依据正式规格修正已确认的行为差异。

## 匹配路径

`internal/database/series_search.go` 的 `SearchGlobalSeries`、`SearchGlobalBooks` 对短词使用 gram 索引，长词使用 trigram；`buildSeriesSearchQuery` 对资料库短词采用直接子串匹配，长词通常共享系列 trigram 索引。

`internal/database/gram_search.go` 的 `gramMatchExpr` 与索引维护依赖对称的 ASCII 大小写规则。索引和查询只改一边，可能静默返回零条。现有索引不是删空白、删标点、折叠宽度后的文本索引。

全局索引查询成功返回零条不会因此触发直接子串回退。若实际问题使用长关键词，两处通常共享系列索引，不能仅用短词路径差异解释现象。

## 结果组织

`internal/catalogsearch/search.go` 的 `Search.Query` 在全部模式先追加书，再追加系列；分数归一化不等于统一排序。`internal/api/controller_search.go` 的 `searchBooks` 固定每类型的首批限额，没有继续检索参数；`SearchResult.Total` 表达的是已返回条数，不是完整匹配总数。

因此既可能存在“匹配却不可达”，也可能存在“系列被书排在后面”。但精确名称已有优先排序，不能未核对实际响应就把某次漏搜一律归咎于截断。

## 会话状态

`web/src/components/layout/useGlobalSearch.ts` 已有防抖与取消，但缺少可供展示消费的完整等待、失败和继续加载状态。非空关键词变化时旧命中可能保留，失败只记录到控制台。

`web/src/components/layout/SearchModal.tsx` 根据结果决定内容，无法可靠区分等待、失败和成功零结果。`web/src/components/Layout.tsx` 的回车打开与关闭行为也需纳入会话有效性验证。

## 选型理由

- 数据库 module 集中归一化及索引维护，提高 locality；对外的系列匹配 interface 具有跨全局与资料库的 leverage。不把其他列表筛选、排序收成万能查询。
- 统一函数配合触发器维护，覆盖实际写入；相较只在部分 Go 写入入口维护派生文本，降低漏维护风险。代价是迁移、运行期及外部直接写入连接必须支持该函数。查询时全库归一化虽然少改结构，但放弃索引收益，未采用。
- catalogsearch module 承担混排、去重和继续检索知识；删除后复杂性应回流调用方，才算通过 deletion test，而不是增加 shallow 转发。
- 前端搜索会话 module 统一请求归属及生命周期。只有一个 HTTP adapter 时，不为假想替代实现新增 seam。
- 稳定游标兼顾深页效率与无服务器会话状态；相比偏移分页更适合继续检索，但不提供整次搜索快照。冻结快照需要额外资源与失效策略，不在本次范围。
- 保留 [ADR 0014](../../docs/adr/0014-use-cases-queries-and-host-assembly.md) 的按场景查询与 SQLite，遵守 [ADR 0010](../../docs/adr/0010-book-facts-and-reader-views.md) 的藏书事实与读者阅读视图区分。没有新增本项目特有领域概念，不为通用技术词扩充 GLOSSARY，也不把未来能力写成已实施的长期 ADR。

## 调查阶段验证

执行过：

```bash
GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./internal/database ./internal/catalogsearch -run 'TestGramSearch|TestSeriesListKeywordFTS' -count=1
```

数据库相关测试通过；catalogsearch 当时没有测试文件。该检查只证明现有夹具通过，不证明真实藏书索引无缺项，不是正式规格新行为的验收。

没有执行本次改进的实现、全量测试、性能验收或浏览器验证。
