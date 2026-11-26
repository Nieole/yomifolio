---
id: mm-config
type: module-design
status: active
title: 配置快照、更新与持久化
parent: mm-architecture
---

## 职责

定义 YAML 配置值与默认值，负责加载、归一化、两档校验、密钥掩码、路径存储策略、原子文件写入及热重载；`Manager` 持有共享运行时配置快照与串行更新边界。

## 公开契约

- `LoadConfig` 可在文件缺失时生成默认配置；`LoadConfigFile` 用于读取既有文件。`ValidateConfigValues` 只检查值，`ValidateConfig` 另检查文件系统目标。配置持久化使用 `AtomicWriteFile` 与 `ConfigFilePerm`。
- `Snapshot`、`Replace` 和构造时通过 `CloneConfig` 隔离当前引用字段；修改快照不会更新 Manager。`Version` 是不暴露密钥的预期状态令牌，作用域限于该 Manager 生命周期。
- `Save`、`Reload` 串行处理校验、持久化与应用回调；检测预期状态或已观察文件冲突，应用失败尝试补偿并返回错误，不保证补偿必成功。回调可读取快照，不得重入更新方法；`Replace` 只替换内存，不代替保存或应用资源。
- `Coordinate` 提供同一串行边界下的读取、校验、写入与发布步骤，外部持久参与者自己决定补偿，`Publish` 不回滚磁盘。`StartWatcher` 去抖后走重载，文件消失时不自动重建。
- 发布共享快照不等于全部消费者已生效。`immediate` 表示即时读取或应用资源效果，`next_operation` 表示后续操作采用新值而不重写在途操作，`restart` 表示需重启才能改变启动绑定资源；不能把保存成功解释为连接或监听地址已切换。字段级分类及受管理、兼容字段以 `FieldEffects` 为准，不在本规格复制字段清单。
- `ResolveStoragePolicy` 按最具体路径策略解析并返回卷键，不改入参；这里只产出策略，不发放 IO 许可。

## 启动路径兼容

`cmd/server` 解析配置文件与日志目录路径：显式 `-config` / `-data-dir` > `YOMIFOLIO_CONFIG` / `YOMIFOLIO_DATA_DIR` > 对应旧 `MANGA_MANAGER_CONFIG` / `MANGA_MANAGER_DATA_DIR` > 默认 `config.yaml` / `data`。两组环境变量均先去除首尾空白，仅非空值参与选择；显式 CLI 参数不经过环境变量回退。数据库与缓存位置仍由 `database.path` / `cache.dir` 决定，默认数据库路径 `./data/manga.db` 保持兼容，不因更名新建或搬迁数据库。

## 边界与限制

不导入兄弟包、不拥有 HTTP 设置端点或资料库数据库行。配置派生的进程资源效果经注入回调协调，本包不自行重建解析池、图片并发闸或日志资源；不得把快照副本当成第二份可写运行时配置。

## 证据

`Manager`、`UpdateSession`、`CloneConfig`、`ResolveStoragePolicy`；`config_extra_test.go` 的 `TestSnapshotIsDeepCopy`、`TestResolveStoragePolicyDoesNotMutateInput`，`runtime_test.go` 的 `TestResolveStoragePolicyUsesMostSpecificPathOverride`，以及 `watcher_test.go`。

## MCP 安全配置

`MCPConfig.Origin` 统一验证可选 mcp.public_url：空值禁用 MCP 协议入口，非空值为无用户信息、查询或业务路径的 HTTPS origin；HTTP 必须显式 allow_lan_http 且主机为 localhost 或字面 private/loopback IP，不以 DNS 解析结果放宽明文范围。两个字段发布后对新请求立即生效；请求级 TLS、host、origin 与可信代理检查归 `mm-api`，显式允许局域网明文不等于允许公网明文。

## 识别配置

`identification` 段的来源间隔（`source_intervals_ms`）、来源优先级（`source_priority`）与标签策略（`tags`，`TagPolicy`）全部即时生效，由读取方在每次预取、重搜或识别补充时取当前快照。数值取 0 为默认值、负数关掉对应限制；`blocklist` 不写取 `DefaultTagPolicy` 的列表，写成 `[]` 表示不屏蔽。`ValidateConfigValues` 拒绝优先级里的未知来源键与编译不过的 `/…/` 正则条目。`config.example.yaml` 的默认值与代码默认值一致由本包测试守卫，`CloneConfig` 深拷贝优先级与屏蔽列表。策略的语义归 `mm-application`「按识别补充」与 `mm-identification`。
