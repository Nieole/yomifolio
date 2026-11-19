# Manga Manager Mihon 源扩展

面向官方 Mihon 的五槽位扩展，支持浏览、搜索、最近添加、系列详情、封面、卷册阅读和普通图片下载，以及资料库、合集、智能合集和阅读列表导航。兼容依据为官方 v0.21.3 的源 API 1.4；构建成功不代表已经完成真机验收。

**本项目只交付独立源插件和自有扩展更新仓库，不修改 Mihon 客户端或阅读器库，也不提供跨端页级双向同步。** 官方源 API 不提供真实翻页事件或远端页码导入；图片下载、预加载和资源请求均不写阅读进度。服务端已有进度接口不代表普通插件能同步。

## 连接自己的书库

1. 在服务端创建站点账户，先通过网页完成强制改密，并在设置中启用默认关闭的 Mihon 协议。扩展不提供账户创建、改密或无账户首启。使用 HTTPS，Basic 认证本身不加密密码。
2. 通过下文的签名仓库安装扩展，并在 Mihon 明确确认信任。本地调试 APK 只用于测试；Android 无法以不同签名直接升级安装，切换正式版本前应备份书库并移除调试扩展。
3. 分别配置 `Manga Manager (1)` 至 `(5)` 的地址、用户名和密码。地址填写服务器根地址，例如 `https://example.org`；子路径部署填写 `https://example.org/manga`，代理需移除该部署前缀再转发。**不要追加 `/api/mihon/v1`**，地址不能包含账号、查询或片段。
4. 直接填写最终地址：扩展拒绝重定向，包括 HTTP 升级 HTTPS 与登录跳转。打开浏览或最近添加，再重置筛选以加载资料库、组织目录、标签和作者选项。

五个源 ID 永久固定，每个槽位有独立偏好和认证客户端；同服务器不同账户也使用不同槽位。内容 URL 绑定规范化地址和精确用户名的哈希，不含密码。更改地址或用户名会拒绝旧条目和排队请求：恢复设置，或移除旧条目后重新添加；密码轮换不改变身份。用户名大小写变化也会改变绑定。

同地址、同账号但更换服务端数据库无法自动识别，切换前需移除旧条目。更改配置不会清除 Mihon 已下载的文件或缓存；换账户时也应清理本地缓存内容，扩展无法撤回这些离线副本。

密码使用官方 Mihon 已支持的 `__PRIVATE_` 偏好前缀：普通备份不包含密码，只有明确选择包含敏感偏好时才允许导出。恢复普通备份后需重新输入密码。早期调试配置在源加载时迁移旧密码键，不改变源 ID 或条目身份；迁移写盘失败会使源加载失败，不会假报成功。该行为已核验官方稳定版源码并经迁移单测验证，真机备份恢复仍待验收。

## 浏览与阅读边界

- 最近添加不代表热门。组织目录保留服务端成员和顺序，不修改合集或阅读列表。
- 组织目录不能与关键词、资料库、标签、作者、状态或排序组合；不支持的组合明确报错，不在客户端截断分页后假筛选。
- 多标签／作者为类内 OR、类间 AND，用逗号分隔；包含逗号的名字不能由当前服务端查询格式可靠表达。
- 作者、画师和标签取自本系列详情，不从全局字典推断；story 映射作者，art 映射画师，标签映射 genre。
- 卷册按 Mihon 源顺序返回所需的倒序列表；用户选择章节号排序可覆盖源顺序。服务端一基页码转换为阅读器零基索引。

扩展使用独立 OkHttp 客户端，不向 Mihon 共享客户端安装拦截器。仅配置的同源 API 前缀接收 Basic 凭证；资源路径重新挂载部署前缀，拒绝路径逃逸、带账号 URL 和重定向。网络错误不包含响应体、请求查询或密码。

这也意味着不继承 Mihon 的共享 cookie／缓存、自定义网络拦截器和主机专用传输设置；仍使用 Android 平台信任，不关闭 TLS 校验。系列、卷册 URL 为绑定的绝对 API 地址：分享或在 WebView 打开时显示 JSON API，而非公共网页阅读器。WebView 不接收该客户端的 Basic 拦截器，可能需要另行网页登录，不是扩展认证入口。

## 构建和测试

使用 JDK 21，安装 `build.gradle.kts` 指定的 Android SDK 平台和 build-tools，并设置绝对路径 `ANDROID_HOME`。在本目录执行：

```sh
./gradlew --no-daemon :protocol-tests:test assembleDebug lint
```

