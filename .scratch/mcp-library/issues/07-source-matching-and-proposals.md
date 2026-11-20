# 07: 检索多来源候选并形成有依据的提案

**What to build:** agent 依据目标系列的本地线索搜索现有来源、比较候选与版本，在确认匹配后形成带来源和简短理由的提案；不确定时只向用户报告候选和疑点。

**Blocked by:** 04 筛选待整理系列并读取本地识别线索；06 发起和控制授权内的后台运行。

**Status:** ready-for-agent

- [x] 搜索现有已配置可用元数据来源并读取多个候选详情，不限 Bangumi；本票不启用内部 LLM 或产生模型调用。
- [x] 返回支持身份和版本比较的证据，agent 可按字段组合已确认同一系列的来源，记录字段出处和简短、可核查摘要，不存模型内部推理。
- [x] confidence 不被包装成身份匹配概率；不自动把首个候选视为确认结果，身份或版本不明时不写入元数据、不积压未定候选。
- [x] 已确认匹配可形成和读取待写入提案；明确处理既有提案复用、没有有效差异及被锁字段的情况，不谎报为本次新建提案或已经写入。
- [x] 保留后续补充与审计所需的匹配依据及字段证据；来源原始数据中的封面、链接不被冒称为现有提案已经支持的裁决字段。
- [x] 耗时检索复用委托运行及当前授权检查；来源失败、取消和部分可用结果有明确可观察状态。
- [x] 生产组装服务使用确定性外部来源替身验证多候选、歧义、字段组合、提案复用和无差异；保留既有普通提案行为，后端门禁通过。

## Comments

- Worker claim: `mcp-source-proposals`, dedicated `mcp-library-07` worktree, HEAD/merge-base `0d9ef6ce260c31d90d0d71dc82ace22ef85c7a8f`; changes intentionally uncommitted, status remains ready-for-agent pending parent review/integration.
- Evidence: [acceptance](../acceptance/07-source-matching-and-proposals.md). Search snapshots are bounded and temporary; confirmed field evidence belongs to persistent proposal raw_payload. No metadata adjudication/write, operation audit/undo or internal LLM implemented.
- Independent merger review: parent-started merge HEAD `18d10fa0` / MERGE_HEAD `ff5c177e`; CHANGELOG text resolved retaining both additions, incoming watcher fix/evidence unchanged, nothing staged/committed. One complete combined backend gate (outer 1800s), two MCP/stdio/run-error rounds, related packages, gofmt, lint and tsgen drift checks passed. **Approval blocked:** existing 07 SearchSources returns next_offset=1010 at the valid offset=1000/limit=10 boundary, but rejects that returned cursor; production-client reproducer failed as expected. See acceptance for exact commands/logs and 08/12 reuse pointers. No feature fix made; status remains ready-for-agent for parent follow-up/integration/closure.
- Fresh-context pagination-only remediation: added production bootstrap + generic SDK regression, observed behavioral RED for 1010/1002/1001/1008 cursors, then GREEN with optional per-source `truncated=true` and no unreachable next_offset. Preserves limit<=10, aligned offset<=1000, upstream total and all candidates; no clamp/prefetch/budget expansion. Covers limits3/7/9, followable normal/boundary pages and source-total tails. All MCP tests twice, bootstrap/delegated/metadata/proposal/runtime packages, gofmt, lint and tsgen passed; full backend not repeated after merger's prior combined pass. See acceptance's pagination remediation section/logs. Existing watcher/uncommitted/merger content preserved; CHANGELOG index still UU, no stage/commit/merge. Remains ready-for-agent pending parent finishing merge, integration commit and request_review.
