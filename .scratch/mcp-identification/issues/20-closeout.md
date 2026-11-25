# 20: 收尾：长期契约、文档与参考验收

**What to build:** 把本 effort 的长期契约归还给各自所属的规格，更新面向用户的文档，记录真机参考验收的结果。

**Blocked by:** 01–19

**Status:** ready-for-agent

- [ ] 把本 effort 的长期契约写入架构规格的 MCP 节、application 模块规格和 api 模块规格。新增的规格节点用 draft 状态。
- [ ] ADR 0016 中额度委托的部分标为 deprecated，并链接 ADR 0017；清理自主权部分保留。ADR 0017 的状态由用户审阅后决定。
- [ ] CHANGELOG 记录用户可见的变化。MCP 接入文档（中文和英文）更新以下内容：工具与能力的对照、主循环说明、已知限制。
- [ ] 把真机参考验收的结果记入本 effort 的验收材料，不作硬性门禁。验收条件：
  - 使用 qwen3.8-flash-next q2 + pi，在 4 个系列的测试资料库上运行；
  - 15 分钟内，全部系列进入已识别、查无此作或待人工之一；
  - 全程不调用 bash 或 curl，也不读仓库。
- [ ] 完整门禁全部通过：`go test`、`gofmt`、`golangci-lint`、前端 test/build/lint、tsgen 与 sqlc 生成物无漂移。
- [ ] 文档风格检查通过。
