# ThinkRail 规格审计

## 范围与判据

覆盖审计前全部 38 份长期规格、全部 15 份 ADR，以及规格归属相关的 agent 文档；先读规格，再静态核对相关实现和测试。静态核对不等于执行测试，不保证穷尽每条业务路径。

允许修正有证据的文档失真、遗漏、重复归属和图结构；不修改业务代码，不以现状覆盖有效契约。历史决定保留历史语义。这里记录本次审计证据与待查问题，不是第二份长期产品规范。

## 组织结论与修正

- 原图可通过结构校验，但 ADR 0010—0015 缺少 ID/type，无法通过图检索与遍历；补齐其元数据及根架构、相关模块的引用。ADR 0002 作为 deprecated 历史节点登记并指向后继，正文不改写。
- 根目标、跨模块架构、领域词汇、模块契约与临时工作记录的分层总体合理；不进行纯审美改名，不为每个目录机械新建 SPEC。
- 补充 `internal/bookcontent/SPEC.md`、`internal/bookidentity/SPEC.md`、`internal/bootstrap/SPEC.md`，分别承载已有内容读取/发布、身份依据、生产装配的独立边界；从根架构移出局部契约。
- 全局搜索交互下沉为 `web/src/components/layout/SEARCH-SPEC.md`，父规格仅留责任与入口，不创建脱离实现的虚拟目录。
- agent 文档区分长期图、临时 `.thinkrail/context/TASK-<slug>.md` 与 `.scratch/` 规划/验收记录；修正“目录级文档没有工具读取”和“runtime 独占生产装配”的过时说法。
- 四份新规格以 draft 交付，等待维护者审阅边界；已有代码与静态核对不能代替规格审阅。侧栏浮层继续保持 draft：已有实现及测试不等于已完成真实浏览器几何和触屏验收。

## 内容核对结果

### 直接修正

- `external`：传输管理器已承担复制、条件发布与会话推进；各平台使用不覆盖已有目标的发布原语，不再描述已消除的普通 rename 覆盖窗口。
- `scanner`：补摄取结果的清理资格、防存储离线误删及身份重连守卫；删除已失效的 doc.go 漂移诊断。
- `reading`：补显式读者与匿名兼容查询，用户查询不回退全局；不把此规则错误扩展为所有写命令拒绝零用户。
- `organize`：补作品群持久修复责任、事务发布、稳定身份与成员时间；纠正 AI 分组完整生成/入队/裁决归属。
- `parser`：补归档暂存、发布排空及源变化拒绝覆盖契约。
- `application`：补同步完整用例与保存/交接结果，不把所有应用命令强制任务化。
- `runtime`：区分共享依赖装配与生产组合根，补 Start、Shutdown 与 Close 的真实顺序及排空失败边界。
- `task`：补资料库覆盖、读租约、预留、真实任务体退出、无窗口升级及删除退休契约。
- `diskwork`：补传输复制的并发归类和取得令牌后的暂停复查。
- `config`：明确配置已发布不等于消费者全部生效，字段分类留在代码。
- `relocation`：保留路径已提交与交接完成的独立事实，不以 error 推导未提交。
- 浏览器阅读：补离线归属、会话失效与显式登出的区别、不可达判据及本机威胁边界。
- API/搜索/数据库：排名定义归搜索能力，HTTP 只拥有参数/错误/授权/投影；当次性能数字归验收材料，保留完整候选、单批一致性和游标等长期约束。
- 词汇与 ADR：纠正中断遗漏排队及过度承诺可重试；移出具体持久层清单；ADR 0008 的旧符号标为历史对账工具，ADR 0014 的当次交付限制归历史证据段。
- 根目标：明确“不提供页级双向同步”指独立 Mihon 源扩展，不能误读为整个服务端缺少同步协议。

### 已覆盖、无独立修正文项

`internal/{proposal,seriesmeta,metadata,images,koreader,taskstore,runhandle,storageio}/SPEC.md`、`web/src/session/SPEC.md`、`extensions/mihon/SPEC.md`，以及 ADR 中未列为修改对象的现行决定。部分文件仅补图引用；metadata、images、session 仍有以下实现疑点，不表示确认其实现完全满足规格。交付边界静态核对了全部六份 workflow。

## 保留的实现违约疑点

