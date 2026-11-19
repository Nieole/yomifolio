---
id: mm-bookcontent
type: module-design
status: draft
title: 书内容访问、缓存与发布交接
parent: mm-architecture
references: [mm-adr-0015, mm-parser, mm-task]
---

## 责任与边界

集中书来源、页清单、页图变体、派生缓存资格与原文件读取，避免协议适配器各自组合归档借还及缓存规则。内容不包含读者身份或阅读位置；协议错误映射和后台运行准入归调用方。

## 访问与缓存契约

页码为一基，越界明确失败。页读取在返回独立字节前归还归档借用；共享缓存返回的字节按不可变数据消费。整书读取返回原文件句柄，不做解码，直到幂等 `Close` 才释放归档及资料库访问资格，读到 EOF 不等于关闭。整书打开须在资料库准入后复核访问版本，避免迁移期间使用旧来源。

来源版本和请求的加工选项共同决定页变体，不能把不同文件或不同加工结果混作缓存命中。仅实际生成的派生图进入加工缓存；原图直通、不可用滤镜及加工失败回落不冒充成功加工。缓存是可丢弃的派生状态，不替代归档原文件。

## 发布交接

ComicInfo 发布遵循 `mm-adr-0015`；底层归档暂存与发布排空归 `mm-parser`。文件提交后，`Publisher.Publish` 交接文件事实更新与缓存失效，分别返回 `Committed` 和后续 `Warning`。提交后的请求取消不能撤销文件替换，也不能跳过有界后置交接；调用方须展示告警，不能将它解释成文件未写入并自动重写。

核对入口：`Service.Open`、`Service.Page`、`Publisher.Publish`；`file_test.go`、`page_test.go`、`publication_test.go` 与 `memory_cache_test.go`。
