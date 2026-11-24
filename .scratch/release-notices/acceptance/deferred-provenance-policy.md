# 发布门禁：上游 provenance 降级为暂缓的非阻断风险

> 2026-11-24 更新：外置证据中的上游原文已纳入清单，`LICENSES/manifest.json` 从 388 文件 / 8 gaps 变为 401 文件 / 2 gaps；本文以下保留当轮（provenance 降级）的结论与日志指向，数字变动与新一轮证据见 `npm-legal-text-adoption.md`。policy 不变：仍不能宣称 release-ready。

## 结论

- AVIF/libyuv/WASI 上游构建来源取证与可复现构建/attestation 证据不再是发布硬门禁，改为 `LICENSES/release-review.json` 中逐条声明的 deferred 风险，gate 通过时明确打印它们，不视为已核验。
- 仍然阻断：适用许可/版权/NOTICE/PATENTS 全文缺失或被改动、确有分发组件的许可适用性未定（当前 2 个 NPM gaps）、MPL-2.0 对应源码获取与修改提供、同版本通知包送达与解包校验、产物版本/SHA/库存/路径不一致。
- 当轮不新增第三方法律原文，不改 codec/依赖/协议/UI，不把外置候选纳入库存，因此当轮 `LICENSES/manifest.json` 为 388 个文件、8 个 gaps；外置上游原文纳入后为 401 个文件、2 个 gaps。**不能据此宣称 release-ready 或 provenance 已核验。**

## 实现

- `scripts/release_notices.py`：`evaluate()` 返回 `{status, blockers, deferred}`，把阻断项与暂缓风险分开；`gate()` 先逐条打印 deferred 风险再对 blockers 失败。新增 `legal_text_failures()`，直接检查根部法律文件与 manifest 记录的字节存在且未变（缺全文/字节变更即阻断）；`check_artifact()` 保留并按 target 收集三平台版本、commit、二进制 SHA、Go 运行时、CGO、编译器/动态依赖清单、MPL 源码比较与依赖输入指纹的对应检查。通知 ZIP 的 create/verify（成员集合、重复/越界路径、逐文件字节与 SHA、版本、本地引用）未改。
- `LICENSES/release-review.json`：`blockers` 只留法律与交付缺陷；`deferred` 列出 `avif-libyuv-wasi-build-provenance` 与 `reproducible-build-attestation`，各自绑定可选的 content-bound evidence 类别。
- `.github/workflows/release.yml`：`notices` job 的 gate step 更名并注明“provenance 只打印、不认证”；下载→解包→verify→gate 顺序，以及 publish 前再次 verify/gate 均未变。
- 文档：`.scratch/release-notices/spec.md`、issues/01、`THIRD_PARTY_NOTICES.md`、`DEPENDENCY_MANIFEST.md`、`CHANGELOG.md` 只写当前策略；上游取证作为未完成的 follow-up 保留。

## 测试与证据

隔离 fixture 证明（`scripts/release_notices_test.py`）：

- 仅 provenance 未核验时门禁通过并给出 deferred warning：`test_unverified_provenance_only_passes_with_deferred_warning`、`test_recorded_deferred_evidence_keeps_gate_passing_without_claiming_verification`。
- 暂缓证据的增减不改变阻断列表：`test_deferred_evidence_changes_do_not_change_blockers`。
- 缺 LICENSE/NOTICE 全文或法律字节被改仍拒绝：`test_missing_applicable_legal_text_blocks`、`test_changed_legal_bytes_block`。
- 缺 MPL 源码证据或本地源码与精确归档不一致仍拒绝：`test_missing_mpl_source_evidence_or_comparison_blocks`。
- 实际库存不一致（collection gaps、依赖输入指纹不符）仍拒绝：`test_inventory_gaps_and_mismatches_block`。
- SHA 损坏、版本/commit 不符仍拒绝：`test_broken_sha_and_version_mismatch_block`；无证据时不能仅靠状态放行：`test_no_status_flag_can_substitute_for_evidence`。
- 真实仓库仍被 collection gaps 与法律/交付项阻断，且阻断列表不含 provenance：`test_repository_stays_blocked_on_legal_gaps_not_provenance`（当轮为 8 个 gaps；纳入上游原文后该用例断言的数字同步为 2，并额外断言三平台 native/WASI 那条阻断仍未被纳入的 tag 级原文关闭）。
- 通知归档 5 项回归（字节、版本、悬空引用、越界/重复成员、缺成员）保持不变。

