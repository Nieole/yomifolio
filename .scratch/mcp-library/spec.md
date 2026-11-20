# MCP 资料库自动化管理

Status: ready-for-agent

## Problem Statement

资料库所有者维护约 10TB 的漫画藏书。新增系列和历史缺失信息需要反复检索来源、比较候选、检查本地文件、确认提案，再逐项整理作者、标签、评分和封面。现有提案与网页操作不能让外部 agent 在明确权限内完成这条链路；单纯开放现有写入接口又可能覆盖人工内容、误改共享作者、绕过字段锁，或留下无法追溯和撤销的结果。

所有者需要临时向 agent 下达整理指令，由 agent 自主处理有把握的匹配，报告歧义和未完成项，而不是再维护一个元数据待办系统、常驻 agent 或新的必经工作流。

## Solution

在正在运行的程序上提供 MCP 服务，并提供连接该服务的 stdio 辅助进程。外部 agent 使用管理员签发的独立受限凭据，按资料库、加入时间、缺失字段或指定系列发现目标，读取受限识别线索，搜索现有元数据来源，综合判断身份，并通过提案与受保护的系列操作补充信息、进行获准的作者校正及改善自动封面。

以单个系列的一次操作为变更和撤销单位。服务端保护已有描述字段、字段锁、人工封面及后续修改，保留来源、简短判断依据和必要前后状态；批量处理允许部分成功并如实报告。内部 LLM 受本次声明额度限制，撤销数据默认一直保留，清理需要独立能力授权。管理员在网页设置中管理凭据、查询变更、撤销和手动清理。

本规格是已完成访谈的 effort 规划与验收输入，不表示功能已经实现，不替代当前有效的领域、架构和模块契约。

## User Stories

