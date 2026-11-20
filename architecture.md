---
id: mm-architecture
type: architecture-design
status: active
title: 系统架构与依赖边界
parent: manga-manager
references: [mm-domain-language, mm-adr-0001, mm-adr-0003, mm-adr-0004, mm-adr-0005, mm-adr-0006, mm-adr-0007, mm-adr-0008, mm-adr-0009, mm-adr-0010, mm-adr-0011, mm-adr-0012, mm-adr-0013, mm-adr-0014, mm-adr-0015]
---

## 系统形状

Yomifolio 的 Go 模块为 `yomifolio`。单进程 Go 服务使用 chi 接收 HTTP，在 SQLite 中保存索引及用户状态，并内嵌 React/Vite 前端。归档文件仍是藏书内容来源，封面和页图缓存是派生数据；可选元数据源、LLM 与超分引擎在各自适配边界外。

`cmd/server` 持有配置加载、数据库迁移、HTTP 中间件、静态资源与进程信号，调用 `bootstrap.Open` 完成生产装配。`mm-bootstrap` 拥有生产应用、共享资源和 HTTP 宿主的组合；运行时恢复与资源寿命归 `mm-runtime`。`internal/api` 适配 Web、OPDS、Mihon、KOReader 与 SSE；`Host` 只保存已绑定的 HTTP 适配器，不构造应用、不持有 Runtime 或复制全量服务图。装配次序与取舍见 [ADR 0014](docs/adr/0014-use-cases-queries-and-host-assembly.md)。

以下归属由实际导入、完整用例与集成测试核对。禁止反向依赖的表述是既有设计约束，不宣称编译器自动检查了每条边。

## 功能归属与依赖

- `reading` 持有阅读事实及用户投影；`organize` 持有藏书编排，阅读列表的继续阅读依赖 `reading`。两者使用 `database`，不依赖 HTTP 或应用总对象。
- `seriesmeta` 持有各来源的系列元数据写入策略；`proposal` 组合 `metadata` 候选与 `seriesmeta` 完成裁决。`scanner` 通过 `seriesmeta` 导入内嵌元数据，并使用 `parser`、`images` 和 `bookidentity` 的内容/路径证据能力，不依赖 KOReader 协议。
- `bookidentity` 向扫描与设备匹配提供已有书身份的证据，兼容规则及补建契约归 `mm-bookidentity`。`bookcontent` 组合 `parser` 的归档借用与发布、`images` 的加工和 `storageio` 的读取许可；API、扫描与回写消费者共用内容访问边界，契约归 `mm-bookcontent`。
- `koreader` 持有设备账号、协议记录、设备文档匹配与进度对账，使用 `bookidentity` 的证据匹配已有书身份，命中的书位置交给 `reading`；身份补建及其运行生命周期不归本包。`external` 管传输会话，不成为扫描资料库。
- `application` 拥有完整的同步用例与后台协作链，后台工作使用 `task.Engine` 的统一发起口。维护调用扫描完成重建；扫描收尾经注入出口串联维护，扫描实现不反向导入维护编排。配置发布组合 `config.Manager`，系列编辑组合 `seriesmeta` 与事务；`organize` 拥有分组候选生成和裁决，应用智能生成编排仅持有运行策略。`application/runtime` 装配应用能力与重启注册表，拥有恢复、后台输入和停止等待。
- `relocation` 持有持久映射、补偿、配置协调与保护；应用迁移编排负责独占访问、监听、阅读缓存与外部会话交接。它不是一个与 IO、任务和数据库无关的纯领域包。

## 基础能力的实际边

