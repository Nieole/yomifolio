---
id: mm-identification
type: submodule-design
status: draft
title: 系列识别、证据包与 agent 主循环
parent: mm-application
references: [mm-domain-language, mm-adr-0009, mm-adr-0011, mm-adr-0017, mm-metadata, mm-seriesmeta, mm-proposal, mm-scanner, mm-database, mm-bootstrap]
---

## 责任与边界

`identification.Service` 拥有**系列识别**：按「系列 × 来源」保存的识别结论、排除候选、上报、查无此作结论与已审定决定，以及由它们推导的**识别状态**。它还拥有**证据包**及准备它的预取与重搜运行、agent 的主循环（领取租约、提交裁决）、agent 的纠正与巡检上报，以及所有者在网页上的识别决定。词汇归 `mm-domain-language`，取舍理由归 ADR 0017（draft）。

它不直接写系列元数据。识别确认后的补充、撤换时的撤回与 agent 的字段改正，都在识别的同一写事务内交给 `enrichment.Service`（策略见 `mm-application`「按识别补充」）；来源范围、扇出、快照校验与限速节拍归 `mm-metadata`；占位标题与来源记录的判定归 `mm-seriesmeta`；提案结案归 `mm-proposal`。agent 用例每次调用经 `delegated.Service` 重查当前授权，写入在实际事务内 `AuthorizeTx`：领取、提交、重搜、撤换与改正要 `metadata_write`，进度、识别图片、巡检与挂备注只要 `read`，质疑识别会改识别状态，另要 `metadata_write`（用户决定）。所以普通用户的凭据只能读和挂备注。重搜是运行，只有 `metadata_write` 的凭据也能用运行类工具列出并跟进它（用户决定，规则归 `mm-application`）。所有者用例接收操作账户，在写事务内重查它是当前管理员。

属于系列的持久记录都随系列级联删除（存储布局归 `mm-database`）；路径迁移不换系列 ID（ADR 0009），结论原样保留。时间以 Unix 毫秒保存，取自装配注入的时钟（`mm-bootstrap`「识别时钟接缝」）。

## 识别结论与识别状态

- 结论记来源条目（候选 ID 形如 `<来源>:<条目编号>`，`SourceKey` 与 `EntryOf` 是来源键与条目编号的唯一入口）、确认者类别（`owner`，或 `agent` 及其用户与凭据）、确认时间、一句理由（所有者确认可为空）、来源条目的原名与别名，以及确认时该候选的来源快照。补充取值和作者校正的证据都读这份快照。别名取自来源快照的 `Aliases`：各来源能给出的其它名称（归属见 `mm-metadata`），拿不到的来源照实为空。
- 识别状态由视图 `series_identification_status` 唯一定义，每次启动重建，规则改动对存量库立即生效。优先级从高到低：已审定、待复核、待人工、已识别、查无此作、未处理。上报的 class 决定它的影响：`identification` 对应待人工，`recheck` 对应待复核，`note` 不影响状态。被新启用来源重开的查无此作算作未处理。
- **排除候选**只由人的否决产生：所有者撤换或撤回识别、历史驳回、在待人工页否决。证据包读取时按当前排除过滤，抓取之后才否决的条目也不会出现；agent 的裁决与撤换引用它即被拒绝。所有者在手动搜索里亲手挑中排除候选时，排除解除，照常记为人工确认（用户决定）。
- **已审定**由所有者设置、随时撤回；重复设置保留第一次的决定，撤回本来没有审定的系列不做改动。已审定的系列不被领取、不做识别补充、agent 的撤换与改正以 `owner_holds` 拒绝；重搜与备注不受影响，agent 可以重搜已审定的系列，只并入证据包（用户决定）。

## 存量推导

`DeriveFromHistory` 在 `bootstrap.Open` 里执行，推导与完成标记在同一事务提交，中断后重跑、重复执行结果都不变：

