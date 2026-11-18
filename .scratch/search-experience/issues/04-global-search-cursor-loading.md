# 04: 稳定游标与弹窗继续加载

**What to build:** 读者可以在同一弹窗继续找到首批之外的系列和书，看到已加载数量与是否还有更多；追加失败不丢失结果，游标失效时能明确重新搜索。长标题不能让后续结果永久不可达。

**Blocked by:** 01 — 搜索等待、失败与旧响应隔离；03 — 书宽松匹配与全局精确命中优先。

**Status:** resolved

- [x] 按正式规格扩展既有全局请求：默认 target 为 all，保留 series/book/title 别名；limit 默认 20、最多 100，针对混排整批。非法 target、非法/非正/超上限 limit 返回 HTTP 400。
- [x] 使用统一排名后的 keyset 分页与 limit 加一探测，不执行全局完整 COUNT，不先对各类型截断再错误宣称后续不可达。需要多次底层读取形成一批时使用同一读事务。
- [x] 游标绑定格式/排序版本、规范化关键词 SHA-256 摘要、规范化 target、有效读者身份、权限上下文摘要及完整有界排序键；摘要采用版本化规范编码，不携带无限长查询、标题或权限列表。
- [x] 验证 URL 安全载荷的结构、类型、数值、版本及 8 KiB 上限；服务端生成的游标必须能被自身在同一限制内接受并续页。游标不是授权凭据，每次查询重新鉴权并在筛选后分页。
- [x] 无效、过长或查询/身份/权限上下文不匹配的游标返回 HTTP 400 与稳定错误码 invalid_search_cursor，不静默忽略。只有输入分隔或宽度不同但归一化后相同的关键词可以共用游标。
- [x] 保留 hits、total_hits、max_score、身份与既有 fields 字段，新增 has_more、next_cursor；hits 始终为数组，末页游标为空字符串。total_hits 仅为当前批条数，不是完整总数或累计数。具名响应与 fields 类型纳入既有 Go 到 TypeScript 生成和漂移检查。
- [x] 弹窗保留全部/系列/书切换，显示去重后已加载数和是否还有更多；继续加载立即执行，同一查询代际只允许一个追加请求，成功后才推进游标。
- [x] 普通追加失败保留结果、有效选择和未消费游标，可重试；游标失效显示显式“重新搜索”，禁用继续追加，由用户动作从首批重新开始，不静默混接新旧结果。
- [x] 新查询、切范围、关闭或重置使旧游标和追加请求失效；取消无效的旧响应不能回写。追加去重并保持有效选择，键盘操作不越界。状态文案完成本地化及辅助技术支持。
- [x] 同一静态数据集按任意合法批量走到末页，与完整预期集合和顺序一致，无重漏。至少覆盖书、系列都超过旧首批限额、跨资料库、全部/单类型、同名同分、空批及末页。
- [x] 经既有元数据写入入口保存足以使完整标题游标超过 8 KiB 的标题并放在分页边界，证明生成游标仍合法且可续页、标题未截断、结果未丢弃；同时覆盖长但合法关键词的固定摘要绑定。
- [x] 使用真实 HTTP 验证契约、权限、错误与兼容；前端组合测试覆盖连按、追加失败重试、重复响应去重、失效后重新搜索及过期追加隔离。协议分页和真实总数不被全局分页改动破坏。
- [x] 明确游标不是快照：静态数据保证无重漏，并发增删改不承诺跨批完整性；前端去重不冒充快照保证。完成本票前后端门禁及契约生成检查，不把核心验证推迟到 05。


## Answer

当前结果：全局搜索按完整有界键续页，每类型读取键后前 limit+1 个最佳身份，再统一混排取整批；没有全局 COUNT、offset 深页、全库原文归一化或候选来源截断。两个类型共用只读事务。HTTP 默认 all/20，保留 title/book 别名，严格校验 limit；空批形状一致，total_hits 为本批条数。SearchResult、SearchHit、SearchHitFields 纳入 tsgen，前端直接消费生成类型。

游标是无状态固定宽度二进制载荷，严格 URL-safe 无填充 base64；输入最多 8 KiB，当前生成长度固定 124 字符。格式和排序版本、版本化规范 JSON 元组编码的归一化查询 SHA-256、target、有效读者、版本化目录策略/当前角色的权限摘要、完整数值键均参与绑定与校验；没有携带或截断查询、标题或权限列表。拒绝非法编码、结构、枚举、整数域、键内部关系、版本或上下文失配，返回 400/invalid_search_cursor；非法 URL 转义与重复 cursor 也不能被静默丢弃。每次仍走现有 authGate；当前项目没有资料库 ACL，所有有效读者可读全部目录，不虚构新的权限模型。角色变化使游标失效，要求改密或缺少认证仍分别返回 403/401。未来 ACL 必须在候选 LIMIT 前筛选并扩展摘要。

弹窗显示身份去重后的累计数量与更多/末页状态；同步单请求守卫防止连按，追加成功才推进游标。普通失败保留列表、选择与未消费游标并可立即重试；invalid_search_cursor 保留可查看列表、禁用追加、由显式“重新搜索”建立新代际。关键词、范围、关闭、重置与有效身份/连接作用域变化隔离不可取消的旧追加。状态完成中英本地化、英文单复数与可访问通知；结果 listbox 表达有效选择，底部继续加载区不随结果滚动消失。

