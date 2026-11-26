# 13: 系列详情页的人工操作与识别状态筛选

**What to build:** 所有者可以在系列详情页撤换识别；在手动搜索弹窗里挑中条目，就完成这个系列的识别；在资料库里可以按识别状态筛选系列。

**Blocked by:** 07 识别后的元数据补充

**Status:** ready-for-agent

- [x] 详情页撤换识别（人可以撤换任何识别）：旧条目记为排除候选；撤回旧识别带来、之后没被改动的字段；再按新识别补充。
- [x] 单系列手动搜索弹窗：挑中条目即记为人工确认的识别，写入改为按 07 的补充策略执行，不再生成提案。
- [x] 资料库浏览的筛选条件增加识别状态，可以与现有条件组合使用。
- [x] 新的响应结构纳入生成契约，无漂移。
- [x] 前端组件测试和后端集成测试覆盖上述三项。
- [x] 后端门禁、前端 test/build/lint 通过；提交时附 UI 截图。

## Comments

本票按规格「留给实施决定的工程细节」自己定下的取值：

- **撤换的可复用用例**（票 11 合并时改用它）：入口是识别模块的 `identification.Service.replace(ctx, tx, actor, confirmer, receiptKey, seriesID, cited, reason)`，跑在调用方的事务里。被引用的来源已识别为别的条目时，先删掉那条结论，再调 `enrichment.Service.RetractIdentified` 撤回该条目带来、之后没被改动的字段（单独一张收据），最后走 07 的 `confirm`，按系列现有的全部识别重新补充；同一条目则只按新的确认者重新确认。已审定的系列只改结论。谁能撤换谁、旧条目是否记为排除候选，由调用方决定。所有者这一侧的封装是 `identifyByOwner`：把旧条目记为排除候选，结案识别类和复核类上报，删掉查无此作和领取租约。票 12 的「挑候选」可以直接复用。
- **「之后没被改动」的判据**：字段的来源记录指向一张仍然有效的识别补充收据，当前值也还等于那张收据写入后的值（沿用撤销的期望值校验：锁会拦住，占位标题例外）。从这张收据往回，沿着「之后没被改动过的连续识别补充」找到最早的写入前值；链上只要有一张收据引用了被撤换的条目，就恢复到这个值。所以分几次识别的多来源系列（例如先 Bangumi、后 AniList），撤换 Bangumi 时也能撤掉它参与过的评分、链接和作者，其它来源的值再由随后的补充算回来。撤回收据的 kind 仍是 `enrich`，用 `identification.retracted` 记下被撤回的条目，可以用现有工具撤销。
- **收据键**：网页请求没有请求键。所有者每次识别生成一个随机键 `owner-identify-<hex>`，撤回收据的键由它加来源派生（`retract-<hex>`）。
- **接口形状**：`PUT /api/identification/series/{id}/conclusions/{source}`，请求体是 `{"result": <搜索结果>}`，响应是 `OwnerIdentification`（已纳入生成契约）。搜索结果按预取用的快照规则校验（抽成 `metadata.Snapshot`），条目编号由 `identification.EntryOf` 从 SourceID 或 SourceURL 取。LLM 等非目录来源返回 400。前端遇到 LLM 结果仍走原来的提案流程，因为 LLM 的回答不指向任何来源条目，不能成为识别。
- **所有者识别的理由**：结论的 reason 留空，识别区显示「在手动搜索中挑中」。
- **上报**：所有者识别一个系列时，它开着的识别类和复核类上报以 `resolution='identified'` 结案，否则状态会一直停在待人工或待复核；备注类不动。
- **识别状态筛选**：`GET /api/series/search?identification=<状态>` 直接读状态视图，和识别区、进度计数用同一个口径。按 03「识别信息只给管理员」的边界，只有管理员可以用：普通账号带这个参数返回 403，前端也不显示这一行。取值不在六个状态里时返回 400。URL 参数是 `ident`；和关键字一样不算智能视图的条件，应用视图时清空。
- **挑中排除候选**：词汇表把排除候选定义为「不得再被识别为该系列」，所以暂时按这个定义拒绝（409），弹窗里标为「已排除」，按钮置灰。是否改为「所有者亲手挑中时解除排除」，已列为待用户决定。
- **07 留下的端到端用例**：「新来源的识别到达时重算公式评分」由 `TestOwnerReplacementKeepsWhatOtherSourcesBroughtAndRecomputesTheRating` 在 bootstrap 接缝上覆盖（所有者追加 AniList 后评分从 6 变为 7）。

UI 截图（不进仓库）：

- `/tmp/claude-1000/-home-alex-workboox-yomifolio/22a53a57-0164-4ff1-ab9d-d2ae36a5b907/scratchpad/screenshots/13-detail-identification-panel.png`：详情页识别区，每条结论带「撤换」
- `/tmp/claude-1000/-home-alex-workboox-yomifolio/22a53a57-0164-4ff1-ab9d-d2ae36a5b907/scratchpad/screenshots/13-search-modal-replace.png`：从识别区撤换时的手动搜索：撤换提示、当前识别与已排除标记、按识别策略的预览
- `/tmp/claude-1000/-home-alex-workboox-yomifolio/22a53a57-0164-4ff1-ab9d-d2ae36a5b907/scratchpad/screenshots/13-search-modal-excluded.png`：选中排除候选时按钮置灰并说明原因
- `/tmp/claude-1000/-home-alex-workboox-yomifolio/22a53a57-0164-4ff1-ab9d-d2ae36a5b907/scratchpad/screenshots/13-library-identification-filter.png`：资料库筛选里的识别状态行和已选条件

这台机器上的无头 Chrome 访问不了本地 HTTP 服务（连代理也不行，只能打开 file:// 和 data:）。所以截图是在 jsdom 里渲染真实组件、套上构建产物的样式、再经 file:// 截的，数据是模拟数据。

