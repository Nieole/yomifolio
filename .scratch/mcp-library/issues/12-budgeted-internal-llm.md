# 12: 按本次额度调用内部 LLM

**What to build:** 具有内部模型能力的 agent 可转述用户对本次操作的调用次数额度，使用程序已有 LLM 来源辅助检索与整理；服务端累计用量，耗尽停止模型调用并汇报，非模型工作仍可继续。

**Blocked by:** 07 检索多来源候选并形成有依据的提案。

**Status:** done

- [x] 内部 LLM 必须同时满足当前用户权限、凭据独立模型能力及本次明确次数额度；缺少任一条件均不调用，其他获准查询仍可使用。
- [x] 按 mm-adr-0016 草案记录的已选边界，接受 agent 转述额度，不新增受信任人工审批渠道，也不把计数当作独立用户批准证明；agent 声明不能扩大凭据权限。
- [x] 明确操作归属及额度记账契约，服务端跨请求、并发调用和断线重连累计，不得通过重复声明、分拆请求或重试重置已经使用的额度。
- [x] 确定失败调用、重试及服务重启时的计数语义并测试；不因恢复或重放静默重新发送付费调用，不冒充可以准确限制货币成本。
- [x] 每次实际调用前重新鉴权，安全检查点服从撤权；额度耗尽不再发起模型请求，返回实际用量和未处理项，允许非模型操作继续。
- [x] LLM 可辅助身份判断，但生成分数不参与综合评分，不保存内部推理，不将未解决歧义写入系列。
- [x] 生产服务使用可控模型替身验证无额度、无能力、并发耗尽、跨请求和重启记账、撤权及部分工作继续；不用真实计费验证限额。
- [x] 后端门禁及受影响契约生成检查通过，明确记录用量保证与委托信任的区别，ADR 不因实现者自行判断而转 active。

## Comments

- Final closure: `1e5995b0` integrates the pruning correction for `rc_69896dd6`. Independent re-review approved durable projections, unchanged current authorization, HTTP/stdio regressions and the full backend/format/Go1.26.10 lint gates. Historical pending implementation notes below are superseded; ADR0016 remains draft.

- Worker mcp-model-budget: HEAD/merge-base verified b3083e4b; implementing uncommitted. Confirmed seam: production bootstrap + generic MCP client + deterministic external AI factory, plus production provider HTTP boundary for no hidden reissue. No UI. Budget operations use a separate model-budget namespace/table, not metadata audit.

- 接续完成：保留原未提交实现；复查发现并以真实TLS/h2 REFUSED_STREAM测试证明Do外围守卫会漏算透明重试（2次POST），仅在额度路径去除GetBody重放配方后变为1次。最终完整后端、gofmt、lint、tsgen漂移检查通过；具体证据、保守消耗/委托信任边界及未覆盖实验见 `../acceptance/12-budgeted-internal-llm.md`。仍ready-for-agent，等待父级审阅/集成，未stage/commit。

- 独立merger：HEAD d6c92649 + 待合入46d06126，merge-base b3083e4b。逐段合并CHANGELOG/api规格，保留05+07+12全部工具、最终JSON256KiB、来源默认及取消/断连边界。复用原fixture补真实SDK+stdio helper跨进程重连预算验收；metadata/预算及MCP各两轮、一次组合全后端（外层1800s）、Go1.26.10 gofmt/lint/tsgen漂移/SQLASCII均实际通过。12内容merge-ready，仍ready-for-agent；父级stage并完成已有merge，05共享RAR锁取消窄修与08不归本票。ADR0016仍draft。详见acceptance12独立检查及真正剩余门禁。

- request_review rc_69896dd6：在 HEAD 0018c90c 上修复正常历史retention后预算投影不可读；仅 task.ErrRunNotFound 返回 history_unavailable，当前授权/归属不依赖已裁剪run，原用量/结果及不可重申重置语义保留。生产HTTP/stdio裁剪红绿、重开、拒权反例、全后端/MCP两轮/lint/生成漂移实际通过，详见acceptance12顶部。工单仍ready-for-agent，未stage/commit，等父集成后立即独立重审；不编辑05/08。
