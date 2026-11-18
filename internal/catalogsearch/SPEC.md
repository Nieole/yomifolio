---
id: mm-catalogsearch
type: module-design
title: 全局藏书搜索排名
status: active
parent: mm-architecture
depends-on: [mm-database, mm-reading]
---

## 职责与契约

`Search.Query` 验证明确的读者主体，消费 database 的独立字段匹配及最佳命中投影，按一个总序组织全局首批；不读取用户进度、不改变权限或导航身份。支持 all、series、book，title 是 book 别名。

自身名称或标题精确命中、自身连续子串命中、仅所属系列命中的书分别为层级 0、1、2；所属系列精确不提升书的自身层级。每个身份在数据库按最佳字段键去重后参与限额。完整 `SortKey` 仅含层级 uint8、起始字符位置 uint32、额外字符数 uint32、类型 uint8（book=0、series=1）、持久身份 int64，依次升序；位置从零开始，字符为归一化后的 Unicode 字符。没有名称或标题字典序键，不截断字段；SQLite 的 TEXT 长度上限保证字符位置和长度差在 uint32 域内。

各类型先按同一键取前 N 个已去重身份，catalogsearch 合并排序后再取整批 N 个；各类型的前 N 必然包含其所有可能进入混排前 N 的成员，不能先截断字段来源。默认首批 20 项，不再每类型各返回 20 项。兼容 score 只按层级取 1、0.5、0.25，max_score 是当前批最大值（空批为 0），不比较跨语料 BM25、不按批缩放。HTTP 仍消费既有 hits、total_hits（返回条数）及 max_score，前端不展示分数。

## 边界与证据

数据库拥有归一化、触发器、派生索引和 SQLite 内最佳字段的排名投影；本模块拥有最终混排、完整身份键和批量限额。不建立万能目录查询，不改变资料库或协议排序。此实现仅提供首批，没有游标、继续加载或新增响应字段，不承诺跨批快照。

真实 HTTP + SQLite 的独立字面顺序、宽松书匹配、升级、维护及长标题回归见 `internal/api/book_rank_search_test.go`；弹窗和真实搜索会话组合回归见 `web/src/components/layout/SearchModal*.test.tsx`。
