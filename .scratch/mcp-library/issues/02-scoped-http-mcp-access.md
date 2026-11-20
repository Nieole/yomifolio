# 02: 通过受限凭据连接 HTTP MCP 并读取系列

**What to build:** 管理员通过网页为真实用户签发受限独立凭据，通用 MCP 客户端经 Streamable HTTP 完成连接并读取授权资料库内的系列；管理员能收紧和撤销凭据，普通用户不能借凭据取得额外权限。

**Blocked by:** 01 打通生产组装的外部来源测试替身。

**Status:** done

- [x] 核验并记录 SDK、协议及独立凭据方案的实际兼容边界，完成客户端初始化、能力发现和最小只读系列调用；不把静态凭据宣传为未实现的标准交互授权。
- [x] 网页管理员可创建、查看限制、收紧和撤销用户关联凭据，支持资料库与查询、元数据写入、扫描控制、内部 LLM、清理能力的独立限制；尚未实现的工具不因此获得可用实现。
- [x] 服务端强制当前用户权限与凭据限制的交集；覆盖未认证、无效及撤销凭据、普通用户写入越权和直接猜测系列标识的访问。现有网页目录读取权限不改变。
- [x] 本票已提供的每次调用使用当前授权；修改密码撤销该用户 MCP 凭据，网页 Cookie 不作为 agent 长期凭据，agent 不能修改自身授权。
- [x] 允许显式配置局域网明文 HTTP 并清晰说明截获风险，公网使用 HTTPS；不隐式扩大明文适用范围。
- [x] MCP 通过显式应用能力在生产资源组装中挂载，不直读数据库或复制运行时；其他协议继续工作。
- [x] 生产组装服务上用通用客户端及真实网页管理 HTTP 验证完整接入、授权变更和密码失效；补管理页交互测试。
- [x] 后端与前端门禁通过；涉及持久化及生成契约时同步生成物，凭据和授权生命周期的长期契约归还所属规格。

## Comments

- Implementation claim (mcp-scoped-http): merge-base and HEAD verified as e0da1f3ad8243d63643f892e918187aac5c3cfa6. TDD seams are the approved production bootstrap + real management HTTP + generic MCP SDK client, and Settings interaction tests. No parent worktree edits. Cached official Go SDK v1.8.0 is available for source/protocol verification; no blocked web fetch bypass.

- Implementation complete; all eight acceptance checks have evidence in `../acceptance/02-scoped-http.md`. Final complete backend suite passed; frontend complete suite passed with `--maxWorkers=2` (120 files/1146 tests), build/lint passed, Go formatting/lint and generation/notice checks passed. Intermediate baseline async UI/relocation wait failures and successful reruns are recorded, not suppressed.
- Independent merger verified all acceptance evidence and found no blocking defects; parent closes this implementation ticket with the integration commit. No wayfinding statuses used. Reusable authority is `delegated.Subject` + `Service.Authenticate/Authorize`; current protocol is only `library_get_series`. No later-ticket tools implemented.
- Mockups are in this worktree's `.thinkrail/context/mockups/mcp-02-before.html` and `mcp-02-after.html`; parent may copy/display them. SDK v1.8.0 source/cache and generic client `2025-11-25` were actually verified, without external-fetch safety bypass.
