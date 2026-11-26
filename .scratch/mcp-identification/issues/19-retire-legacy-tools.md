# 19: 下线旧识别链与内部模型工具，写入主循环说明

**What to build:** MCP 工具面收缩为新的主循环加上各个原语。agent 一连上就知道该怎么做。

**Blocked by:** 06 主循环：领取与提交裁决；07 识别后的元数据补充；10 作者校正与封面改善成为独立原语

**Status:** ready-for-agent

- [x] 下线旧的识别链：`library_search_sources`、`library_get_source_search`、`library_propose_match`、`library_get_match_proposal`、`library_get_enrichment_state`、`library_enrich_metadata`、`library_enrich_metadata_batch`。它们不再出现在工具清单中，调用时被统一拒绝。
- [x] 下线内部模型：`library_assess_match`、`library_get_model_operation` 和 `internal_llm` 能力。
  - 签发和收窄凭据时不再接受这项能力；已有凭据上的这项能力不再对应任何工具。
  - 已有的额度记录数据保留、不删除，但不再提供查询入口。
  - 网页的凭据签发界面去掉内部 LLM 这一选项。
- [x] 只为旧链服务的代码（如内存中的搜索状态）随之移除；补充核心和提案的普通路径不受影响。
- [x] 初始化结果的 `instructions` 写明主循环：领取 → 提交 → 难判时上报；有权限就可以直接写入，不需要请示人。另外用一句话说明每个非主循环原语的用途。
- [x] 回归测试：更新工具清单与能力映射的测试；04 的文本副本回归测试覆盖最终的工具集。
- [x] 后端门禁、前端 test/build/lint 通过，生成契约无漂移。

## Comments

本票按规格「留给实施决定的工程细节」（工具命名与参数形状、持久化结构）自己定下的取值：

- **下线方式**：九个工具直接不注册。调用它们得到 SDK 的未知工具协议错误，除工具名外与一个从未存在的名字逐字相同，这就是统一拒绝；不另写文案。理由：注册一个恒拒的处理器，只能回 `errMCPDenied`，那句「ID 取自本凭据的结果」对下线工具是误导。
- **`internal_llm`**：能力集合收为四项，签发与收窄都按参数无效拒绝它。凭据从存储读出时丢掉不在集合里的旗标，所以旧凭据上的 `internal_llm` 不授权任何东西（包括它名下的旧运行）、不进工具清单，管理员列表也不显示它；这样网页收窄旧凭据时不会因为带着它被拒，收窄一次后它就从存储里消失。只有 `internal_llm` 的旧凭据清单为空。额度表 `mcp_model_operations`、`mcp_model_calls` 原样保留，schema 不动。
- **initialize 的 instructions**：按凭据当前授权生成（`mcpInstructions`）。能领取时先写主循环（领取 → 逐个裁决后一次提交 → 难判上报、没合适候选就重搜，空批看 `empty_reason` 决定停还是稍后再来，有权限即直接写、不请示人），再给清单里其余每个工具一句用途（`toolPurposes`）；不能领取的凭据不提主循环，进度工具单列一句。测试要求清单里的每个工具都在说明里出现，以后新增工具缺一句就会失败；合入 11 时已为 `library_replace_identification`、`library_correct_series_field` 补上。
- **随旧链移除的代码**：委托模块的多来源搜索、内存搜索快照、确认提案与额度用例；补充模块的 `Apply`/`Batch`/`State`、提案证据收集与裁决命令及其命令摘要；提案模块只为裁决服务的 `AdjudicationPending`/`ConsumeAdjudication`；metadata 的单次模型请求守卫；`ExternalSources.ConfiguredAI`。`enrichment.New` 不再接收提案证据端口。保留 `AdjudicationResult`：旧收据还带着它，撤销靠它认领旧来源记录。补充核心（`applyEvidence`/`plan`）与提案的普通路径不动。
- **旧测试的去留**：只守旧链的测试随工具删除（匹配与分页、额度、提案裁决、base08 重放、旧作者与封面命令）。其中守的是保留工具或共用机制的部分，改由新原语产生收据后保留：封面收据清理边界（`TestCoverReceiptCleanupKeepsItsBoundaries`）、封面改善断线释放（`TestCoverImprovementLetsGoWhenItsClientLeaves`）、撤销边界与失败收据（`mcp_undo_test.go`，用识别收据）、RAR 线索断线与坏 solid 流（`TestComicInfoCluesLetGoWhenTheClientLeaves`）、运行清单按各运行的能力过滤（`TestRunListingFollowsEachRunsCapability`）。来源扇出的逐源校验移到 `internal/metadata/fanout_test.go`，直接测 `SourceFanout`。
- **文档**：模块规格（api、application、bootstrap、metadata、proposal）里描述已删用例的段落一并删改，避免规格写着不存在的东西；新工具面的长期契约、`docs/mcp*.md` 与 README 留给 20。

