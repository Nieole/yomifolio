# 纳入外置证据的 NPM 上游法律原文

## 结论

外置证据目录（C:/Users/saber/.thinkrail/release-license-evidence/npm）中已取得真实原文的 6 个 NPM 缺口按证据强度分成两档纳入 `LICENSES/`，`@humanfs/types` 0.15.0 与 nested `@tybys/wasm-util` 0.10.3 保持缺口。**仍不能发布**：剩余 2 个缺口、三平台 native/WASI 实际组件适用性、MPL 对应源码交付都还是阻断项；本次不是法律审计/SBOM，也不证明任何预编译产物的来源。

清单数字：`LICENSES/manifest.json` 388 → **401** 个文件、8 → **2** 个 gaps；`DEPENDENCY_MANIFEST.md` 的 383 package entries 不变，依赖四项输入指纹（`go.mod`、`go.sum`、`web/package.json`、`web/package-lock.json`）不变。

## 按证据强度分档

A 档（完整适用文本，可关闭缺口；原文按 Git blob 原字节或 registry SRI 对应字节取得，未规范化换行）：

| 缺口 | 纳入文件 | 来源与强度 |
|---|---|---|
| @napi-rs/wasm-runtime 1.2.6 | `LICENSES/npm/node_modules/@napi-rs/wasm-runtime/1.2.6/LICENSE`（2138 B） | published gitHead `4fff029f…` 的根 LICENSE（GitHub blob `7fe7e35e…`）。含 LongYinan 2020-present 与 GitHub 2018 两段 MIT，整份保留，未截成第一段。registry tarball 内无 LICENSE，故按 commit 级原文而非 tarball 成员记录 |
| nested @napi-rs/wasm-runtime 1.2.2 | `…/@tailwindcss/oxide-wasm32-wasi/node_modules/@napi-rs/wasm-runtime/1.2.2/LICENSE`（2138 B）+ `LICENSE-EXTRACT-microsoft.txt`、`LICENSE-EXTRACT-joyent.txt` | 根 LICENSE 取自 gitHead `5b344c36…`（与 1.2.6 同一 blob 内容）。lock 为 `inBundle`、无独立 integrity：registry 精确 1.2.2 tarball 的 8 个文件与 SRI 已核验的 oxide 外层 tarball 内同名文件逐字节相同（本轮重新独立比对通过）。两段附注是真实 `dist/fs.js` 内未改动的完整注释区间（字节 1532:2337、277379:278518），作为补充文件单列，不与根 LICENSE 合成 |
| saxes 6.0.0 | `LICENSES/npm/node_modules/saxes/6.0.0/LICENSE`（3011 B） | gitHead `211fa0eb…` 的根 LICENSE（GitHub blob `187f3ba3…`）。含两组 ISC（Contributors、fork 来源 sax/Isaac Z. Schlueter）与已弃用的 Mathias Bynens MIT 说明，整份保留 |

B 档（有保留候选：原文真实，但不是该版本内法律文件/构建对应证明；已纳入以减少缺失，限制逐条保留在 `DEPENDENCY_MANIFEST.md` 与 `THIRD_PARTY_NOTICES.md`）：

| 缺口 | 纳入文件 | 保留意见 |
|---|---|---|
| natural-compare 1.4.0 | `…/natural-compare/1.4.0/README.md`（3352 B，tarball 精确成员）+ `author-mit-license.txt`（1087 B） | 1.4.0 tarball 无 LICENSE。README 保留 2012–2015 Lauri Rooden 版权行并指向作者 MIT 全文；该链接 `http://lauri.rooden.ee/mit-license.txt` 于 2026-11-23 实取 200 并保存原字节。**这是链接原文，非 tarball 内 LICENSE**，作者页不是 commit 固定文本，关联性与持久化仍需复核 |
| @rolldown/binding-win32-x64-msvc 1.2.19 | `…/1.2.19/LICENSE`（1302 B）+ `THIRD-PARTY-LICENSE`（2209 B） | tarball 无法律文件、registry 无 gitHead。取公开 tag `v1.2.19`（commit `b9c75ddc…`）的根 MIT 与其指向的第三方清单。**tag 对应，非 native 构建证明**；根 MIT 不替代第三方清单 |
| @tailwindcss/oxide-wasm32-wasi 4.3.5 | `…/4.3.5/LICENSE` + `crates/ignore/{COPYING,LICENSE-MIT,UNLICENSE}` | tarball 无自有法律文件、无 gitHead。取公开 tag `v4.3.5`（commit `9b2c925c…`）的根 LICENSE 与 crates/ignore 的三份文本。**WASI 预编译输入范围待核**：哪些 Rust/第三方输入进入 `tailwindcss-oxide.wasm32-wasi.wasm` 未映射；嵌套包文本仍记在它们自己的条目下，不当作本包许可 |

C 档（不可关闭，未合成）：`@humanfs/types` 0.15.0 与 nested `@tybys/wasm-util` 0.10.3 的包内与对应 source commit（gitHead `85069f19…`、`efb17cc1…`）均无完整法律文本，只有 metadata 标签（Apache-2.0 / MIT）。未借 sibling 包文本、未由 metadata 生成文本，二者仍为 gap 与阻断。

