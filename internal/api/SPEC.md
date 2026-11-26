---
id: mm-api
type: module-design
status: active
title: HTTP 与客户端协议适配
parent: mm-architecture
references: [mm-adr-0003, mm-adr-0007, mm-database, mm-adr-0014, mm-catalogsearch, mm-identification]
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

生产 `Host.SetupRoutes` 在 Cookie 子树外挂载 `/mcp`，显式消费装配根注入的 `delegated.Service`；不把 POST 等同于管理员写入，不读取数据库或创建运行时。采用官方 Go SDK 的 stateless Streamable HTTP / JSON 响应，支持 initialize、tools/list 与 `listingCapability`（`mcp_tools.go`）登记的工具；GET/DELETE 不建立持久会话，无事件恢复或服务器主动请求。工具 schema 与控制提示由 SDK 提供；权限裁决、运行归属、受限等待与断线语义仍由应用用例执行（`mm-application`）。静态 Bearer 凭据不是 OAuth，未提供 OAuth 发现、授权服务或交互登录。

每个 HTTP 请求都重新认证 Bearer，工具调用还按实际对象重查能力；不接受 Cookie、查询参数、重复 Authorization 或客户端指定的 Subject。当前工具不暴露凭据管理。`/api/mcp/credentials` 的 GET/POST 与 `/{credentialId}` 的 PATCH/DELETE 属管理员网页管理树，保留 Cookie、CSRF、当前角色及强制改密守卫。列表和命令结果均 no-store，不回显摘要或历史密钥；签发结果的密钥不可记录在日志中。错误为通用信息，不带凭据或底层 SQL。

MCP 安全入口仅接受 `mcp.public_url` 配置的精确 authority；有 Origin 时须与该 origin 一致，重复 Origin 拒绝。SDK 的 localhost-only Host 检查由此更严格的单一 authority 检查替代，使显式 HTTPS 反向代理可用。HTTPS 的直连 TLS 或已配置可信代理的协议声明有效，Cookie Secure 的 always/never 不参与 MCP 判定；不信任 Forwarded Host。显式 LAN HTTP 还校验直连或可信代理报告的真实客户端为 private/loopback IP，可信代理缺失客户端地址时拒绝，公网明文不可用。请求体限制为 64 KiB，无记录协议 payload/Authorization 的 SDK logger；部署代理也不得记录这些秘密。

工具结果契约对所有工具一致：

- 每个成功结果在 structuredContent 之外都带一个文本块，内容是同一份 JSON，只读文本的客户端不读 structuredContent 也能拿到完整结果。
- 拒绝只有一句：`errMCPDenied`。它附下一步提示（ID 取自本凭据自己的查询结果；已是如此则说明授权不再覆盖，应上报而非重试），但对象不存在、跨资料库与能力不足读来完全相同，不泄露存在性。
- 工具描述不超过 400 字符，以一个可直接照抄的 `Example: {...}` 调用结尾，示例参数都在输入 schema 中声明。
- 工具清单按凭据当前的**有效**能力过滤（`delegated.Service.GrantedCapabilities` 与 `listOnlyGranted`）：凭据带着、`Authorize` 也会认的能力，所以普通用户的凭据只按 `read` 列。清单只披露授权，每个处理器仍自行鉴权。不在清单里的工具名（含已下线的）按 SDK 的未知工具统一拒绝。运行类工具随任一能力列出。
- initialize 的 instructions 由 `mcpInstructions` 按同一份有效能力生成：凭据能领取时先写主循环（领取 → 提交裁决 → 难判上报，有权限即直接写入、不请示人），再给清单里其余每个工具一句用途（`toolPurposes`；能力不同、用途不同的工具另写一句带 `metadata_write` 时的用途）。新工具须在那里补一句，否则说明会漏掉它。

核对入口：`bootstrap` 的 `TestMCPEveryToolResultCarriesItsJSONText`、`TestMCPListingFollowsGrantedCapabilities`、`TestInitializationExplainsTheMainLoopAndEveryListedTool`、`TestRetiredToolsAreNeitherListedNorAnswered`、`TestMCPDelegatedSeriesLifecycle`、`TestMCPAccountPasswordAndManagementIsolation`、`TestMCPRejectsUnsafeTransport`；独立凭据与交集规则归 `mm-application`，配置合法性归 `mm-config`。

发现工具只映射应用用例与结构化结果，查询条件、分页、证据及响应上限归 `mm-application`；工具描述提供条件交集、默认页长、硬上限与后继用法。不提供路径参数、任意磁盘读取或整书下载。

