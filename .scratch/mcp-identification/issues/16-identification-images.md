# 16: 识别图片原语

**What to build:** 有视觉能力的 agent 可以按需查看一个系列的本地封面、指定页的缩略图和证据包里候选的封面，用来区分相似的候选。弱模型不调用这个原语，也能走完主循环。

**Blocked by:** 05 证据包预取（手动回填）

**Status:** ready-for-agent

- [x] 一个按系列调用的原语（`read`），返回三类图片：本地封面、指定的 1 到 3 页缩略图、证据包候选封面的缩小图。
- [x] 候选封面由服务端只从已知来源的域名拉取。以下情况一律拒绝：其它域名、重定向到其它域名、文件过大、内容不是图片。不接受调用方提供的 URL。
- [x] 沿用现有的图片体积预算和 MCP 传输上限；超出时如实报告，不截断成损坏的图片。
- [x] 本地图片沿用现有的内容访问资格检查和句柄释放。
- [x] 新工具的描述在约 400 字符以内并附调用示例，结果带 JSON 文本副本。
- [x] 集成测试由来源替身提供封面，覆盖域名限制、体积预算和拉取失败。
- [x] 后端门禁通过。

## Comments

本票按规格「留给实施决定的工程细节」自己定下的取值（都不碰已确认的产品或安全边界）：

- **工具与参数**：`library_get_identification_images`（`read`），参数 `{series_id, cover?, book_id + pages?, candidate_ids?}`，至少要一张图。
  - `pages` 是同一本书里 1 到 3 个不重复的正整数页码，必须和 `book_id` 一起给。
  - `candidate_ids` 最多 3 个，取证据包里的候选 id（`<来源>:<条目编号>`）。没有任何接受地址的参数，传地址只会得到 `not_in_evidence`。
- **结果形状**：`{series_id, images: [{kind, book_id?, page?, candidate_id?, status, width?, height?}]}`，顺序是封面、页、候选。首个文本块是这份 JSON，后面按列表顺序跟每张 `included` 的 JPEG。
  - 逐张给状态：`included`、`over_budget`、`no_cover`、`page_not_found`、`not_in_evidence`、`host_rejected`、`too_large`、`not_image`、`unavailable`。一张图出问题不连累其它图。
  - 只有拒绝、参数不合法和调用方取消才让整个调用失败。拒绝沿用统一文案。
  - 理由：远端拉取失败很常见，而且彼此独立；逐张报告让 agent 不用重试整批。
- **体积预算**：沿用现有常量：单张最长边 384 px、96 KiB，输入原图 16 MiB，整个结果 256 KiB。
  - 按请求顺序装入，放不下的图整张不给，标 `over_budget`，后面更小的图仍可能放得下。
  - API 层构造结果后再量一次，超了就整体报错，不发出截断的图片。
- **已知来源域名**：每个来源只认自己的图片域名，域名本身及其子域都算：
  - Bangumi：`bgm.tv`、`bangumi.tv`、`chii.in`；AniList：`anilist.co`；MangaDex：`mangadex.org`；MyAnimeList：`myanimelist.net`；Comic Vine：`comicvine.gamespot.com`。
  - 候选封面只能从它自己来源的域名拉取。
  - 只走 HTTPS。已知域名上的 http 地址升级成 https，非默认端口、带用户信息的地址一律拒绝。
  - 最多跟随 3 次重定向，每一跳都重新校验域名，出界记为 `host_rejected`，请求不会发往界外。
- **拉取限制**：
  - 只接受 200 和 `image/*`；声明长度或实际读到的字节超过 16 MiB 记为 `too_large`；解码失败记为 `not_image`。
  - 单次超时 10 秒。全进程同时最多 2 个下载，下载槽同时限住解码的并发。
  - 不缓存。不走预取的来源节拍：拉的是图片 CDN，不是搜索 API。
- **本地图片**：每页单独调用 `delegated.Service` 的现有封面、页缩略图用例，原样继承内容访问资格、存储令牌、归档句柄与断线释放。所有读取结束后再校验一次 `read` 授权。
- **排除候选**：证据包抓取之后才被否决的候选同样记为 `not_in_evidence`。
- **测试接缝**：在既有的 `api.ExternalSources` 上加 `Covers http.RoundTripper`，生产环境用默认传输。它只负责搬运字节；域名、重定向、大小和内容的检查都在它之外由服务端执行，所以集成测试能覆盖「请求从未发往界外」。
- **文档**：MCP 接入文档和模块规格里的工具对照，留给 20 统一更新。

