---
id: mm-catalogsearch
type: module-design
title: 全局藏书搜索排名
status: active
parent: mm-architecture
depends-on: [mm-database, mm-reading]
---

## 职责与契约

`Search.Query` 验证明确的读者主体，消费 database 的独立字段匹配及最佳命中投影，按一个总序组织全局 keyset 批次；不读取用户进度、不改变权限或导航身份。支持 all、series、book，title 是 book 别名。

自身名称或标题精确命中、自身连续子串命中、仅所属系列命中的书分别为层级 0、1、2；所属系列精确不提升书的自身层级。每个身份在数据库按最佳字段键去重后参与限额。完整 `SortKey` 仅含层级 uint8、起始字符位置 uint32、额外字符数 uint32、类型 uint8（book=0、series=1）、持久身份 int64，依次升序；位置从零开始，字符为归一化后的 Unicode 字符。没有名称或标题字典序键，不截断字段；SQLite 的 TEXT 长度上限保证字符位置和长度差在 uint32 域内。

各类型在完整键之后按同一总序取 N+1 个已去重身份，catalogsearch 合并排序后再取整批 N 个，以额外一项探测 has_more；各类型的前 N+1 必然包含其所有可能进入混排前 N+1 的成员，不能先截断字段来源。默认 20 项，合法范围 1–100，不执行完整 COUNT 或 offset 深页。数据库在同一读事务内读取两个类型，单批视图一致。兼容 score 只按层级取 1、0.5、0.25，max_score 是当前批最大值（空批为 0），不比较跨语料 BM25、不按批缩放。HTTP 仍消费既有 hits、total_hits（返回条数）及 max_score，前端不展示分数。

## 边界与证据

数据库拥有归一化、触发器、派生索引和 SQLite 内最佳字段的排名投影；本模块拥有最终混排、完整身份键和批量限额。不建立万能目录查询，不改变资料库或协议排序。游标不保留跨批快照；静态数据集保证有序无重漏，并发增删改不承诺跨批完整性，seek 不要求边界行仍存在。

## 游标

无服务器状态的固定二进制载荷经严格无填充 URL-safe base64 编码；输入上限 8 KiB，当前格式为 93 字节、编码后 124 字符。格式与排序版本均为 1；包含规范化规则版本与关键词的 SHA-256 摘要、规范化 target 枚举、有效读者 ID（匿名为 0）、权限上下文摘要和完整 SortKey。摘要采用版本化 JSON 字符串元组的确定性编码，不拼接任意分隔符，不携带标题、关键词或权限列表。数值使用 uint8、uint32、正 int64 域，并校验合法层级、类型、字符位置与长度差关系。严格拒绝结构、版本、编码、取值或上下文不匹配，返回 ErrCursor；允许归一化等价查询和 book/title 别名共用游标，也允许换批量。

游标只定位，不授予权限。HTTP 每次重新认证账号与角色；当前目录策略允许所有有效读者访问全部资料库，权限摘要绑定版本化目录策略与当前角色。没有新增资料库 ACL 或筛选参数；未来引入 ACL 时必须在候选 LIMIT 前过滤并扩展权限摘要，不能分页后丢弃越权行。

真实 HTTP + SQLite 的独立字面顺序、宽松书匹配、升级、维护及长标题回归见 `internal/api/book_rank_search_test.go`、`internal/api/search_cursor_test.go` 与 `internal/api/search_cursor_contract_test.go`；弹窗和真实搜索会话组合回归见 `web/src/components/layout/SearchModal*.test.tsx`。
