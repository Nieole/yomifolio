---
id: mm-api
type: module-design
status: active
title: HTTP 与客户端协议适配
parent: mm-architecture
references: [mm-adr-0003, mm-adr-0007]
---

## 责任与入口

把 HTTP 与客户端协议转换成应用调用：解析路径和请求、会话与管理员授权、CSRF、响应及错误映射。Web API、OPDS、Mihon、KOReader 保留各自认证和协议兼容语义，不因共用进程而合成一个授权入口。

只接受经过授权的用户身份；阅读写入显式传递该用户，不能从请求任意指定另一个人。用户名契约归 `mm-adr-0003`，任务寻址归 `mm-adr-0007`。

## 会话 Cookie 兼容

新会话使用 `yomifolio_session`；仅在请求没有新 Cookie 时读取旧 `mm_session`。新 Cookie 即使为空、失效或非法也不得回退旧值；被选名称出现重复 Cookie 时拒绝认证。旧会话验证及适用的 CSRF、角色和强制改密校验通过后换发新 Cookie 并清除旧名，保持令牌、用户和 CSRF 不变；名称过渡本身不延长有效期，滑动续期仍须服务端更新成功后才下发。公开状态端点验证旧会话成功时也按现有到期时间换名。

登录、首次建管理员及改密建立新会话时清除旧 Cookie；登出撤销请求携带的两种名称下的会话令牌及当前会话，并清除两种 Cookie，防止旧登录复活。写入与清除沿用 `Path=/`、HttpOnly、SameSite=Lax 及同一 Secure 判定，不降低既有 CSRF、授权或代理信任规则。

## 边界

控制器可直接使用藏书查询、协议投影与缓存适配，不在此重建阅读、编排、提案或迁移事务。后台工作的声明与重启配方归应用能力；这里映射任务控制、历史和 SSE 快照，不持有另一套准入。

`NewTransport` 创建认证、事件 broker、运行投影及缓存/内容效果等技术 leaves；`Transport.Bind` 接受显式家庭能力与窄查询，在共享鉴权子树下组合 `Host`。`Host` 不构造应用、不持有 Runtime 或全量 Store，生产资源创建与恢复启动归 `bootstrap.Open`；底层服务不回调 HTTP 请求处理器。响应契约与前端生成类型须一致，协议差异在各适配器内消化。

核对入口：`host.go`、`auth_username_case_test.go`、`collection_authorization_test.go` 与协议控制器测试。生产装配顺序另经 `bootstrap` 的 HTTP 与恢复回归核对。
