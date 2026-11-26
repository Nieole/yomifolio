# 11: agent 纠正原语

**What to build:** agent 发现自己做错了，可以纠正：撤换自己做出的识别，或者改正来自来源、之后没被人改过的字段。人的工作 agent 永远碰不到。

**Blocked by:** 07 识别后的元数据补充

**Status:** ready-for-agent

- [x] 撤换识别（`metadata_write`）：
  - 只能撤换 agent 确认的识别；
  - 先撤回旧识别带来、之后没被改动的字段（沿用撤销的期望值校验），再按新识别补充；
  - 新旧两次操作都留收据。
- [x] 改正字段（`metadata_write`）：只能改当前值有来源记录、且记录值与当前值一致的字段；写原因、留收据，可以撤销。
- [x] 以下对象一律拒绝改动：人确认的识别、人工编辑过的值、ComicInfo 导入的值、锁定字段、人工封面。拒绝时提示 agent 可以改为上报。
- [x] 新增一个上报分类「质疑人确认的识别」：系列进入待复核，原有字段保持不变。
- [x] 标题占位例外不对 agent 开放。
- [x] 新工具的描述在约 400 字符以内并附调用示例，结果带 JSON 文本副本。
- [x] 集成测试覆盖：撤换、改正、所有拒绝路径、撤销、质疑上报。
- [x] 后端门禁通过。

## Comments

本票按规格「留给实施决定的工程细节」自己定下的取值：

- **工具命名与参数**：撤换 `library_replace_identification {request_key, series_id, candidate_ids, reason}`，改正 `library_correct_series_field {request_key, series_id, field, value, reason}`，都挂 `metadata_write`。`candidate_ids` 取自该系列证据包（1–5 个，每个来源至多一个），每个候选替换该系列在它那个来源上由 agent 做的识别；该来源没有识别（`not_concluded`）、已经是这个条目（`already_identified`）、是人确认的（`owner_confirmed`）都拒绝。改正的 `field` 取 `title`、`summary`、`publisher`、`status`、`rating`（输入 schema 给出枚举）；作者校正与封面改善归票 10 的原语，标签问题走上报。`value` 一律是文本：状态取 ongoing/completed/hiatus/cancelled，评分是 0–10 的十进制文本，空串清空该字段（标题除外）。理由的规则与提交裁决相同。结果沿用 06 的风格：被拒时给 `problem` 代码和 `message` 下一步，格式错误与授权拒绝仍是工具错误（拒绝不区分不存在与无权）。
- **撤换复用 13 定形的用例**：agent 的 `identification.Service.Replace` 判定通过后调 13 的 `identification.Service.replace`（逐个被替换的条目调 `enrichment.Service.RetractIdentified`，再 `confirm` 补充），本票不另有撤回实现。被替换的条目不记排除候选（排除只由人的否决产生）。结果里 `retraction_ids` 是每个被替换条目的撤回收据（普通 `enrich` 收据，`identification.retracted` 指名条目，可撤销），`operation_id` 是新识别的补充收据。撤回的判据是 13 的：只撤回该条目参与写下、之后没被改动的字段，沿来源记录指向的未改动补充链回到链首的写前值。
- **改正的判据与来源记录**：字段未锁，且 `seriesmeta.SourceBacked` 认定当前值仍等于它的来源记录值（升级回填的 `manual` 不算）。不区分记录是 agent 识别、人工确认的识别还是历史提案应用写的：规格只排除没有来源记录的人工值和 ComicInfo 值。改正后的值写一条来源记录（source 记为 `agent`），扫描因此照样保留它，agent 也能再次改正；收据新增 `correction{field, replaced}`，保存被替换值原来的来源记录（已纳入生成契约），撤销时恢复原值并原样放回那条来源记录，恢复的值重新受扫描保护、可再改正。改正后的评分不是公式结果，之后不会被重算。
- **所有者持有的系列**：撤换和改正都拒绝待人工、待复核、已审定三种状态的系列（`owner_holds`，并带出当前状态）。已审定是规格写明的「不想让流程触碰」；待复核对应用户故事 6「复核之前内容不会被删掉」；待人工同属等所有者处理，一并拒绝。未处理、查无此作的系列仍可改正带来源记录的值。
- **受保护值备注（用户决定的实现）**：分类 `protected_value_doubt`，class `note`，可经 `library_report_series` 与 `library_submit_verdicts` 的 escalate 提交，规则与其它备注相同。改正被拒为 `locked`、`not_from_source` 时，提示指向它；票 10 的 `library_improve_cover` 返回 `conflict` 收据时，结果在收据之外带一句 `message`，同样指向它（冲突也可能来自比较之后封面又变了，提示里一并说明）。
- **质疑分类**：`disputed_owner_identification`，经 `library_report_series` 提交（挂 `read`，与其它上报同一工具，所以只读凭据和普通用户的凭据也能用），class 为 `recheck`：系列进入待复核，结论和字段都不动；同一系列已有同分类未处理的上报时不再重复记。只接受至少有一条人工确认结论的系列，否则提示 agent 的识别要用撤换工具改。撤换被拒为 `owner_confirmed` 时，提示就指向这个分类。
- **重放**：撤换与改正的判定结果和提交裁决共用 `identification_verdicts`，按「凭据 × 请求键 × 系列」保存，摘要带上用例种类，所以同一请求键换了用例或内容都以 `request_key_reused` 拒绝。被拒的判定也保存，格式错误、授权拒绝和写入失败不保存。

**用户决定（2026-11-25）**：字段类拒绝（人工编辑过的值、ComicInfo 值、锁定字段）与 `library_improve_cover` 拒绝人工、锁定、所有者选过的封面时，提示 agent 改为上报一个新增的备注类分类 `protected_value_doubt`：只挂备注、不改识别状态，所有者可以关闭，关闭后不再重复记，规则与其它备注相同。`library_report_series` 的枚举与描述同步。

与本票第一版的差别（改用 13 的撤换后）：去掉了 `withdraw` 参数，也就不再支持「撤掉一个来源而不补新的」和「把识别从一个来源挪到另一个来源」；撤回从「整张补充收据逐字段撤回、撤回收据不可撤销」改为 13 的「按条目逐字段撤回、撤回收据可撤销」，补充收据里其它来源带来的字段不再一并撤回重填。
