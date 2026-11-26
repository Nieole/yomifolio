# 20: 收尾：长期契约、文档与参考验收

**What to build:** 把本 effort 的长期契约归还给各自所属的规格，更新面向用户的文档，记录真机参考验收的结果。

**Blocked by:** 01–19

**Status:** done

- [x] 把本 effort 的长期契约写入架构规格的 MCP 节、application 模块规格和 api 模块规格。新增的规格节点用 draft 状态。
- [x] ADR 0016 中额度委托的部分标为 deprecated，并链接 ADR 0017；清理自主权部分保留。ADR 0017 的状态由用户审阅后决定。
- [x] CHANGELOG 记录用户可见的变化。MCP 接入文档（中文和英文）更新以下内容：工具与能力的对照、主循环说明、已知限制。
- [ ] 把真机参考验收的结果记入本 effort 的验收材料，不作硬性门禁。验收条件：
  - 使用 qwen3.8-flash-next q2 + pi，在 4 个系列的测试资料库上运行；
  - 15 分钟内，全部系列进入已识别、查无此作或待人工之一；
  - 全程不调用 bash 或 curl，也不读仓库。
- [x] 完整门禁全部通过：`go test`、`gofmt`、`golangci-lint`、前端 test/build/lint、tsgen 与 sqlc 生成物无漂移。
- [x] 文档风格检查通过。

## Comments

本票按规格「留给实施决定的工程细节」与收尾范围自己定下的取值：

- **识别模块的长期契约单列为子节点**：`internal/application/identification/SPEC.md`（`mm-identification`，draft，parent 为 `mm-application`），父规格只留入口与引用。理由：识别模块的契约体量与委托访问、补充相当，`docs/agents/doc-style.md` 允许独立子能力在实现旁单列具名规格；新增节点按规则用 draft。各票 Comments 里的用户决定都落在它或下面列出的节点里，正文标「用户决定」。
- **识别补充策略（票 07）与标签策略（票 08）写进 `mm-application`**：新增「按识别补充」「识别撤回与字段改正」「作品群关系收据」三节。实现归 `enrichment.Service`，和既有补充契约放在一处；`mm-identification` 只引用，不复制。
- **其它节点只改正文、不新建**：architecture 的 MCP 节（识别模块的依赖边；删去委托多来源搜索与模型额度的过时段落；ADR 0016/0017 的状态）、`mm-api`（识别工具与 `/api/identification/` 接口）、`mm-bootstrap`（识别装配与候选封面的 `Covers` 接缝）、`mm-metadata`（`SourcePacer`、`SourceFanout.Only`、`Snapshot`；核对入口改指 `fanout_test.go`）、`mm-database`（识别表；额度表只作留存）、`mm-config`（识别配置）、`mm-web`（识别界面与关系收据的显示）、`goal-and-requirements`（MCP 能力描述）。
- **ADR 0016**：正文开头加一行状态，写明额度委托部分 deprecated、后继为 ADR 0017、清理自主权部分仍有效；frontmatter 保持 draft，因为节点整体没有被取代。ADR 0017 未动。
- **CHANGELOG**：在「未发布」最前面加本 effort 的 11 条，保留原有三条修复；下线工具、`internal_llm` 与批量刮削路由合成一条，标「不兼容」。
- **签发凭据页的能力说明**（locale 键 `mcp.bounds`，中英两份）还写着「目前仅实现系列读取」「不能获得付费模型权限」，与现状不符，顺手改成当前的权限规则；`docs/features*.md` 里「单系列手动搜索生成提案」的说法同样改为记录人工确认的识别。
- **真机参考验收**：材料在 `.scratch/mcp-identification/acceptance.md`（前置准备、操作步骤、判定条件、留空的结果记录区）。要所有者本人用 qwen3.8-flash-next q2 + pi 执行，本项未勾。

**用户决定（2026-11-26）**：票 20 记为 done。真机参考验收（qwen3.8-flash-next q2 + pi）由用户本人执行，本票交付了验收材料 `.scratch/mcp-identification/acceptance.md`；执行结果记入该文件的结果记录区，因此上面那一项保持未勾。规格写明它不作硬性门禁。
