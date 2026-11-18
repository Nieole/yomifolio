# Yomifolio 入门与运行维护

[中文首页](../README.zh-CN.md) · [English guide](getting-started.md) · [功能与客户端](features.zh-CN.md)

## 下载与首次运行

优先使用[预编译版本](https://github.com/Nieole/yomifolio/releases/latest)。选择 Linux AMD64、Windows AMD64 或 macOS ARM64 的实际二进制附件，不是 GitHub 自动生成的源码压缩包。以发布页实际文件名为准，旧 release 可能仍叫 `manga-manager`；当前源码构建及发布工作流产物叫 `yomifolio`。本文与截图描述当前源码，下载二进制的功能以其 release notes 为准。不拼接未发布资产的下载链接；若该版提供 SHA256 校验文件，可据此核对下载完整性。

1. 建立服务账号可写的工作目录，放入二进制。账号需能读取藏书；ComicInfo 写回、移入回收目录还需写权限。
2. Linux/macOS：`chmod +x ./实际下载文件名` 后执行 `./实际下载文件名`。PowerShell：`.\实际下载文件名`，文件名包含 `.exe`，不再追加扩展名。
3. 在服务器本机打开 `http://localhost:8080`，在对外开放前创建管理员。无图形界面的主机可通过可信局域网或 SSH 隧道访问；另一台电脑的 `localhost` 不是服务器。
4. 在设置中添加藏书绝对目录并扫描。支持 ZIP/CBZ、RAR/CBR，不支持 EPUB/PDF。请只使用有权存储和阅读的文件。界面可在英文与简体中文之间切换，也会参考支持的浏览器语言。

二进制内嵌网页，基础阅读不需要 Node、Go、云数据库或 LLM。在线刮削、远程模型与远端封面需要网络。

## 路径、配置与安全

首次缺少配置时，在当前工作目录生成 `config.yaml`。`-data-dir` 只改变日志目录；数据库和缓存分别由 `database.path`、`cache.dir` 决定，默认 `./data/manga.db`、`./data/cache`。完整配置入口参见[配置模板](../config.example.yaml)、设置页和程序 `-help`，此处仅列容易误解的边界。

- **路径与优先级：** `-config` / `-data-dir` > 非空且去除首尾空白的 `YOMIFOLIO_CONFIG` / `YOMIFOLIO_DATA_DIR` > 对应旧 `MANGA_MANAGER_CONFIG` / `MANGA_MANAGER_DATA_DIR` > 默认 `config.yaml` / `data`。数据库/缓存仍由 YAML 决定；服务管理器应明确工作目录，建议使用绝对路径。`实际文件名 -version` 可查看版本。
- **初始化与 HTTPS：** 可先复制 [config.example.yaml](../config.example.yaml) 为 `config.yaml`，把 `server.host` 改为 `127.0.0.1` 进行本机初始化；模板并不穷举所有字段。公网部署用 Nginx/Caddy 等终结 TLS，并阻止直接访问后端。保留同源 API、下载、长连接 SSE `/api/events`，避免代理缓冲/过短超时截断请求。
- **可信代理与 Cookie：** `server.trusted_proxies` 只填真实代理 CIDR，关系到真实客户端 IP 和限流，不能为方便而信任所有网络。`cookie_secure: auto` 仅信任直连 TLS 或可信代理的 `X-Forwarded-Proto: https`。反代终结 TLS 时，登记代理或明确设 `cookie_secure: always`；后者不能修好客户端 IP。`never` 仅是刻意使用 HTTP 时的逃生口，不是公网建议，Secure Cookie 在普通 HTTP 下不可用。
- **认证与 CSRF：** Web 使用 HttpOnly / SameSite=Lax 会话 Cookie、bcrypt 口令和会话绑定的 CSRF 令牌；代理应保留 `X-CSRF-Token`，不要为解决反代问题关闭校验，旧共享 API token 不是鉴权捷径。跨域按需配置精确来源，通配来源会关闭带凭据 CORS；优先同源部署。登录和协议 Basic 失败按客户端 IP 限流退避，默认也有安全响应头，但不代表已完成全面安全审计。
- **配置权限：** YAML 明文存储模型/刮削器密钥。新建/设置页保存按 `0600` 写入；旧文件或手动复制文件权限可能较宽，多用户 Unix 主机可执行 `chmod 600 config.yaml`，Windows 用 ACL 限制。数据库按 umask 创建，含口令摘要、会话/CSRF，需单独保护文件、目录及备份。
- **热重载：** 文件事件 500ms 去抖；配置相同跳过，校验不通过保留当前运行配置，并记日志，此时磁盘与运行态可能不同，应修正后重存。字段生效时机以设置页或 `/api/system/config` 的 `field_effects` 为准，不是所有配置都实时生效。改数据库路径不等于搬迁数据库。
- **备份与运维：** 升级前停服备份数据库、配置和原始藏书，避免直接复制不一致的运行中 SQLite；缩略图/离线缓存不是藏书备份。管理员可查看日志与任务历史，任务按能力提供暂停/继续/取消/重试，不能认为每种任务都支持全部动作；重启恢复也区分可续跑工作与需人工决策的破坏性/付费工作。日志轮转为 10MB、5 个备份、28 天保留、gzip 压缩；诊断支持空页/缺封面/缺元数据/重复或缺失身份/KOReader 未匹配等检查和对应修复。`GET /api/health` 检查服务/数据库，数据库不可达返回 `503`，不代表外部依赖和所有藏书盘都正常。

未初始化时受保护 Web API 返回 `401`，不是 API 直通；公开认证/setup 入口及健康检查仍可访问。默认监听 `0.0.0.0:8080`，任何能到达 setup 的人都有机会抢先创建管理员。初始化阶段用防火墙或回环绑定隔离，公网部署前阻止直接访问后端。这里不表示内置 TLS 或提供官方容器部署。

管理员创建的新账号须先在网页改密，之后才能使用协议：此前 OPDS/Mihon 返回 `401`，绑定的 KOReader 账户返回 `403`。管理员不能删除自己，也不能删除或降级最后一位管理员。三种协议与 KOReader 自助注册均默认关闭；在不可信网络传输 Basic 凭据或同步密钥必须使用 TLS，见[客户端边界](features.zh-CN.md#客户端与进度)。

离线阅读需 Service Worker / Cache Storage，通常要求 HTTPS 或 localhost；手机访问局域网 HTTP 不是 localhost。浏览器配额、回收、清站点数据、登出和切换账号会影响本机离线藏书，不能当永久备份。离线进度可在同一账号重新联网后回传。

## 从 Manga Manager 升级

升级前停服并备份数据库、配置和原始藏书，再替换可执行文件。

保留原工作目录与配置，停掉旧进程再替换二进制，无需因更名重新导入或移动数据库。默认仍为 `data/manga.db`，OPDS 的 `urn:manga-manager:*` 身份保留。旧环境变量继续作为回退。验证通过的 `mm_session` 安全换发为 `yomifolio_session`，不改变身份/CSRF；新 Cookie 存在时即使无效也不回退旧值，登出撤销并清除两种 Cookie。

普通偏好新键优先，缺失时读取复制旧键，失败保留旧数据重试；离线索引、页图、待同步进度与 owner **保留底层稳定名称，不复制、不迁移、不双写**，原隔离与登出规则不变。旧日志保留，新日志写 `yomifolio.log`。
