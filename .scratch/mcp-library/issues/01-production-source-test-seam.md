# 01: 打通生产组装的外部来源测试替身

**What to build:** 后续功能可通过生产组装的完整服务接受确定性来源数据，不访问真实站点或产生模型费用；保持正常启动、恢复、关闭及现有业务行为不变。这是唯一前置预重构，不另造一套业务测试宿主。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] 沿现有 bootstrap 集成测试方式使用真实临时 SQLite、资料库、归档和图片，只在外部元数据与 LLM 来源边界注入替身，不替换应用用例、数据库或任务引擎。
- [x] 测试可控制来源返回值、错误及阻塞时机，供以后从公开接口验证取消、竞争与费用边界，不依赖盲等或内部调用顺序。
- [x] 通过生产服务公开 HTTP 验证替身确实由同一应用运行时使用；无注入时仍使用现有生产来源，启动与恢复失败不暴露半组装服务。
- [x] 不增加新的产品工具、权限或工作流，不机械重构无关调用方，不静默修改现有来源行为。
- [ ] 生产组装、共享任务队列、资源释放和 shutdown/drain 回归通过；后端测试、格式与 lint 通过。

## Answer

票01实现完成；最后一项全量门禁尚未全绿，保留未勾选，不代表完整 effort 或 MCP 功能交付。

- 工作树：`C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-01`，分支 `implement-mcp-source-seam`，实测与 `implement-mcp-library` 的 merge-base 为 `8197246c4bf90137096d90f26a837d59668c92ff`。未修改父工作区，未提交、合并或操作父计划。
- `bootstrap.Options.Sources` 在生产装配前注入 `api.ExternalSources`。Metadata 工厂负责按来源键 fetch/search（包括 LLM 元数据）；AI 工厂负责分组、推荐和连接测试。未覆盖的工厂仍调用原生产构造器，按调用时配置创建客户端。没有替换数据库、应用用例或任务引擎，也没有增加测试专用业务宿主。
- 新增生产 HTTP 集成测试：真实临时 SQLite、资料库、CBZ/PNG，公开读取页面；分页候选返回；阻塞异步刮削并查询同一 HTTP 运行队列，释放来源后公开查询提案；确定性来源错误；LLM 连接返回；公开取消分组后来源 context 收到取消、HTTP 运行进入 cancelled 且无提案。使用来源 channel 屏障和有截止时间的公开投影轮询，不以盲等或内部调用顺序判断完成。
- TDD：先运行元数据切片，因缺少 `Options.Sources` / `api.ExternalSources` 编译失败，再实现并通过；随后 LLM 切片因缺少 AI 工厂失败，再实现并通过。测试运行中按现有公开契约修正分组/取消的预期响应码为 202。

实际验证（共享父树 GOCACHE，独立本树 GOTMPDIR；缺失的忽略产物 web/dist 已从指定父树拷贝）：

- `go test ./internal/bootstrap -run 'TestProduction(Source|LLM)' -count=1`：通过。
- `go test ./internal/bootstrap ./internal/application/runtime ./internal/application/intelligence ./internal/metadata ./internal/task -count=1`：全部通过，包含既有共享队列、恢复失败不开放宿主、关闭与 HTTP drain 回归。
- `gofmt -l ./cmd ./internal`：输出为空；`git diff --check`：通过。
- 第一次 `go test ./...` 超过工具 240 秒限时；重跑获得完整结果但失败：API 的 `TestRelocationPolicyRuntimeFailureRecoversThroughFreshManager` 未到 completed；diskjobs 的 transfer pause 测试未到 mid-book barrier。
- 为降低并发资源争用，再执行 `go test -p 1 ./...`：diskjobs 及其余包通过（包括 bootstrap/runtime），API 两个迁移子测试失败：`TestRelocationPolicyPersistenceFailuresCompensateOrRemainBlocked/compensation`、`TestRelocationPolicyCrashRecoversFromActualDurabilityBoundaries/runtime_apply`，均为 Windows source → target rename `Access is denied`。没有改动这些测试或迁移实现；未以此断言基线必然通过或必然失败。
- `golangci-lint run`：使用本树独立 cache，退出码 7。typechecking 报 `cannot decode "internal/goarch", export data version 5 is greater than maximum supported version 4`；工具虽打印 `0 issues.`，本次 lint **未通过**。环境为 Go 1.27.4、golangci-lint 2.12.2（built with go1.27.2）。缓存已清理，没有将工具产物加入交付。
- 详细日志保留于本树忽略目录 `.tmp/ticket01-{backend,backend-serial,focused,lint}.log`。没有前端、SQL 或生成响应契约改动，未执行前端门禁或重新生成代码。

限制与交接：需父代理在可用 lint 工具链及稳定 Windows 文件句柄环境下补齐全量门禁。替身的 context 取消仍是协作式；未增加强制中止、不响应取消客户端的超时包装或费用保证。未来测试若要屏蔽全部外部来源，应同时传入 Metadata 和 AI 两个工厂。所有改动保持未提交。