- `applied` 的提案，以及 `proposal.Accepted` 认定已部分接受、整体仍待审的提案（用户决定），转为对应来源的人工确认识别。确认时间依次取 `applied_at`、仍指向它的字段来源记录里最早的写入时间、提案的 `updated_at`。同一系列同一来源多次应用取最近一次；同一条目既应用过又驳回过时只记识别。
- `rejected` 的提案条目转为排除候选；`supplemented_closed` 与没人接受过的待审提案不转换。
- 来源按提案的 provider 归一化，条目编号取来源 ID，缺失时从来源 URL 的形状解析；定位不到条目的提案（LLM 提案、多来源委托提案）跳过。
- 推导出识别的系列，其余从没被接受过的待审提案在同一事务以 `superseded` 结案（用户决定），沿用 ADR 0011 的事务内 pending CAS。
- 推导出的结论没有证据包快照，只贡献条目链接；原名与别名取自提案原始结果，较早的结果没有别名就为空。作者校正的证据改读它来自的已应用提案的原始结果。

## 证据包

- 每个系列一份，整包替换，只含文本：本地线索（目录名、书数、前三本的文件名、前三本里第一份可读 ComicInfo 的系列名、作者与年份）、各来源的状态（`available` 或 `unavailable`）、每个来源至多三条候选（标题、原名、作者、年份、卷数、简介摘要）、抓取时间，以及最近的自定义重搜记录。按候选 ID 另存校验过的来源快照（含 Bangumi 标签票数），供识别时取值，不进 agent 看到的文本。
- 每条候选附可核对的比对信号：`title_matches`（候选标题或原名规范化后等于清洗后的目录名或 ComicInfo 系列名）、`author_matches`（ComicInfo 编剧、作画与候选作者有交集，任一侧没有作者时不给）、`source_volumes` 与 `local_books`（两个数并列，不作比较）。它们是事实，不是匹配概率，与 `mm-metadata`「来源 confidence 不是身份概率」一致。
- 搜索词由 `searchTerms` 确定性生成，按顺序去重：清洗后的目录名、清洗后的 ComicInfo 系列名、系列名（没有时用目录名）加 ComicInfo 第一作者。清洗去掉括号段（书名号只去符号）、卷号与话数标记、汉化与扫图组名、版本词；整个名字都在括号里时退回括号内容。每个词至多 100 字符。
- 每个词向每个来源取前五条，合并去重后标题与本地名称一致的排前，每个来源留三条；定位不到条目的结果不进包。一个来源对所有词都失败记为 `unavailable`，其它来源照常保存；没有任何来源应答的系列不存包，在运行里记一条失败，保持待预取。
- 字节预算：预取包的 JSON 至多 4 KiB，重搜过的包至多 8 KiB。超出时先去掉摘要，再从排名靠后的候选删起；重搜只删本次新增的候选，原有候选从不删，最后删最旧的重搜记录。

## 预取

- 任务类型 `PrefetchTask`（`evidence_prefetch`）有两类身份。**手动回填**（变体 `all_libraries` 为系统作用域，`one_library` 为资料库作用域）由所有者在网页发起，可暂停、取消、可重试、**不可续跑**，只处理还没有证据包的未处理系列，所以再次发起时从没备好的系列继续（过期的包由领取触发的增量刷新处理）：无人看着时自动续跑会连发数天没人要求的外部请求，与刮削的先例一致。**增量预取**（变体 `incremental`，资料库作用域）**可续跑**：它本来就是无人值守发起的，工作量限于请求过的系列，再发起一次只会把没做完的做完。两类身份不同、可以同时运行，共用各来源的节拍，总请求速率不变；同一资料库的两次增量发起合并，因为要处理的系列记在持久的请求里，运行开始时才读取。
- 增量请求有两个来源：扫描每个已提交批次新建的系列（扫描开始时不存在的，经 `Scanner.SetCreatedSeriesHook`），以及领取时发现失效的证据包。扫描收尾由 `application/scanning` 经 `Effects.Prefetch` 调 `ChainIncremental` 发起该资料库的增量运行：整库、监听、定时与 agent 经 MCP 发起的委托扫描都串联（用户决定）；扫描被取消时不串联，部分归档失败时照常串联。增量运行不带委托归属，只读本地 ComicInfo 并访问外部来源，不写系列元数据。没有来源应答的请求保留待下次，不再需要证据包的请求由下一次增量运行清掉。升级前就有、还没有证据包的系列留给手动回填。不把系列推给 agent。
- 「低优先级」指扫描结束后才发起、不影响扫描结果；与其它预取和重搜共用 `metadata.SourcePacer` 的各来源节拍；ComicInfo 以 MetadataScan 磁盘作业读取，阅读优先。各来源的最小请求间隔来自 `identification.source_intervals_ms`，修改即时生效，负值不限速（供测试）。
- 证据包**有效**指：抓取不超过 `PacketMaxAge`（30 天），并且系列若被新来源重开，抓取晚于该来源启用的时间。刷新重新搜索派生词与包里记下的自定义重搜词，用新的抓取时间整包重建；刷新期间包被重搜或别的运行改过时不覆盖，请求保留。
- **新启用来源**：`SyncSources` 在 `Open` 与每次配置保存或重载后记录各目录型来源的启用时间，第一次同步时已启用的来源视为一直启用，停用即删、再启用算新启用。查无此作的判定早于某来源的启用时间、又没搜过它时，系列被重开，增量刷新只问重开的来源；判定时已启用但不可用的来源不算新启用。再判查无此作时搜过的来源取并集，识别成功则删掉旧的查无此作。已识别的系列不因新来源重开。

