# Contributing to Yomifolio

Thanks for helping make self-hosted comic libraries easier to browse and read.

## Report a problem or propose an improvement

Use [GitHub Issues](https://github.com/Nieole/yomifolio/issues/new/choose) for public feedback. Search existing reports first. Include the version, operating system, how you installed it, and a minimal set of steps to reproduce a problem. For a feature request, describe the reader's problem and the outcome you need, rather than only an implementation.

Use a small, redistributable sample if a particular archive triggers a bug. Do not upload copyrighted comics without permission, your library database, credentials, API keys, session cookies, or unredacted configuration and logs. Replace private paths and book titles where necessary.

Security reports need a private channel: see [SECURITY.md](SECURITY.md).

## Send a change

1. Discuss substantial behavior or architecture changes before implementation.
2. Follow the [development guide](docs/development.md) for setup and verification, and [AGENTS.md](AGENTS.md) for repository conventions. Work on a focused branch.
3. Add regression coverage for behavior changes. Update the owning specification when a documented contract changes, and update [CHANGELOG.md](CHANGELOG.md) for user-visible changes.
4. Use a scoped Conventional Commit subject such as `fix: preserve reading progress on reconnect`. Keep unrelated changes separate.
5. Open a pull request explaining the problem, the chosen approach, checks actually run, and remaining limitations. Include before/after screenshots for UI changes, using redistributable demo content.

Public GitHub reports are feedback intake. Engineering tickets and specifications remain in the repository's local tracker under `.scratch/`, as described in [the issue-tracker convention](docs/agents/issue-tracker.md). A report is not a promise of a particular implementation or delivery date.

## Licensing

Contribute only code and assets you have permission to share. Contributions intended for inclusion in this project are provided under its [Apache-2.0 license](LICENSE), unless explicitly identified otherwise. Preserve third-party license and attribution notices; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). The project license does not grant rights to other people's comics or book artwork.

For dependency or notice changes, review the actual upstream legal texts and embedded components, then update the inventory and input fingerprints together. With Python 3.9 or newer, run:

```sh
python scripts/check-license-notices.py
python -B -m unittest discover -s scripts -p '*_test.py'
```

These are offline integrity/regression checks, not a legal audit or release clearance. Outstanding distribution work is listed in the third-party notices.

---

欢迎中文反馈和贡献。请通过 GitHub Issues 描述问题、版本、系统、安装方式与最小复现步骤；功能建议应说明使用场景和期望结果。不要上传无权分发的漫画、个人数据库、密钥、Cookie 或未脱敏日志。安全问题请先阅读 [安全反馈说明](SECURITY.md)。

提交修改前请阅读[开发指南](docs/development.zh-CN.md)和 [AGENTS.md](AGENTS.md)，保持改动聚焦、补充回归测试，并如实列出验证结果。公开反馈使用 GitHub Issues，工程工单仍按 `.scratch/` 本地约定维护，不意味着承诺排期。仅提交有权共享的内容，并保留第三方许可与署名。依赖或通知变更需复核实际法律文本和内嵌组件，同步更新清单及输入指纹，再以 Python 3.9+ 执行上面的完整性检查和回归测试；检查通过不代表完成合规审计或获准发布。
