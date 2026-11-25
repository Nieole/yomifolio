# 18: 作品群关系写入

**What to build:** agent 可以给一个系列添加续作、外传等作品群关系，每次添加都留下收据，并且可以撤销。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] 原语（`metadata_write`）在两个系列之间添加关系：关系类型沿用现有的作品群关系类型，两个系列都必须在凭据的授权范围内。
- [x] 每次添加都要写原因、留收据，可以查询和撤销。撤销只移除本次添加、之后没被改动过的关系。
- [x] 作品群的推导重建沿用现有机制（ADR 0012），不另造一套。
- [x] 不提供删除既有人工关系的能力。
- [x] 新工具的描述在约 400 字符以内并附调用示例，结果带 JSON 文本副本。
- [x] 集成测试覆盖添加、撤销、跨授权范围被拒绝。
- [x] 后端门禁通过。

## Comments

本票按规格「留给实施决定的工程细节」自己定下的取值：

- **工具命名与参数形状**：`library_add_series_relation`，参数 `request_key`、`series_id`、`target_series_id`、`relation_type`、`reason`（1–1000 字节）。语义是「target 是 series 的 relation_type」，与网页关系编辑器的方向一致。关系类型以 enum 写进输入 schema（即网页编辑器那 14 种，`organize.RelationTypes`），描述因此能压到 386 字符并附示例。理由：弱模型直接从 schema 挑值，不用在描述里读一长串类型。
- **收据**：沿用补充的操作日志，`kind=relation`；`relation` 字段记下关系的 id、存储方向与类型，agent 写的原因放在收据的 `reason`，before/after 是系列当时的（未改变的）元数据。请求键与补充、撤销共用同一命名空间，摘要是带 `command` 判别符的独立命令。理由：查询、撤销、清理都直接复用现有工具，不另造第二套收据。
- **已有关系**：两系列间不论哪个方向已有关系，都不改它，收据记为 no-change 并报告那条已有关系；这种收据撤销时也是 no-change。理由：满足「不提供删除既有人工关系的能力」，同时让 agent 知道已有关系是什么。
- **撤销判据**：按 id、方向、类型三者都与本次添加一致才删除；之后被改过类型、被删除、或被删后重建（id 变了）都记 conflict，关系原样保留。撤销时重查两端系列的 `metadata_write`，已被删除的一端跳过（关系已随之级联删除）。
- **持久化结构**：新增 sqlc 查询 `GetSeriesRelation`、`DeleteUnchangedSeriesRelation`，`CreateSeriesRelation` 改为返回 id。关系增删与 `MarkFranchiseRebuildPending` 同事务的不变量收进 `organize.AddRelationTx` / `RemoveUnchangedRelationTx`，网页的 `Relations.Create` 也改走它；提交后用 `Relations.Changed` 安排重建（ADR 0012 的持久待重建标记仍兜底）。
- **网页审计页**：操作记录页给关系收据加一行「作品群关系：#源 → #目标 · 类型」。撤销按钮仍只对补充收据出现：所有者在系列页直接删关系即可，本票不为它加新入口。