## 领取与裁决

- `Claim` 可按资料库过滤，默认取凭据的全部资料库，任一越权则整次拒绝。每批至多 `MaxClaim`（20）个系列，整批 JSON 不超过 30 KiB：逐个放入，下一个放不下就截止，没放进去的不领取、下次再发；单包预算不为凑满一批而再压缩。只发放有有效证据包、处于未处理、不在他人有效租约下的系列。在同一个写事务里重查每个资料库的 `metadata_write` 并写租约，并发领取由这个事务串行化、不会重叠。失效的证据包按领取顺序记为增量请求并在提交后发起刷新，刷新期间这个系列暂不发放。
- 一次领取发一个租约，时长 `LeaseDuration`（30 分钟）。过期但没被别人领走时原租约照样能提交；被新领取接手后，原租约的提交以 `claimed_by_other` 拒绝，没被领取过的系列以 `not_claimed` 拒绝。空批带 `empty_reason`：`prefetch_pending`（有未处理系列还没有有效证据包，优先报）、`all_claimed`（就绪的系列都在有效租约下，包括调用者自己的）、`all_processed`（没有未处理系列）。每个系列附 `open_notes`，列出它已有备注的分类，提醒这次要给结论。
- `Submit` 每个系列一条裁决，理由去掉首尾空白后 1–1000 字节：`identify` 引用 1–5 个候选、每个来源至多一个，一次可识别到多个来源；`not_found` 不带候选与分类，记证据包里状态为 `available` 的来源为搜过的来源；`escalate` 必须带分类。结果逐系列返回，不保证整批原子。拒绝顺序为：格式、授权（不存在与无权读来相同）、请求键、状态已不是未处理、租约、候选（`excluded_candidate` 先于 `not_in_evidence`）；被拒的系列原样不动，拒绝带 `problem` 代码和下一步提示。
- 识别与补充在同一写事务提交：写结论后调 `enrichment.Service.EnrichIdentified`。补充失败时整条裁决回滚，报 `unavailable`，重试是安全的；拆成两个事务的话，已识别的系列不再被领取，补充一旦失败就永远补不上。裁决结果带补充收据的 `operation_id`。补充收据的请求键由「凭据、请求键、系列」派生（`identify-` 前缀），与其它补充收据同在凭据的请求键命名空间。
- **重放**：判定结果按「凭据 × 请求键 × 系列」保存，与判定写在同一事务。同键同内容直接返回原结果（先重查当前授权），同键换内容以 `request_key_reused` 拒绝；摘要带用例种类，撤换与改正共用这个命名空间。格式错误、授权拒绝与写入失败不保存，重试时重新判定。

## 上报

