# Reporting a security issue

Do not post vulnerability details, exploit steps, credentials, session cookies, databases, private library paths, or unredacted logs in a public issue or pull request.

If the repository's **Security → Advisories → Report a vulnerability** action is available, use GitHub's private vulnerability reporting channel. This document does not imply that the feature is enabled or that a response time is guaranteed.

If no private reporting action or maintainer contact is available, open an issue containing only a request for a private security contact. Do not include the vulnerability details until a private channel has been agreed.

In a private report, include the affected version, deployment context, a minimal reproduction using non-sensitive sample content, the potential impact, and any suggested mitigation. Do not share a live exploit against a third party's server.

For deployment precautions, read the [getting-started guide](docs/getting-started.md). Complete first-run account setup on a trusted network and protect access with HTTPS before exposing the server. A default configuration is not a public-hosting security guarantee.

## 中文

请勿在公开 Issue 或 PR 中发布漏洞细节、利用步骤、密钥、Cookie、数据库或未脱敏日志。

如果仓库 Security 页面提供 **Report a vulnerability**，请使用 GitHub 私密漏洞报告。若没有可用的私密入口或维护者联系方式，仅公开请求建立私密联系，不要附带漏洞细节；确认私密渠道后再提供复现与影响说明。本文不承诺功能已启用或固定响应时限。

首次账号设置应在可信网络完成；对外开放前请阅读[部署说明](docs/getting-started.md)，配置 HTTPS 和访问边界。