图片工具只调用应用完整用例，以 SDK v1.8.0 `ImageContent.Data` 的原始字节字段交付真实 `image/jpeg`，让 SDK 执行 base64 序列化，不能预先 base64 后二次编码。首个文本块是结构化元数据的 JSON 副本，其后 image content 与结构化 `images` 顺序一致；文本不重复图片字节。stateless SDK 的工具上下文不自行代表 HTTP 请求生命周期：ComicInfo 线索、图片工具和封面改善显式合并请求取消上下文，HTTP 断线、stdio EOF 取消上游后，在封面准备的 IO 等待时也归还读资格及图片/准备槽；封面改善沿用同一合并上下文直至事务提交，不在准备后脱离取消；这不改变已接纳后台运行的断线后继续语义。

所有 MCP JSON HTTP 响应在写入网络前受最终响应体硬上限约束，上限容纳两份应用结果（structuredContent 与其 JSON 文本副本）、64 KiB 请求体上限所界定的调用方 JSON-RPC ID 与 SDK framing；有界缓冲溢出后返回短 HTTP 413，不发出部分图片 JSON。应用预留不是任意长请求 ID 的豁免。该接口仍为 stateless JSON，不用此缓冲提供 SSE 会话恢复；stdio 代理沿同一 HTTP 契约转发。


## 系列识别工具与管理员接口

MCP 的识别工具只映射 `identification.Service` 与 `enrichment.Service` 的完整用例，身份来自认证凭据；工具名、所需能力与清单过滤以 `listingCapability` 为准，用途句以 `toolPurposes` 为准，用例契约归 `mm-identification`。

- 主循环是 `library_claim_series`、`library_submit_verdicts`、`library_research_series`（`metadata_write`）与 `library_identification_progress`（`read`）。凭据能领取时，initialize 的说明先写主循环。
- 原语中 `library_replace_identification`、`library_correct_series_field`、`library_correct_authors`、`library_improve_cover`、`library_add_series_relation` 与撤销挂 `metadata_write`；`library_get_series_authors`、`library_get_identification_images`、`library_find_duplicate_series`、`library_count_tags`、`library_report_series` 挂 `read`。所以只读凭据与普通用户的凭据能巡检与挂备注，不能领取或写入。`library_report_series` 的 `disputed_identification` 会改识别状态，另要 `metadata_write`（用户决定）：输入 schema 的分类 enum 与工具描述按有效能力生成，没有 `metadata_write` 的凭据看不到这个分类；授权在用例里另查，事务内被收回时拒绝并提示只能挂备注。
- `library_undo_metadata_operation` 拒绝撤销所有者网页操作留下的收据（`enrichment.ErrOwnerOperation`），提示改为上报；其它凭据的收据照撤销规则处理。
- 能逐系列判定的业务拒绝（裁决、撤换、改正）不是工具错误：结果逐项给 `problem` 代码与一句 `message` 下一步，成功项照常返回。格式错误与授权拒绝仍是工具错误，授权拒绝统一为 `errMCPDenied`。领取结果带 `next` 提示，空批带 `empty_reason`；`library_improve_cover` 得到冲突收据时附一句 `message`，指向 `protected_value_doubt` 上报。
- 提交、撤换、改正与补充类原语带 `request_key`；领取每次都是新的领取，重搜按搜索词去重、上报按分类去重，都不需要请求键。
- 取值有限的参数（裁决种类、上报分类、可改正字段、关系类型、标签排序）以 enum 写进输入 schema，弱模型直接从 schema 挑值，描述不必列一长串。
- `library_research_series` 的 `wait_ms` 上限 30000，与 `library_wait_run` 一致；等不到时返回 `running` 与 `run_id`，断线不取消运行。
- `library_get_identification_images` 的首个文本块是结构化结果的 JSON，其后按 `images` 顺序跟每张 `included` 的 JPEG；结果构造后在 API 层再量一次总大小，超出则整体报错，不发出截断的图片。

管理员网页的 `/api/identification/` 整个前缀只对当前管理员开放，读取也一样，因为结论带着凭据与账户归属；写命令沿用管理树的 Cookie、CSRF、当前角色与强制改密守卫。每个决定返回操作后的权威识别（挑候选另带补充收据 `operation_ids`），响应 no-store，新结构纳入生成契约。

