# 06: 发起和控制授权内的后台运行

**What to build:** agent 可发起必要扫描，查询、等待和取消授权内运行；断线后能够重连查询。委托工作的权限在排队及执行期间持续有效，撤销权限不是只影响下一次新请求。

**Blocked by:** 02 通过受限凭据连接 HTTP MCP 并读取系列。

**Status:** ready-for-agent

- [x] 经 MCP 发起必要扫描、查询进度、受限等待及取消；耗时操作及时返回运行标识，列表和直接控制均执行当前资料库与能力授权。
- [x] 使用现有任务、运行、作用域和生命周期，委托工作持久保留必要的用户、凭据及权限归属，供后续来源和写入用例复用。
- [x] 撤销凭据、收紧权限或密码修改后，受影响排队工作不启动，运行中在安全检查点取消；定义供后续写入和内部模型调用使用的重新鉴权边界。
- [ ] 客户端 HTTP 或 stdio 断开不取消已受理工作，重连可查询相同运行；不使查询或取消跨越授权范围。
- [x] 明确操作标识、请求重试与现有排队合并的区别，以及进程重启后的授权和运行行为；不得把断线续查推导成无人批准的付费自动续跑。
- [x] 从真实服务通过可控屏障测试排队撤权、运行撤权、跨资料库控制拒绝和断线重连；已提交内容不因撤权自动回滚，已发请求不虚假宣称可追回。
- [x] 不新增常驻 agent、元数据维护状态机或第二套任务引擎；保留队列、终态及 shutdown/drain 回归，后端门禁通过。

## Comments

- Independent merger found no blocking server-side defect and verified HTTP disconnect/reconnect, authority checkpoints and lifecycle tests. Real stdio accepted-run continuation, reconnect to the same run and explicit cancellation remain joint 03/06 acceptance work; the corresponding checkbox stays open until that evidence exists.

- Worker06: implemented in mcp-delegated-runs; production bootstrap/MCP and controlled execution barriers are the agreed test seams. Changes remain uncommitted; Status remains ready-for-agent pending parent review/integration. Evidence: [06 acceptance](../acceptance/06-delegated-runs.md). Full backend rerun passed; first full run had two relocation failures, their focused rerun passed. Real stdio process smoke tests belong to parallel ticket03. Integration advanced to 34eb50dd836b49844b1b4578c4437ed0b43212d4 during work; parent must integrate safely.
