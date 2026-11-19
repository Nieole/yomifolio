---
id: mm-web-search
type: submodule-design
status: draft
title: 全局搜索会话与分页交互
parent: mm-web
references: [mm-catalogsearch, mm-session]
---

## 责任与边界

全局搜索会话持有输入、范围、打开状态、首批、追加请求、游标与有效条目；`Layout` 与 `SearchModal` 消费同一会话，不从非空输入推断查询已成功。空白输入为 idle，非空输入约 300ms 防抖；成功零条与失败分别呈现，失败可立即重试。状态、错误及异步通知使用 locale 文案。

## 迟到隔离

关键词、范围、打开状态或有效读者/连接作用域替换时，取消请求并隔离迟到响应；取消仅节约资源，不能代替归属和请求代际检查。新输入或范围立即使旧列表失效；关闭与重置不能被在途请求回填，重开对保留输入重新请求。复用已有 `apiClient`；连接作用域沿用 `mm-session` 生命周期，普通 SSE 重连不建立新的搜索身份。

## 选择与分页

只有当前成功结果可被鼠标或键盘选中；上下键保持索引边界，空列表回车无动作。系列进入 `/series/:id`，书进入 `/reader/:id`。消费生成的 SearchResult 与 SearchHit，不在前端重排，服务端排名及游标规则归 `mm-catalogsearch`。

继续加载立即执行，同代际至多一个追加请求；只有成功接受响应后推进游标。按身份去重累积并显示是否还有更多，追加时保留有效选择。普通追加失败保留列表、选择和未消费游标，可立即重试；`invalid_search_cursor` 保留列表供查看，禁用追加并呈现显式“重新搜索”，由该动作从首批建立新代际，不静默混接。

新查询、范围、身份/连接作用域、关闭或重置都使旧追加响应失效。跨批不是快照，去重不保证并发修改后的完整性。结果 listbox 的选中项、加载状态及可访问通知须与实际请求结果一致。

## 核对入口

`useGlobalSearch`、`useGlobalSearch.test.tsx`、`searchSession.test.tsx` 与 `SearchModal.session.test.tsx`。验证应经真实客户端组合覆盖取消无效、乱序完成、失败重试与游标失效，静态 hits 不足以证明会话隔离。
