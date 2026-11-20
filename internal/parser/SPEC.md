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
- `ReadPageLimited` 对已选定的精确页条目应用调用方更小的解压预算，ZIP/RAR 同时检查声明大小与实际读取大小；循环与字节读取观察取消，仍由调用方归还共享借用。不回退普通页读取；普通读取上限与排序不变。
- `WriteComicInfoIntoArchive` 只支持 zip/cbz，rar/cbr 返回 `ErrArchiveNotWritable`。已有 XML 合并保留未建模字段等内容，危险原文拒写；写回分为 `PrepareComicInfo` 同目录暂存和 `PreparedComicInfo.Publish` 最终发布，不承诺任意归档格式可写。准备阶段仍准入读者，并在交出暂存对象前归还自身源借用；调用方须 `Close` 清理未发布暂存，成功发布不可重复使用。
- 最终发布通过 `AcquireArchivePublication` 在有界、可取消窗口关闭该路径的新读准入（返回 `ErrArchivePublishing`），等待真实借用排空，包括已逐出池的在借句柄及整书原始流；EOF 不等于关闭，池淘汰或观察借用计数为零均不能替代发布保留。发布前复核源文件身份、大小、mtime 与内容摘要，变化返回 `ErrArchiveChanged` 而不覆盖；复核通过后替换原文件。超时或取消须释放读准入，未发布暂存仍由所有者清理。此协调限于本进程受管借用，不是对任意外部写入的跨进程锁。
- `GetArchiveFromPool` 返回共享句柄与归还函数，借用方必须归还，不能自行关闭共享句柄。池按修改时间与大小判断文件变化；淘汰立即移出池，引用归零才关闭。`InitPool` 可调整容量，`CloseArchivePool` 用于所有者退出后的收尾。

## 边界与限制

只识别路径与归档内条目，不认识书、系列、资料库或数据库；不遍历资料库、不处理页图缩放与 HTTP，也不负责卷 IO 许可，调用方须在适当入口申领。无兄弟包导入，归档内页序不得替代系列内卷话排序。

## 证据

`Archive`、`OpenArchive`、`readEntryLimited`、`MergeComicInfoXML`、`ArchivePool`；`parser_test.go` 的 `TestReadEntryLimited`、`TestPoolEvictionDoesNotCloseBorrowedHandle`，以及 `comicinfo_merge_test.go`、`comicinfo_writeback_test.go`、`publication_test.go`、`borrow_test.go`、`rar_test.go`。
