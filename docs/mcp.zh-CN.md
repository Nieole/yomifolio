# MCP 接入配置指南

[中文首页](../README.zh-CN.md) · [English](mcp.md) · [功能与客户端](features.zh-CN.md) · [部署与安全](getting-started.zh-CN.md)

MCP 让外部 AI 客户端以**独立受限凭据**读取和改写资料库：能力与资料库在服务端逐次授权，浏览器 Cookie 不能当 agent 凭据。这是静态 Bearer 凭据 + Streamable HTTP，**不是 OAuth**，没有授权服务器发现或交互式授权流程。

本文只讲配置顺序与排错；安全边界与已知限制见[功能与客户端](features.zh-CN.md)和根 `README.zh-CN.md` 的「受限 MCP 访问」。

## 配置顺序

1. 配置 `mcp.public_url`——在**设置 → MCP 访问 → 服务器配置**里填，或写进 `config.yaml`（见下）。
2. 在同一页签发凭据。
3. 在客户端填配置，先用一个只读工具跑通。

设置页把第 1–3 步浓缩成一张可折叠的说明卡：推导出的 `/mcp` 地址、下面两段客户端配置，以及不离开应用就能打开本文档的按钮。

## 1. 服务端：`public_url`

在**设置 → MCP 访问 → 服务器配置**里填（即时生效），或写进 `config.yaml`（位于**当前工作目录**，不在 `data/` 内）：

```yaml
mcp:
  public_url: "https://library.example.com"   # 客户端实际要连的来源
  allow_lan_http: false
```

- 客户端连的是 `<public_url 的 origin>/mcp`；`/mcp` 路径由服务端和 stdio 辅助进程各自追加，不要写进 `public_url`。
- 两项都是**即时生效**（热重载），改完不必重启。
- `public_url` 为空时 `/mcp` 直接返回 **404**，这是最常见的「客户端连不上」原因。
- 请求的 **`Host` 必须与 `public_url` 的主机（含端口）一致**（不区分大小写）。反向代理必须原样保留这个 authority：把 Host 改写成 `127.0.0.1:8080` 会让所有 MCP 请求被拒。
- `public_url` 用 `https` 时，服务端要求这次请求确实是 TLS：直连 TLS，或由 `server.trusted_proxies` 里登记的代理报告 `https`。
- `public_url` 用 `http` 时，必须同时设 `allow_lan_http: true`，且主机是字面的 `localhost` 或私网 IP。**在本机调试也一样要开这个开关**，并且只接受私网/回环客户端——内网流量被截获即可泄露密钥。

## 2. 签发凭据

**设置 → MCP 访问 → 签发凭据**，填写关联用户、名称、资料库与能力。

- **资料库不选不等于全部**：空选择没有访问权。
- 五项能力互不蕴含：`查询`(read)、`元数据写入`(metadata_write)、`扫描控制`(scan_control)、`内部 LLM`(internal_llm)、`清理`(cleanup)。
- 密钥**只在签发时显示一次**，请立即保存；之后只能重新签发。
- 签发时可选任意用户；凭据照该用户当前权限与限制工作，普通用户不会因此获得管理或付费模型权限。
- 修改或重置该用户密码会撤销其全部 MCP 凭据。撤销后队列中的工作不再派发，运行中的工作在安全点停止，**已提交的修改不回滚**。
- 收紧能力或资料库同样即时生效：客户端下一次列工具时就看不到被收回的工具。清单只公开凭据当前可用的工具；调用清单里没有的工具仍会被同一句统一拒绝挡住——每个工具都自己鉴权，清单不是访问边界。

## 3. 客户端配置

字段名随客户端而异（有的用 `type: "http"`，有的用 `httpUrl` 或 `transport`），要求只有四条：URL 指向 `/mcp`；只带一条 `Authorization: Bearer <密钥>`；不要把密钥放进查询串（带查询串的请求一律拒绝）；不要同时发出浏览器 Cookie。

### 直接连 HTTP 端点

```json
{
  "mcpServers": {
    "yomifolio": {
      "type": "http",
      "url": "https://library.example.com/mcp",
      "headers": { "Authorization": "Bearer 粘贴签发的密钥" }
    }
  }
}
```

