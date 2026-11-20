# Actions 操作指南

## 选择操作

**只想确认代码是否可用**：选择 **Server / Validate** 或 **Mihon / Validate**，手动运行所选 ref 的全量检查。日常 PR 与 `main` 提交自动按改动选择相关检查；手动运行不会沿用路径筛选结果。

**需要主程序测试二进制**：选择 **Server / Manual test binaries**，指定待验证的分支或 tag。通过当次完整检查后，三平台二进制出现在该次运行的 Artifacts 中，名称带 `manual-`；不会创建或更新 GitHub Release。静态前端由 Linux 构建，Windows/macOS 不再验证前端工具链兼容性，Go 仍在三个系统原生测试和 CGO 构建。

**正式发布主程序**：推送版本 tag 会自动触发 **Server / Release**。补发时在默认分支手动运行此入口，并在 `tag` 输入框填写已有的版本 tag，而不是把普通分支作为发布目标。检查和打包均固定到解析出的 tag 提交；不会拿历史 CI 的成功记录代替当次全量验证。失败后修复问题再重跑，不通过手动测试构建绕过门禁。`Server / Package` 是内部复用流程，不是单独的操作入口。

**验证或分发 Mihon 扩展**：调试 APK 只用于验证，`NOT-FOR-PUBLICATION` 产物不能上传扩展更新仓库。正式签名只从默认分支手动运行 **Mihon / Publish signed repository**，操作前提、秘密配置和版本要求见 [Mihon 维护说明](../extensions/mihon/README.md#维护者签名与自动发布)。它与主程序的 tag 发布互不触发。

## 阅读结果与定位失败

主程序结果按前端、Go 静态分析、三平台原生验证、race、安全、生成代码、文档和工作流分类。Mihon 结果区分发布工具测试、协议测试、Android lint 和调试 APK 构建；协议与 lint 报告保存在对应 Artifacts。先查看失败类别，再打开该 job 的日志。

未涉及的检查显示为跳过，不表示这些检查已通过。日常验证总会报告汇总门禁；只有范围识别成功且所有选中检查成功，汇总才会通过。缺失比较历史、新分支或未知文件保守运行全量检查；扩展自身改动不运行主程序验证，Mihon 服务端 API、共享图标和工作流等共享输入触发双方。具体路径规则归 `scripts/ci_changes.py`。

## 必需检查迁移

合入后，维护者须在仓库的 branch protection / rulesets 中，将旧矩阵 job 的必需检查替换为：

- `Server / required checks`
- `Mihon / required checks`

请先在真实 PR 中确认 GitHub 展示的检查名称，再移除旧必需项。不要将可能按路径跳过的单独类别设为唯一门禁，也不要保留已不存在的旧名称，否则 PR 会一直等待。仓库设置不随工作流提交自动更新。

## 验证边界

本地回归命令：`python -B -m unittest discover -s scripts -p 'ci_*_test.py' -v`，另运行 `actionlint` 检查工作流语法。测试直接执行工作流中的汇总门禁和 tag 解析脚本，覆盖失败、取消、跳过以及无效发布目标；不能替代首次 GitHub runner 验证、真实 Android 构建或签名发布验收。

工作流不会复用历史运行的前端或二进制产物；前端仅在同次运行内共享。CI 成功及 SHA256 校验不等于第三方许可或来源合规审计。