- **识别类**（`ambiguous_candidates`、`uncertain_edition`、`missing_from_sources`）使系列进入待人工，只能经提交裁决给出（要 `metadata_write`）。**复核类**使系列进入待复核：系统的 `new_book_contradiction`，以及 agent 经 `library_report_series` 给出的 `disputed_identification`。**备注类**（`directory_grouping`、`suspected_duplicate`、`tag_problem`、`protected_value_doubt`）只挂备注：不改识别状态，不结束租约，任何状态的系列（包括已审定）都可以挂。
- **质疑识别**（`disputed_identification`）接受至少有一条识别结论的系列，人确认的与 agent 确认的都可以（用户决定）：agent 认错了自己的识别、又不能撤换时（系列已不在已识别，或它拿不准该换成哪条）用它交给所有者。结论与字段都不动，系列进入待复核；所有者在详情页撤回该识别（旧条目记为排除候选）或在待人工页点「识别无误」。agent 不获得撤回原语。质疑会改识别状态，所以要 `metadata_write`（用户决定）：只读凭据与普通用户的凭据只能挂备注，工具的分类 enum 与描述也只给它们备注类。没有任何结论的系列以 `ErrNothingToDispute` 拒绝，提示走领取与提交。
- `protected_value_doubt`（用户决定）用于 agent 认为有误但不得改动的值：人工编辑过的值、ComicInfo 导入的值、锁定字段、人工或锁定的封面。改正被拒为 `locked`、`not_from_source`，以及封面改善得到冲突收据时，提示都指向它。
- 去重由 `raiseOnce` 执行。同一系列已有同分类的备注就不再记，不论它还开着还是已被所有者关闭（用户决定）：经 `library_report_series` 再报返回 `recorded:false`、原理由不变；经提交裁决再报，裁决照常接受但不新记。识别类与复核类只在同分类仍未关闭时不重复记。所以上报是幂等的，不需要请求键。
- 上报的授权在记录事务内用 `read` 重查，系列不存在与无权读来相同；结果带系列当前的识别状态，表明备注没有改变它。

## 待复核检查

`RecheckNewBooks` 由装配经 `Scanner.SetAddedBooksHook` 在扫描每个批事务提交后同步调用，自开一个短事务，只读本地数据、不访问外部来源；出错只记 warn 日志，不让扫描失败。判据：

- 线索只取扫描**新加入**、扫描开始前已存在的系列的书的 ComicInfo 系列名；改名重连、强制重扫重读的旧书不算。没有系列名，或规范化后为空，就是没有线索，状态不变。
- 只检查至少有一条识别结论、未审定、没有未关闭复核上报的系列。
- 系列名与该系列所有来源结论的标题、原名、别名逐一比对，规范化用 `database.NormalizeSearchText`（与资料库搜索同一口径）。与任一名称相等，或一方包含另一方（被包含的一方至少两个字符），就不矛盾；结论里一个名称都没有时无从比对，不算矛盾；等于该系列认可名字的不算矛盾。来源名常与 ComicInfo 差一个副标题，较早的历史推导结论也没有别名，包含关系用来压住这类误报。
- 判为矛盾时记一条 `recheck` 上报（`raised_by=system`、分类 `new_book_contradiction`，理由写书名和系列名，`clue` 存那个系列名），不改识别结论和系列元数据。

## 重搜

`Research` 是 MCP 原语与管理员网页共用的同一用例：用自定义词为一个系列重搜，结果并入它的证据包。

- 搜索词必须是合法 UTF-8，不含控制字符与 Unicode Cf 格式字符；连续空白合并后 1–100 个字符，规范化后至少含一个字母或数字。原样发给来源，不做目录名那套清洗。规范化后等于某个派生词或已记录的重搜词时直接返回当前的包（`already_searched`），不建运行。
- 每个来源取前五条，去掉包里已有的、被排除的与重复的，按「标题与本地名称一致的在前」每个来源至多新增三条，比对信号对照包里原有的本地线索计算。原有候选原样保留、位置不变，新候选排在同一来源原有候选之后；应答过的来源转为可用。每次搜索在包里记 `{query, searched_at, added, dropped}`，只留最近五条；抓取时间不变。没有证据包的系列先像预取那样读本地线索、用派生词建包，再并入；预取保存时发现包已由重搜写下就保留它。没有任何来源应答时运行失败，包不变，这个词不记录，可以用同一个词再试。
- 重搜是一次运行：任务类型 `evidence_research`，作用域是系列所在资料库，变体 `series:<系列 ID>`，agent 发起的运行经 `PrepareRun` 带上凭据归属。每个发起方对每个系列只有一个任务，排队时再发起会并进那条运行。可以取消，不能暂停，不可重试；断线只结束等待。每访问一个来源前后调用运行检查点，写入事务里再查一次授权。用例最多等 `wait_ms`（默认 20000，上限 30000），等不到时返回 `running` 与 `run_id`，之后用同样的参数再调用一次读到这次运行存下的结果，不会再搜一遍。

