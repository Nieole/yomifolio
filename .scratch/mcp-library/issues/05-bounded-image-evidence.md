# 05: 读取受限封面和书页缩略图

**What to build:** agent 可以读取目标系列的封面和自行选定的少量书页缩略图以辅助识别，不能借此下载整本书或读取任意文件。

**Blocked by:** 04 筛选待整理系列并读取本地识别线索。

**Status:** ready-for-agent

- [x] 通用 MCP 客户端能读取授权范围内封面和指定页缩略图；工具契约明确每次页数、图片尺寸及整体响应体积硬上限。
- [x] 越界页码、过多页、过大响应、无效对象与非授权资料库请求得到明确结果，不回退为原始归档或任意文件下载。
- [x] 复用既有内容访问资格与句柄释放，成功、失败、取消及客户端断连均不遗留归档借用或文件资格。
- [x] 上限是工具响应预算，不照搬归档解析器大条目安全上限；对于大书仍只获取有限证据。
- [x] 在真实临时图片和归档上通过公开客户端验证内容、限制和授权，保留内容句柄及路径迁移相关回归。
- [ ] HTTP 入口可验收完整行为，stdio 沿同一工具契约转发；后端门禁通过。

## Comments

- Worker 05 claimed implementation in dedicated mcp-image-evidence worktree at 0d9ef6ce; status remains ready-for-agent pending parent integration/review. Acceptance seam: production bootstrap + generic MCP client (HTTP and actual stdio proxy), supplemented by public BookContent resource-lifetime regressions.

- 实现与定向验收已完成；详细红绿、接口预算、真实客户端和资源释放证据见 `../acceptance/05-bounded-image-evidence.md`。一次全后端通过；随后新增断连回归发现并修正 SDK 图片上下文绑定，最终相关包/格式/lint 通过。最后一项组合门禁留父在最新集成树完成，不提前 done。
