# 真机参考验收：弱模型走完识别主循环

规格「参考性真机验收（不作硬性门禁）」的执行材料，对应票 20 的第四项。目的只有一个：看工具契约对本机弱模型是否**自解释**——不读仓库、不绕道 HTTP，只靠 initialize 说明、工具描述和结果文本把系列整理完。结果是参考，不阻塞合入。

这一步要所有者本人在自己的机器上跑（本机模型与 pi 只在那里），agent 做不了。

## 判定条件

三条同时满足算通过：

1. **15 分钟内**，从发出第一条提示算起，测试资料库的 4 个系列全部进入 `identified`（已识别）、`not_found`（查无此作）、`awaiting_owner`（待人工）之一。
2. **全程不调用 bash 或 curl**：会话记录里没有 shell 类工具调用，也没有 curl、wget 或任何手写 HTTP 请求。
3. **不读仓库**：会话记录里没有对 yomifolio 仓库路径的读文件、列目录或搜索。

识别得对不对、调用了多少次工具，记在结果里作参考，不进判定。

## 一、前置准备

### 1. 构建被测版本

用合入 `mcp-identification` 之后的提交（记下 commit，填进结果表）：

```bash
cd <仓库>/web && npm run build
cd <仓库> && go build -o ~/yomifolio-accept/bin/yomifolio ./cmd/server
# 只有 pi 走 stdio 接入时才需要：
go build -o ~/yomifolio-accept/bin/mcp-proxy ./cmd/mcp-proxy
```

### 2. 建测试资料库（4 个系列）

沿用首版试用时那个 4 系列的测试资料库最好，结果才能和那次对比。没有的话，从自己的藏书里挑 4 个系列，每个复制 1–2 本到 `~/yomifolio-accept/library/<系列目录>/`，尽量覆盖这几种情形：

| 编号 | 情形 | 例子 | 期望的落点 |
| --- | --- | --- | --- |
| A | 干净的目录名，带 ComicInfo | 一部知名作品的原名目录 | 已识别 |
| B | 目录名噪声重：汉化组、方括号标签、卷号区间 | `[某汉化组] 作品名 第01-10卷 [完结]` | 已识别（服务端清洗搜索词后能搜到） |
| C | 有续作、完全版或同名作品 | 多版本发行的作品 | 已识别，或以 `ambiguous_candidates` / `uncertain_edition` 上报 |
| D | 来源里大概率没有 | 个人整理的合集、冷门同人 | 查无此作，或以 `missing_from_sources` 上报 |

把每个系列的目录名和你认为正确的来源条目先记在结果表里，事后对照。

### 3. 用全新的数据目录起服务

必须是全新的数据库：旧库里的历史提案会在启动时被推导成识别结论，系列就不从未处理开始了。

```bash
mkdir -p ~/yomifolio-accept/{data,agent}
cd ~/yomifolio-accept
cp <仓库>/config.example.yaml config.yaml
```

改 `config.yaml`（相对路径以 `~/yomifolio-accept` 为工作目录）：

```yaml
server:
    port: 18080                      # 避开日常实例
database:
    path: ./data/manga.db
cache:
    dir: ./data/cache
mcp:
    public_url: "http://127.0.0.1:18080"
    allow_lan_http: true             # 本机明文调试要开这个开关
```

MyAnimeList、Comic Vine 的凭据配不配都行，但要记进结果表：它们决定证据包里有几个来源。启动：

```bash
cd ~/yomifolio-accept && ./bin/yomifolio -config ./config.yaml -data-dir ./data
```

### 4. 扫描并备好证据包

1. 浏览器打开 `http://127.0.0.1:18080`，建管理员账户。
2. 添加资料库，路径指向 `~/yomifolio-accept/library`，等首次扫描结束。
3. 扫描收尾会自动串联一次**增量预取**。在任务中心等「证据包预取」运行结束，消息应是 `4/4` 个系列就绪；有没就绪的，在侧栏资料库操作里点「预取识别证据包」再跑一次。
4. 抽查一个系列：详情页「识别」页签里能看到证据包和候选。

