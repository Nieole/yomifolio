# MCP 接入配置指南

[中文首页](../README.zh-CN.md) · [English](mcp.md) · [功能与客户端](features.zh-CN.md) · [部署与安全](getting-started.zh-CN.md)

MCP 让外部 AI 客户端以**独立受限凭据**读取和改写资料库：能力与资料库在服务端逐次授权，浏览器 Cookie 不能当 agent 凭据。这是静态 Bearer 凭据 + Streamable HTTP，**不是 OAuth**，没有授权服务器发现或交互式授权流程。

本文讲配置顺序、各能力对应的工具、系列识别主循环与排错；安全边界见[功能与客户端](features.zh-CN.md)和根 `README.zh-CN.md` 的「受限 MCP 访问」。

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
- 四项能力互不蕴含：`查询`(read)、`元数据写入`(metadata_write)、`扫描控制`(scan_control)、`清理`(cleanup)。旧凭据上残留的 `internal_llm` 不授权任何东西、界面上也不显示，其余能力照常；下次收紧这张凭据时它会被一并去掉。
- 密钥**只在签发时显示一次**，请立即保存；之后只能重新签发。
- 签发时可选任意用户；凭据照该用户当前权限与限制工作。查询以外的能力都要求该用户是管理员，所以普通用户的凭据只能查询与挂备注，它的工具清单和初始化说明也只列这些工具。
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

客户端只会看到凭据当前能用的工具，初始化说明也只介绍这些工具。普通用户的凭据不论还带了什么能力，都只有 `read` 这一行。

| 能力 | 工具 |
| --- | --- |
| `read` 查询 | `library_find_series`、`library_get_series`、`library_list_books`、`library_get_book_clues`、`library_get_series_cover`、`library_get_page_thumbnails`、`library_identification_progress`、`library_get_identification_images`、`library_get_series_authors`、`library_find_duplicate_series`、`library_count_tags`、`library_report_series`、`library_get_metadata_operation`、`library_list_metadata_operations` |
| `metadata_write` 元数据写入 | `library_claim_series`、`library_submit_verdicts`、`library_research_series`、`library_replace_identification`、`library_correct_series_field`、`library_correct_authors`、`library_improve_cover`、`library_add_series_relation`、`library_undo_metadata_operation` |
| `scan_control` 扫描控制 | `library_start_scan` |
| `cleanup` 清理 | `library_start_metadata_cleanup`、`library_get_metadata_cleanup`、`library_cleanup_metadata_operation` |
| 任一能力 | `library_get_run`、`library_wait_run`、`library_cancel_run`、`library_list_runs` |

大多数工具只在本机工作。会出网的有：`library_research_series` 搜索来源站；`library_get_identification_images` 下载候选封面，只从各来源自己的图片域名拉取；`library_claim_series` 可能为过期的证据包安排后台刷新；`library_start_scan` 会为扫描新建的系列串联一次增量预取。各来源的搜索按 `identification.source_intervals_ms` 限速。

每个成功结果都带两份同样的 JSON：`structuredContent` 和一个文本块，只读文本的客户端也能拿到完整结果。图片工具把这段 JSON 文本放在最前，其后的图片顺序与其中 `images` 一致。每个工具的描述不超过 400 字符，末尾附一个可以照抄的 `Example:` 调用。

运行类工具只作用于本凭据自己的运行，并按该运行所属能力重新鉴权。任何一项能力都会产生运行，因此四个运行类工具有任一能力即可使用：只有 `metadata_write` 的凭据也能列出并跟进自己的重搜运行，已收回能力的运行则看不到。

已下线的工具既不列出也不应答：`library_search_sources`、`library_get_source_search`、`library_propose_match`、`library_get_match_proposal`、`library_get_enrichment_state`、`library_enrich_metadata`、`library_enrich_metadata_batch`、`library_assess_match`、`library_get_model_operation` 调用时得到协议的未知工具错误。作者校正与封面改善成了上表里独立的工具。

## 5. 系列识别主循环

agent 的任务是回答每个系列对应哪个来源的哪个条目。服务端按「系列 × 来源」记住这个结论，由它推导**识别状态**（`unprocessed` 未处理、`identified` 已识别、`awaiting_owner` 待人工、`needs_recheck` 待复核、`not_found` 查无此作、`owner_reviewed` 已审定），元数据由服务端自己补。

第一次领取之前，服务端要先备好**证据包**：搜索由服务端完成，搜索词从目录名和 ComicInfo 清洗生成。在侧栏资料库操作或**设置 → 维护工具**里点「预取识别证据包」；它在任务中心运行，可暂停、取消，再次发起时从还没备好的系列继续。此后每次扫描都会为新建的系列准备证据包。

有 `metadata_write` 时，主循环是：

