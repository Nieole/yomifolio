# 19: 下线旧识别链与内部模型工具，写入主循环说明

**What to build:** MCP 工具面收缩为新的主循环加上各个原语。agent 一连上就知道该怎么做。

**Blocked by:** 06 主循环：领取与提交裁决；07 识别后的元数据补充；10 作者校正与封面改善成为独立原语

**Status:** ready-for-agent

- [ ] 下线旧的识别链：`library_search_sources`、`library_get_source_search`、`library_propose_match`、`library_get_match_proposal`、`library_get_enrichment_state`、`library_enrich_metadata`、`library_enrich_metadata_batch`。它们不再出现在工具清单中，调用时被统一拒绝。
- [ ] 下线内部模型：`library_assess_match`、`library_get_model_operation` 和 `internal_llm` 能力。
  - 签发和收窄凭据时不再接受这项能力；已有凭据上的这项能力不再对应任何工具。
  - 已有的额度记录数据保留、不删除，但不再提供查询入口。
  - 网页的凭据签发界面去掉内部 LLM 这一选项。
- [ ] 只为旧链服务的代码（如内存中的搜索状态）随之移除；补充核心和提案的普通路径不受影响。
- [ ] 初始化结果的 `instructions` 写明主循环：领取 → 提交 → 难判时上报；有权限就可以直接写入，不需要请示人。另外用一句话说明每个非主循环原语的用途。
- [ ] 回归测试：更新工具清单与能力映射的测试；04 的文本副本回归测试覆盖最终的工具集。
- [ ] 后端门禁、前端 test/build/lint 通过，生成契约无漂移。
