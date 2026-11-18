# 02: 系列宽松名称匹配与旧库升级

**What to build:** 读者在全局、对应资料库及协议入口按系列名称或元数据标题查询时，得到一致的宽松匹配；已有藏书升级后直接可查，后续改名和维护重建也不会改变规则。不要求用户重新扫描归档。

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] 数据库 module 集中同一确定性归一化规则：宽度折叠、NFC、无语言环境 Unicode case folding、删除 Unicode 空白和 Punctuation、再次 NFC；符号保留，不以完整 NFKC 代替宽度折叠。
- [x] 系列名称与元数据标题独立进行归一化连续子串匹配，不跨字段拼接，不做拆词。覆盖大小写、全半角、标点、空白、单字/双字/长词及归一化后长度变化；C++ 不变为 C，圈号不额外变为普通数字。
- [x] 查询和被索引文本同源归一化，以归一化后的字符长度选择 gram/trigram。空白或仅标点归一化为空时不产生全库命中；资料库既有其他筛选与排序不变，筛选在分页前生效。
- [x] 统一函数覆盖迁移、运行期、维护及测试连接；系列新增、修改、删除经触发器维护索引和必要派生投影。不能只覆盖扫描入口，不修改源名称、标题、路径或身份，不以每次全库归一化代替索引。
- [x] 按版本升级系列搜索派生数据，显式更新旧触发器；成功回填后才记录完成。旧库、重复迁移、失败/中断后重试均验证，不把整个现有迁移描述为天然原子。
- [x] 维护重建、增量维护和升级回填使用同源规则；文档说明直接外部写库需要支持归一化函数的连接，不把函数缺失吞成零结果。
- [x] 同一系列在全局与资料库命中集合一致；协议系列查询共享匹配语义，但保留 OPDS/Mihon 各自分页、真实总数及既有排序。书通过所属系列命中的既有路径不能因系列索引变化而损坏；书自身归一化与新混排留给 03。
- [x] 通过真实 HTTP + SQLite seam，使用独立预期命中集合而非仅比较两个可能同漏的入口；覆盖旧库升级、写入生命周期及实际维护重建后再搜索。注入索引缺项能让测试发现静默漏查。
- [x] 复用既有 gram、双路径、迁移与协议测试风格；本票功能及迁移测试、后端格式/lint 门禁通过。若修改 SQL 绑定或生成契约，完成相应再生成与漂移检查；不以最终验收票代替本票验证。

## Answer

完成于分支 `series-normalized-search-02`。提交：

- `3574acd1`：同源 Unicode 归一化、确定性 SQLite 函数、系列投影/索引/触发器、v5 版本升级及失败重试、全部查询消费者和维护连接契约。
- `682e36ea`：真实 HTTP + SQLite 独立字面 ID oracle；旧库升级、生命周期、索引缺项敏感性及实际 HTTP 重建；OPDS/Mihon 分页真实总数回归。
- `9695fb38`：数据库/API 持久 SPEC、CHANGELOG 与维护边界。
- `3cc87f5f`：合并 `search-experience-integration` 当前 tip `3d5762b638b824187cb13299c9be7913e8f225bc`，CHANGELOG 冲突保留双方内容；已确认 integration 是 HEAD 祖先。结案状态与验证记录另见本文件提交。

### 验证

- TDD：归一化函数/投影缺失的 RED 后实现转绿；HTTP 禁用宽度折叠时实际仅 `[s_2]`、预期 `[s_1 s_2]`，恢复实现后通过。索引双路径缺项会被 HTTP oracle 识别，真实维护任务后恢复完整集合。
- `GOTMPDIR="$(pwd)/.tmp" go test ./internal/database -count=1` → pass。
- `GOTMPDIR="$(pwd)/.tmp" go test ./internal/api -run '^TestSeriesNormalizedSearchHTTP' -count=1` → pass。
- 合并后 `TEMP='C:\tmp' TMP='C:\tmp' TMPDIR='C:\tmp' GOTMPDIR='C:\tmp' go test -p 1 ./...` → 全部 pass（含 API、database、scanner）；无跳过测试。
- `gofmt -l ./cmd ./internal` → 空输出。
- `GOLANGCI_LINT_CACHE="$(pwd)/.tmp/lint-cache-v215" .tmp/bin/golangci-lint run --timeout=10m` → `0 issues.`，exit 0；使用 CI 同版 v2.15.0（本机原 v2.12.2 不支持 Go 1.27 导出格式）。
- PowerShell `sqlc generate; exit $LASTEXITCODE` → exit 0，绑定无漂移；`go run ./cmd/tsgen` → 26 types，`web/src/api/generated.ts` 无漂移。
- `spec_validate` → pass；独立 request_review 前两步均 APPROVE。

### 限制与范围

系列重建使用自己的事务，不能由此声称整个旧库迁移天然原子；升级前须备份。默认 Go SQLite 驱动所有连接注册 `search_normalize`，外部直接写库或重建须同样支持该函数，缺失时报错而不是返回零命中。未改源名称/标题/路径/身份；书自身归一化、全局新排名/分页属于03，本票未实施，也未测量大库性能预算。

本机长临时路径全量重跑曾遇 Windows 目录重命名 `Access is denied` 和 watcher 超时；独立排查未发现本票改动相关性，短 TEMP/TMP 路径全量复验已通过。未为绕开此问题改动无关测试。前端未改动；忽略的 dist 仅用于 Go embed，不提交构建产物。未创建 PR、未删除工作树。本票无遗留功能项。

### 集成审查补充

Mihon 保留原始 q 区分真正空查询的浏览与非空但归一化为空的搜索。新增跨资料库及指定资料库的 ASCII/Unicode 空白 HTTP 回归，真正空输入仍返回浏览结果。新增测试先失败（`got 6; independent oracle wants 0`），修复后 `go test ./internal/api -run 'Test.*(Search|Mihon)' -count=1` 通过，`git diff --check` 通过。集成树 API 整包复验遇到原有 relocation 测试的 Windows rename `Access is denied`，搜索/迁移针对性复验通过，未将整包复验标为通过。
