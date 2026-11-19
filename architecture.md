---
id: mm-architecture
type: architecture-design
status: active
title: 系统架构与依赖边界
parent: manga-manager
references: [mm-domain-language, mm-adr-0001, mm-adr-0003, mm-adr-0004, mm-adr-0005, mm-adr-0006, mm-adr-0007, mm-adr-0008, mm-adr-0009]
---

## 系统形状

Yomifolio 的 Go 模块为 `yomifolio`。单进程 Go 服务使用 chi 接收 HTTP，在 SQLite 中保存索引及用户状态，并内嵌 React/Vite 前端。归档文件仍是藏书内容来源，封面和页图缓存是派生数据；可选元数据源、LLM 与超分引擎在各自适配边界外。

`cmd/server` 持有配置加载、数据库迁移、HTTP 中间件、静态资源与进程信号，调用 `bootstrap.Open` 完成生产装配。`bootstrap` 创建共享资源与 HTTP 技术 leaves，持有唯一的 `application/runtime.New` 调用，绑定 HTTP leaf effects 后才恢复状态并开放后台输入。`internal/api` 适配 Web、OPDS、Mihon、KOReader 与 SSE；`Host` 只保存已绑定的 HTTP 适配器，不构造应用、不持有 Runtime 或复制全量服务图。装配次序与取舍见 [ADR 0014](docs/adr/0014-use-cases-queries-and-host-assembly.md)。

以下归属由实际导入、完整用例与集成测试核对。禁止反向依赖的表述是既有设计约束，不宣称编译器自动检查了每条边。

## 功能归属与依赖

- `reading` 持有阅读事实及用户投影；`organize` 持有藏书编排，阅读列表的继续阅读依赖 `reading`。两者使用 `database`，不依赖 HTTP 或应用总对象。
- `seriesmeta` 持有各来源的系列元数据写入策略；`proposal` 组合 `metadata` 候选与 `seriesmeta` 完成裁决。`scanner` 通过 `seriesmeta` 导入内嵌元数据，并使用 `parser`、`images` 和 `bookidentity` 的内容/路径证据能力，不依赖 KOReader 协议。
- `bookidentity` 持有内容哈希、快速采样及路径证据，`Service.Rebuild` 分批补建缺失身份；整文件读取经调用方提供的磁盘作业资格，路径补建不读书文件，坏文件仍推进游标。证据相同不自动授权合并书籍记录。
- `koreader` 持有设备账号、协议记录、设备文档匹配与进度对账，使用 `bookidentity` 的证据匹配已有书身份，命中的书位置交给 `reading`；身份补建及其运行生命周期不归本包。`external` 管传输会话，不成为扫描资料库。
- `application` 组合扫描、维护、外部磁盘作业、智能生成及迁移的完整协作链，使用 `task.Engine` 的统一发起口。`application/runtime` 构造这些能力和重启注册表，并拥有恢复、后台输入和停止等待。
- `relocation` 持有持久映射、补偿、配置协调与保护；应用迁移编排负责独占访问、监听、阅读缓存与外部会话交接。它不是一个与 IO、任务和数据库无关的纯领域包。

## 基础能力的实际边

- `taskstore → task`：SQLite 实现领域持有的 Store 端口。`task` 使用 `runhandle`、`diskwork`、`taskcontrol`，不得反向依赖 SQL、`database` 或 HTTP。
- `database → taskstore`：统一迁移入口安装任务表；这是实际存在的迁移依赖，不是任务引擎绕道访问数据库。
- `runhandle → diskwork → storageio`：运行资格、一次后台磁盘作业与卷级调度不同；`taskcontrol` 提供协作式暂停。运行槽位、卷令牌和暂停闸门不得合并成一个概念。
- `config` 管配置值、快照和更新；`runtimecfg` 依赖它并应用 `parser`、`images` 与 `logger` 的资源效果。所有组装能力共享同一个 `config.Manager`。
- `booksort` 共享书排序，`logger` 提供结构化日志；这些支撑能力不拥有阅读或编排用例。

这些跨兄弟模块的边集中记录于本节点；叶规格只解释自己的责任、公开契约与禁止越界。

## 浏览器边界

`web/src/App.tsx` 决定路由和会话订阅范围。`auth` 持有身份；`session` 管一条作用域内的 SSE、协议解码及刷新水位；`features` 持有功能查询与投影；页面持有筛选、选择、弹窗和布局。阅读器持有当前书、跨书下载及本机离线状态的不同生命周期，不能把它们归入全站会话。

客户端只消费 API 与推送契约，不承担后端规则。生成契约由 `cmd/tsgen` 维护，SQL 绑定由 sqlc 维护；生成方式与验证命令仍归 `AGENTS.md`。

## Mihon 客户端边界

`extensions/mihon` 是运行在官方 Mihon 中的独立 Android 源 APK，不编入 Go 服务。它通过 `mm-api` 的 Mihon 协议读取目录和内容；服务器与账户绑定、资源地址校验及本机配置归 `mm-mihon-extension`，用户阅读事实仍归 `reading`。

源扩展不能获得实际翻页事件，不能用取图流量代替阅读；跨端页级同步不在独立插件交付范围。不修改或分发 Mihon 客户端及其阅读器库改动。源扩展与服务端分别构建，服务端不依赖 Android 工具链；公开发布签名不随源码或测试 APK 分发。

## 验证与交付边界

日常验证按改动选择相关类别，未知路径或不可用的比较历史保守全量；入口始终报告汇总门禁，必要检查失败、取消或意外跳过均不得放行。主程序的验证、仅供下载的手动测试构建与正式 tag 发布是不同操作，测试产物不得隐式触发公开发布。正式交付强制当次全量验证，并对同一提交构建，不以历史 CI 绿灯或跨运行产物替代门禁。

主程序的前端静态产物仅在 Linux 检查和构建一次，在同次运行内供三平台 Go 验证及原生 CGO 打包共享；此取舍减少重复安装与构建，不保证 Windows/macOS 前端工具链兼容性。验证及构建默认仅读权限，只有最终发布 job 可写；正式发布串行且不取消已运行的发布。Mihon 的签名与仓库安全边界归 `mm-mihon-extension`，操作与必需检查迁移归 `.github/README.md`。

## 决策与规格状态

领域概念只在 `mm-domain-language` 定义；任务身份、准入、寻址、重启、存储分包、用户名和迁移的理由归本节点引用的 ADR，不另复制论证。

`mm-adr-0008` 已实施，重启入参与声明标签的边界及逐次有损合并事件保证归该决定。ADR 0002 已被 ADR 0005 取代，仅保留原历史文件。`.scratch/` 中的实施规划和验收记录作为输入，不提升为长期节点。
