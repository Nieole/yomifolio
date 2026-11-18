# Manga Manager Mihon API 审计

## 结论与边界

**现有 API 足以作为浏览、阅读、下载扩展的基础，但不能称为完整 Mihon 集成，更不能承诺官方客户端双向页级同步。** 优先修正搜索排序契约、补齐同步读回与确认协议，并先推动客户端真实阅读事件能力；不是重写整个 API。[L1][L3][M1–M7]

本次仅调研，未修改实现、测试或规格；不审查并行开发的 `extensions/mihon`。审计目标是公开用户、五个固定独立服务器／账户槽位，以及未来按实际最近阅读发生时间同步、支持离线和重读回退。依据为 `internal/api/SPEC.md`、`internal/reading/SPEC.md`、`internal/organize/SPEC.md` 和 `.thinkrail/context/TASK-mihon-extension.md`；后者的能力假设以本报告直接核验的官方源码为准。

严重性含义：P0＝阻止承诺完整产品的门槛，不等同于已发现远程漏洞；P1＝公开交付／相应功能交付前应修；P2＝兼容性与健壮性完善。**未确认 P0 安全漏洞。** 下文“事实”来自源码／已有测试，“建议”是待设计方案，“风险”不是漏洞定论。

## 证据索引

### 本地一手证据（路径＋符号）

- **[L1]** `internal/api/mihon_controller.go`：`setupMihonRoutes`、全部 `mihon*` handler、响应结构、`positiveQueryInt`、`mihonImageQuery`、`mihonResourceBase`。
- **[L2]** `internal/api/protocol_search.go`：`searchProtocolSeries`；`internal/database/series_search.go`：`SearchProtocolSeries`、`searchProtocolSeriesFTS`、`searchProtocolSeriesSubstring`、`SearchSeriesPaged`、`buildSeriesSearchQuery`。
- **[L3]** `internal/api/controller_progress.go`：`UpdateProgressRequest`、`updateBookProgress`、`bulkSyncBookProgress`、批量请求与结果结构；`internal/api/reader_routes.go`：`setupBooksRoutes`；`internal/api/reader_http.go`：`currentReadingSubject`、`currentUserID`。
- **[L4]** `internal/reading/position.go`：`Position`、`RecordPosition`、`staleOccurrence`；`internal/reading/sync.go`：`SyncPositions`；`internal/database/reading_position.go`：`MutateReadingPositions`、`loadReadingPosition`、`persistReadingPosition`。
- **[L5]** `internal/api/auth_controller.go`：`requireBasicAuth`、`authGate`、`respondBasicAuthChallenge`；`internal/api/administration.go`：`requireProtocolEnabled`；`internal/api/host_routes.go`：`SetupRoutes`；`internal/api/collection_authorization_test.go`：`TestRegularUserCanReadButCannotMutateManualCollections`。
- **[L6]** `internal/api/image_controller.go`：`servePageImageByNumber`、`serveCoverImage`、`serveThumbnailImage`、`serveBookFile`、`pageImageCacheControl`；`internal/api/library_access.go`：`guardContentRead`。
- **[L7]** `internal/api/collection_view_controller.go`：`loadCollectionViews`、`loadStaticCollectionSeries`、`loadSmartCollectionSeries`；`internal/organize/smart_collections.go`：`Views`、`Members`、`SmartCollectionPageLimit`、`normalizeSmartFilter`；`sql/query.sql`：`ListLibraries`、`ListRecentAddedSeries`、`ListReadingListSeriesPage`、`ListStaticCollectionSeriesPaged`、`ListCollectionViews`、`GetMihonSeries`。
- **[L8]** `internal/reading/book_view.go`：`SeriesBooks`、`ReaderState`；`internal/booksort/booksort.go`：`CompareBooks`、`EffectiveBookSortNumber`。
- **[L9]** `internal/api/client_connection_controller.go`：`getClientConnections`、`filterEnabledClientEndpoints`、`requestBaseURL`；`internal/api/params.go`：`maxPageNumber`、`pageOffset`。
- **[T1]** `internal/api/mihon_controller_test.go`：`TestMihonAPILifecycle`、`TestMihonCollectionEndpoints`、`TestMihonReadingListEndpoints`、`TestMihonAPIRejectsInvalidIDs`、`TestMihonPageIndexIsOneBased`、`TestMihonRoutesRespectProtocolToggle`。
- **[T2]** `internal/api/protocol_reader_view_test.go`：`TestProtocolsPreserveReaderIsolationAnonymousProgressAndPagedLinks`；`internal/api/reading_list_controller_test.go`：`TestReadingListProtocolsKeepIdentityOrderAndPagination`；`internal/api/collection_view_paging_test.go`：`TestSmartCollectionProtocolsKeepTheirPagingLimits`。
- **[T3]** `internal/api/auth_controller_test.go`：`TestProtocolBasicAuth`；`internal/api/protocol_password_gate_test.go`：`TestProtocolAuthRejectsPendingPasswordChange`；`internal/api/protocol_bruteforce_test.go`：`TestProtocolBruteForceIsThrottled`、`TestProtocolThrottleBlastRadius`；`internal/api/book_download_controller_test.go`：`TestProtocolWholeBookRangeAndAuthentication`；`internal/api/image_controller_test.go`：`TestServeCoverImage`、`TestServeThumbnailImageETag`。
- **[T4]** `internal/reading/position_test.go`：`TestPositionAllowsNewerBackwardReadingAndRejectsDelayedReports`、`TestConcurrentPositionsKeepNewestOccurrence`、`TestIdentifiedRetryRemainsDuplicateAfterAnotherFact`、`TestOffsetOccurrencesCompareAsInstantsAcrossLegacyAndNewPositions`；`internal/reading/sync_test.go`：`TestSyncPositionsKeepsOrderAndIndependentResults`；`internal/api/progress_reviews_stats_test.go`、`internal/api/progress_offline_timezone_test.go` 的进度隔离、clamp、陈旧与时区测试。

