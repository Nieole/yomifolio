# 08 安全元数据补充、审计及撤销验收

## 工作树与范围

- 唯一工作树：`C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-08`；分支 `mcp-enrichment-audit`，HEAD `b3083e4bd2e608521519f05fb1467fead8e0f6e8`。
- 全部改动未提交，未 stage、commit、merge 或操作 12/父树。接续现有未提交实现，不重做或删除功能；仅统一前代理引入的 CRLF，消除无关整文件 diff。
- 07 确认提案是唯一事实输入；新增同步 enrichment 应用用例、完整收据持久化、薄 MCP/管理员 HTTP、管理员操作历史和撤销交互。没有作者、封面、LLM、清理或第二个 Runtime。

## 实际验收

主接缝已由 effort spec 确认：真实 `bootstrap.Open` + 临时 SQLite + 通用 Go MCP SDK + 网页 HTTP；仅外部来源替换。`TestProductionMCPEnrichmentAuditUndo` 覆盖：

- 明确身份/版本确认提案后组合 Bangumi、AniList 字段；描述只补空、非空和既有评分不变；既有个人标签保留，来源 favorite/年份/站点标签不导入，内容标签追加；HTTP(S) 来源链接补充。
- 9/10 与 80/100 等权得到 8.5，完整审计保留原值、尺度及 `(9/10*10 + 80/100*10)/2 = 8.5`。来源适配回归验证 AniList 88/100、MAL 7.98/10、Bangumi 7.6/10 以及缺失评分不伪造；已有十分制 Rating 转换未变。
- 相同键同载荷返回原 ID、replayed=true/applied=false；同键不同载荷拒绝。
- 可控提案读取屏障（无 sleep）把并发编辑、新锁、撤权放在实际写事务之前：相关字段/锁导致整项 conflict；撤权拒绝；当前用户降级后网页查询/撤销与 MCP 撤销拒绝。
- 无关 publisher 修改不阻止新补充或撤销，也不会被覆盖。整组标签后续新增和当前字段锁阻止整项撤销，未部分恢复。
- SQLite 触发器在标签写入时报错，前面描述写入真实回滚，公开 State 仍为空，失败收据持久可查。
- 多系列 changed/conflict 独立报告；失败不回滚别的系列。公共应用测试另守护大收据后的响应预算和 unprocessed 剩余项。
- 管理员可在源凭据撤销后撤销，恢复原评分、描述、链接及原有标签；查询关联撤销 ID。关闭生产资源、关闭原 SQLite store，再以新连接和新生产图查询并撤销另一个持久操作。
- 旧普通编辑、扫描集合、普通提案守卫随完整后端门禁运行；不改这些入口语义。

网页交互 `metadataOperations.test.tsx`：展示前后/短理由/来源，确认撤销后采用权威结果，冲突不误报恢复，普通用户不请求/渲染，游标分页去重，网络失败后同键重试。两份自包含概念示意保留于 `.thinkrail/context/mockups/08-{before,after}.html`；不是浏览器截图或视觉回归证据。

## 红绿与发现的真实失败

1. 接续后的首次定向 Go 测试：bootstrap 编译失败，前代理把无返回值 Process.Close 当作有返回值；修复后通过。
2. 新增公开冲突审计断言先红：第一个相关字段冲突时 fields=[]，丢失所选来源；补充/撤销不再提前 break，保留全部相关审计，再整体复位 Changed/After，通过。
3. 新增大收据 Batch 用例先红：实现继续执行后续项，没有兑现描述中的响应预算。执行下一独立单位前预留最大 200 KiB 收据和剩余占位项；不足预算明确 unprocessed，通过。非法请求键不原样放大失败响应。
4. 前端初次 build 失败：SettingsSectionKey 缺 metadata-operations；接通非草稿 section 后通过。初次 eslint 还有 cleanup ref warning，修正请求 generation 检查后无警告。
5. Go 初次 lint 报 unused newMCPHandler wrapper，移除冗余 wrapper、保留单一注入函数；随后 gofmt 提示 host.go，格式化后 0 issues。
6. 第一次完整后端失败仅在旧 MCP 精确工具清单，未加入新六个工具。补齐精确名单和只读注解断言（不删守卫）后定向 bootstrap 通过；有明确修复后仅做一次纠正全量运行并通过，不是盲重试。日志保留所有失败。

## 实际门禁

环境：Go `go1.26.10`；共享 GOCACHE=`C:/Users/saber/.thinkrail/agent-worktrees/mcp-library-01/.tmp/merger01/gocache126`；本树 GOTMPDIR=`.tmp/08/gotmp`，GOLANGCI_LINT_CACHE=`.tmp/08/lintcache`；Node 26.13.1。

- `go test -p 1 ./... -count=1 -timeout=10m`：纠正后完整通过，`.tmp/08/backend-full-corrected.log`。首次失败日志 `.tmp/08/backend-full.log` 不隐藏。
- 此后增加更严格公开断言、关闭原 store 再重开及失败响应键大小保护；最终 `go test -p 1 ./internal/application/enrichment ./internal/bootstrap -run 'Enrichment|MCP' -count=1 -timeout=3m` 通过（`.tmp/08/backend-final-focused.log`），最后扩展的重开/同键不同载荷公开用例再通过（`.tmp/08/public-final.log`）。未把定向运行冒称又一次最终全量。
- `gofmt -l ./cmd ./internal`：空；`golangci-lint run`：最终 **0 issues.**（`.tmp/08/lint-delivery.log`）。`git diff --check`：通过。
- `npm test -- --maxWorkers=2`：121 文件、1149 测试通过（`.tmp/08/frontend-tests-final.log`）；随后新增分页和网络失败同键重试断言，定向该文件 5/5 通过（`.tmp/08/ui-final-focused.log`）。不冒称全套已运行 1151 项。
- `npm run build` / `npm run lint`：最后改动后通过，无警告（`.tmp/08/frontend-{build,lint}-delivery.log`）。
- `go run ./cmd/tsgen`：生成前后 SHA-256 一致，未漂移。
- PowerShell `sqlc generate`：退出 0，只有已知 parser 诊断；sqlc 生成文件无 diff。schema_handwritten 不进入 sqlc 模型输入。
- `sql/query.sql`、新 handwritten SQL 均 ASCII。

## 所属契约与复用

长期行为更新到 architecture、application、database、seriesmeta、metadata、api、bootstrap、web 的所属规格及 CHANGELOG；未改 ADR 状态或工单 triage 为 done。`enrichment.Service` 提供 State/Apply/Batch/Get/List/Undo，供同一生产图的网页及 MCP 复用；`MetadataJournal` 绑定真实写事务；授权继续复用 delegated.Subject/AuthorizeTx，不存授权快照；ContentTags/EnrichmentRating 是来源策略函数；State、EnrichmentLocked、WriteEnrichmentState 把字段锁解释和事务绑定写入归口 seriesmeta，应用只编排授权、预期值、收据与提交。最后做这项边界归位后，`.tmp/08/domain-boundary.log` 中 enrichment/bootstrap/seriesmeta 的 Enrichment/MCP 定向回归通过，tsgen SHA-256 仍一致，最终 lint 0 issues；不冒称这之后又全量测试。12 合并时共享装配/契约/CHANGELOG 文件可能冲突，父代理应合并而非覆盖。

无已知票 08 未满足项。限制：此轮未跑真实浏览器截图/像素测试，HTML 为概念示意；不宣称所有潜在并发顺序均已穷尽。清理、作者、封面与 LLM 是后续票范围，不是本票已实现能力。建议 commit subject：`feat: add audited metadata enrichment and safe undo`。