若客户端会发 `Origin`，它必须等于 `public_url` 的 origin，否则被拒。

### 只能启动子进程的客户端（stdio）

一个可独立构建的辅助进程即可接入，它不需要数据库、配置或藏书访问权，只连接已运行的服务：

```bash
go build -o mcp-proxy ./cmd/mcp-proxy
```

```json
{
  "mcpServers": {
    "yomifolio": {
      "command": "/opt/yomifolio/mcp-proxy",
      "args": ["--url", "https://library.example.com"],
      "env": { "YOMIFOLIO_MCP_TOKEN": "粘贴签发的密钥" }
    }
  }
}
```

- 私网明文再加 `--allow-lan-http`（与服务器端的 `allow_lan_http` 对应）。
- 密钥只从环境变量读取，命令行与 URL 都不接受；加载后即从进程环境移除。保护客户端配置/环境文件，环境变量传递挡不住本机特权进程。
- 不支持关闭 TLS 校验、跟随重定向或走环境 HTTP 代理；上游只支持本服务的 stateless JSON-response 端点，**不是**任意 SSE/有状态会话服务器。
- stdin EOF 即退出，不发送业务取消；断线不自动重放，重连后请查询已接纳的运行（丢失响应不等于未接纳）。

## 4. 工具与能力对照

客户端只会看到凭据当前允许的工具。

| 能力 | 工具 |
| --- | --- |
| `read` 查询 | `library_get_series`、`library_find_series`、`library_list_books`、`library_get_book_clues`、`library_get_enrichment_state`、`library_get_metadata_operation`、`library_list_metadata_operations`、`library_get_series_cover`、`library_get_page_thumbnails`、`library_search_sources`、`library_get_source_search`、`library_get_match_proposal` |
| `metadata_write` 元数据写入 | `library_enrich_metadata`、`library_enrich_metadata_batch`、`library_undo_metadata_operation`、`library_propose_match` |
| `scan_control` 扫描控制 | `library_start_scan`、`library_list_runs`、`library_get_run`、`library_wait_run`、`library_cancel_run` |
| `internal_llm` 内部 LLM | `library_assess_match`、`library_get_model_operation` |
| `cleanup` 清理 | `library_start_metadata_cleanup`、`library_get_metadata_cleanup`、`library_cleanup_metadata_operation` |

搜索来源需要出网，因此 `library_search_sources` 与 `library_get_source_search` 会访问外部来源站；其余工具只在本机工作。

运行类工具只作用于本凭据自己的运行，并按该运行所属能力重新鉴权。任何一项能力都会产生运行，因此 `library_get_run`、`library_wait_run`、`library_cancel_run` 随任一能力列出；`library_list_runs` 仍要有 `scan_control`、`read`、`cleanup` 之一。

## 5. 依次验证

1. 不带凭据请求 `/mcp` → **401**。看到 404 说明 `public_url` 为空或没生效；502/503 是代理没通。
2. 带错误的凭据 → **401**；用公网 HTTP 访问私网配置 → **403**。
3. 让客户端列工具，只应出现该能力对应的工具；改了能力后应立即变化。
4. 调 `library_get_series`，参数 `{"series_id": 1}`，应返回已授权资料库内的身份字段。
5. 用未授权的资料库尝试 → 统一返回「委派访问被拒」，不区分“不存在”与“无权”，这是刻意不泄露存在性。

## 6. 已知限制

- 等待类工具最长 30 秒；超时或断线**不取消**已接纳的工作，取消要用 `library_cancel_run`。
- `library_start_scan` 不是幂等的：排队中的扫描可能合并，否则会接受新的一次运行。
- `internal_llm` 的额度是「agent 转述的预算」，准入计数在请求前原子取用、失败不退款；它不是货币意义上的硬上限。
- 服务重启保留中断历史，但不会自动重放已派发的工作。
- 已实测的是官方 Go SDK v1.8.0 客户端与 protocol `2025-11-25`；其他客户端与协议版本不作兼容承诺。
- 若 `public_url` 走 HTTPS 反向代理：保留 Host、清除客户端转发头、正确设置协议与真实客户端 IP，且只把代理地址加进 `server.trusted_proxies`。不要在 URL、代理日志或调试抓包里记录密钥。
