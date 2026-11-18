---
id: mm-web
type: module-design
status: active
title: 浏览器应用与客户端状态归属
parent: mm-architecture
---

## 责任与内部边界

浏览器应用负责藏书浏览、编排、阅读与管理交互。身份与角色由 `auth` 持有，`App` 配置路由；`session` 协调全站连接和失效；功能 hooks 持有查询、功能投影及补拉；页面拥有筛选、选择、展开、弹窗与待提交草稿。页面卸载不能销毁应持续存在的全站连接，连接重建也不能成为清空页面编辑态的理由。

Layout 路由位于 `ApplicationSessionScope` 内；全屏阅读器与作品群图不在该作用域，不能要求所有页面依赖会话 Provider。`features/task-observation` 共享任务快照与气泡投影，不由每个页面各建 SSE；`features/external-library` 管传输会话功能状态。本机阅读数据的生命周期归 `mm-web-reader`。

## 服务边界

API 是规则与授权的最终裁决者；前端守卫只避免向无权限用户呈现无法使用的整屏管理操作。`src/api/generated.ts` 是生成的服务契约，不手工维护平行类型。功能异步请求必须校验所请求的作用域仍有效，避免切库或切系列后旧结果覆盖新视图。

locale 与 theme 提供表现偏好，不混入业务事实或用户在线会话。普通设备偏好（含阅读偏好）统一使用 `yomifolio_*` 键，由 `src/utils/browserPreferences.ts` 集中定义；新键存在时优先，否则兼容读取并复制旧 `manga_manager_*` / `manga_*` 值。保留旧值以便写入失败后重试，storage 不可用不得导致应用崩溃，解析与取值校验仍归调用方。此迁移不涉及离线书、队列、owner、cache 或认证与 Service Worker 身份。Layout 与 Dashboard 的资料库内部事件统一使用 `yomifolio:open-add-library` / `yomifolio:open-edit-library`。

验证命令、依赖说明归 `AGENTS.md` 和 `web/README.md`，不在此复制。

核对入口：`src/App.tsx`、`src/session/ApplicationRoutes.test.tsx`、任务观察 hooks 与页面切换竞态测试。

## 全局搜索首批会话

`components/layout/useGlobalSearch` 持有输入、范围、打开状态、首批请求及有效条目；`Layout` 与 `SearchModal` 消费该会话，不从非空输入推断查询已成功。空白输入为 idle，非空输入先等待约 300ms 防抖再请求；成功零条与请求失败分别呈现，失败可对当前输入和范围立即重试，不再等待防抖。状态与错误使用 locale 文案及可访问的异步通知。

会话对象与 application query scope 共同标识响应归属，每次 effect 生命周期是独立请求代际；关键词、范围、打开状态或现有有效读者/连接作用域替换时取消请求并隔离迟到响应。取消仅节约资源，不能代替归属与代际检查。新输入或范围在渲染时立即使旧列表失效；关闭与重置不能被在途请求回填，重开对保留输入重新请求。复用已有 `apiClient`，不建立第二种 transport。此处连接作用域沿用 `session` 生命周期，不把普通 SSE 重连扩成新的搜索身份。

只有当前成功结果可被鼠标或键盘选中；上下键保持索引边界，空列表回车无动作。系列进入 `/series/:id`，书进入 `/reader/:id`。首批契约、服务端排序、权限及检索字段保持不变；本会话尚不提供分页或继续加载。

核对入口：`src/components/layout/useGlobalSearch.test.tsx`、`src/components/layout/searchSession.test.tsx` 与 `src/components/layout/SearchModal.session.test.tsx`。组合测试通过真实 HTTP 客户端的可控 adapter 制造取消无效、乱序完成及失败，不以静态 hits 替代会话验收。

## 视口内可达性

弹性输入框与连续长标题必须允许收缩，不能将旁边的清空、目录浏览、编辑或快照操作挤出容器。标题可截断并保留完整名称提示；目录选择统一由 `DirectoryPicker` 保证输入与浏览按钮的空间分配。

跨裁切容器的操作菜单复用 `AnchoredPopover` 的文档根挂载、锚定和视口避让能力；业务适配器保留操作目标、权限和回调，不复制定位规则。触屏用户不能依赖鼠标悬停才能发现菜单入口。

响应式交互须在实际组件组合与浏览器布局下验证边界、命中目标及滚动后的可达性；仅验证 JSX 类名不能证明内容可见或可点击。