以下未做运行复现，不能作为已确认缺陷或据此弱化长期契约；后续应分开诊断与修复。

### 高：离线下载未完整绑定用户归属代次

- 契约：`web/src/pages/book-reader/SPEC.md` 要求下载仍持有书令牌及用户归属代次，换用户后不得恢复旧索引。
- 证据：`web/src/features/offline/offlineReader.ts` 的 `cacheBookForOffline` 在首次 `await caches.open` 前未捕获 owner/代次；`stillOwnsDownload` 只比书令牌，代次检查存在于进度同步路径。
- 待验证：受控暂停缓存打开，在恢复前换用户或登出；检查旧任务是否能在新归属下登记索引。现有下载中换用户测试不覆盖该入口窗口。

### 高：LLM 上游错误可能回显密钥

- 契约：`internal/metadata/SPEC.md` 要求上游错误脱敏密钥。
- 证据：`internal/metadata/openai.go` 与 `internal/metadata/openai_legacy.go` 的错误响应截断路径未按配置 APIKey 脱敏；公共源错误测试不证明 LLM 路径满足要求。
- 待验证：让受控上游在错误响应中回显哨兵密钥，确认返回错误及日志是否泄漏；不使用真实密钥。

### 高：普通迁移提交是否复核全部已检查事实

- 契约：`internal/relocation/SPEC.md` 要求提交前复核已检查事实。
- 证据：`internal/relocation/path_store.go` 的 `commitPaths` 使用 `recordsOnly: true`，复查数据库记录而跳过目标磁盘事实；`restoration.go` 的恢复解除保护则重新执行完整检查并比较摘要。
- 待验证：预检后改变目标可读性、大小或修改时间再提交，确认保护与异常确认是否仍覆盖；需裁决“已检查事实”的既有安全意图，不能仅将文档改成只复查记录。

### 高：双边尺寸下部分滤镜的 Fit 行为

- 契约：`internal/images/SPEC.md` 要求未启用 FitInside 时按双边尺寸精确缩放。
- 证据：`internal/images/processor.go` 的 bspline/catmullrom 分支无条件使用 `imaging.Fit`；其他分支使用精确 resize。
- 待验证：非等比例输入与双边目标尺寸，分别覆盖这两个滤镜。现有精确尺寸测试未覆盖二者。

### 中：SSE 重连恢复通知未覆盖目录消费者

- 契约：`web/src/session/SPEC.md` 要求重连或投递链缺口通知消费者补拉。
- 证据：`web/src/session/ApplicationSessionProvider.tsx` 重连仅向管理员广播 recovery，未推进目录失效水位；`internal/api/sse_broker.go` 接入不补发目录 refresh。
- 待验证：普通用户断线期间改变目录，重连后检查当前查询是否补拉；管理员目录消费者也需单独覆盖，运行订阅恢复测试不能代替目录测试。

### 中：维护重建的扫描观察者与统一准入边界

- 契约：`GLOSSARY.md` 称每次动盘扫描都有观察者；ADR 0005 保留统一准入意图。
- 证据：`internal/application/maintenance/maintenance.go` 调 `scanning.ReindexLibraries`，`internal/application/scanning/maintenance.go` 直接向 `IngestLibrary` 传 nil observer；应用规格已承认系统重建与单库扫描可能同时操作同库。
- 待验证：分别确认扫描运行归属、进度观察者与同库互斥是否为同一契约；有运行归属不能推出一定有扫描观察者，也不能直接推导所有同库扫描必须互斥。

## 验证口径

- `spec_validate`：通过；只覆盖断链、重复 ID 与父子环。
- 全部长期节点的 Markdown 相对链接与正文 `mm-*` ID 检查：49 个节点、15 份 ADR，0 个失效目标；文件/符号入口另做静态核对。
- `git diff --check`：通过。
- `bash scripts/check-doc-style.sh`：未完成。多次尝试均超时，最终单次等待 300 秒仍未完成；日志出现 `scripts/check-doc-style.sh: line 93: [: : integer expected`，早先尝试有 MSYS `fork: retry: Resource temporarily unavailable`。不将它报告为通过，也不在本次范围内修改检查器。
- 未执行完整 Go、前端、Android 或真实浏览器测试：本次仅修改文档。已有测试和历史验收记录不能视为本次执行成功。