### 5. 签发凭据

**设置 → MCP 访问 → 签发凭据**：

- 关联用户：刚建的管理员（`metadata_write` 要求管理员账户）。
- 资料库：只勾测试资料库。
- 能力：只勾 `查询`(read) 与 `元数据写入`(metadata_write)。不给扫描控制和清理，主循环用不到它们。

保存好密钥。

### 6. 接入 pi

按首版试用时的方式（经 ThinkRail）把这个 MCP 服务配给 pi，模型用 **qwen3.8-flash-next q2**。两种接法任选，字段名以客户端为准（详见 `docs/mcp.zh-CN.md` 第 3 节）：

- HTTP：地址 `http://127.0.0.1:18080/mcp`，请求头 `Authorization: Bearer <密钥>`。
- stdio：命令 `~/yomifolio-accept/bin/mcp-proxy`，参数 `--url http://127.0.0.1:18080 --allow-lan-http`，环境变量 `YOMIFOLIO_MCP_TOKEN=<密钥>`。

两点要求：

- pi 的工作目录设为空目录 `~/yomifolio-accept/agent`，**不要**在仓库里启动，也不要把仓库路径告诉它。
- **保留** pi 自带的 bash、读文件等工具，不要为了过判定而关掉它们：判定看的是它有这些工具时选不选用。打开完整的会话记录。

### 7. 开跑前自检

在 pi 里只做一件事：让它列出 yomifolio 的工具（或看客户端的工具面板），确认能看到 `library_claim_series`、`library_submit_verdicts`。这一步不计时；做完**新开一个会话**再正式开始，避免自检的上下文影响结果。

## 二、操作步骤

1. 新开 pi 会话，记下开始时间，发出下面这一句，不附任何提示：

   > 用 yomifolio 的 MCP 工具，把资料库里的系列都识别完。

2. 之后不再给提示。agent 停下来请示时，只回一句「继续」，并在结果里记一笔（这本身说明 initialize 说明里「有权限即可直接写入」没有被理解）。
3. agent 自己说完成，或到 15 分钟，就结束会话，记下结束时间，导出会话记录。
4. 在管理员网页上看结果：资料库筛选按识别状态过滤，或逐个打开详情页「识别」页签。不要在 agent 的会话里查，免得混进记录。
5. 重跑：停服务，删掉 `~/yomifolio-accept/data`，从第 3 步「用全新的数据目录起服务」重来，凭据要重新签发。

## 三、结果记录

### 运行环境

| 项 | 值 |
| --- | --- |
| 日期 | |
| yomifolio commit | |
| 模型与量化 | qwen3.8-flash-next q2 |
| pi 版本 / 接入方式（HTTP 或 stdio） | |
| 已启用的来源 | Bangumi、AniList、MangaDex（+ MyAnimeList？+ Comic Vine？） |
| 开始 / 结束时间 | |

### 每个系列的落点

| 编号 | 目录名 | 预期条目 | 最终识别状态 | 识别到的条目 / 上报分类 | 对不对 |
| --- | --- | --- | --- | --- | --- |
| A | | | | | |
| B | | | | | |
| C | | | | | |
| D | | | | | |

### 判定

| 条件 | 结果（通过 / 不通过） | 依据 |
| --- | --- | --- |
| 15 分钟内 4 个系列全部落到已识别、查无此作或待人工 | | |
| 全程没有 bash、curl 或手写 HTTP | | |
| 没有读仓库 | | |

### 参考观察

- 工具调用总数，其中领取、提交、重搜各几次：
- 是否一次提交了整批裁决：
- 是否停下来请示过人：
- 上下文是否被压缩、压缩后是否丢了计划：
- 遇到的拒绝（`problem` 代码）以及它是否按 `message` 自行纠正：
- 其它卡住或绕路的地方：

### 结论

（通过 / 不通过；不通过时写卡在哪一步，以及是工具描述、说明文字还是结果文本没讲清楚。）