Windows Git Bash 使用 `gradlew.bat` 和 Windows 格式 SDK 路径。调试 APK 位于 `build/outputs/apk/debug/`；JVM 测试报告位于 `protocol-tests/build/`。宿主依赖为 compile-only，禁用 Kotlin 自动打包 stdlib；AndroidX 完整偏好 API 在兼容 stub 前，APK 不应打包宿主类。

Python 工具只使用标准库，从仓库根目录执行：

```sh
python -m unittest discover -s extensions/mihon/tools -v
python extensions/mihon/tools/repository.py --inspect-only --apk <local.apk> --tools <sdk>/build-tools/<version>
```

`--inspect-only` 只读，也能检查调试 APK；它不生成发布索引，不代表该 APK 可公开分发。省略此选项会拒绝调试 APK。`publish.py` 是持有 token 的实际发布入口，**不是本地 dry-run 工具**。

## 公开安装与更新

维护者首次成功发布后，在官方 Mihon 的设置、浏览、扩展仓库中添加：

```text
https://raw.githubusercontent.com/Nieole/yomifolio/mihon-repo/index.min.json
```

首次发布前该地址尚不可用；菜单名称随客户端版本可能不同。这是 GitHub raw 地址，不是分支 HTML 页面。在扩展列表安装 Manga Manager，核对仓库所有者和签名指纹后确认信任。仓库信任和 APK 信任是不同的确认。

更新必须保留包名、工厂、五个源 ID 及签名证书。索引的 nullable `sources` 为 null，由 APK 工厂提供实际源，安装前列表可能显示一个占位源。`repo.json` 提供证书 SHA-256 指纹；`releases.json` 保存精确 APK 哈希、证书及版本历史。APK 路径为 `apk/<package>-v<code>.apk`，图标从本项目 `web/public/icons/icon-192.png` 原样复制，不引入新设计。

## 维护者：签名与自动发布

1. 审查并合入扩展、工具、共享 CI 路径选择器与 `mihon-*` 工作流到默认分支。新的 `workflow_dispatch` 只有进入默认分支后才可运行；发布拒绝非默认分支和 fork，不接受 PR 触发。扩展发布不触发服务端版本标签或发布通道；入口选择和必需检查配置见 [Actions 操作指南](../../.github/README.md)。
2. 私下准备长期 Android 发布 keystore，并验证离线备份；不要使用 Android debug key。通过私密渠道配置 Actions Secrets：`MIHON_KEYSTORE_BASE64`、`MIHON_KEYSTORE_PASSWORD`、`MIHON_KEY_ALIAS`、`MIHON_KEY_PASSWORD`。Base64 应为 keystore 的单行编码，秘密不得进入源码、命令行、日志或产物。本工具不会替维护者生成或保管私钥，也不支持证书轮换；丢失原私钥将无法正常升级。
3. 允许 Actions 创建及非强推更新专用 `mihon-repo` 分支。只有发布 job 有 `contents: write`，token 只传入最终发布工具；构建 job 仅读，checkout 不保留凭证。PR 不获取签名秘密。可以另加环境保护要求发布审批。
4. 手动运行 **Mihon / Publish signed repository**。流程先复用同一提交的当次全量无秘密验证，独立展示发布工具测试、协议测试、Android lint 和调试构建；全部成功后，再将 keystore 解码到权限 0600 的 runner 临时目录，通过环境变量签名，失败也清理。公共交接产物只含签名 APK、图标及发布工具，不含仓库检出或私钥。缺少秘密时 release 打包失败；只验证扩展可手动运行 **Mihon / Validate**，不需要签名秘密。
5. 发布前重新验证全部历史 APK 的哈希与证书，并核对仓库指纹。相同 versionCode 的不同字节即使仅为非确定构建也拒绝覆盖，必须递增版本；低版本拒绝，同字节重试安全。已有索引但缺少可核验签名历史时拒绝自动迁移。保留历史 APK 与无关分支文件，通过非强推原子更新分支；竞争更新失败，不覆盖他人提交。

APK 版本名使用 `1.4.<revision>` 并递增 versionCode；编译用 extensions-lib 的依赖版本不是 APK 版本。官方旧索引读取器将最后一段之前解析为数值 API 版本，不支持四段 APK 版本名。工具读取实际 APK 元数据而非从文档复制版本。具体插件、SDK、版本和哈希配置以 Gradle 与工作流为准。

## 验收限制

协议、身份、代理路径及认证单测不能替代官方客户端加载、信任和升级、真实子路径反代、同主机多账号及离线下载验收。上述真机／部署验收、真实签名配置与首次 Actions 发布仍待完成。页级双向同步不在独立插件交付范围；不得因 APK、索引或服务端测试通过就宣称具备该能力。