- 读取与已审定：`GET series/{id}`；`PUT`、`DELETE series/{id}/owner-review`。
- 预取：`POST prefetch` 接受 `{"library_id": n}`，空体表示全库，回 `{run_id, series}`，`series` 为 0 时不建运行。网页的全库与资料库批量入口调它；不提供按首条结果批量生成提案的路由，库里留着的 `scrape` 运行照常显示但不能重试。
- 重搜：`POST series/{id}/research`，请求体 `{query, wait_ms?}`，与 MCP 共用用例。
- 待人工：`GET pending?category=&offset=&limit=`（默认 20、上限 50，`limit=0` 只回计数）；决定为 `POST series/{id}/identify {candidate_ids}`、`exclusions {candidate_id}`、`not-found`、`keep-identification`、`notes/{noteId}/close`。
- 手动搜索与详情：`PUT series/{id}/conclusions/{source}` 的请求体是 `{"result": <搜索结果>}`，按来源快照规则校验，LLM 等非目录来源回 400，挑中排除候选时响应带 `lifted_exclusion`；`DELETE series/{id}/conclusions/{source}` 撤回识别。
- 决定落到已经变了的系列时回 409 与 `code`：`not_pending`、`candidate_unavailable`、`not_a_note`，前端据此重读这个系列；重复否决同一候选、关闭已关闭的备注回 200 且不改动。识别用例的拒绝到状态码、`code` 与文案的映射只在 `identificationRefusals` 一张表里，各路由不另写 switch。
- 资料库浏览 `GET /api/series/search?identification=<状态>` 读同一状态视图，可与其它条件组合；普通账号带这个参数回 403，取值不在六个状态里回 400。

核对入口：`bootstrap` 的 `TestPendingListAndDecisionsNeedAnAdministrator`、`TestLibraryBrowsingFiltersByIdentificationStatus`、`TestInspectionNeedsOnlyReadAndGrantsNoWrite`、`TestAgentSubmitsVerdictsSeriesBySeries`、`TestIdentificationImagesLeaveOutWholeImagesBeyondTheResultBudget`。

## 受限补充和操作历史

MCP 的作者校正、封面改善、作品群关系写入、操作查询/按系列分页与撤销工具直接委托 `enrichment.Service`（撤换与改正经识别模块在同一事务里调用它），身份来自认证凭据，不接受调用者自报。工具声明只读或幂等提示，但最终以事务授权和持久重放结果为准。管理员网页 `GET /api/mcp/operations`、`GET /api/mcp/operations/{operationId}`、`POST /api/mcp/operations/{operationId}/undo` 复用网页认证和 CSRF，命令携带 request_key；普通用户不可管理或撤销。审计、响应预算、预期值与回滚契约归 `mm-application`。


封面改善的页选择与故障一致性归 mm-application，收据复用同一管理历史/Undo，不接受任意 URL 或路径。原 cover 路由支持已提交的虚拟材料键；原 thumbnail 路由须检查当前书或持久保留引用后读取材料私有根。普通缩略图文件使用 os.Root 约束在普通缓存根，保留内容与条件请求语义；未提交虚拟键、Windows 大小写/尾点等实体目录别名均不能公开准备材料。

作者校正复用同一操作查询/撤销路径；`corrections` 使用精确 from/to name、role 与短 reason，不接受自由作者事实。收据 before/after 返回作者组，协议层不执行共享身份重命名。系列内例外、当前事务保护和字段补空限制归 `mm-application`。

曾裁决过旧提案的收据仍带 adjudication.status/accepted，区分全部拒绝、部分接受仍 pending 与 supplemented_closed；没有命令再产生它。

## 撤销材料清理入口

`library_cleanup_metadata_operation` 映射有界单项 Cleanup，`library_start_metadata_cleanup` 映射显式同库列表的共享异步运行，`library_get_metadata_cleanup(request_key)` 读取本凭据持久结果。独立 cleanup 权限不隐含 metadata_write/read，运行 Get/Wait/Cancel 和列表仍按原归属及该运行 cleanup 能力检查。工具不管理或扩大凭据。同步单项合并真实入口取消；异步接纳后断连不取消。幂等、partial/pending 与恢复损失规则归 mm-application。

管理员网页 `POST /api/mcp/operations/{operationId}/cleanup` 与 `GET /api/mcp/cleanups/{cleanupId}` 保留原 Cookie、CSRF（写命令）、当前角色和改密守卫，响应 no-store。不增加逐批确认参数。操作投影 undo_unavailable/cleaned_by 明示快照已裁剪，不能把 cleaned outcome 或失败/缺失文件翻译成安全 Undo 成功。

管理员 GET cleanup 响应可为本人原网页 pending 请求返回 `retry_request_key`，供同一 operation 的原 POST 同键续办；不返回其他执行者的 retry identity，不绕过当前库/凭据/capability 或管理员守卫。前端根据 cleaned_by 查询，不以 Undo unavailable 推断回收完成。
