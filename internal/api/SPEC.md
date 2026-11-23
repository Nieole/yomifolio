---
id: mm-api
type: module-design
status: active
title: HTTP 与客户端协议适配
parent: mm-architecture
references: [mm-adr-0003, mm-adr-0007, mm-database, mm-adr-0014, mm-catalogsearch]
---

## 责任与入口

把 HTTP 与客户端协议转换成应用调用：解析路径和请求、会话与管理员授权、CSRF、响应及错误映射。Web API、OPDS、Mihon、KOReader 保留各自认证和协议兼容语义，不因共用进程而合成一个授权入口。

只接受经过授权的用户身份；阅读写入显式传递该用户，不能从请求任意指定另一个人。用户名契约归 `mm-adr-0003`，任务寻址归 `mm-adr-0007`。

## 会话 Cookie 兼容

新会话使用 `yomifolio_session`；仅在请求没有新 Cookie 时读取旧 `mm_session`。新 Cookie 即使为空、失效或非法也不得回退旧值；被选名称出现重复 Cookie 时拒绝认证。旧会话验证及适用的 CSRF、角色和强制改密校验通过后换发新 Cookie 并清除旧名，保持令牌、用户和 CSRF 不变；名称过渡本身不延长有效期，滑动续期仍须服务端更新成功后才下发。公开状态端点验证旧会话成功时也按现有到期时间换名。

登录、首次建管理员及改密建立新会话时清除旧 Cookie；登出撤销请求携带的两种名称下的会话令牌及当前会话，并清除两种 Cookie，防止旧登录复活。写入与清除沿用 `Path=/`、HttpOnly、SameSite=Lax 及同一 Secure 判定，不降低既有 CSRF、授权或代理信任规则。

## 边界

控制器可直接使用藏书查询、协议投影与缓存适配，不在此重建阅读、编排、提案或迁移事务。后台工作的声明与重启配方归应用能力；这里映射任务控制、历史和 SSE 快照，不持有另一套准入。

`NewTransport` 创建认证、事件 broker、运行投影及缓存/内容效果等技术 leaves；`Transport.Bind` 接受显式家庭能力与窄查询，在共享鉴权子树下组合 `Host`。`Host` 不构造应用、不持有 Runtime 或全量 Store，生产资源创建与恢复启动归 `bootstrap.Open`；底层服务不回调 HTTP 请求处理器。响应契约与前端生成类型须一致，协议差异在各适配器内消化。

`NewTransportWithSources` 接收生产装配根传入的外部客户端工厂，HTTP 来源请求和应用来源端口共享这些工厂；接缝契约归 `mm-bootstrap`，不允许替换业务能力。`NewTransport` 等价于不覆盖来源的生产默认装配。

## 藏书名称搜索

名称匹配语义归 `mm-database`；全局排名、去重、分数与游标语义归 `mm-catalogsearch`，HTTP 不另定义排序或 score。`GET /api/search` 的 target 默认 all，支持 series、book 与 title（book 别名）；非法 target 返回 400。limit 默认 20，显式参数须为单个十进制正整数且不超过 100，非法值返回 400。cursor 省略或为空表示首批；游标规则归 mm-catalogsearch，无效、重复或上下文失配的游标，以及非法查询串编码，返回 400、稳定 code 为 invalid_search_cursor。每次请求仍由现有 authGate 重新校验会话和当前账号，游标不绕过授权。

具名 `SearchResult`、`SearchHit` 与 `SearchHitFields` 纳入 tsgen：保留 hits、total_hits、max_score 及原身份和 fields，新增 has_more 和 next_cursor；hits 始终为数组，末页游标为空字符串。total_hits 只表示当前批条数，不是全局总数或累计数。空关键词、归一化为空或无命中均返回同一空批形状，不进行目录全匹配；资料库与协议的无关键词目录浏览语义不变。

资料库筛选在分页前执行，原排序与游标不变；OPDS 与 Mihon 保留各自分页和上限，以真实命中总数生成分页状态或导航，不能拿某页条目数冒充总数。HTTP 回归以真实 SQLite 和独立字面命中集合验证，还覆盖旧库升级、增量写入及实际维护任务后的查询。

## Mihon 目录契约

`/api/mihon/v1` 与 OPDS 保留独立协议开关和站点账户 HTTP Basic 鉴权；要求改密的账户须先在网页完成改密，无站点账户时仅允许首启匿名访问。连接信息必须准确反映此条件，不能把会话入口放行解释成资源免认证。

系列搜索未指定 `sort` 且无其他筛选时使用跨库相关性搜索；显式排序与资料库、作者、标签、状态筛选必须在分页前共同执行，不能先跨库分页再过滤。系列详情在原有字段之外返回本系列的 `authors`（名称与角色）和 `tags`，无元数据时为数组；目录列表不为详情元数据引入逐项查询。

## Mihon 页级同步契约

同一协议鉴权子树内，`GET /capabilities` 声明 `sync_revision: 1`、一基页码、服务端同步能力、按校准发生时间合并、同刻保留现有胜者、未来容差秒数和无批量入口；它不宣称官方 Mihon 客户端具备翻页同步能力。`GET /time` 返回 UTC 高精度 `received_at` 与 `sent_at`，客户端以往返测量建立时钟锚点，不能把重连时刻当作离线阅读发生时刻。

