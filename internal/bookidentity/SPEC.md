---
id: mm-bookidentity
type: module-design
status: draft
title: 书身份的内容与路径证据
parent: mm-architecture
---

## 责任与边界

为已有书身份派生内容与路径匹配证据；补建证据不改变书的持久 ID，不因证据相同自动合并记录。设备文档键、协议匹配策略和账号认证不归本模块。

## 证据契约

完整内容证据沿用整文件 MD5；快速证据采样文件大小及首尾字节，采样相同不能证明完整内容相同。路径证据遵循现有相对路径归一化及可选忽略扩展名的兼容规则；它用于匹配，不授权移动、覆盖或合并文件。

批量完整读取使用可取消入口；路径补建不读取书文件。`Service.Rebuild` 仅补缺失证据，匹配模式由本次调用固定，不中途追随配置改变。调用方必须提供非空任务资格来检查取消、暂停并对完整文件读取执行磁盘准入。

单书读取失败跳过该书且推进分页游标，避免坏文件在末尾无限重试；存储写入或任务准入失败仍返回错误。补建不是整批事务，不把已更新的证据回滚为缺失。

核对入口：`FingerprintFileContext`、`FingerprintQuickFile`、`FingerprintRelativePath`、`Service.Rebuild` 及 `evidence_test.go`、`rebuild_test.go`。
