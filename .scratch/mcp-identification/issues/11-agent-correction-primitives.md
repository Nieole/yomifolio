# 11: agent 纠正原语

**What to build:** agent 发现自己做错了，可以纠正：撤换自己做出的识别，或者改正来自来源、之后没被人改过的字段。人的工作 agent 永远碰不到。

**Blocked by:** 07 识别后的元数据补充

**Status:** ready-for-agent

- [x] 撤换识别（`metadata_write`）：
  - 只能撤换 agent 确认的识别；
  - 先撤回旧识别带来、之后没被改动的字段（沿用撤销的期望值校验），再按新识别补充；
  - 新旧两次操作都留收据。
- [x] 改正字段（`metadata_write`）：只能改当前值有来源记录、且记录值与当前值一致的字段；写原因、留收据，可以撤销。
- [ ] 以下对象一律拒绝改动：人确认的识别、人工编辑过的值、ComicInfo 导入的值、锁定字段、人工封面。拒绝时提示 agent 可以改为上报。
- [x] 新增一个上报分类「质疑人确认的识别」：系列进入待复核，原有字段保持不变。
- [x] 标题占位例外不对 agent 开放。
- [x] 新工具的描述在约 400 字符以内并附调用示例，结果带 JSON 文本副本。
- [ ] 集成测试覆盖：撤换、改正、所有拒绝路径、撤销、质疑上报。
- [ ] 后端门禁通过。

## Comments

本票按规格「留给实施决定的工程细节」自己定下的取值：

- **工具命名与参数**：撤换 `library_replace_identification {request_key, series_id, withdraw, candidate_ids, reason}`，改正 `library_correct_series_field {request_key, series_id, field, value, reason}`，都挂 `metadata_write`。`withdraw` 是要撤回的 agent 识别（`source:entry_id`，1–5 个），`candidate_ids` 取自该系列证据包（1–5 个，每个来源至多一个，与 `withdraw` 不重叠）；撤回后仍保留识别的来源不能再引用（`source_taken`），要换就一并撤回。改正的 `field` 取 `title`、`summary`、`publisher`、`status`、`rating`（输入 schema 给出枚举）；作者校正与封面改善归票 10 的原语，标签问题走上报。`value` 一律是文本：状态取 ongoing/completed/hiatus/cancelled，评分是 0–10 的十进制文本，空串清空该字段（标题除外）。理由的规则与提交裁决相同。结果沿用 06 的风格：被拒时给 `problem` 代码和 `message` 下一步，格式错误与授权拒绝仍是工具错误（拒绝不区分不存在与无权）。
- **撤换的用例形状（供 13 复用）**：识别模块里的 `identification.Service.replace(ctx, tx, actor, confirmer, receiptKey, seriesID, withdrawn, cited, reason)`，在调用方的写事务里：删掉 `withdrawn` 指名的结论 → `enrichment.Service.WithdrawIdentified` 撤回 → `confirm` 写入新结论并按该系列全部识别补充。是否允许撤这些结论、引用这些候选由调用方先判定（agent 用 `judgeReplacement`，候选查找可复用 `citeEvidence`）；排除候选由调用方自己记（13 记、agent 不记）。已审定的系列不撤回也不补充。历史推导出的结论没有识别补充收据，撤换它时不撤回任何字段（提案应用写入的值留在原处），13 若要更多需自己处理。
- **「旧识别带来、之后没被改动的字段」的判据**：凡是在被撤结论参与下算出的识别补充收据（`identification.entries` 含该条目、实际改过字段、没被撤销），按从新到旧逐张、逐字段撤回，判据与撤销相同：当前值仍等于该收据的写后值且字段未锁（占位标题例外）才恢复到收据的写前值，并删掉该收据写下的来源记录；不满足的字段保持现值。一张收据里其它来源带来的字段也一并撤回，随后的补充再按新的全部识别重新取值，所以结果等同于一开始就按新识别补充。理由：补充按「全部已识别来源 × 优先级」整体计算，只撤被撤来源的那几个字段，集合字段（来源链接、作者）和公式评分会残留旧条目。
- **收据**：每张被撤回的收据各得一张撤回收据（kind `undo`，`parent_id` 指向原收据，结果里是 `withdrawal_ids`），新识别的补充是普通识别收据（`operation_id`）。原收据因此显示已撤销、不能再撤销；撤回收据本身也不能撤销，要改回就再撤换一次。收据键由「凭据、请求键、系列」派生（撤回收据再加上原收据 ID）。
- **改正的判据与来源记录**：字段未锁，且 `seriesmeta.SourceBacked` 认定当前值仍等于它的来源记录值（升级回填的 `manual` 不算）。不区分记录是 agent 识别、人工确认的识别还是历史提案应用写的：规格只排除没有来源记录的人工值和 ComicInfo 值。改正后的值写一条来源记录（source 记为 `agent`），扫描因此照样保留它，agent 也能再次改正；收据新增 `correction{field, replaced}`，保存被替换值原来的来源记录（已纳入生成契约），撤销时恢复原值并原样放回那条来源记录，恢复的值重新受扫描保护、可再改正。改正后的评分不是公式结果，之后不会被重算。
- **所有者持有的系列**：撤换和改正都拒绝待人工、待复核、已审定三种状态的系列（`owner_holds`，并带出当前状态）。已审定是规格写明的「不想让流程触碰」；待复核对应用户故事 6「复核之前内容不会被删掉」；待人工同属等所有者处理，一并拒绝。未处理、查无此作的系列仍可改正带来源记录的值。
- **质疑分类**：`disputed_owner_identification`，经 `library_report_series` 提交（挂 `read`，与其它上报同一工具，所以只读凭据和普通用户的凭据也能用），class 为 `recheck`：系列进入待复核，结论和字段都不动；同一系列已有同分类未处理的上报时不再重复记。只接受至少有一条人工确认结论的系列，否则提示 agent 的识别要用撤换工具改。撤换被拒为 `owner_confirmed` 时，提示就指向这个分类。
- **重放**：撤换与改正的判定结果和提交裁决共用 `identification_verdicts`，按「凭据 × 请求键 × 系列」保存，摘要带上用例种类，所以同一请求键换了用例或内容都以 `request_key_reused` 拒绝。被拒的判定也保存，格式错误、授权拒绝和写入失败不保存。