- `taskstore → task`：SQLite 实现领域持有的 Store 端口。`task` 使用 `runhandle`、`diskwork`、`taskcontrol`，不得反向依赖 SQL、`database` 或 HTTP。
- `database → taskstore`：统一迁移入口安装任务表；这是实际存在的迁移依赖，不是任务引擎绕道访问数据库。
- `runhandle → diskwork → storageio`：运行资格、一次后台磁盘作业与卷级调度不同；`taskcontrol` 提供协作式暂停。运行槽位、卷令牌和暂停闸门不得合并成一个概念。
- `config` 管配置值、快照和更新；`runtimecfg` 依赖它并应用 `parser`、`images` 与 `logger` 的资源效果。所有组装能力共享同一个 `config.Manager`。
- `booksort` 共享书排序，`logger` 提供结构化日志；这些支撑能力不拥有阅读或编排用例。

这些跨兄弟模块的边集中记录于本节点；叶规格只解释自己的责任、公开契约与禁止越界。

## 浏览器边界

浏览器只持有客户端会话、交互及本机数据，不把后端事实搬成第二份规则。全站连接与本机离线能力具有独立生命周期；前端内部状态分工与子规格入口归 `mm-web`，不由后端运行生命周期管理。

客户端只消费 API 与推送契约，不承担后端规则。生成契约由 `cmd/tsgen` 维护，SQL 绑定由 sqlc 维护；生成方式与验证命令仍归 `AGENTS.md`。

## Mihon 客户端边界

`extensions/mihon` 是运行在官方 Mihon 中的独立 Android 源 APK，不编入 Go 服务。它通过 `mm-api` 的 Mihon 协议读取目录和内容；服务器与账户绑定、资源地址校验及本机配置归 `mm-mihon-extension`，用户阅读事实仍归 `reading`。

源扩展与服务端分别构建，服务端不依赖 Android 工具链；客户端交付范围归根目标，源接口限制、签名与仓库安全边界归 `mm-mihon-extension`。服务端的同步协议能力不代表官方 Mihon 客户端能产生或回传翻页事实。

## 验证与交付边界

日常验证按改动选择相关类别，未知路径或不可用的比较历史保守全量；入口始终报告汇总门禁，必要检查失败、取消或意外跳过均不得放行。主程序的验证、仅供下载的手动测试构建与正式 tag 发布是不同操作，测试产物不得隐式触发公开发布。正式交付强制当次全量验证，并对同一提交构建，不以历史 CI 绿灯或跨运行产物替代门禁。

主程序的前端静态产物仅在 Linux 检查和构建一次，在同次运行内供三平台 Go 验证及原生 CGO 打包共享；此取舍减少重复安装与构建，不保证 Windows/macOS 前端工具链兼容性。验证及构建默认仅读权限，只有最终发布 job 可写；正式发布串行且不取消已运行的发布。Mihon 的签名与仓库安全边界归 `mm-mihon-extension`，操作与必需检查迁移归 `.github/CI.md`。

## 决策与规格状态

领域概念只在 `mm-domain-language` 定义；跨功能的身份、准入、恢复、事务裁决、宿主装配及内容发布理由归本节点引用的 ADR，不另复制论证。

`mm-adr-0008` 已实施，重启入参与声明标签的边界及逐次有损合并事件保证归该决定。`mm-adr-0002` 已被 `mm-adr-0005` 取代，以 deprecated 节点保留历史且不作为当前约束。`.scratch/` 中的实施规划和验收记录作为输入，不提升为长期节点。

## MCP 委托访问边界

`bootstrap → application/delegated → database/sql` 组装独立凭据与当前授权用例，`api → application/delegated` 适配管理员网页和 Streamable HTTP；协议层不获得数据库入口，也不复制应用运行时。统一迁移安装凭据 DDL，账号密码事务共同撤销浏览器会话与委托凭据。现有网页、OPDS、Mihon 与 KOReader 的认证入口不复用 MCP Bearer，也不因委托资料库限制改变网页目录读取。主语、能力交集及扩展约束归 `mm-application`，协议与安全代理约束归 `mm-api`；ADR 0016 仍为 draft，不将静态凭据或未来额度声明解释为已实现的 OAuth/人工审批。