### 官方客户端及参考源（不可变链接）

使用 `git rev-parse HEAD` 核验：官方稳定版 **v0.21.3 = feadad5e1e2bf638be6e2ca317d11657b09d4596**；官方 main **4823ba69ca65fa707fb89406f3a4c43c9e0053dd**；Keiyoushi 参考源 **f1bd88dacd89dfe644369c397efedf79965bf4c0**。部分稀疏检出未落盘文件通过 `git show HEAD:<path>` 直接读取，未依赖搜索片段。

- **[M1]** 稳定版 [Source](https://github.com/mihonapp/mihon/blob/feadad5e1e2bf638be6e2ca317d11657b09d4596/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/Source.kt)、[CatalogueSource](https://github.com/mihonapp/mihon/blob/feadad5e1e2bf638be6e2ca317d11657b09d4596/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/CatalogueSource.kt)：目录、详情／章节、页列表接口，无实际翻页／阅读发生回调。
- **[M2]** 稳定版 [Page](https://github.com/mihonapp/mihon/blob/feadad5e1e2bf638be6e2ca317d11657b09d4596/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/model/Page.kt)、[HttpPageLoader.getPages](https://github.com/mihonapp/mihon/blob/feadad5e1e2bf638be6e2ca317d11657b09d4596/app/src/main/java/eu/kanade/tachiyomi/ui/reader/loader/HttpPageLoader.kt)：`number = index + 1`，reader 按列表顺序重新从零编号，且存在缓存及预加载。
- **[M3]** 稳定版 [SManga](https://github.com/mihonapp/mihon/blob/feadad5e1e2bf638be6e2ca317d11657b09d4596/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/model/SManga.kt)、[SChapter](https://github.com/mihonapp/mihon/blob/feadad5e1e2bf638be6e2ca317d11657b09d4596/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/model/SChapter.kt)：源模型不提供写入客户端 `lastPageRead` 的标准字段；`memo` 也不是自动导入阅读状态协议。
- **[M4]** 稳定版 [ReaderViewModel.updateChapterProgress / writeChapterProgress](https://github.com/mihonapp/mihon/blob/feadad5e1e2bf638be6e2ca317d11657b09d4596/app/src/main/java/eu/kanade/tachiyomi/ui/reader/ReaderViewModel.kt)：真实页状态由客户端保存，`lastPageRead` 为零基；隐身模式、延迟保存等由 reader 控制。
- **[M5]** 稳定版 [TrackerManager](https://github.com/mihonapp/mihon/blob/feadad5e1e2bf638be6e2ca317d11657b09d4596/app/src/main/java/eu/kanade/tachiyomi/data/track/TrackerManager.kt)、[EnhancedTracker](https://github.com/mihonapp/mihon/blob/feadad5e1e2bf638be6e2ca317d11657b09d4596/app/src/main/java/eu/kanade/tachiyomi/data/track/EnhancedTracker.kt)：tracker 在客户端内置注册；增强跟踪不是扩展自行注册任意 tracker 的入口。
- **[M6]** main [SyncChapterProgressWithTrack.await](https://github.com/mihonapp/mihon/blob/4823ba69ca65fa707fb89406f3a4c43c9e0053dd/app/src/main/java/eu/kanade/domain/track/interactor/SyncChapterProgressWithTrack.kt)、[TrackChapter.await](https://github.com/mihonapp/mihon/blob/4823ba69ca65fa707fb89406f3a4c43c9e0053dd/app/src/main/java/eu/kanade/domain/track/interactor/TrackChapter.kt)、[DelayedTrackingStore](https://github.com/mihonapp/mihon/blob/4823ba69ca65fa707fb89406f3a4c43c9e0053dd/app/src/main/java/eu/kanade/domain/track/store/DelayedTrackingStore.kt)：合并取章节最大值；上报跳过不大于已有章节值的事件；离线队列仅保留每 track 最大章节数并存为 Float。稳定版同路径 `SyncChapterProgressWithTrack`、`TrackChapter` 也直接核验了相同关键逻辑。
- **[M7]** 稳定版 [ExtensionLoader](https://github.com/mihonapp/mihon/blob/feadad5e1e2bf638be6e2ca317d11657b09d4596/app/src/main/java/eu/kanade/tachiyomi/extension/util/ExtensionLoader.kt)：支持库版本 1.4、1.6。
- **[E1]** 参考 [Komga 源定义](https://github.com/keiyoushi/extensions-source/blob/f1bd88dacd89dfe644369c397efedf79965bf4c0/src/all/komga/build.gradle.kts)、[Komga](https://github.com/keiyoushi/extensions-source/blob/f1bd88dacd89dfe644369c397efedf79965bf4c0/src/all/komga/src/eu/kanade/tachiyomi/extension/all/komga/Komga.kt)：固定不同 source ID、多实例配置；此参考实际为三个实例，不是已实现五槽位的证据；源码还明确注明按 host 共享 session cookie 的账户限制，不应照搬到同 host 多账户方案。

## 端点完整性逐项核验

所有路径以下均相对 `/api/mihon/v1`。[L1]

| 范围 | 已核验事实 | 判断／建议 |
| --- | --- | --- |
| `GET /libraries` | 返回全站库 ID/name，按库名称列出，无分页。[L1][L7] | 可保留；这是共享藏书，不是每用户库 ACL。 |
| `GET /series` | `page` 从 1 开始，通常 `limit` 默认 30、上限 100，含 `total/page/limit/has_next`；支持 `libraryId/q/tag/author/status/sort`。[L1][L9] | 主干齐全；排序与组合语义有下述 P1。 |
| `GET /recently-added` | 可按库过滤、分页；SQL 按创建时间降序，另有稳定次序。[L1][L7] | 可映射“最近加入”，不要宣称是热门或真实阅读热度。 |
| `GET /continue` | 当前用户最近阅读书，只有 limit（默认 30、最大 100），无 page、库过滤与全量游标；`updated_at` 在此取阅读时间。[L1][T2] | 适合接续阅读书架，不可当完整进度拉取源；可能包含已读完条目，不能假定全是未完成书。 |
| `GET /collections` | 混合静态与智能视图；ID 是 `collection:N`／`smart:N`，另有 `numeric_id/kind`；保留数据库视图排序后的数组顺序。[L1][L7][T1] | 导航必须按 kind 选择成员端点，不能把字符串 ID 原样代入数字参数。 |
| `GET /collections/{collectionId}/series` | 静态合集分页，按成员顺序、显示标题、series ID 排序；无叠加 q/tag/library 参数处理。[L1][L7] | 阅读目录够用；筛选 UI 若允许同时选合集和关键词，将被静默忽略，需契约化。 |
| `GET /smart-collections/{filterId}/series` | 应用保存条件／排序和当前用户读态；分页显式 limit 上限 100。[L1][L7][T2] | 与普通搜索不是同一可组合查询；异常保存 pageSize 的 fallback 需防御。 |
| `GET /reading-lists`、`/{listId}/series` | 列表及有序系列分页；SQL 保留成员 sort_order，已有跨页顺序测试。Mihon 投影不携带列表条目备注。[L1][L7][T2] | 浏览组织可保留；不能称为完整组织编辑 API。 |
| `GET /tags`、`/authors` | 返回全站名称数组，非库内字典，非 ID／角色结构。[L1] | 可用于基础过滤，标明全站作用域。 |
| `GET /series/{seriesId}` | ID、库、标题、摘要、字符串状态、册数、总页、封面、更新时间；**没有系列作者与标签**。[L1][L7] | P1 补 detail 的作者／画师／标签；否则能按作者筛选却无法展示作者详情。 |
| `GET /series/{seriesId}/books` | 一书映射一 Mihon chapter；一次返回全部书。服务层按自然卷册顺序重排，不仅是 SQL 原始顺序；字段含 number/volume/sort_number/page_count/last_read_page，但无 is_read/last_read_at。[L1][L8] | 可阅读；扩展需稳定使用 book ID 作章节身份，不能用浮点章节号作同步主键。 |
| `GET /books/{bookId}/pages`、`/{pageNumber}` | 服务端页列表 index 与资源地址都是 **1 基**；可传格式／质量，图片实际媒体类型可能随转换改变，列表 media_type 是 manifest 原始类型。[L1][L6][T1] | API 不必破坏兼容改零基；扩展按数组顺序构造零基 Page，页请求仍用原 URL。 |
| `GET /covers/{bookId}`、`/thumbnails/*`、`/books/{bookId}/file` | 全在 Basic 子树；封面／缩略图／页图为 private、回源验证并支持 ETag；整卷用 ServeContent 支持 Range，MIME 按归档格式；图片请求不记录进度。[L1][L6][T3] | 基本正确；Mihon 标准下载是逐页取图，存在整卷下载端点不等于客户端会调用它。[M1][M2] |
| `POST /books/{bookId}/progress` | 可写 page、updated_at、fact_id；仅返回 status 字符串。[L1][L3] | 领域规则良好，协议读回／确认不足，见同步章节。 |

**组织边界：** 此协议没有合集、智能合集、阅读列表的创建／编辑／排序写入接口；现有普通用户契约允许读共享合集、不允许改写共享组织。未来“完整组织”若指导航与筛选，现状可复用；若指 Mihon 内编辑服务器组织，属于新增产品／授权边界，不能直接开放 Web 管理 handler，也不是源扩展标准 UI 能自动提供的功能。[L1][L5][M1]

## P0：完整页级同步尚不具备交付条件

### 已核验事实

1. **服务端能收位置，不代表普通源扩展能获取真实阅读。** 标准 Source 接口只有内容获取等方法；图片预取、缓存、下载与实际阅读不是同一事件。客户端 ReaderViewModel 拥有真实页变化且有隐身模式处理；不能在图片 handler 或扩展 image 请求中伪造上报。[M1][M2][M4][L6]
2. **内置增强跟踪不是所需同步协议。** main 当前章节同步取 max；延迟队列存最大 Float 章节数，没有逐页事实 ID、发生时间和重读回退语义。官方稳定版关键章节路径也如此。把 page 编码成章节小数会损失身份与精度，且仍无法表达最新事件回退。[M5][M6]
3. **远端页状态不能通过普通详情解析自动导入客户端。** SChapter 不含客户端 lastPageRead/read 写入字段；现有 enhanced tracker 主要导入章节读完状态。[M3][M6]

### 必须的后续工作（建议，未实现）

优先向官方客户端提出独立页事件／增强跟踪能力：真实阅读时生成稳定 fact ID、持久保存实际发生时间与时钟依据、离线队列、用户授权后导入远端页位置、隐身／下载不产生阅读事件，并按 source 槽位与账户隔离。上游接受并发布前，扩展只能宣告浏览／阅读／下载，不宣告完整双向页同步。**不要为绕过客户端限制而污染服务端进度真相。**[M1–M6][L4；目标规格见开头]

## P1：需要修改或明确契约的地方

### 1. 搜索排序确实存在分支不一致

**事实：** `mihonSeries` 的跨库纯关键词快路径条件不检查 `sort`，进入 `SearchProtocolSeries` 后按相关性等排序；因此 `q=foo&sort=updated_desc`／`books_desc` 不执行请求的排序。加 libraryId／tag／author／status 后反而进入普通搜索并执行 sort。库内关键字分页已经正确下推 SQL，不能把旧的内存过滤问题重新算成当前缺陷。[L1][L2]

**建议：** 明确定义默认相关性与显式排序，显式 sort 走能够共同处理过滤和排序的查询；测试跨库／库内、短中文／FTS 词、两页以上相同条件的总数、稳定次序与无漏项。热门入口若用 books_desc，只能称册数排序，不能伪称热度。[L1][L2]

**事实：** tag 与 author 都是单个参数按逗号拆分；同类列表内是 SQL IN（OR），不同类别／库／关键词／状态之间 AND；不 trim 每个成员、不支持可靠表达含逗号名称，重复参数也不是数组契约。组织成员端点不解析这些额外过滤字段。[L1][L2]

**建议：** 公开写清当前语义；为联合筛选提供明确端点能力或拒绝不支持的组合，不要只在当前页内存过滤。可兼容增加重复参数或 ID 形式、成员 trim 与长度上限。真实合集／阅读列表内搜索是否首发必需由产品确定，现状不是“已经完整组合支持”。[L1][L2]

### 2. 同步读回与 ACK 不足

**事实：** books 响应的 `updated_at` 来自书目 `Facts.UpdatedAt`，不是阅读事实时间；没有 `last_read_at/is_read/changed_at/fact_id`。continue 只覆盖最近有限条；缺少独立、完整的个人进度快照／增量读取。单写 ACK 只有 updated／unchanged 文字，无法区分 stale、重复或读取权威页值。[L1][L3][L8]

**事实：** 通用 `bulkSyncBookProgress` 有逐条 updated/skipped_stale/skipped_unchanged/not_found/invalid 与 page，但**未挂在 Mihon Basic 路由下**；Web `/api/books/bulk-progress/sync` 不能直接作为 Basic 客户端协议。通用批量是按输入顺序逐条提交、允许部分成功，不是整批原子事务；结果无 fact_id 回显。[L1][L3][L4]

**建议：** 最小补齐是在 Mihon 同一认证子树增加独立进度读取，并增强现有单条写入 ACK，复用 positions 能力；读取返回权威页、是否已读、实际发生时间、变更版本／删除清除信息；ACK 明确 applied/stale/duplicate/unchanged/not_found 与 fact_id、服务器当前状态、服务器时间。单条接口可以支持离线有序重放，批量不是正确同步的必备条件。全量／增量拉取与有界批量可在客户端需求核实后增加；届时须定义分页／游标、并发变化、body、条数及 fact ID 长度限制，不要开放任意 user_id。权威 ACK 应取自同一事务的裁决快照，不要先写入再独立查询并暗示后读结果就是该次写入结果。[L3][L4][L5]

### 3. 时间、去重与重读：已有正确基础，但仍缺设备时钟契约

**事实：** `updated_at` 在写入中作为 OccurredAt 使用；省略时取服务器接收时刻。领域允许较新时间写较小页，拒绝较旧事实；时间相同的带时间事件不能覆盖已提交事实。页面 clamp 到至少 1、已知页数内；page=0 不能表达清除／未读。事实收据按 `(user_id, book_id, fact_id)` 持久去重，并在事务内连同位置、basis 更新。只有实际 apply 才写入收据；stale／unchanged 不是完整事件日志。[L3][L4][T4]

**事实：** 当前 RecordPosition 不对未来客户端时间作偏差校正／上限判断，也没有设备时钟注册／锚点。时间很超前的同账户写入会使随后较早时间的正常事件被判旧；这是代码规则的直接后果，不是已证实跨账户攻击。无时间的离线补传会被当作“现在”，不能代表实际最近发生。[L4]

**建议：** 同步协议保留 actual occurrence，不以接收时间替代离线事实；制定时钟误差、未来时间拒绝／隔离、同刻并发仲裁与设备重装规则，返回服务端时间作为校准依据。不要简单把每条时间 clamp 到 now 后声称恢复真实先后。清除与标记已读如要同步，应走已有 reading 用例、保留抗重放证据，不把 page=0 当清除。当前字段 updated_at 应兼容保留，但文档明确它是发生时间而非书目更新时间。[L3][L4；目标规格]

### 4. 公共详情投影和连接说明需要补齐

**事实：** 系列作者／标签缺失，无法完整填充 SManga author/artist/genre；全站字典不能替代系列归属。书籍 sort_number 是持久字段，而实际返回次序还依赖文件名推导自然排序，不能假定 sort_number 唯一且单调；服务端字符串 status 需映射客户端枚举并为未知值兜底。[L1][L8][M3]

**建议：** 补充详情作者角色与标签；定义章节显示编号、服务端顺序与稳定 ID，扩展保留顺序并按 Mihon chapter-list 约定适配，不能重排成纯 Float 后破坏卷序。`date_upload` 如暂以 updated_at 填充，应明确那不是出版日期；可后续补真实发布日期。[L1][L8][M3]

**事实：** `getClientConnections` 会过滤关闭的协议，并非关闭仍显示 ready；但启用的 Mihon 端点 AuthNote 仍写 “No authentication is required by Manga Manager.”，与已有账户时的 Basic 认证不符。Mihon API 根路径只是配置基址，没有 GET 根 handler，不宜把根 404 自动诊断为协议关闭。[L1][L5][L9]

**建议：** 修正连接说明为条件式 Basic／首启匿名；可加版本与 capabilities 探测，清楚声明 paging、页基、同步能力。必须区分关闭 404、错误凭据 401、需改密 403、限流 429、存储繁忙 503，不能统统解释成登录失败。[L5][L6][T3]

### 5. 五槽位、同 host 多账户与反向代理

**事实：** 服务端从 Basic 解析当前用户，写入不读取请求指定的 user_id；用户进度投影／收据按用户隔离。库、目录和组织对象为站点共享；多个普通用户能看到同一目录不是已确认越权。服务端 ID 为本部署数字身份，不是全网唯一身份。[L1][L3–L5][T2][T4]

**建议：** 不必添加“slotId”到服务器请求。五个固定 source ID 分别绑定完整基址＋账户；缓存、离线队列、tracker 绑定、凭据与远端导入均按槽位隔离。更换某槽位服务器或账户不能悄悄复用旧漫画／进度；同源同 ID 也须有绑定变更保护。至少验五站同 book ID、同 host 两账户、同账户多设备、改密／重装。参考 Komga host-cookie 模式有明确限制，不可复制后宣称多账户隔离完成。[L4][M1][E1]

**事实：** 返回资源路径以 `/api/mihon/v1` 开头，是 origin-root 相对路径；不会反射请求 Host，因此这些资源链接本身没有观察到 Host 注入问题。若用户部署在 `/manga/` 且代理剥掉前缀，通用 URL resolve 会丢掉 `/manga/`。`requestBaseURL` 连接信息只拼 scheme/host/port，不恢复部署子路径，并读取转发头。[L1][L9]

**建议：** 选定并测试明确的前缀契约：例如配置 API 基址、仅把本协议已知根路径映射到该基址；或由服务端通过显式受信公共前缀产生链接。不要盲信客户端可控 X-Forwarded-Prefix／Host，也不要向任意绝对 image URL 或跨 origin 重定向自动带 Basic。根域 HTTPS 反代现有相对路径可保留；子路径兼容是待验收项，不是已完成能力。[L1][L5][L9]

## P2：健壮性与安全待核验项

- **错误一致性（事实）：** 非数字 ID 返回 400，详情无记录 404；不存在 series 的 books 路径只查询成员，可返回 200 空数组；页列表 guard 能拦缺书，但 manifest 后续错误除 busy 外统一 500。JSON error 与协议关闭的标准文本 404 并存。建议整理错误 code 与重试策略，允许空系列和不存在系列可区分。[L1][L6][L8][T1]
- **异常智能书架配置（静态核验）：** `positiveQueryInt` 对缺失／无效 limit 直接返回 fallback，未再 clamp；若数据库遗留 pageSize=0，Mihon 外层 limit=0、offset 恒为 0，而 Members 内部回退 30，响应 limit／has_next 与实际查询不一致。正常 Save 会把 0 规范成 30，因此不能说普通用户正常保存即可触发。建议先规范有效 limit，再统一计算 offset／响应；补异常存量配置测试。[L1][L7]
- **资源安全已有保护（事实）：** Basic 覆盖整卷、页图、封面、缩略图；缓存命中也经过认证中间件。协议关闭在 Basic 前返回 404；无账户首启可匿名直通，默认关闭协议降低初始暴露。公开部署应先建账户并使用 HTTPS，Basic 本身不提供传输加密。[L1][L5][T1][T3]
- **缩略图路径（风险，未证实漏洞）：** `serveThumbnailImage` 将 wildcard 与缩略图目录 Join，未见显式目录包含校验；但 ServeFile 还有 URL 路径防护，路由／解码／平台行为影响结果，不能仅凭 Join 宣称存在任意文件读取。建议对 `..`、编码分隔符、Windows 反斜杠、绝对路径／盘符、符号链接作完整路由测试，并考虑在文件打开前校验目录边界。cover_path 来自库中数据，不应等同于直接外部输入。[L6]
- **归档缓存（建议）：** 原始附件未像图片显式设置 private 策略；当前仍有路由认证，尚无共享缓存泄漏复现。公开部署补明确 Cache-Control 与鉴权缓存测试，防止代理错误缓存敏感附件。[L6][T3]
- **时间并发（非缺陷定论）：** 已有位置事务与并发新旧事件测试，不能凭多个客户端同时写就认定 lost update；真正缺口是跨设备时间可信度及相同 occurrence 仲裁契约。[L4][T4]
- **规模（事实／建议）：** 书列表、字典、库／组织列表不分页；页列表全量是源接口常规需求。先用大库测试观察延迟／内存，再决定书目分页或字典缓存；不得强行分页后让扩展只拿第一批章节。[L1][M1]

## 可保持不变

1. `/api/mihon/v1` 独立 Basic 鉴权子树、默认关闭、账户强制改密和限流；不要退回 Web cookie，也不要放开图片匿名访问。[L1][L5][T3]
2. 服务端 1 基页码与 URL，扩展／未来客户端边界做 `+1/-1`；已有页图回读测试应保留。[T1][M2][M4]
3. shared reading 用例、事务去重、当前用户投影、较新事件允许回退；不新造一套 max-page 同步存储。[L4][T4]
4. 组织成员顺序、读取已有智能条件、reading-list 页化 SQL；系列／书稳定数字 ID 在单部署内继续复用。[L7][L8][T2]
5. 协议范围资源、图片 private+ETag、归档 Range 与内容读取期间的库访问协调。[L6][T3]

## 验收建议（尚未新增测试）

- **P0 同步链路：** 官方客户端真实翻页、隐身、离线阅读、下载／预取无写入、重新联网按发生时间导入／重放；重读回退；不使用章节 max 队列替代事件队列。[M1–M6]
- **P1 服务端契约：** 搜索排序与所有组合多页遍历；组织过滤的支持／拒绝语义；双向 progress 快照、清除、增量断点、ACK fact ID、clamp、重复及部分失败重试；未来时间／偏差与同刻仲裁。[L1–L4][L7]
- **P1 部署／隔离：** 五个槽位相同 series/book ID、同 host 不同用户、凭据更新后缓存、子路径反代、非标准端口、HTTPS、跨 origin URL／重定向不泄漏凭据。[L1][L5][L9][E1]
- **P2 资源／错误：** 页、封面、缩略图、附件的 401／403／429／404／503 和条件请求；整卷 Range 206／无效 Range；路径边界跨平台；异常 pageSize；大目录与零页／空系列。[L1][L5–L8]

## 本次实际验证

只执行现有 Go 测试，未创建临时测试文件；未构建 Android APK、未做真机／反代实测。第一条命令中的 database 没匹配测试，随后对 reading/database 跑了全包补足；全部下列命令成功：

```bash
go test ./internal/api ./internal/reading ./internal/database -run 'Test(Mihon|Protocol|Protocols|ReadingListProtocols|SmartCollectionProtocols|Position|SamePageNewFact|IdentifiedRetry|ConcurrentPositions|SyncPositions|SyncClamped|SyncPosition|ReceivedFacts|UpdateBookProgress|BulkSync|BookProgressPageClamp|ReadingProjectionHTTP)' -count=1
go test ./internal/reading ./internal/database -count=1
go test ./internal/api -run 'Test(ServeCoverImage|ServeThumbnailImageETag|ServeBookFileWholeArchiveDownload|GetClientConnections|ReadingListProtocolsKeepIdentityOrderAndPagination|SmartCollectionProtocolsKeepTheirPagingLimits|ProtocolWholeBookRangeAndAuthentication|ProtocolAuthRejectsPendingPasswordChange)' -count=1
```

测试通过说明已覆盖行为未回归，**不证明上述新增同步契约、五槽位、攻击路径或部署场景已实现／验证**。本次应先确认 P0 客户端同步门槛与 P1 服务端最小补齐范围，再启动实现。