### TDD 证据

正式规格已批准的两个 seam：真实 HTTP + SQLite，以及真实 Layout/弹窗/会话配合可控 axios 响应；没有增加测试专用生产 transport。

- RED：TestGlobalCursorHTTPWalk 首批请求 limit=1 返回 20 项。GREEN：跨两个资料库、45 本书及 45 个系列，all/series/book/title 按 1、7、20、100 项走到末页，与完整字面顺序相同；混合层级/同分/多字段 oracle 也覆盖全部后续结果。
- RED：具名 fields 改造后空 cover_path 被省略。GREEN：保留全部兼容字段；空输入、仅标点、无命中均为数组和完整响应形状。
- RED：不可能的排序键（起始位置大于额外字符数）被接受；畸形 URL 转义导致 cursor 被 URL.Query 静默丢弃。GREEN：两者真实 HTTP 均返回 invalid_search_cursor，并覆盖重复 cursor、版本/数值/类型、超长、跨关键词/target/读者、角色变化及重新鉴权。
- RED：真实弹窗仍只播报“找到 2 条结果”，没有累计数或继续加载。GREEN：组合交互覆盖单追加、失败同游标重试、重叠去重、选择保留、显式重新搜索、末页，以及切词/范围/关闭/重置后的不可取消旧追加；hook 补充有效身份切换时的旧追加隔离。
- 经 UpsertBookByPath 保存 30 KiB 以上完整标题并使其成为分页边界，短查询及完整长标题查询均可逐页续读，标题原样返回；title/book 共用游标、变批量、边界行删除及后继空批也通过。现有 OPDS/Mihon、资料库筛选/真实总数测试未改协议契约。

### 验证命令与结果

命令均在本工作树执行，Go/Node 使用指定真实 bin，TEMP/TMP/TMPDIR/GOTMPDIR=C:/tmp，GOCACHE=C:/Users/saber/AppData/Local/go-build；lint v2.15.0 使用独立 GOLANGCI_LINT_CACHE=C:/tmp/lint-search-cursor-loading-04。

- `go test ./... -p 2 -count=1 -timeout=480s`：有一次全量通过，api 136.292s，database 9.719s；最后一次重复全量遇到现有 TestConfigFailedUpdatePreservesConcurrentExternalEditor/reload 的 Windows config.yaml rename Access is denied（日志 .tmp/04-verification/go-all.log），其他包通过。最后 `go test ./internal/api -run 'TestGlobalCursorHTTP|TestConfigFailedUpdatePreservesConcurrentExternalEditor' -count=1` 再验通过（18.211s）；本票全部 HTTP 用例及该环境失败用例均通过，不能把最后一次全量日志说成全绿。更早高并发运行/过短超时还出现启动超时与无关迁移测试偶发失败，未更改这些产品路径。
- `go test ./internal/api ./internal/database ./internal/catalogsearch -run 'TestGlobalCursorHTTP|TestGlobalRankHTTP|TestSeriesNormalized|TestBookNormalized|TestCatalogueSearchFailure' -count=1`：通过；后续严格 URL/权限回归再次单独运行通过。
- `gofmt -l ./cmd ./internal`：空输出；golangci-lint v2.15.0 `run ./... --timeout=5m`：最终 0 issues；`git diff --check`：通过。
- 指定 Node 26 运行真实 npm-cli `npm test -- --maxWorkers=2`：119 文件、1143 测试通过。最后补充身份追加断言后，`vitest run` 四个搜索文件及 pluralForms，`--pool=threads --maxWorkers=1`：5 文件、214 测试通过。早期全量 threads 模式出现 worker 启动超时及既有 heatmap 时区测试失败；最终标准 forks 模式全量通过。新增英文 loaded 词条曾被 pluralForms 检出未分单复数，已修正并通过。
- `npm run build`、`npm run lint`：最终通过；不是以拷贝 dist 代替自身构建。
- `go run ./cmd/tsgen`：成功，重生成前后 SHA-256 相同；PowerShell `sqlc generate`：退出 0，SQL 生成产物无差异。产品 SQL/schema 没有变更。
- 只读确认分支 search-cursor-loading-04，`git merge-base --is-ancestor 6fd351d8555dfcfa8d07fd9c1e4e4e01b790b97e HEAD` 成功；未写索引、提交、合并或清理工作树。

### 文档与交接

已同步 catalogsearch、database、API、web 持久 SPEC 及 CHANGELOG；规格明确游标不是跨批快照，静态无重漏不扩大为并发增删改下的完整性保证。没有执行 05 的性能预算，也没有声称性能通过；设计保持索引驱动候选、有界键与无 offset 深页。没有运行真实浏览器视觉验收或文档风格全库脚本，不将 jsdom/构建当作实际视觉证据。

自包含对照 HTML 已写本树，供协调者 visualize：`.thinkrail/context/mockups/04-search-loading-before.html`、`04-search-loading-after.html`；after 包含追加、失败重试和游标失效重新搜索的演示状态。未开 PR，所有产品、测试、文档与视觉稿均保留未提交，由协调者收尾。
