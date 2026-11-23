# 11: 裁决已有提案并关闭剩余差异

**What to build:** agent 可在受保护策略下处理已有提案，补充获准内容后放弃其余差异；用户能区分这种结果与全部拒绝，已成功应用的内容不被撤销，终态提案不能被继续写入。

**Blocked by:** 09 校正所选系列的作者关联。

**Status:** ready-for-agent

- [x] 读取和应用已有提案时执行当前用户、凭据、资料库及元数据写入授权，复用已交付补充、标签和作者策略，不借普通提案的整组替换语义覆盖受保护内容。
- [x] 提供补充后关闭剩余差异的完整操作，确定可持久区分的状态或呈现；网页与 MCP 可分辨全部拒绝、部分应用尚待裁决和补充后关闭。
- [x] 关闭或拒绝不撤销先前已应用字段；单系列实际变更继续产生可追溯记录及符合条件的撤销，而非把提案状态回退当作内容恢复。
- [x] 严格遵守 mm-adr-0011，在实际写入事务中重读待裁决状态、当前值、锁和已消费字段；终态竞争失败不提交元数据、提案字段或来源沿革。
- [x] 并发部分应用不得重复消费字段，重放和关闭竞争有明确公开结果；不改变既有人工提案使用方式。
- [x] 从生产服务验证旧提案输入、作者保护、成功后关闭、先拒绝后应用及部分应用竞争，保留现有真实事务回滚守卫；网页呈现不误报为全部拒绝。
- [ ] 后端与前端门禁及受影响生成物检查通过，更新裁决长期契约但不削弱有效 ADR。

## Comments

- 在专属 mcp-library-11 / mcp-proposal-adjudication 实施；保持 ready-for-agent，未提交，等待父独立 merger/review。验收证据归 acceptance/11-existing-proposal-adjudication.md，勾选仅代表实测，不代表父批准。完整后端唯一一次运行在 TestMCPStdioSaturatedEOF 的代理构建处失败，最终门禁项保持未勾；未盲重跑或放宽测试。

- Fresh merger（7a082c16）：完整backend本轮一次通过、定向两轮及前端/生成/lint通过；但真实HTTP与stdio均复现撤掉read保留metadata_write后同键裁决重放仍返回完整收据。首项重新打开，交父freshfix，不自行改业务代码；当前BLOCKED，详见acceptance的Fresh章节。唯一代码增量仅增加饱和EOF测试构建elapsed/context/error/output诊断，不改预算或测试条件。

- Freshfix（7a082c16，未提交）：按持久裁决收据在同一retry事务重查当前Read；永久HTTP/真实stdio撤权红绿、最终定向两轮及相关六包全测通过，原摘要/终态重放保留。首项据实勾回；唯一一次完整backend在配置并发编辑save的Windows rename Access is denied失败，非本轮stdio失败，不冒称baseline、不盲重跑；最终门禁框仍不勾，ready-for-agent，待父独立review/门禁。历史页仅定向10tests通过，未重跑merger已绿的完整frontend/build/lint。详见acceptance Freshfix节。

- Final fresh merger（HEAD6311bca3 + 父预置MERGE_HEAD5e7dfaf0）：逐块解决9项正文冲突，两票增量与read freshfix均保留；定向两轮、唯一一次完整backend（38包/0fail）、完整frontend121files/1158tests、build/lint、gofmt/golangci-lint、48types tsgen漂移和diff检查全部通过，最终门禁据实勾选。当前组合merge-ready，保持ready-for-agent而非done；索引UU留父stage，父canonical request_review。13另树pending离页恢复后续fix未纳入，不宣称已验收；历史rename故障未定位、不归baseline、不宣称修复。详见acceptance末节。

- 最终parent ef44小增量（HEAD0a634bb9 + 预置MERGE_HEADef44f9b6，含13 approved fix727820e9）：仅CHANGELOG正文冲突已逐块保留双方；自动合并契约/48types+retry_request_key/双语UI/hook/完整remount tests与11三态/read事务重鉴权均核查保留。新定向两轮、唯一一次完整backend38包/0fail、完整frontend121files/1162tests及历史页16tests、build/lint/gofmt/Go lint/tsgen无漂移/diff全部PASS，证据 `.tmp/11/parentef44/`，详见acceptance末节。当前merge-ready，保持ready-for-agent；13已done不改关闭状态。索引UU CHANGELOG留父stage/提交，并对11发canonical request_review；历史Windows rename未定位，不宣称修复。

- Fresh narrow fix（HEAD ab462e9c，未提交）：已复现并集中修复普通网页 fill_empty partial 的 provenance 接受事实遗漏；真实 HTTP/stdio 红绿、最终 transport 双轮及相关定向/全包通过。11 仍 ready-for-agent，完整门禁及当前结论见 acceptance 最新 Fresh narrow fix 节；等待父 fresh 独立 review，不标 done。

- 本轮最终门禁：定向双轮/最终 HTTP+stdio 双轮、相关八包全测、完整 frontend121files/1162tests、build/lint/gofmt/Go lint/tsgen无漂移/diff均通过；唯一一次完整backend失败于 relocation occupied 与 derived_effects 两处Windows rename Access is denied（37包PASS，仅apiFAIL），未盲重跑或归因baseline，最终门禁框重新取消。修复待父fresh独立review和故障诊断，不称merge-ready；详见acceptance最新节与 `.tmp/11/acceptedfix/`。
