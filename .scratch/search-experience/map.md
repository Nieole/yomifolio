# 统一搜索体验交付索引

## Notes

- 集成分支：`search-experience-integration`。
- [正式规格](spec.md)与[实施设计](design.md)定义交付范围；当前实现契约归所属模块 SPEC。
- 本地 Markdown 为工程工单系统，不创建 GitHub PR。

## Decisions-so-far

- 01：[可靠搜索会话](issues/01-search-session-reliability.md)已解决；隔离过期响应、明确等待与失败、支持立即重试。
- 02：[系列归一化与旧库升级](issues/02-series-normalized-search.md)已解决；统一宽度/大小写/分隔规则并维护派生索引。
- 03：[书匹配与统一相关度](issues/03-book-matching-and-global-ranking.md)已解决；自身精确、自身子串及系列继承层级统一混排去重。
- 04：[有界游标与继续加载](issues/04-global-search-cursor-loading.md)已解决；绑定查询、读者及权限，支持可靠追加与显式重新搜索。
- 05：[全链路及大库验收](issues/05-integrated-search-acceptance.md)已解决并获独立审查批准；两轮低负载完整矩阵通过2倍预算，失败与未执行项保留于[验收证据](acceptance/README.md)。
- [最终分支双轴审查与结案验证](final-review.md)：Standards 与 Spec 均批准；非阻塞测试发现已补回归，维护等待仅接受本次新任务的终态，不复用历史完成记录。

## Fog

- 尚无真实用户漏搜样本，未宣称复现其原始故障根因。
- 默认 metadata_scan 未完成；完整真实文件维护证据采用 fast_scan。人工有头视觉及浏览器在途身份切换未执行，详见验收限制。
- 性能结论基于记录的低负载窗口，不保证任意外部计算负载下的延迟；污染轮失败不改写、不删除。