实际运行：

- `python -B scripts/check-license-notices.py` → exit 0，当轮 388 files / 8 gaps（`integrity-deferred.log`）；纳入上游原文后 401 files / 2 gaps（`integrity-adoption.log`）。
- `python -B -m unittest discover -s scripts -p '*_test.py'`（过滤 `ci_changes_test.GitComparisonTests`、`ci_workflows_test.TagResolverTests` 四个会在自己的临时目录建 commit/tag 的用例）→ `Ran 45 tests ... OK`（`checks-deferred.log`）。保留真实仓库策略测试，因为它只读 `notices.ROOT`，不写 index/history。
- `actionlint` → exit 0（`actionlint-deferred.log`）。
- `bash scripts/check-doc-style.sh` → exit 0，仅打印既有“疑似历史叙述”提示。
- `python -B scripts/release_notices.py gate --version v1.10.0` → exit 1，先打印两条 deferred warning，再列出当轮 8 个 NPM gaps 等阻断（`readiness-gate-deferred.log`）；纳入上游原文后为 2 个 gaps，其余阻断不变（`gate-adoption.log`）。
- 本地 `release_notices.py create` + `verify`（真实仓库字节）→ 通过，394 个成员、ZIP SHA256 `4adde6fa3b71cd10fa6c7465bf10098f40277cb48c132e064c27d7d75c791df9`；本地 ZIP 已删除，未作为发行物。
- `git diff --check` → exit 0。

未运行：Go/UI 全量（无业务代码改动）；远端 release workflow 的实际上传→下载→解包与三平台构建，因此“送达路径已执行”仍未被证明。

## 仍未满足的法律阻断项

1. 2 个 NPM 目录缺适用完整法律原文（@humanfs/types 0.15.0 与 nested @tybys/wasm-util 0.10.3）：锁定 tarball 与对应 source commit 只有 metadata 标签，既不能合成文本也不能借 sibling 包；需上游适用全文或真实 bundle 排除证据。原来的另六项缺口已纳入上游原文，但证据强度按项记录（natural-compare 为作者链接文本、rolldown 与 oxide 为 tag 对应而非 native/WASI 构建对应），不构成 provenance 或 native 组件适用性已核验。
2. 三平台真实构建的 native compiler/CGO 支持库/runtime 法律文本尚未与实际产物核对，bundled Web/WASI 载荷的适用通知是否在发布载荷中尚无逐 target 证据。
3. MPL-2.0 对应源码目前只有上游获取位置，实际 release build 的源码对应性与修改源码提供义务未核。
4. 通知包的远端上传→下载→解包→校验链路尚未在真实 release run 执行。

## 未关闭的 follow-up（非阻断）

- AVIF v0.6.0 预编译 WASM 指向的 libavif/AOM/dav1d/libyuv 精确 revision、WASI SDK/compiler/runtime 输入及对应法律文本仍未取得；记录为 deferred，不代表已核验或合规完成。
- 可复现构建/attestation 证据未取得，同样只记录为 deferred。

## 发布门禁取舍（2026-11-24）

维护者决定为尽快发出 v1.10.0：门禁只对“记录在清单里的许可文本缺失或被改、版本不符、证据被篡改、产物版本/构建设置/完整性/路径不符”失败；仍缺适用全文的分发组件、未定的许可适用性、MPL 对应源码相符性与未收齐的清单项作为残留风险输出，不阻断。AVIF/libyuv/WASI 构建来源与可复现构建证据仍为非阻断暂缓。发行说明与 THIRD_PARTY_NOTICES.md 必须同时申明这些残留项未清。