## agent 的纠正

- **撤换** `Replace`：`candidate_ids` 取自该系列证据包（1–5 个，每个来源至多一个），每个候选替换该系列在它那个来源上由 agent 做的识别。该来源没有识别（`not_concluded`）、已经是这个条目（`already_identified`）、是人确认的（`owner_confirmed`，提示改报 `disputed_identification`）都拒绝。判定通过后走与所有者撤换相同的 `replace`：删掉旧结论，`enrichment.Service.RetractIdentified` 撤回旧条目带来、之后没被改动的字段（单独一张可撤销的收据），再按系列现有的全部识别重新补充。被替换的条目不记排除候选，因为排除只由人的否决产生。
- **改正字段** `CorrectField`：只改 `title`、`summary`、`publisher`、`status`、`rating`，只限未锁、且 `seriesmeta.SourceBacked` 认定当前值仍等于来源记录值的字段（升级回填的 `manual` 记录不算）。不区分来源记录出自 agent 的识别、人工确认的识别还是历史提案应用；人工编辑与 ComicInfo 值没有来源记录，因此碰不到。拒绝为 `locked`、`not_from_source` 或 `unchanged`。改正写一条新的来源记录（source 为 `agent`），扫描照样保留它、agent 也能再改正；收据保存被替换值原来的来源记录，撤销时原样放回。改正后的评分不是公式结果，之后不会被重算。
- agent 不能撤销所有者网页操作留下的收据（挑候选、手动识别、撤换与撤回的补充和撤回收据），拒绝时提示改为上报（规则归 `mm-application`「安全补充与持久撤销」）。
- 撤换与改正都拒绝待人工、待复核、已审定的系列（`owner_holds`，带出当前状态）：这三种都在等所有者，复核之前内容不该被动。标题占位例外只由服务端在识别补充中执行，不对 agent 开放。作者校正与封面改善是 `enrichment.Service` 的独立原语（`mm-application`），作者证据经 `SourceEvidence` 依次读证据包、历史提案与结论里的快照，所有者在手动搜索里挑中、不在证据包里的条目也能作证据。

## 识别图片

`Images`（`read`）按系列一次返回三类图片：本地封面、同一本书 1–3 个页码的缩略图、至多三个证据包候选的封面缩小图；至少要一张。

- 候选只认证据包里的候选 ID，没有任何接受地址的参数；不在包里或抓取后才被否决的候选记为 `not_in_evidence`。候选封面只从它自己来源的图片域名（含子域）拉取，只走 HTTPS；已知域名上的 http 地址升级为 https，非默认端口与带用户信息的地址拒绝；至多跟随三次重定向，每一跳重新校验域名，出界记为 `host_rejected`，请求不会发往界外。只接受 200 与 `image/*`，超过 16 MiB 记 `too_large`，解码失败记 `not_image`；拉取有单次超时与全进程并发上限，不缓存，不走搜索节拍（拉的是图片 CDN，不是搜索 API）。
- 本地图片逐张调用 `delegated.Service` 的封面与页缩略图用例，继承内容访问资格、句柄与断线释放；所有读取结束后再校验一次 `read`。
- 每张图逐一给状态（`included`、`over_budget`、`no_cover`、`page_not_found`、`not_in_evidence`、`host_rejected`、`too_large`、`not_image`、`unavailable`），一张出问题不连累其它；只有拒绝、参数不合法和调用方取消让整个调用失败。体积沿用受限图片证据的常量（单张最长边 384 px、96 KiB，结果 256 KiB）：按请求顺序装入，放不下的整张不给、标 `over_budget`，不发出截断的图片。

## 巡检

- `Duplicates`（`read`）在凭据可读的资料库里找疑似重复：`same_entry` 组（识别结论指向同一来源条目，键与候选 ID 同形）在前，`same_name` 组（目录名或标题规范化后相同，读资料库搜索维护的规范化投影）在后，同类内按键排序。一个系列可以出现在多个组里，一组至少两个范围内的系列；每组列前十个系列并给总数，每个系列只带目录名、标题、书数与识别状态，不带备注和结论归属。
- `TagUsage`（`read`）给出每个标签在范围内的系列数和前三个样例系列，按使用数或规范化名称排序。
- 两者都全量重算后排序再按偏移切页，`next_offset` 只在还有下一页时出现；单页 JSON 不超过 30 KiB，至少放一项。资料库参数的规则与进度、领取相同。

