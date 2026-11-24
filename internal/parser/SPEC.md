---
id: mm-parser
type: module-design
status: active
title: 归档、页面与内嵌元数据解析
parent: mm-architecture
references: [mm-adr-0015]
---

## 职责

按文件路径解析 zip/cbz、rar/cbr 归档，筛出图片页并确定归档内页序，读取原始条目字节，解析与合并 ComicInfo.xml；管理共享归档句柄池与 ZIP 元数据写回。

## 公开契约

- `OpenArchive` 按扩展名分发，`Archive` 提供页面列表、按条目名读取、元数据读取与关闭。页序使用封面关键字、浅层目录优先及自然数排序，是启发式而非漫画语义识别。
- 条目读取经 `readEntryLimited` 检查声明大小与实际读取上限，不直接信任归档头；ComicInfo 解析容忍部分脏数字、BOM 与字符集声明，须校验完整单根文档直至 EOF，根前后额外非空文本、第二根或损坏尾部返回错误且不返回部分元数据；合法序言、注释、处理指令和 XML 空白不影响解析。沿用标准解码器不解析外部 DTD、不展开自定义实体的边界。
- `ReadMetadataLimited` 为按需线索读取提供调用方指定的较小字节预算，拒绝声明大小或实际解压超限，复用各格式的元数据选取规则，不改变普通读取和扫描语义；`ErrMetadataNotFound` 与 `ErrMetadataTooLarge` 区分缺失与超限。
- `ReadMetadataLimited(ctx, ...)`、`GetPagesLimited` 与 `ReadPageLimited` 为证据用途提供独立于普通阅读会话的目录与精确页读取。RAR 三处均可取消地获取会话锁，等待普通阅读或关闭时取消不依赖持锁 IO 完成；获取后仍持锁至独立 reader 关闭，保持与 `Close`、游标及缓存的生命周期互斥，不在关闭后重开。RAR 三处均在创建 reader 前传入固定 16 MiB `MaxDictionarySize`，不沿用依赖的普通默认字典上限；每个卷的打开与底层读取、目录循环及解压读取均观察取消。每个独立证据 reader 的跳过/目录解压另有累计 16 MiB 预算，使用同一显式 drain helper；ComicInfo 目标仍按调用方预算检查（委托 64 KiB），元数据条目选择规则不变。依赖的 `Read` 会在声明大小处截断，而全局 solid 的 `Next` 会绕过该读取器继续解压剩余输出；显式 drain 和字典限制不能封顶这部分工作。三处统一在 `pageContextFS.Open` 将同一已打开文件交给 SDK 前，以不解压的严格 RAR4/5 头预检拒绝全局/文件 solid、多卷/分片、未知输出大小、归档头或文件加密、SFX、未知/服务块（包括注释）及损坏布局，返回独立的 `ErrRarEvidenceUnsupported`；不依据当前条目的 Solid 假定全局非 solid。不重开已批准文件，预检最多 65536 块、累计 16 MiB 头字节，RAR5 单头最多 1 MiB，payload 只校验文件内边界并 seek 跳过，不解压。允许普通单卷非 solid、已知大小的 RAR4/5 证据；跳过条目显式流向丢弃器，检查声明和 SDK 可见字节（目录每条 16 MiB，选页跳过每条使用调用方字节预算），不保留跳过内容，因此未选中的超限 RAR 条目也可使证据不可用。字典超限与条目超限统一返回 `ErrMetadataTooLarge`，不回退普通目录或页读取；ZIP 目录沿用页过滤与排序并观察取消，选页检查声明与实际解压大小。共享借用仍归调用方归还；普通 `GetPages`、阅读缓存、元数据扫描及原有上限不变。
- `WriteComicInfoIntoArchive` 只支持 zip/cbz，rar/cbr 返回 `ErrArchiveNotWritable`。已有 XML 合并保留未建模字段等内容，危险原文拒写；写回分为 `PrepareComicInfo` 同目录暂存和 `PreparedComicInfo.Publish` 最终发布，不承诺任意归档格式可写。准备阶段仍准入读者，并在交出暂存对象前归还自身源借用；调用方须 `Close` 清理未发布暂存，成功发布不可重复使用。
- 最终发布通过 `AcquireArchivePublication` 在有界、可取消窗口关闭该路径的新读准入（返回 `ErrArchivePublishing`），等待真实借用排空，包括已逐出池的在借句柄及整书原始流；EOF 不等于关闭，池淘汰或观察借用计数为零均不能替代发布保留。发布前复核源文件身份、大小、mtime 与内容摘要，变化返回 `ErrArchiveChanged` 而不覆盖；复核通过后替换原文件。超时或取消须释放读准入，未发布暂存仍由所有者清理。此协调限于本进程受管借用，不是对任意外部写入的跨进程锁。
- `GetArchiveFromPool` 返回共享句柄与归还函数，借用方必须归还，不能自行关闭共享句柄。池按修改时间与大小判断文件变化；淘汰立即移出池，引用归零才关闭。`InitPool` 可调整容量，`CloseArchivePool` 用于所有者退出后的收尾。

## 边界与限制

只识别路径与归档内条目，不认识书、系列、资料库或数据库；不遍历资料库、不处理页图缩放与 HTTP，也不负责卷 IO 许可，调用方须在适当入口申领。无兄弟包导入，归档内页序不得替代系列内卷话排序。

## 证据

`Archive`、`OpenArchive`、`readEntryLimited`、`MergeComicInfoXML`、`ArchivePool`；`parser_test.go` 的 `TestReadEntryLimited`、`TestPoolEvictionDoesNotCloseBorrowedHandle`，`rar_budget_preflight_test.go` 的真实压缩/隐藏 drain 与严格头分类回归、`evidence_public_lock_test.go` 的生产 HTTP/stdio 断连加持锁/并发关闭组合，以及 `comicinfo_merge_test.go`、`comicinfo_writeback_test.go`、`publication_test.go`、`borrow_test.go`、`rar_test.go`。