1. As a library owner, I want to connect an external agent on demand, so that I can delegate a specific organizing request without maintaining a permanent agent.
2. As an agent operator, I want both Streamable HTTP and a stdio helper, so that I can use clients with different transport support.
3. As an administrator, I want the stdio helper to connect to the running service, so that it cannot create a competing application runtime or access the database directly.
4. As an administrator, I want explicitly configured LAN HTTP access, so that I can choose local deployment convenience while accepting the documented credential interception risk.
5. As an administrator, I want public network connections to use HTTPS, so that remote credentials are not exposed through an implicitly permitted plaintext connection.
6. As an administrator, I want to issue credentials associated with a real user, so that every delegated operation has an accountable identity.
7. As an administrator, I want to restrict each credential to selected libraries, so that one agent cannot browse or modify unrelated resources.
8. As an administrator, I want separate read, metadata-write, scan-control, internal-LLM and cleanup capabilities, so that delegation need not grant every operation.
9. As a regular user, I want credentials to remain bounded by my current account permissions, so that possession of a credential cannot turn me into an administrator.
10. As an administrator, I want to revoke credentials and narrow their permissions, so that queued work stops before starting and running work stops at safe checkpoints.
11. As an administrator, I want password changes to revoke that user's MCP credentials, so that changing an account's password also ends its delegated access.
12. As an agent operator, I want authorization failures to apply equally to direct object lookups and listing tools, so that object identifiers cannot bypass library restrictions.
13. As a library owner, I want to select series by library, added time and missing metadata together, so that I can organize recent additions or a specific backlog.
14. As a library owner, I want to name particular series explicitly, so that a one-off correction does not require a whole-library scan.
15. As an agent operator, I want paginated series and book information, so that I can work through a large collection without unbounded responses.
16. As an agent operator, I want directory names, filenames and ComicInfo clues, so that matching uses evidence already present in the collection.
17. As an agent operator, I want covers and a small number of selected page thumbnails, so that I can distinguish visually similar candidates without downloading entire books.
18. As a library owner, I want page counts, image dimensions and response sizes to be bounded, so that agent access does not become arbitrary file reading or oversized archive transfer.
19. As an agent operator, I want to search all existing available metadata sources, so that matching is not limited to Bangumi.
20. As an agent operator, I want multiple candidates and source details, so that I can compare identity and edition rather than blindly select the first result.
21. As a library owner, I want ambiguous identity or edition matches reported to me, so that uncertain candidates are not silently written into my collection.
22. As a library owner, I want unconfirmed candidates kept out of persistent proposal queues, so that agent exploration does not create a new backlog for me to maintain.
23. As an agent operator, I want to combine fields from confirmed sources for the same series, so that one incomplete source does not limit the result.
24. As a library owner, I want a short verifiable matching summary and field-level sources, so that I can inspect why changes were made without storing hidden model reasoning.
25. As a library owner, I want empty descriptive fields filled without replacing nonempty values, so that enrichment preserves existing work.
26. As a library owner, I want locked fields protected during writes and undo, so that delegation does not override explicit protection.
27. As a library owner, I want selected content tags appended without deleting existing tags, so that useful classification grows without losing prior organization.
28. As a library owner, I want personal markers and noisy source tags excluded from automatic additions, so that content classification remains useful.
29. As a library owner, I want missing authors added and aliases or roles corrected for the selected series, so that its author information can be improved.
30. As a library owner, I want author corrections confined to that series' associations, so that shared author records do not cause changes to other series.
31. As a library owner, I want missing source links added, so that I can revisit the underlying source material.
32. As a library owner, I want a missing rating calculated from valid source ratings normalized to ten points, so that the aggregate rating is reproducible.
33. As a library owner, I want source scores, scales and the equal-weight formula retained, so that I can audit the aggregate without trusting an unexplained model opinion.
34. As a library owner, I want existing ratings and personal evaluations preserved, so that enrichment does not replace them with site or model scores.
35. As a library owner, I want the agent to improve automatically generated covers when quality warrants it, so that series are easier to recognize visually.
36. As a library owner, I want manually chosen or uploaded covers protected, so that the agent does not undo an intentional choice.
37. As a library owner, I want text and cover changes in one series operation to become effective together, so that a failed operation does not leave a half-applied result.
38. As an agent operator, I want to read and adjudicate existing proposals, so that delegated work fits the existing metadata model.
39. As a library owner, I want the agent to close remaining differences after successful enrichment, so that unwanted differences need not stay pending.
40. As a library owner, I want enrichment followed by closure distinguished from complete rejection, so that the history accurately reflects what was accepted.
41. As a library owner, I want terminal proposals to reject later writes, so that concurrent decisions cannot apply already rejected differences.
42. As an agent operator, I want conflicts checked against the fields involved in my operation, so that unrelated edits do not unnecessarily block it.
43. As a library owner, I want later changes to either the tag group or author group detected before replacing or undoing that group, so that concurrent work is not silently lost.
44. As an agent operator, I want to initiate necessary scans and query, wait for or control authorized runs, so that I can coordinate preparation with metadata work.
45. As an agent operator, I want long operations to return run identifiers, so that I need not keep one request open until completion.
46. As an agent operator, I want accepted work to survive a client disconnect and remain queryable after reconnection, so that a broken connection is not mistaken for cancellation.
47. As a library owner, I want each series' outcome reported separately, so that one failed or ambiguous match does not hide successful results elsewhere.
48. As a library owner, I want internal LLM calls disabled without an explicit allowance for the current operation, so that model usage is not silently enabled.
49. As an agent operator, I want the service to enforce the allowance I relay for an operation across requests, so that reconnecting or splitting tool calls does not reset usage.
50. As a library owner, I want model usage to stop at the allowance while other permitted work can continue, so that budget exhaustion does not discard useful non-model results.
51. As a library owner, I want actual model usage and unfinished work reported, so that I can decide whether to authorize more work later.
52. As a library owner, I want credential and account permissions rechecked before writes and internal model calls, so that revoked authority does not continue through an accepted run.
53. As a library owner, I want per-series change records with before and after state, so that I can inspect and undo an incorrect match independently.
54. As a library owner, I want undo to refuse conflicting later edits, so that recovery cannot overwrite newer human work.
55. As a library owner, I want necessary old images retained with undo data by default, so that undo can actually restore a replaced cover.
56. As an administrator, I want a web view of agent changes, reasons, sources and undo availability, so that I can inspect delegated work without a separate management command.
57. As an administrator, I want manual cleanup with no automatic expiry, so that I control when rollback material is discarded.
58. As an administrator, I want cleanup authority granted independently, so that an agent allowed to edit metadata is not automatically allowed to destroy rollback material.
59. As an authorized agent operator, I want to clean rollback material without per-batch human confirmation, so that delegated cleanup does not require another approval workflow.
60. As a library owner, I want cleaned operations identified as no longer guaranteed undoable while minimal audit summaries remain, so that cleanup does not misrepresent recovery or erase accountability.
61. As a library owner, I want cleanup to preserve images still in use and all collection files, so that reclaiming rollback storage does not damage the library.
62. As an administrator, I want a generic protocol client to verify both transports, so that acceptance is not coupled to one agent product.
63. As a library owner, I want existing web, scanning and ordinary proposal behavior preserved, so that adding agent access does not silently change other workflows.