## 改动文件

- `LICENSES/npm/**`：新增 13 个原字节文件（上述）。
- `LICENSES/manifest.json`：`date` 改为 2026-11-24；每项 source 记录可核验的 git blob/tag/tarball 引用（`#gitHead=`、`#tag=…;commit=`、`tgz!/member#bytes=a:b`）、字节数与 SHA-256；6 个 package 的 `files` 补齐；`gaps` 降为 2 项。
- `DEPENDENCY_MANIFEST.md`：legal files 388 → 401；6 行 Texts 列更新；新增 “Texts adopted from version refs” 一节逐条写明来源与强度限制；Collection gaps 改为 2 项并要求上游适用全文或真实 bundle 排除证据。
- `THIRD_PARTY_NOTICES.md`：8 → 2 gaps 与对应表述；增补六项纳入文本的限制；第 3 条阻断补一句 tag 级原文不满足它；保留 “not a legal audit/SBOM” 表述。
- `LICENSES/release-review.json`：第 1 条阻断改写为 2 个缺口 + 六项纳入文本的强度说明，明确不关闭 native/WASI 适用性那条。
- `scripts/release_notices_test.py`：`test_repository_stays_blocked_on_legal_gaps_not_provenance` 的清单数字同步为 2，并断言三平台 native/WASI 阻断仍存在。该文件在 74cf7243 中是以 CRLF 提交的（`scripts/*.py` 其余均为 LF）；新增的 CRLF 行会被 `git diff --check` 判为行尾空白，因此本轮将该文件统一为 LF（行尾规范化），另将 `test_deferred_evidence_changes_do_not_change_blockers` 的 fixture 标签从旧阻断句改为中性的 `Example blocking legal gap`（该标签不参与断言真实仓库）。

## 验证（实际运行）

- `python -B scripts/check-license-notices.py` → exit 0，`401 files verified`、`Recorded collection gaps: 2`（`integrity-adoption.log`）。
- `python -B -m unittest discover -s scripts -p 'release_*_test.py'` → `Ran 18 tests ... OK`（`checks-adoption.log`）；`check_*_test.py` 另跑 `Ran 9 tests ... OK`。
- `python -B scripts/release_notices.py gate --version v1.10.0` → **exit 1**，先打印两条 deferred warning，再以 2 个 gaps、三平台 native/WASI、MPL 源码、未见 review evidence 与缺失三平台产物 metadata 阻断（`gate-adoption.log`）。
- `release_notices.py create` + `verify`（真实仓库字节，临时目录）→ 通过，407 个成员（`LICENSES/**` 401），ZIP SHA256 `40e3a8d593d1f6765b77ea561660ca6a0e3c4052dc12dffd1a9981d6a8e6421f`（`archive-adoption.log`、`archive-summary-adoption.json`）。本地 ZIP 已删除，未作为发行物。
- `bash scripts/check-doc-style.sh` → exit 0，`文档风格检查通过。`，只打印既有的“疑似历史叙述”提示，无超预算与行号/现状数字拦截（`docstyle-adoption.log`）。
- `git diff --check` → exit 0。

未运行：Go/UI 全量（无业务代码、codec、依赖版本、协议或 UI 改动），未 push/tag/release，未在远端 runner 执行三平台构建或通知包上传→下载链路。

## 剩余缺口与仍阻断项

1. `@humanfs/types` 0.15.0、nested `@tybys/wasm-util` 0.10.3：需上游适用全文（或上游说明其发布位置），或真实 bundle/module 贡献证据证明未分发。
2. 三平台真实构建的 native compiler/CGO 支持库/runtime 法律文本，以及 bundled Web/WASI 载荷的适用通知仍无逐 target 证据；B 档的 tag 级原文不满足这一条。
3. MPL-2.0 对应源码交付与本地修改对应性仍未在真实 release build 核对。
4. 通知包远端上传→下载→解包→校验链路仍未在真实 release run 执行。

需向上游索取的：`@humanfs/types`（humanfs/types 维护者）与 `@tybys/wasm-util`（toyobayashi/wasm-util）在当前版本的适用完整许可文本，或二者对“发布包内不含许可文本”的说明；`natural-compare` 作者 MIT 文本的 commit 级/版本内出处（当前只能取到非版本化的作者页）；rolldown 与 tailwindcss 的 `v1.2.19` / `v4.3.5` 预编译产物与其 tag 提交的对应关系（含 native/WASI 构建输入清单）。任何上游联络需另行授权。

## 说明

- `integrity.log`、`archive.log`、`archive-summary.json`、`readiness-gate-deferred.log` 等是本轮之前的原始捕获输出，保留其当时的 388/8 与 394 成员数字未改写；本轮数字见上文新增的 `*-adoption.*` 文件。
- 依赖输入指纹未变，因此本次不是依赖升级，也未据此更新任何 hash 蒙混检查。
