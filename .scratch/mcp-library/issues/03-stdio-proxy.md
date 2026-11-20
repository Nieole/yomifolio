# 03: 通过 stdio 辅助进程连接同一服务

**What to build:** 支持 stdio 的通用 MCP 客户端可启动本机辅助进程，通过它访问已经运行的程序，取得与 HTTP 直连一致的工具结果和权限保护。

**Blocked by:** 02 通过受限凭据连接 HTTP MCP 并读取系列。

**Status:** ready-for-agent

- [x] 真实客户端完成 stdio 初始化、工具发现及至少一次授权系列查询；代理对后续服务工具扩展无需另造业务实现。
- [x] 辅助进程仅连接正在运行的服务，无数据库配置或访问资格也能工作，不启动另一套应用运行时。
- [x] 上游使用独立凭据，权限收紧和撤销即时影响后续调用；上游明文连接服从与 HTTP 入口相同的显式局域网边界。
- [x] stdout 仅有协议内容，日志不污染协议；上游不可用、授权失败和非法输入有明确失败结果。
- [ ] 真实子进程烟测覆盖 EOF、正常退出、断连和重连，不泄漏进程或连接。已受理长运行的断线后行为随运行能力交付后补充验收，不能把断线映射成业务取消。
- [x] 对接生产组装的同一服务测试，不复制业务测试环境；后端门禁通过，记录可复现的接入方式及已核验兼容边界。

## Comments

- Worker 03: implementing in mcp-library-03; status remains ready-for-agent until parent integration. Seam: real stdio subprocess + generic SDK client against bootstrap.Open; long-run disconnect acceptance deferred to 06/14.
- Implementation and transport acceptance complete; evidence: `../acceptance/03-stdio-proxy.md`. Full backend rerun passed, formatting empty, lint 0 issues. First full run had two external-transfer timing failures, preserved honestly in evidence (not labeled baseline).
- Fifth checkbox intentionally remains open only for **06 → 14**: accept a long-running run, disconnect, reconnect/query the same run and distinguish explicit cancel. Current real subprocess tests prove clean EOF (including an in-flight transport request), process reaping and socket/new-process reconnect, not server accepted-run survival.