## Implementation Decisions

### 模块与接入边界

- 生产 composition root 继续独占资源组装、恢复和关闭。MCP 是协议适配器，使用显式注入的应用能力；stdio 仅代理到该服务，不读取数据库，不组装第二套运行时。
- HTTP 与 stdio 必须提供相同的业务授权和保护语义，不能把“复用应用用例”误当作已经完成协议入口授权。首要验收客户端是通用协议客户端，不要求特定 agent 产品的交互兼容性。
- 应用层承载完整受限用例及跨能力协调；系列元数据模块继续归口字段锁、补充与校正写入；提案模块继续归口裁决；任务与运行模块继续归口排队、进度及生命周期；内容访问模块继续归口读取资格和句柄释放。协议层不另造字段写入器或任务状态机。
- 管理员网页提供凭据管理和变更查询、撤销、手动清理入口。不新增独立管理命令；已有示意只确认功能，不固定最终视觉样式。

### 发现、证据与匹配

- 支持按资料库、加入时间和缺失字段组合筛选，以及明确指定系列；系列和书目查询须分页。近期新增筛选不承诺完整增量水位。
- 暴露识别所需的本地名称、ComicInfo、封面和按需选定的少量页面缩略图；同时约束页数、尺寸及整体响应体积，复用内容访问资格与资源关闭契约，不开放任意路径或整本下载。
- 搜索范围是现有已配置可用来源，支持多候选搜索与详情。现有 confidence 不能直接解释为身份匹配概率。agent 对身份及版本负责综合判断，不能排除冲突时报告用户，确认匹配后才形成待写入提案，不持久堆积未定候选。
- 同一系列允许按字段组合来源，记录字段级出处和简短、可核查的判断摘要，不保存模型内部推理。来源候选中存在某个属性，不等于既有提案已支持该属性的裁决。

### 写入与提案契约

- 描述字段仅补空；非空值及锁定字段不改。标签筛选后追加，保留已有成员，不纳入个人标记和来源噪声。
- 作者可追加、归并别名或修正角色，但只调整目标系列的作者关联，不修改共享作者记录，不做全库归并。作者校正是保留现有内容规则中的明确例外。
- 补充来源链接；仅在评分缺失时计算综合评分。有效站点评分统一为十分制后等权平均，一个有效来源时使用其值。保留原始分数、尺度及公式，不覆盖现有评分，不混入个人评价或 LLM 自评。
- 可改善归档自动生成的封面；手选或上传封面必须保护。一项系列操作包含文字与封面时先准备内容，再一起生效，保证该系列全部成功或不生效；外部调用和费用不可回滚。不同系列允许部分成功，不提供整批原子保证。
- 写入与撤销都按涉及字段校验预期当前值，不因无关字段变化冲突；标签和作者各按整组校验，整组后续变化必须报冲突。提交时仍须检查当前字段锁和当前授权，不能只依赖请求预检。
- 支持现有提案的读取、应用与裁决，以及“补充后关闭剩余差异”。后者必须可区分于全部拒绝，且不撤销已应用内容；具体采用独立状态还是其他可区分呈现尚未定稿。
- 遵守 mm-adr-0011：完整和部分应用都须在写入事务内重读待裁决状态及当前值、锁；终态竞争失败时不提交元数据、提案字段和来源沿革。既有 fill_empty 的集合整组补空、普通提案集合替换以及人工编辑语义不能被 MCP 静默改写。

### 授权、运行与内部模型

