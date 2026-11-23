---
id: mm-seriesmeta
type: module-design
status: active
title: 来源区分的系列元数据写入
parent: mm-architecture
---

## Responsibility

集中系列元数据的来源特定写入策略、内容版本计算与 ComicInfo 导出投影；不统一抹平手工、提案、扫描和批量编辑的不同关联语义。

## Contract

- 查询接口接收调用方的事务句柄；本包不开事务。调用方负责失败回滚及派生数据刷新编排；`ApplyProposal` 内的沿革和统计刷新也在该事务中执行。
- `SaveManual` 替换标量，可修改锁及锁住的字段；标签、作者、链接为 nil 时保留，显式空集合清空。可选 `ExpectedVersion` 与当前内容指纹比较，冲突返回 `ErrConflict` 和当前版本；不擅自清洗标量原文。
- `ApplyProposal` 接受已裁决字段和系列快照，非空标量覆盖未锁字段；仅 `WrittenFields` 选中的未锁标签、作者整体清空重建。写入沿革，关联失败上抛；Bangumi 来源且条目 ID 有效时，缺少同名链接才追加来源链接。
- `PrepareScanInitial` 用目录名作标题并默认锁标题；`PrepareScanUpdate` 使用调用方缓存的行与锁，保留已有标题、为空且未锁时回落目录名，其余未锁标量可被空值清除。`AppendScanAssociations` 只追加标签、作者，不做替换。
- `EditSelection` 修改非 nil 的状态、出版商，标签先增后删，不改锁和沿革；错误要求整个选择回滚。`Version` 对可编辑内容求指纹，集合去空白、去重排序，不纳入身份、阅读状态和统计；`BuildComicInfoForBook` 只投影已知事实。

## Boundary

不持有提案入队、状态推进或裁决事务，不创建系列身份、书目或封面文件，不执行归档写回。跨模块协作归根 `architecture.md`。

实现观察：扫描关联忽略集合锁和单次写入错误，扫描标量不从 ComicInfo 提取连载状态；这是当前来源差异，不是所有写入器共享的保证。

## Evidence

- `manual.go`、`proposal.go`、`scan.go`、`scan_associations.go`、`bulk.go`、`version.go`、`comicinfo.go`：各入口与事务能力范围。
- `manual_test.go`、`proposal_test.go`、`proposal_compat_test.go`：版本冲突、回滚、未选与显式空集合、锁及来源沿革。
- `scan_test.go`、`bulk_test.go`：扫描锁矩阵与追加容错、批量先增后删及跨分块整体回滚。


## 委托补充策略

`ContentTags` 以保守内容类型词表筛除个人阅读标记、来源名、年份与未知自由标签，调用方追加而不替换旧成员。`EnrichmentRating` 只接受 Bangumi、AniList、MyAnimeList 的原始站点评分及有效尺度，同来源只计一次，换算十分制后等权平均；返回参与的完整来源和可重算公式，无有效评分返回缺失，不回落到 LLM 或已有归一化 Rating。实际来源适配器的原始尺度归 `mm-metadata`；单系列事务及审计归 `mm-application`。这些入口不改写手工、扫描或普通提案原有规则。

`State` 定义受限补充/撤销使用的字段快照；`EnrichmentLocked` 解释当前字段锁，`WriteEnrichmentState` 通过窄事务查询端口写入已准备状态及实际改变的集合并刷新统计。应用调用方必须先在同一事务核对当前权限、锁和相关预期值，任一步失败整体回滚写入（事务或包住完整写入的 savepoint）；写入器不持有事务或协议身份。


`State.cover` 描述目录选中的现有封面，`ArchiveCover` 区分扫描自动键/本能力保留键与人工内容哈希键。`WriteEnrichmentCover` 只在调用方已重查授权、来源、当前相关值与锁的事务内切换未锁书的封面引用，不创建文件、不加人工锁。应用负责把文字、统计、材料引用和收据一起提交；普通扫描与人工封面入口不改用该受限策略。

`State.Authors` 与 `AuthorSet` 定义精确 name/role 集合；`WriteEnrichmentState` 仅对 authors 被实际改变的目标系列重建关联，使用 `EnsureAuthor` 获取身份，绝不修改或删除旧作者身份。旧关联可由 Undo 重建，共享其他系列不受影响。授权、明确别名/角色映射、来源证据与整组预期值检查归应用层；普通手工替换、扫描追加及提案集合规则不改变。

`State` 是可扩展的响应及审计快照，不是持久命令摘要格式；命令 ExpectedState 与冻结摘要映射归 `mm-application`，新增 State 字段不得隐式改变已有请求键的 SHA。旧审计缺失 authors 不表示曾清空作者组。

已有提案的受保护实际变更使用 `RecordEnrichmentProvenance` 记录最新字段来源及提案引用，与补充/消费同事务。它不执行普通提案的整组替换。实际 Undo 后 `ForgetEnrichmentProvenance` 只移除仍指向原提案的已恢复字段来源，不触及其他字段/后来来源，也不伪造未知的早期来源；完整历史证据仍由不可变操作收据保留。提案状态不属于内容恢复。
