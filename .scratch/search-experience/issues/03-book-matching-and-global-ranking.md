# 03: 书宽松匹配与全局精确命中优先

**What to build:** 读者按书名称、元数据标题或所属系列找到书；全局中系列与书按真实命中依据统一排列，不再固定书先系列后，也不会因大量归属书而掩盖更直接的命中。本票交付可验证的统一首批结果，为后续继续加载提供完整排序键。

**Blocked by:** 02 — 系列宽松名称匹配与旧库升级。

**Status:** resolved

- [x] 书自身名称、元数据标题采用 02 的同源宽松匹配；名称和标题分别匹配。仅所属系列命中的书仍可返回，不扩大到作者、标签或路径。
- [x] 书新增、改名、元数据更新、删除、旧库升级、失败重试和维护重建均维护一致的归一化投影与两类索引；直接从原有旧版本升级以及从 02 的中间版本升级都能得到完整结果。
- [x] catalogsearch module 按正式规格统一排名：自身精确命中、自身子串命中、仅所属系列命中。所属系列精确不等于书自身精确；多个来源命中同一书时，取最佳排序键且仅返回一次。
- [x] 同层依次按起始字符位置、额外字符数、类型与持久标识组合排序。完整排序键只使用有界数值和唯一身份，不含完整标题、名称或其字典序；字符计数不按 UTF-8 字节。
- [x] 全部模式按同一顺序选择整批结果，资料库列表原有排序不变。候选裁剪和去重必须保证统一首批完整性，不能截断原始重复来源后导致漏项或缺额。
- [x] 不比较不同语料的原始 BM25，不按每批最大分数缩放后排序；兼容 score 按三层分别为 1、0.5、0.25，空批 max_score 为 0。全局界面隐藏分数，保留名称、封面、类型与导航。
- [x] 沿用现有检索范围及 title 作为 book 的别名；不扩大权限或改变读者阅读视图。首批采用正式规格的统一默认批量；后续结果可达性和新分页响应由 04 完成。
- [x] 通过真实 HTTP + SQLite 验证跨类型混排、各层及同层排序、同名同分、仅标题命中、多来源去重、大量归属书与直接匹配系列共存；对照完整预期排序的首批前缀，而非只断言各类型内部有序。
- [x] 前端验证分数不再显示、系列/书导航正常；与 01 已完成的会话状态兼容，但不把 01 当成此票的实施前置。完成本票后端测试、格式/lint，以及涉及前端的测试、构建/lint和必要生成检查。


## Answer

当前结果：书与系列同源归一化，版本 6 回填书投影与双索引（含从版本 4、5 升级、失败重试），触发器覆盖插入、修改与删除；维护书重建事务回滚。全局候选先按身份取最佳字段键再裁剪，每类型前 N 的并集包含混排前 N，catalogsearch 统一取首批 20。层级分数固定 1/0.5/0.25；完整排序键仅有 uint8/uint32/int64，不携带任何标题或名称。30 KiB 以上标题经现有 UpsertBookByPath 元数据入口写入，真实 HTTP 验证搜索、完整返回及维护后恢复；未截断标题。未实现 04 的分页、游标或响应字段。

### TDD 与验证

确认的 seam 沿用正式规格：真实 HTTP + SQLite、真实前端搜索会话和弹窗。RED：书宽松匹配测试在 A STRASSE、é、C++、孤—星 上失败；GREEN：同一测试通过。RED：混排 oracle 检出旧 BM25/批缩放分数，GREEN：完整预期集合的首批前缀、固定分数、跨类型层级与同层排序通过。RED：中英弹窗检测到旧相关度标签，GREEN：隐藏数值且会话/导航测试通过。证据日志保存在工作树忽略目录 `.tmp/03-verification/`，不作为产品文件。

以下命令均从本工作树执行，Go 使用 `C:/Users/saber/scoop/apps/go/current/bin/go.exe`，Node 使用指定 v26.13.1 安装；TEMP/TMP/TMPDIR=C:/tmp，GOCACHE=C:/Users/saber/AppData/Local/go-build：

- `go test ./... -count=1 -timeout=240s`：通过（含 database、catalogsearch、api；日志 03-go-all.log）。之后新增升级 HTTP 回归及完整 oracle 前缀断言，再运行下述所涉测试通过。
- `go test ./internal/api ./internal/database ./internal/catalogsearch -run 'TestBookNormalized|TestGlobalRank|TestBookOwnSearch|TestSearchKeepsFixedTierScores|TestWindowFunctionOrderByTotalOrder' -count=1`：通过，含真实升级、维护、索引故障独立 oracle、长标题、Unicode 字符位置与最优字段。
- `npm --prefix web test -- --maxWorkers=2`：119 文件、1136 测试通过；Vitest 报 4 个 forks worker 结束超时警告。
- 最终 `vitest run src/components/layout/SearchModal.test.tsx src/components/layout/SearchModal.session.test.tsx src/components/layout/searchSession.test.tsx src/components/layout/useGlobalSearch.test.tsx --maxWorkers=1 --pool=threads`：4 文件、17 测试通过。单独重跑 forks 模式曾发生 Windows worker 启动退出，threads 重跑通过，不压制告警。
- `npm --prefix web run build`、`npm --prefix web run lint`：通过。
- `gofmt -l` 所有修改 Go 文件：空输出；`git diff --check`：通过。
- CI 同版 golangci-lint v2.15.0 `run ./...`：最终 0 issues（独立 GOLANGCI_LINT_CACHE=C:/tmp/lint-search-book-ranking-03，PATH 优先 Go 真正 bin 避开 Scoop shim 启动超时）。
- `go run ./cmd/tsgen`、PowerShell `sqlc generate`：成功，生成产物无差异。没有修改产品 sql/query.sql 或公开响应类型。
- `git merge-base --is-ancestor 4f4e60cc HEAD`：成功；保持原分支，未提交、未变更索引或历史。

### 限制与交接

早期全 API 回归出现一次 Windows config.yaml rename Access is denied，以及旧分数测试预期；分数测试已按新兼容契约更新，配置失败测试独立重跑及随后全 Go 回归通过。部分 Scoop shim、下载、文档风格脚本运行超时；不把文档风格脚本计为通过。没有执行真实性能预算或真实浏览器视觉验收；自动化前端是 jsdom 下真实会话/弹窗，不冒充浏览器验收。

没有 visualize 工具。UI 改动前已写自包含对照稿，供协调者展示：`.thinkrail/context/mockups/03-search-score-before.html`、`03-search-score-after.html`。仅删除分数展示，保留封面、类型、名称和导航。持久契约更新在 database/API SPEC、新增 catalogsearch/SPEC，维护限制在 docs/development.md，用户结果在 CHANGELOG.md。

后续 04 仍需实现并验证分页、单批读事务及游标自解析/8 KiB 上限；本票只交付有界完整键，不声称已验证游标往返。未开 PR、未提交、未删除工作树。
