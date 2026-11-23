# 10: 改善自动封面并与文字一起生效

**What to build:** agent 可在比较质量后替换系列的自动封面；如果本次同时补充文字，两者在该系列内全部成功或不生效，保留旧图片以便安全撤销，人工封面始终受保护。

**Blocked by:** 05 读取受限封面和书页缩略图；08 安全补充元数据并支持追溯和撤销。

**Status:** done

- [x] 仅允许改善归档自动生成的封面；手选、上传及受当前锁保护的封面不可替换，提交边界再次检查授权、封面归属和预期当前状态。
- [x] 图片先准备，随后与本次文字一起生效；图片获取或准备失败、字段冲突、锁变化和权限失效均不导致系列半提交。
- [x] 选定并记录数据库与图片切换的故障一致性方案，包含资源清理及服务重开行为；不以单个数据库事务代称跨存储原子性，不回滚外部请求或费用。
- [x] 保留必要旧图片与操作前后状态，网页与 MCP 记录可核查；在当前内容和锁仍允许时，撤销一起恢复涉及文字及旧封面。
- [x] 对后续人工换封面或相关文字变化安全报冲突，不覆盖新内容；不同系列依然允许部分成功。
- [x] 通过实际公开封面读取和系列查询验证成功、准备失败、提交冲突、撤销及故障后重开，不仅检查内部引用或存在的图片文件。
- [x] 保留人工封面和内容句柄回归；完善管理页差异呈现，后端及受影响前端门禁和生成物检查通过。


## Comments

- Final closure: `3efc7dba` includes the persistent pre-preparation conflict repair for `rc_0b83bbc6`, on top of atomic covers,09 stable receipts and transport cancellation. Independent re-review approved the fix and public HTTP/stdio regressions; full combination/backend and frontend gate evidence remains in acceptance. Earlier pending notes below are historical. Explicit undo-material cleanup remains13, not automatic retention.

- 独立实施树 mcp-library-10 / mcp-atomic-covers，确认 HEAD 与 merge-base 均为 c4540ce4；仅本树未提交改动。沿用已批准 TDD 接缝，采用不可变私有材料 + 同事务引用发布，详见 acceptance/10-atomic-covers.md。保持 ready-for-agent，等待父集成与独立 review。

- 接续 usage-limit 中断的既有 delta；核对历史红/绿日志后扩充公开系列读取、封面切换后的文字失败/savepoint 回滚、失败幂等与重开审计，以及管理页撤销图片方向/失败结果。当前实际完整门禁和限制已记录于 acceptance/10-atomic-covers.md；仍为 ready-for-agent，不自行 done，未改 09/11/13 或其他树。

- 独立审阅 transport 取消阻塞已窄修：单项/批量入口合并真实请求上下文，贯穿封面准备与提交。真实 TCP 断连、stdio EOF 卡卷 IO 红测后转绿；三次连续准备验证读资格/IO等待/图片及cover槽释放，批次已提交文字与审计幂等保留。最终两轮定向回归、完整后端、lint/format/tsgen/diffcheck 与定向历史UI 8测通过；实跑日志及09组合限制见 acceptance10 新节。仍 ready-for-agent，等待父 fresh 独立重审，不自行 done。


- canonical 独立 review rc_0b83bbc6 窄修已实施，起点 81229c6f；准备前 stale cover/lock/人工来源与相关字段冲突进入最终当前授权事务持久 conflict，不生成无用材料。真实 HTTP 首次收据、恢复后 HTTP/stdio 同键重放、异载荷拒绝及重开公开审计有回归；非法页码/比较声明/源身份不落收据。最终两轮定向、完整 backend、lint/format/tsgen 与历史 UI 定向均通过，证据见 acceptance10 新节。保持 ready-for-agent；全部未 stage/commit，等待父提交与 fresh request_review，不自标 done。