- 管理员统一签发、限制和撤销关联真实用户的独立凭据，不把浏览器 Cookie 当作长期 agent 凭据。实际许可是当前用户权限与凭据资料库、能力限制的交集；普通用户不会借凭据获得原本没有的写入权，现有网页读取权限不随本项目改变。
- 查询、元数据写入、扫描控制、内部 LLM、清理须能分别限制；按对象直接访问、列表、图片读取及运行控制都必须受相关资料库与能力限制。agent 不能自行扩权或修改自己的凭据授权。
- 撤销或收紧授权后，受影响的排队工作不得再启动，运行中工作在安全检查点取消，写入和内部模型调用前重新鉴权。修改密码同时撤销该用户 MCP 凭据。已提交系列不自动回滚，已发出请求及费用不保证追回。
- 耗时操作异步返回运行标识，沿用任务、运行和作用域语义；客户端断线不是取消，可重连查询。不能借本功能建立专用常驻元数据维护工作流。
- 可显式允许局域网明文 HTTP，接受内网凭据截获风险；公网使用 HTTPS。stdio 上游同样遵守此边界，不能因为辅助进程本地运行就豁免远端明文风险。
- 内部 LLM 没有本次明确额度不调用；服务端累计调用次数，跨请求不能重置额度，耗尽停止模型调用并报告用量和未完成项，其他获准操作可继续。凭据是否具有模型能力与本次额度是两项独立限制。
- 委托信任取舍以 mm-adr-0016 草案记录的已选边界为准：允许 agent 转述本次额度，服务端不独立证明真实用户批准；不得把额度计数宣传为受信任审批。该 ADR 仍待正式审阅，不因本规划发布自动转为 active。

### 审计、撤销与清理

- 按一个系列的一次操作保留来源、判断摘要及撤销所需前后状态和旧图片。变更记录与当前字段来源沿革不是同一种数据，既有最新来源记录不能充当完整操作历史。
- 支持查询及安全撤销；后续字段变化、集合变化或当前字段锁不允许强行覆盖。网页及工具结果需明确区分仍可撤销、发生冲突和清理后不再保证可撤销。
- 撤销材料默认不自动到期，仅显式清理。清理独立授权，可由获授权 agent 在其作用域内自主执行，不要求逐批人工确认，不引入受信任逐批审批工作流。
- 清理保留最小审计摘要，不删除当前使用图片或藏书文件。清理后的对应变更不再保证可撤销，不能继续显示为完整可恢复。
- 持久化设计须承载凭据授权与撤销、操作归属、跨请求额度、字段证据、操作前后状态、封面保留引用及清理结果。这里规定逻辑数据需求，不预定表名、存储布局或尚未决定的 schema。

## Testing Decisions

测试接缝已经用户确认。优先在最高公开边界验证可观察行为，避免复制一套业务测试宿主或把私有函数顺序、内部 SQL 结构当作验收结果。

### 主要接缝

- 从现有生产 bootstrap 组装真实服务，经通用 MCP 客户端与管理员网页管理 HTTP 操作。使用真实临时 SQLite、资料库、归档及图片文件；仅替换外部元数据与 LLM 来源，避免真实网络依赖和费用。
- 已有 bootstrap 集成测试可作为基础，TestOpenUsesOneManagerQueueAndServesBoundHTTP 是共享应用组装与任务队列的先例。当前 bootstrap 尚无完整外部来源注入，需在生产组装边界接入窄的来源替身，而不是替换应用服务。
- 通过公开查询、网页投影、实际封面读取及重开服务后的结果验证持久化；测试同步使用可控制的外部来源和运行屏障，不依赖盲等或私有方法调用顺序。

### 核心场景

- **发现与内容**：组合筛选、分页、明确目标、边界外资料库拒绝、对象直接访问不绕权、受限页面返回及过大请求拒绝。
- **匹配与来源**：多候选及详情、身份或版本歧义不写入、不积压未定候选、跨来源字段证据和可核查摘要。
- **字段策略**：空值补充、非空及字段锁保护、标签追加、目标系列内作者别名与角色校正且其他系列不变、来源链接、综合评分的尺度换算与等权计算、无有效分数不伪造模型评分。
- **文字与封面一致性**：自动封面可改善，人工封面不改；准备失败或提交冲突后文字与封面均不半生效，成功后两个公开投影一致。故障后的重开服务检查应验证不出现被报告为成功的半提交结果。
- **提案竞争**：补充后关闭可区别于全部拒绝；关闭不撤销已提交字段；终态后应用被拒绝；并发部分应用不重复消费。保留现有 proposal 并发与回滚测试作为细粒度守卫。
- **字段级冲突**：相关字段变化阻止写入和撤销，无关字段变化不阻止；标签、作者按整组冲突；操作期间新加锁有效。
- **权限生命周期**：用户权限与凭据交集、不同资料库和能力隔离、撤权后的排队拦截与运行检查点取消、密码修改撤销凭据、已提交内容不自动回滚。
- **运行与模型**：异步运行标识、断线后继续及重连查询、授权内运行控制、逐系列部分成功；无额度不调用、额度跨请求累计、耗尽不再产生模型调用且非模型操作仍可进行。测试只证明声明额度被执行，不声称证明用户真实批准。
- **撤销与清理**：前后状态及旧封面真实恢复、后续编辑与锁冲突、默认不自动过期；无独立清理权限拒绝，有权限可不经逐批确认清理；保留最小审计、在用图片及藏书文件，清理结果明确失去保证。