## 所有者决定

所有者决定都走管理员网页接口，在写事务内重查管理员身份，返回操作后的权威识别；落到已经变了的系列时以 `ErrNotPending`、`ErrCandidateUnavailable`、`ErrNotANote` 报冲突（协议映射归 `mm-api`），重复否决同一候选、关闭已关闭的备注不改动。

- **识别**（`identifyByOwner`）：写人工确认的结论（理由留空），被引用来源已识别为别的条目时把旧条目记为排除候选再走 `replace`，同一条目只改记确认者；挑中排除候选时解除排除；以 `identified` 结案该系列开着的识别类与复核类上报（备注不动），删掉查无此作结论与领取租约；按 `mm-application`「按识别补充」补充。已审定的系列只改结论、不动元数据。手动搜索的挑中项按预取的快照规则校验，LLM 等非目录来源不能成为识别。待人工页的「挑候选」逐个候选复用它，全部在一个事务里：只对待人工的系列开放，候选须仍在证据包里、没被否决、该来源还没有识别；多来源时按配置的来源优先级依次确认，让优先级最高的来源先填空字段，每个候选一张收据。
- **撤回识别**（`WithdrawAsOwner`，用户决定）：旧条目记为排除候选，`RetractIdentified` 撤回它带来、之后没被改动的字段，删掉这条结论，再按剩下的识别重新补充；没有其它识别时系列回到未处理，agent 会重新领取，证据包里已没有这个条目。同时结案识别类与复核类上报。已审定的系列只动结论。
- **否决候选**：对待人工、待复核的系列开放，当前识别的条目不能这样否决。
- **判查无此作**：只对待人工的系列开放，搜过的来源取证据包里应答过的来源，以 `not_found` 结案识别类上报。
- **识别无误**（用户决定）：只对待复核的系列开放。同一事务以 `kept` 结案未关闭的复核上报，把该系列所有 agent 结论改记为所有者确认（确认者与时间更新，理由保留），此后 agent 不能再撤换它们；这些上报记下的 ComicInfo 系列名存为该系列认可的名字，以后新书带规范化后相同的名字不再判矛盾，新的名字照常报。agent 发起的复核没有 `clue`，只结案并改记确认者。
- **关闭备注**：以 `closed` 结案，不改识别状态；识别类上报只能靠决定结案，不能关闭。
- **待人工列表** `Pending`：未关闭的备注总在列；识别类、复核类上报只在系列待人工或待复核时在列，已审定的系列留着上报但不在列，撤回已审定后回到列表。按最早一条在列上报的时间排序，可按分类筛选；分类计数取整张列表，不随所选分类变。

## 进度

`Progress`（`read`）在凭据可读的资料库里给出六个识别状态的系列数（总是齐全）与预取情况 `prefetch: {ready, pending}`：只统计未处理的系列，`ready` 只数有效证据包，与领取口径一致。资料库参数默认取凭据的全部资料库，任一越权则整次拒绝。

## 核对入口

`bootstrap` 包的识别集成测试经 `bootstrap.Open`、官方 Go SDK MCP 客户端与管理员 HTTP 覆盖上述行为，例如 `TestParallelClaimsNeverOverlapAndExpiredLeasesReturn`、`TestAgentSubmitsVerdictsSeriesBySeries`、`TestPrefetchPausesCancelsAndContinuesWhereItStopped`、`TestClaimRefreshesPacketsOlderThanThirtyDaysBeforeHandingThemOut`、`TestEnablingASourceReopensNotFoundSeriesForThatSourceOnly`、`TestNewBookNamingAnotherWorkSendsSeriesToRecheck`、`TestAgentMayNotCorrectWhatAPersonOrComicInfoOrALockDecided`、`TestOwnerWithdrawingTheOnlyIdentificationHandsTheSeriesBackToTheAgents`、`TestIdentificationImagesRefuseCoversOutsideTheSourcesKnownHosts`、`TestNoteTheOwnerClosedIsNotRaisedAgain`。时间相关的行为推进 `bootstrap.Options.Clock`，不靠等待。