`GET /books/{bookId}/progress` 返回当前授权用户的 `page`、`is_read`、`state`（`absent`、`position`、`cleared`）、`occurred_at`、胜出 `fact_id` 及 `server_time`。清除证据不因进度行删除而消失；无精确事实依据的旧位置保留页码但 `occurred_at` 为 null，不把历史本地墙上时间伪标成 UTC 事实时间。已有进度行可能仅包含已读标记，返回 `state: position`、`page: 0`；此时仅同步 `is_read`，保留客户端已有页码，不能伪造第一页或解释为清除。

原有单书 POST 保留 `page`、`updated_at`、`fact_id` 和原 `status`，新增 `outcome`（`applied`、`duplicate`、`stale`、`unchanged`）、提交的 `fact_id`、事务内 `authoritative` 快照与 `server_time`。ACK 只证明本次事务结束时的状态，后续并发事实仍可能覆盖；客户端不得以提交页码替代 ACK 中的裁决页码。

显式 `sync_revision: 1` 要求非空、最多 128 个可打印非空白 ASCII 字符的事实 ID，以及可由持久纳秒基准表达的非零 RFC3339 时间。超前服务端五分钟以上返回 422 `clock_error`，非法输入与未知版本返回 400 `invalid_sync`／`unsupported_revision`；不静默夹到接收时间。缺失或零版本保持旧写入兼容。同步数据和错误响应 `Cache-Control: no-store`；客户端持久保留事实 ID 与原校准时间、按原值重试，时钟错误保留待处理事实。校时不能证明任意失准设备离线事件的物理先后。

核对入口：`host.go`、`auth_username_case_test.go`、`collection_authorization_test.go`、`mihon_progress_test.go` 与协议控制器测试。生产装配顺序另经 `bootstrap` 的 HTTP 与恢复回归核对。

## MCP 与凭据管理

生产 `Host.SetupRoutes` 在 Cookie 子树外挂载 `/mcp`，显式消费装配根注入的 `delegated.Service`；不把 POST 等同于管理员写入，不读取数据库或创建运行时。采用官方 Go SDK 的 stateless Streamable HTTP / JSON 响应，支持 initialize、tools/list、`library_get_series(series_id)`、`library_find_series`、`library_list_books`、`library_get_book_clues`、`library_get_series_cover`、`library_get_page_thumbnails` 及 `library_search_sources`、`library_get_source_search`、`library_propose_match`、`library_get_match_proposal`、`library_get_enrichment_state`、`library_enrich_metadata`、`library_enrich_metadata_batch`、`library_get_metadata_operation`、`library_list_metadata_operations`、`library_undo_metadata_operation`、`library_assess_match`、`library_get_model_operation`、`library_start_scan`、`library_get_run`、`library_list_runs`、`library_wait_run`、`library_cancel_run`；GET/DELETE 不建立持久会话，无事件恢复或服务器主动请求。工具 schema 与控制提示由 SDK 提供；权限裁决、运行归属、受限等待与断线语义仍由应用用例执行（`mm-application`）。静态 Bearer 凭据不是 OAuth，未提供 OAuth 发现、授权服务或交互登录。

每个 HTTP 请求都重新认证 Bearer，工具调用还按实际对象重查能力；不接受 Cookie、查询参数、重复 Authorization 或客户端指定的 Subject。当前工具不暴露凭据管理。`/api/mcp/credentials` 的 GET/POST 与 `/{credentialId}` 的 PATCH/DELETE 属管理员网页管理树，保留 Cookie、CSRF、当前角色及强制改密守卫。列表和命令结果均 no-store，不回显摘要或历史密钥；签发结果的密钥不可记录在日志中。错误为通用信息，不带凭据或底层 SQL。

MCP 安全入口仅接受 `mcp.public_url` 配置的精确 authority；有 Origin 时须与该 origin 一致，重复 Origin 拒绝。SDK 的 localhost-only Host 检查由此更严格的单一 authority 检查替代，使显式 HTTPS 反向代理可用。HTTPS 的直连 TLS 或已配置可信代理的协议声明有效，Cookie Secure 的 always/never 不参与 MCP 判定；不信任 Forwarded Host。显式 LAN HTTP 还校验直连或可信代理报告的真实客户端为 private/loopback IP，可信代理缺失客户端地址时拒绝，公网明文不可用。请求体限制为 64 KiB，无记录协议 payload/Authorization 的 SDK logger；部署代理也不得记录这些秘密。

核对入口：`bootstrap` 的 `TestMCPDelegatedSeriesLifecycle`、`TestMCPAccountPasswordAndManagementIsolation`、`TestMCPRejectsUnsafeTransport`；独立凭据与交集规则归 `mm-application`，配置合法性归 `mm-config`。

发现工具只映射应用用例与结构化结果，查询条件、分页、证据及响应上限归 `mm-application`；工具描述提供条件交集、默认页长、硬上限与后继用法。不提供路径参数、任意磁盘读取或整书下载。