### 必要的外围验收与回归

- **stdio 进程烟测**：真实启动辅助进程，连接同一生产组装服务，验证 initialize、工具发现与调用、凭据传递及撤权、协议 stdout 不被日志污染、断线重连、EOF 与退出，以及上游不可用时的失败。辅助进程不需要数据库访问资格；不为它再建立一套业务验收环境。
- **网页交互**：对凭据签发与限制、撤销、变更查询和手动清理补少量用户交互测试。现有 accountManagement 与 metadataReviewConcurrency 测试是权威响应及冲突呈现的先例，模拟 HTTP 的组件测试不能替代真实服务授权验收。
- **保留既有守卫**：生产管理集成测试对密码变更失效的覆盖、提案事务竞争测试、人工封面保护、内容句柄释放、生产服务 shutdown/drain。增加 MCP 路径回归，不把旧测试通过当成新功能已经具备。
- 实现门禁包含完整后端测试、Go 格式和 lint；前端测试、构建和 lint；涉及 SQL 或生成响应契约时检查生成物漂移。此处列的是未来验收，不是本次已执行结果。

## Out of Scope

- 不建立元数据待办、扫描后自动交给 agent、常驻 agent、增量水位或专用维护状态机；agent 不成为既有操作的必经环节。
- 不提供任意文件读取、整本归档下载、藏书文件删除或搬运、全库作者归并、用户管理或系统管理工具。
- 不把合集与个人阅读数据纳入本 effort 核心能力，不把资料库路径迁移解释为文件移动。
- 不覆盖已有非空描述字段、已有评分、锁定字段、人工封面；作者的系列内校正是已明确批准的例外，不推广到其他字段或全局记录。
- 不承诺整批原子提交、撤销已发出的外部请求或费用、清理后仍可恢复、撤权自动回滚已提交系列。
- 不引入独立用户审批渠道来验证 agent 转述的模型额度，不要求获授权清理的逐批确认；也不允许 agent 扩大凭据权限。
- 不为 Claude Code、Codex 或 ThinkRail 等具体产品的配置交互提供本次专属兼容性保证；先以通用协议客户端验收。
- 本次只发布规格，不运行 to-tickets，不创建实施工单，不开始业务实现。

## Further Notes

- 领域词义使用 mm-domain-language；提案终态约束遵守有效的 mm-adr-0011。mm-adr-0016 保存委托额度与清理自主权的取舍，当前仍为 draft。本规划的 ready-for-agent 是本地工程 tracker 的 triage 状态，不是永久规格的 active 状态，也不是已交付状态。
- 现有实现的重要差距：集合补空不等于追加；人工保存与扫描关联入口不能直接充当受限 agent 写入器；作者角色绑定共享作者行；提案可能复用或因无有效差异不保存；当前字段来源不是完整审计；受理时授权不会自动把凭据撤销传到后台运行。实施不能跳过这些差距，也不能改文档把它们当成已支持。
- SDK、标准授权兼容性、工具 schema、分页和图片硬上限、管理 API、操作标识与重试幂等、跨请求记账及服务重启行为、文字与封面的故障一致性、撤销存储归属和“补充后关闭”的具体表示尚未选定。它们是后续设计须落定的工程细节，不得在拆票时伪称访谈已经选定；若影响已确认的产品或安全边界，应回到用户确认。
- 尤其不能把任务的排队合并当作请求幂等，也不能把客户端断线不取消推导为进程重启自动续跑或自动重新发出付费调用。
- 官方协议与 SDK 资料的外部抓取受当前网络安全检查阻断，尚未完成兼容性核验，不以既有经验冒充当前标准验证。
- 本 effort 发布后由用户自行触发 to-tickets。实施时应把长期契约更新到所属目标、架构及模块规格；本规划保留为 effort 记录，不整体提升为新的永久契约来源。