1. `library_claim_series` 一次领取至多 20 个未处理的系列及其证据包：本地线索（目录名、书数、前几本的文件名、ComicInfo 的系列名、作者与年份），以及每个来源至多 3 条候选和可核对的比对信号（`title_matches`、`author_matches`、`source_volumes`、`local_books`），不是概率。这些系列租给本会话 30 分钟，并行的会话不会拿到同一批。
2. 按证据包逐个判断，再用一次 `library_submit_verdicts` 全部回答，每条附一句理由：`identify`（引用 `candidate_ids`，每个来源至多一个，可以同时识别到多个来源）、`not_found`，或带分类的 `escalate`（`ambiguous_candidates`、`uncertain_edition`、`missing_from_sources` 交给所有者；`directory_grouping`、`suspected_duplicate`、`tag_problem`、`protected_value_doubt` 只挂备注）。结果逐系列返回，被拒的带 `problem` 代码和下一步提示 `message`；同一 `request_key` 重试返回原结果。
3. 没有合适候选时，用 `library_research_series` 以自己的词重搜，新候选并入证据包，再按上一步判断。

空批会在 `empty_reason` 里说明原因：`all_processed` 表示停下；`prefetch_pending` 或 `all_claimed` 表示稍后再来。`library_identification_progress` 给出各识别状态的系列数和可领取的数量。凭据就是所有者的授权：agent 应直接写入，不必先请示人。

`identify` 之后服务端做的事：按可配置的来源优先级（Bangumi 优先，取中文名）只填**空**字段；用所有来源的评分算出缺失的评分；追加来源链接，以及通过票数阈值、屏蔽列表和每系列上限的标签；仍等于目录名的标题替换为来源标题；把该系列的待审提案结案。封面不变。每次补充都留一张收据（`operation_id`），可以用 `library_undo_metadata_operation` 撤销。

主循环之外：

- **纠正**（`metadata_write`）：`library_replace_identification` 撤换 agent 自己做错的识别，并撤回它带来、之后没被改过的字段；`library_correct_series_field` 改正来源给错、之后没人改过的标题、简介、出版社、状态或评分；`library_correct_authors`（先用 `library_get_series_authors` 读证据）和 `library_improve_cover` 校正作者和自动封面。每次纠正都留可撤销的收据。
- **受保护的值**：人工编辑过的值、ComicInfo 导入的值、锁定字段、人工封面和人确认的识别，agent 永远改不了，`library_undo_metadata_operation` 也拒绝撤销所有者自己操作留下的收据。只能上报：`protected_value_doubt` 挂备注。
- **质疑**（`metadata_write`）：觉得某个识别有误时，不论是人确认的还是 agent 自己做的，经 `library_report_series` 报 `disputed_identification`，系列进入待复核，识别和字段都不动。所有者随后在系列详情页撤回该识别（旧条目记为排除候选），或在「待人工」确认识别无误。agent 自己不能撤回识别。
- **巡检**（`read`）：`library_find_duplicate_series`、`library_count_tags`、`library_report_series` 让只读凭据也能巡检资料库、留下备注，但改不了任何内容；只读凭据看不到质疑这个分类。
- **关系**：`library_add_series_relation` 在两个系列之间添加续作等作品群关系，留可撤销的收据；已有关系不会被改动。

所有者在**审核中心 → 待人工**处理上报和待复核的系列，在系列详情页查看每个系列的识别结论。

## 6. 依次验证

1. 不带凭据请求 `/mcp` → **401**。看到 404 说明 `public_url` 为空或没生效；502/503 是代理没通。
2. 带错误的凭据 → **401**；用公网 HTTP 访问私网配置 → **403**。
3. 让客户端列工具，只应出现该能力对应的工具；改了能力后应立即变化。
4. 调 `library_get_series`，参数 `{"series_id": 1}`，应返回已授权资料库内的身份字段。
5. 用未授权的资料库尝试 → 统一返回 "delegated access denied"，末尾附下一步提示（ID 应取自本凭据自己的查询结果，如 `library_find_series`；已是如此就上报，不要重试）。它对“不存在”与“无权”读来完全相同，这是刻意不泄露存在性。
6. 有 `metadata_write` 时，初始化说明以主循环开头；第一次领取前，`library_identification_progress` 的 `prefetch.ready` 应大于 0。

## 7. 已知限制

- 等待类工具最长 30 秒；超时或断线**不取消**已接纳的工作，取消要用 `library_cancel_run`。
- `library_start_scan` 不是幂等的：排队中的扫描可能合并，否则会接受新的一次运行。
- 一次领取至多 20 个系列、约 30 KiB；证据包还没预取、或超过 30 天正在刷新的系列暂不发放。租约 30 分钟，过期后别的会话可以接手领取。
- 提交裁决不是整批原子的：每个系列各自成败。
- 即使所有信号都一致，服务端也不会自己认定识别；每条识别都来自 agent 或所有者的裁决。
- 所有者关闭过的备注，同一系列同一分类不会再记一次。
- MCP 不提供内部模型辅助。已下线的 `internal_llm` 工具留下的额度记录保存在数据库里，没有读取入口。
- 服务重启保留中断历史，但不会自动重放已派发的工作；只有服务端自己的增量预取会续跑。
- 已实测的是官方 Go SDK v1.8.0 客户端与 protocol `2025-11-25`；其他客户端与协议版本不作兼容承诺。
- 若 `public_url` 走 HTTPS 反向代理：保留 Host、清除客户端转发头、正确设置协议与真实客户端 IP，且只把代理地址加进 `server.trusted_proxies`。不要在 URL、代理日志或调试抓包里记录密钥。
