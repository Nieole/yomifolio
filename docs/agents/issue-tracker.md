# Issue tracker: Local Markdown

Engineering tickets and effort planning records for this repo live as markdown files in `.scratch/`. Durable ThinkRail specs and temporary task-specs have separate ownership and lifecycle rules in [doc-style.md](doc-style.md#thinkrail-规格); `.scratch/` is not a parallel source of current product contracts.

GitHub Issues is a public feedback intake surface, not the engineering tracker. Public reports do not change the publish/fetch rules below; skills continue to create and read engineering tickets locally.

## Conventions

- One feature per directory: `.scratch/<feature-slug>/`
- An effort's planning spec or PRD is `.scratch/<feature-slug>/spec.md`; it records that effort, not a replacement for durable ThinkRail specs
- Implementation issues are one file per ticket at `.scratch/<feature-slug>/issues/<NN>-<slug>.md`, numbered from `01` — never a single combined tickets file
- Triage state is recorded as a `Status:` line near the top of each issue file (see `triage-labels.md` for the role strings)
- Comments and conversation history append to the bottom of the file under a `## Comments` heading

## 集成分支在 PR 上跑 CI

一批票合到一条集成分支上时（如 `implement-spec`），第一次合并后把集成分支推到 `origin`，开一个
draft PR（`gh pr create --draft --base main`），之后每次合并都推一次，让 CI 的三平台测试跟着跑。工单
照旧在本地改 `Status:` 结案，PR 只为跑 CI。快进 `main` 之前，这个 PR 上的 CI 必须全绿：Windows 路径、
Windows runner 上的超时这类问题，本地的 `scripts/check.sh --full` 查不出来。

## 两份长期文件

- `.scratch/GAPS.md` — **产品缺口清单**。跟着产品走，不随任何一批票关闭；`/settle` 把
  「是产品缺口而不是实现期岔口」的条目挪到这里。
- `.scratch/<effort>/deferred.md` — **挂账清单**，一个 effort 一份。装的是跑票时冒出来的
  非阻塞岔口，由 `/settle` 一批批清空；清空后只留一行索引与去处。

## When a skill says "publish to the issue tracker"

Create a new file under `.scratch/<feature-slug>/` (creating the directory if needed).

## When a skill says "fetch the relevant ticket"

Read the file at the referenced path. The user will normally pass the path or the issue number directly.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a file with one **child** file per ticket.

- **Map**: `.scratch/<effort>/map.md` — the Notes / Decisions-so-far / Fog body.
- **Child ticket**: `.scratch/<effort>/issues/NN-<slug>.md`, numbered from `01`, with the question in the body. A `Type:` line records the ticket type (`research`/`prototype`/`grilling`/`task`); a `Status:` line records `claimed`/`resolved`.
- **Blocking**: a `Blocked by: NN, NN` line near the top. A ticket is unblocked when every file it lists is `resolved`.
- **Frontier**: scan `.scratch/<effort>/issues/` for files that are open, unblocked, and unclaimed; first by number wins.
- **Claim**: set `Status: claimed` and save before any work.
- **Resolve**: append the answer under an `## Answer` heading, set `Status: resolved`, then append a context pointer (gist + link) to the map's Decisions-so-far in `map.md`.
