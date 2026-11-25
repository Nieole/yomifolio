# 01: 多来源搜索扇出成为可复用能力

**What to build:** 把「按已配置的目录型来源依次搜索、校验每个来源的结果并保存快照」从 MCP 来源搜索用例中抽出，成为不依赖内存搜索状态的独立能力，供后续的证据包预取与单系列重搜复用。这是纯预备重构，外部行为不变。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] 以下规则只在一处定义：来源范围（Bangumi、AniList、MangaDex，以及配置了凭据的 MyAnimeList、Comic Vine；不含 LLM）、每个来源的状态（可用、失败、无效响应、过大）、结果快照的校验规则。
- [ ] 扇出过程中，每访问一个来源前后仍调用当前授权检查点；取消与授权撤销的行为不变。
- [ ] 现有来源搜索工具的结果、分页、截断与 30 分钟过期行为不变；现有 MCP 匹配与分页集成测试不改断言即通过。
- [ ] 后端门禁（完整 `go test`、`gofmt -l` 为空、`golangci-lint` 0 issues）通过。
