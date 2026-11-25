# 01: 多来源搜索扇出成为可复用能力

**What to build:** 把「按已配置的目录型来源依次搜索、校验每个来源的结果并保存快照」从 MCP 来源搜索用例中抽出，成为不依赖内存搜索状态的独立能力，供后续的证据包预取与单系列重搜复用。这是纯预备重构，外部行为不变。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] 以下规则只在一处定义：来源范围（Bangumi、AniList、MangaDex，以及配置了凭据的 MyAnimeList、Comic Vine；不含 LLM）、每个来源的状态（可用、失败、无效响应、过大）、结果快照的校验规则。
- [x] 扇出过程中，每访问一个来源前后仍调用当前授权检查点；取消与授权撤销的行为不变。
- [x] 现有来源搜索工具的结果、分页、截断与 30 分钟过期行为不变；现有 MCP 匹配与分页集成测试不改断言即通过。
- [x] 后端门禁（完整 `go test`、`gofmt -l` 为空、`golangci-lint` 0 issues）通过。

## Comments

本票自己定下的工程细节（都不碰已确认的产品或安全边界）：

- **扇出放在 `metadata` 包**：`metadata.CatalogueSources(cfg)` 定义来源范围，`metadata.SourceFanout.Search` 负责逐源搜索、前后检查点、单源状态与快照校验。来源适配器、出站约束和「来源不得携带决策记录」本就归 `mm-metadata`，delegated 与后续识别模块都已依赖它，不新增模块边。
- **结果预算不进扇出**：扇出只给出 `too_large` 状态与 `SourcePage.TooLarge`，是否放得下由调用方按自己的结果形状判（MCP 来源搜索沿用 240 KiB 整体上限）；证据包的字节预算留给 05。
- **分页、候选编号与运行结局留在调用方**：`next_offset`/`truncated`、`来源:序号` 候选 ID、failed/partial/completed 仍在 `delegated.SearchSources`，因为预取只取首页、一次运行覆盖多个系列，粒度不同。
- **网页刮削来源列表改由 `CatalogueSources` 派生**（再附加 LLM），让「配了凭据才启用」只有一处定义；响应内容与顺序不变。
- **先补特征测试再重构**：`TestMCPSourceSearchReportsEachCatalogueStatus` 在旧实现上先跑绿，钉住逐源状态（含过大的跨来源累计预算）、各条快照校验规则、快照与来源对象脱钩；另有一个子测试断言刮削来源列表与扇出同一范围。