图片工具只调用应用完整用例，以 SDK v1.8.0 `ImageContent.Data` 的原始字节字段交付真实 `image/jpeg`，让 SDK 执行 base64 序列化，不能预先 base64 后二次编码。结构化元数据与 image content 顺序一致，不在文本重复图片。stateless SDK 的工具上下文不自行代表 HTTP 请求生命周期：图片工具和单项/批量同步补充工具显式合并请求取消上下文，HTTP 断线、stdio EOF 取消上游后，在封面准备的 IO 等待时也归还读资格及图片/准备槽；同步补充沿用同一合并上下文直至事务提交，不在准备后脱离取消；这不改变已接纳后台运行的断线后继续语义。

所有 MCP JSON HTTP 响应在写入网络前受 256 KiB 最终响应体硬上限约束，包含 SDK framing、调用方 JSON-RPC ID 与 base64/JSON 开销；有界缓冲溢出后返回短 HTTP 413，不发出部分图片 JSON。应用预留不是任意长请求 ID 的豁免。该接口仍为 stateless JSON，不用此缓冲提供 SSE 会话恢复；stdio 代理沿同一 HTTP 契约转发。

匹配工具只映射 `delegated.Service` 的完整用例；输入确认、来源白名单、分页/字节上限、临时证据寿命、持久提案证据及读/写能力映射归 `mm-application`。只返回结构化数据，文本不再复制一份候选；底层来源/SQL 错误统一脱敏。

模型工具只映射应用的不可变额度操作；操作 status=history_unavailable 明确表示关联运行历史已缺失，并非运行终态，预算及逐项结果仍返回，运行工具不再保证可查/可取消。声明是 agent 转述而非受信任人工审批。具体记账、归属、返回状态和重启不重放契约归 `mm-application`。协议适配器不直接接触计数表或调用模型工厂。


## 受限补充和操作历史

MCP 的 enrichment state、单项/批量补充、操作查询/按系列分页与撤销工具直接委托 `enrichment.Service`，身份来自认证凭据，不接受调用者自报。工具声明只读或幂等提示，但最终以事务授权和持久重放结果为准。管理员网页 `GET /api/mcp/operations`、`GET /api/mcp/operations/{operationId}`、`POST /api/mcp/operations/{operationId}/undo` 复用网页认证和 CSRF，命令携带 request_key；普通用户不可管理或撤销。审计、响应预算、预期值与回滚契约归 `mm-application`。


补充工具的 cover 字段选择与故障一致性归 mm-application，仍复用同一 enrichment 工具/管理历史/Undo，不新增图片宿主或任意 URL 获取工具。原 cover 路由支持已提交的虚拟材料键；原 thumbnail 路由须检查当前书或持久保留引用后读取材料私有根。普通缩略图文件使用 os.Root 约束在普通缓存根，保留内容与条件请求语义；未提交虚拟键、Windows 大小写/尾点等实体目录别名均不能公开准备材料。

作者校正复用同一 enrichment 工具与操作查询/撤销路径；输入可选择 authors，`author_corrections` 使用精确 from/to name、role 与短 reason，不接受自由作者事实。state 与收据 before/after 返回作者组，协议层不执行共享身份重命名。系列内例外、当前事务保护和字段补空限制归 `mm-application`。

补充命令的 expected schema 来自独立 `enrichment.ExpectedState`，authors 可省略（含批量嵌套输入）；不因响应 State 增加字段而要求旧客户端补送新字段。持久摘要版本与兼容规则归 `mm-application`。

已有提案读取须当前 read + metadata_write，返回原确认依据、当前剩余字段、status 与 accepted；不把旧提案缺证据当已确认。`library_enrich_metadata{,_batch}` 可携带显式 adjudication 命令，模式和原子裁决规则归 mm-application。操作收据的 adjudication.status/accepted 区分全部拒绝、部分接受仍 pending、supplemented_closed；reject 命令在此前有接受时不能呈现为全部拒绝。新命令沿用相同同步 transport 取消上下文及脱敏错误路径，不增加数据库或模型入口。

## 撤销材料清理入口

`library_cleanup_metadata_operation` 映射有界单项 Cleanup，`library_start_metadata_cleanup` 映射显式同库列表的共享异步运行，`library_get_metadata_cleanup(request_key)` 读取本凭据持久结果。独立 cleanup 权限不隐含 metadata_write/read，运行 Get/Wait/Cancel 和列表仍按原归属及该运行 cleanup 能力检查。工具不管理或扩大凭据。同步单项合并真实入口取消；异步接纳后断连不取消。幂等、partial/pending 与恢复损失规则归 mm-application。

管理员网页 `POST /api/mcp/operations/{operationId}/cleanup` 与 `GET /api/mcp/cleanups/{cleanupId}` 保留原 Cookie、CSRF（写命令）、当前角色和改密守卫，响应 no-store。不增加逐批确认参数。操作投影 undo_unavailable/cleaned_by 明示快照已裁剪，不能把 cleaned outcome 或失败/缺失文件翻译成安全 Undo 成功。
