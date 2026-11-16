# 仓库展示、传播与效果复查

## 目标与边界

让寻找自托管漫画书库、网页阅读、OPDS 和 KOReader 集成的用户更容易发现并理解 Yomifolio。准确描述已实现能力，改善首次浏览体验，不承诺搜索排名、Stars 或访问量增长。

仓库展示与软件发布分开处理：合并 README 不等于发布新二进制。截图描述当前源码，发布包能力以对应 release notes 为准。公开传播、上传社交预览图、创建新 tag 和发布版本都需要维护者实际执行，本清单不会自动执行或创建提醒。

## 上线前清单

- [ ] 将经过审查的提交推送或合并到 GitHub 默认分支；在 GitHub 实际打开英文首页、中文入口、四张截图及文档链接。
- [ ] 核对 About 简介和 topics 与当前功能一致。保留少量真实相关词，不添加未支持的协议或借竞品名称堆词。
- [ ] 用未登录窗口检查仓库名称、简介和截图是否容易理解；搜索品牌 `yomifolio`，确认指向同一仓库，而非仅依赖本地渲染。
- [ ] 检查 latest release 实际存在的资产与版本边界。旧包名称、旧品牌和源码截图可能不同，不手写猜测的下载深链。
- [ ] 核对 LICENSE、贡献说明、公开问题模板和安全联系指引可达；私密漏洞报告只在 GitHub 实际提供时使用。
- [ ] 若准备新二进制，先完成 [第三方通知的发布限制](../THIRD_PARTY_NOTICES.md) 和 `.scratch/release-notices/` 工单，再核对三平台构建、通知交付、对应源码及下载校验。完整性检查不是合规审计。

## 小范围传播

1. 先在维护者自己控制的渠道分享仓库链接、藏书浏览实拍和一段明确定位，收集首次启动、客户端配置和文档可读性反馈。
2. 再选择允许项目自荐的自托管或漫画阅读社区。先读规则，披露维护者身份，解释用户问题与适用边界，不重复刷帖、买 Stars 或声称全面替代其他产品。
3. 只有在真实使用并验证集成后，才向相关 OPDS/KOReader/阅读客户端资料或资源目录提交收录建议；遵守对方贡献要求，不把仅有服务端 API 说成任意客户端即插即用。
4. 社交预览图是可选后续项：使用有权分享的演示素材，另外确认图文设计并实际上传。不要把仓库截图或 README 已提交等同于 GitHub 社交预览已配置。

可按受众改写的简短介绍：

> Yomifolio is a self-hosted manga and comic library server: browse your local collection, read in the browser, and connect through OPDS or KOReader sync. Current screenshots show the source version; check release notes for binary availability. https://github.com/Nieole/yomifolio

> Yomifolio 是面向个人藏书的自托管漫画管理与阅读服务器，支持藏书浏览、网页阅读、OPDS 和 KOReader 同步。截图展示当前源码；下载版本能力以发布说明为准。https://github.com/Nieole/yomifolio

## 观测基线与复查节奏

以默认分支展示实际上线的时间记为 D0，不以本地提交时间代替。若已在更名或简介更新后采集一次数据，标记为“部分变更后的观测”，不能当作所有改善前的对照组。

维护者在私有位置保存带 UTC 时间戳的快照，不提交访问凭证、完整流量来源或其他私有分析数据。可记录：

- 公共状态：仓库名称、简介、topics、Stars、Forks、当次默认分支提交及最新发布版本。
- Insights → Traffic：最近 14 天的 views、unique visitors、full clones 和 unique cloners；如权限不足，记为“不可取得”，不是零。
- 固定查询的可见性：`yomifolio in:name`、`topic:manga topic:self-hosted`、`manga comic server in:description`、`opds koreader in:description,readme`。保存查询、筛选/排序、时间、是否登录和结果截图，不把一次出现位置称为稳定排名。
- 反馈质量：真实启动/配置反馈、可复现问题、文档疑问和实际贡献；无数据时写未知，不推测活跃用户数。

建议 D+7 检查首次反馈，D+14 保存首个完整窗口，D+28 比较两个不重叠的 14 天窗口。GitHub 的流量视图只保留近期窗口，至少每周留存一次；重叠窗口不能相加，抓取当天可能尚未完整。未创建定时任务或复查提醒。

解释数据时注意：

- views/clones 可包含维护者自身、自动化和爬虫行为；不能直接换算安装量或真实读者数。
- Stars/Forks 是关注信号，不等于活跃使用或留存；小样本不宜报告百分比“显著提升”。
- 更名、README、topics 和外部传播若同时变化，无法单独归因；记录传播时间及当次改动，再逐次调整定位或文档。
- 搜索索引可能延迟，结果也受查询、筛选和排序影响。没有可靠对照就只报告观察，不宣称因果。

### 每次复查记录

- UTC 采集时间、当次默认分支提交、实际已上线的改动及外部分享时间。
- 统计窗口起止与是否覆盖完整日期；views/unique visitors、full clones/unique cloners 分开记录。
- 固定查询的完整条件、可见性截图及相较上次的观察；没有数据时标记未知。
- 真实用户反馈、可能的自身/自动化流量干扰、下一项需要验证的假设。
- 下一次复查时间及负责人员；需要提醒时另外创建，不假定本清单具有调度能力。

## 复查结果如何推进

- 搜得到但用途不清楚：检查简介、首屏定位和截图，不先加更多主题词。
- 有浏览但安装反馈集中失败：优先修复启动/安全/客户端指引；产品行为问题按本地工单与设计流程推进。
- 真实使用者有共同诉求：整理场景和验收条件，另立功能问题，不在传播文案中提前承诺。
- 样本仍小：继续留存同口径快照和定性反馈，不为追求漂亮数字改变统计口径。

## GitHub 参考

- [查看仓库流量：权限、近期窗口及统计口径](https://docs.github.com/en/repositories/viewing-activity-and-data-for-your-repository/viewing-traffic-to-a-repository)
- [使用 topics 分类仓库](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/classifying-your-repository-with-topics)
- [配置社交媒体预览](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/customizing-your-repositorys-social-media-preview)
