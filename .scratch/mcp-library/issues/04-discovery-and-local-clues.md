# 04: 筛选待整理系列并读取本地识别线索

**What to build:** agent 可组合条件找到要整理的系列，或明确指定系列，分页读取书目及名称、文件名、ComicInfo 等本地证据，而不必先扫描或下载整个资料库。

**Blocked by:** 02 通过受限凭据连接 HTTP MCP 并读取系列。

**Status:** ready-for-agent

- [x] 支持按资料库、加入时间、缺失元数据字段组合筛选，以及明确指定系列；定义并记录这些条件的查询语义、分页和响应上限。
- [x] 分页获取系列及所属书目，返回目录名、文件名和可取得的 ComicInfo 识别线索；缺失或损坏的线索如实报告，不伪造数据。
- [x] 限制列表和按标识读取均受当前用户与凭据资料库范围约束，不能通过书或系列标识穿透限制，不开放任意磁盘路径。
- [x] 工具返回足以关联下一步候选检索的稳定对象标识；近期新增筛选不冒充完整增量水位或维护状态。
- [x] 从通用客户端验证组合条件、分页、多资料库隔离、明确目标及异常线索，使用真实临时归档，不访问外部来源。
- [ ] 不改变既有网页筛选、扫描或读取语义；后端门禁及受影响生成契约检查通过。

## Comments

- Worker04: implementing uncommitted on mcp-discovery-clues at 1c96d2fe. Test seam: production bootstrap + generic MCP client, real temporary archives; no external sources. Status remains ready-for-agent until parent integration.

- Implemented discovery/clue acceptance and checked the first five items. Final all-backend gate remains unchecked: repeated whole-suite runs encountered the concrete failures recorded in acceptance/04-discovery-and-local-clues.md; focused MCP, lint, format and generated-contract checks pass. No baseline claim.

- Merger04 independent integration review (HEAD `d6945b04`, incoming `0ab7dbb8`): all conflict markers reconciled without staging/ending the parent's merge; discovery/content and run capabilities share the single bootstrap runtime. Complete nine-tool discovery and annotations are asserted by name; regular-account scan denial and all credential/password isolation assertions remain. Full Go1.26.10 backend, repeated MCP tests, lint, format and generated-contract drift checks pass; see acceptance integration addendum.
- **Open defect, not merge-induced:** a real CBZ with `ComicInfo.xml` = `<ComicInfo><Title>Valid</Title></ComicInfo><broken` returns `available` / `Valid`, not `corrupt`. `parser.ParseComicInfo` decodes only the first root and does not validate the trailing XML; `bookcontent.readComicInfo` trusts that result. The clues truthfulness checkbox is reopened. No parser/web/scanner behavior was changed to fix this outside merger scope. Status stays ready-for-agent pending parent disposition/fix, not done.

- Worker04 targeted truthfulness fix (uncommitted, HEAD `4839e85c`, contains requested integration `a0245ff5`): reopened clue item repaired by `parser.ParseComicInfo` whole-document token validation through EOF; original CBZ now returns `corrupt` without partial fields. Public parser and real-CBZ HTTP/TLS regressions first failed then passed; all parser/bookcontent/bootstrap tests passed three times, including integrated real stdio joint. Full backend gate, lint and drift results are in the acceptance fix addendum. Status remains ready-for-agent pending parent commit/review. Integration branch observed advanced to `acaecadd`; no merge/stage/commit was attempted.

- Worker04 request_review `rc_6c56374b` repair at clean `f6d2b19e`: all five run-tool errors use canonical typed public mapping; production SQLite failure regression went red then green. Repeated MCP/run/stdio and lint/format/drift pass. Full backend gate reopened after four failed attempts (relocation/archive/watcher); no baseline attribution. See acceptance review addendum. Status remains ready-for-agent pending review; 03/06 untouched; no stage/commit/merge.
