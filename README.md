# Easy Proxies

Easy Proxies 是一个基于 [sing-box](https://github.com/SagerNet/sing-box) 的代理节点导入、测速、筛选、节点池与多端口管理工具。项目的核心目标是把各种来源的节点统一导入 WebUI，自动测速，保留失败节点，筛选可用节点，并将节点池中的节点映射为本机可用的 HTTP/SOCKS5 代理端口。

本项目基于 [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies) 二次开发，重点扩展了 WebUI、导入格式兼容、节点生命周期、端口一致性、批量测试、失败节点重试、订阅来源管理和运行状态可视化。

## 目录

- [核心特性](#核心特性)
- [快速开始](#快速开始)
- [WebUI 工作流](#webui-工作流)
- [导入节点](#导入节点)
- [节点生命周期](#节点生命周期)
- [节点池与端口](#节点池与端口)
- [测速与国家检测](#测速与国家检测)
- [订阅与导入来源管理](#订阅与导入来源管理)
- [设置页](#设置页)
- [运行日志](#运行日志)
- [配置文件](#配置文件)
- [REST API](#rest-api)
- [从源码构建](#从源码构建)
- [支持格式与协议](#支持格式与协议)
- [持久化文件](#持久化文件)
- [常见问题](#常见问题)
- [License](#license)

## 核心特性

- WebUI 优先：导入、测试、入池、删除、端口扫描、订阅刷新、日志查看都可以通过浏览器完成。
- 支持多种导入格式：订阅链接、URI 列表、Base64 编码内容、Clash/Mihomo YAML。
- 订阅链接自动识别内容类型：HTTP/HTTPS 订阅返回 Clash YAML、Base64、URI 列表都可以解析。
- 订阅导入采用“标签快照”策略：同一标签再次导入会先成功解析最新内容，再替换旧的节点池、候选、失败节点。
- 只提取 Clash YAML 中的 `proxies` 节点，忽略 `proxy-groups`、`rules` 等规则配置。
- 导入任务支持动态进度：导入测速时会持续刷新 `进度 x/y，成功 x，失败 x，入池 x`。
- 节点分为候选节点、节点池、失败节点，失败节点不会丢失，可以后续重测。
- 支持批量测速、批量测试国家、测速成功后自动加入节点池。
- 支持候选节点、节点池、失败节点各自配置自动测速。
- 失败节点测速成功后可以进入候选节点，也可以按开关直接加入节点池。
- 测试国家与测速分离，国家检测不会隐式执行测速。
- 节点池端口和实际 sing-box 监听端口保持一致。
- Multi-Port 模式支持每个节点一个独立端口。
- Pool 模式支持一个统一代理入口。
- 端口扫描会显示节点池占用端口、外部进程占用端口和空闲端口。
- 设置页显示所有导入来源，包括订阅、URI、Base64、Clash YAML。
- 支持删除单个导入来源。
- 支持一键删除全部导入来源，同时清空订阅列表。
- 支持空节点启动。没有任何节点时 WebUI 仍然可以启动，用于首次导入。
- 支持日志查看和清空日志。
- 支持 WebUI 修改运行模式、监听地址、Multi-Port 起始端口、认证信息、刷新间隔等配置。

## 快速开始

### 1. 准备文件

运行目录通常包含：

```text
easy_proxies.exe
config.yaml
GeoLite2-Country.mmdb
```

`GeoLite2-Country.mmdb` 用于 GeoIP 国家检测和国家路由。如果只使用基础导入和测速，缺失 GeoIP 数据库时仍可运行部分功能，但国家检测和 GeoIP 路由会受影响。

### 2. 启动

```powershell
.\easy_proxies.exe -config config.yaml
```

不传 `-config` 时默认读取当前目录的 `config.yaml`：

```powershell
.\easy_proxies.exe
```

程序启动后会先启动管理端 WebUI，默认地址：

```text
http://127.0.0.1:9091
```

如果 `config.yaml` 中没有任何节点，程序不会因为 `nodes` 为空而退出。此时 WebUI 仍然可用，可以在浏览器中导入节点。

### 3. 打开 WebUI

浏览器访问：

```text
http://127.0.0.1:9091
```

如果配置了 `management.password`，WebUI 会要求登录。

## WebUI 工作流

典型使用流程：

```text
导入节点
  -> 自动解析
  -> generate_204 测速
  -> 成功节点进入候选或自动入池
  -> 失败节点保留
  -> 可选测试国家
  -> 节点池分配端口
  -> 使用 127.0.0.1:端口 访问代理
```

WebUI 页面说明：

| 页面 | 作用 |
| --- | --- |
| 导入节点 | 导入订阅链接、URI、Base64、Clash YAML，并执行导入测速 |
| 候选节点 | 显示测速成功但未加入节点池的节点 |
| 节点池 | 显示当前真实参与代理服务的节点和端口 |
| 失败节点 | 显示测速失败节点，支持后续重测 |
| 批量测试 | 跨候选、节点池、失败节点执行批量测速和国家检测 |
| 端口状态 | 查看从起始端口开始的端口分配和占用情况 |
| 日志 | 查看和清空运行日志 |
| 设置 | 修改运行参数、订阅刷新、导入来源管理 |

## 导入节点

进入 WebUI 的 `导入节点` 页面，点击 `选择导入格式`，可以选择四种格式。

### 订阅链接格式

用于导入 HTTP/HTTPS 订阅链接，每行一个：

```text
https://example.com/api/v1/client/subscribe?token=xxx
https://another.example/sub
```

订阅链接本质是先下载内容，再自动识别内容格式。返回内容可以是：

- Clash/Mihomo YAML
- Base64 编码节点列表
- URI 列表
- 混合的普通文本节点列表

导入订阅链接后，WebUI 会把订阅 URL 记录到设置页。订阅链接支持一次粘贴多个 URL，每行一个；这些 URL 会作为同一个标签的一次快照导入。

当前策略是“以最新导入为准”，不是并集：

- 同一个标签再次导入同一个 URL：重新拉取并解析，成功后替换该标签下的旧节点。
- 同一个标签导入不同 URL：旧 URL 对应的节点会被删除，只保留这次输入的 URL 解析结果。
- 同一个标签先导入 5 个 URL，之后只导入 1 个 URL：最终只保留这 1 个 URL 的节点。
- 替换前会先完成新内容的拉取与解析；如果新订阅拉取或解析失败，旧节点不会被删除。
- 旧节点无论在节点池、候选节点还是失败节点中，都会被该标签的新快照替换。

### URI 格式

每行一个节点 URI：

```text
vless://...
vmess://...
trojan://...
ss://...
hysteria2://...
tuic://...
```

如果不填写标签前缀，默认使用 `local`。

### Base64 格式

用于导入 Base64 编码后的节点列表。解码后的内容通常是一行一个 URI。

示例：

```text
dmxlc3M6Ly8...
```

### Clash YAML 格式

用于导入 Clash/Mihomo 配置。只需要包含 `proxies:` 即可：

```yaml
proxies:
  - name: example
    type: vless
    server: example.com
    port: 443
    uuid: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
    tls: true
```

程序只会提取 `proxies` 下的节点，`proxy-groups`、`rules`、`dns`、`tun` 等配置不会进入节点池。

### 标签前缀

标签前缀会放到节点名称前面，用于后续筛选和识别来源。

示例：

```text
标签前缀: Liangxinyun
节点原名: 日本高速01
最终名称: Liangxinyun-日本高速01
```

重复导入同一个订阅 URL 时，如果该 URL 已存在，系统会继续使用原来的标签前缀；导入成功后按该标签替换旧快照。

### 自动加入节点池

导入页默认开启 `测速成功后自动加入节点池`。

开启时：

```text
导入 -> 测速成功 -> 自动加入节点池 -> 分配端口
```

关闭时：

```text
导入 -> 测速成功 -> 进入候选节点
```

失败节点会保留在失败节点列表，不会丢失。

### 动态导入进度

导入测速时，WebUI 会轮询导入任务状态并显示：

```text
进度 49/49，成功 35，失败 14，入池 35
```

后端会按批写入进度，小导入基本逐个更新，大导入最多每 10 个节点更新一次。前端每 500ms 轮询一次，所以节点数量多或测速耗时较长时可以看到中间进度，而不是只看到 `0` 和最终完成。

## 节点生命周期

节点在 WebUI 中主要有三种用户可见状态：

| 状态 | 页面 | 含义 |
| --- | --- | --- |
| 候选节点 | 候选节点 | 测速成功，但还没有加入节点池 |
| 节点池 | 节点池 | 当前实际参与代理服务，已经分配端口 |
| 失败节点 | 失败节点 | 最近一次测速失败，保留等待重试 |

内部状态大致如下：

```text
parsed
  -> testing
  -> passed
  -> in_pool
  -> failed
```

常见流转：

```text
导入成功 -> parsed
开始测速 -> testing
测速成功 -> passed
加入节点池 -> in_pool
测速失败 -> failed
失败节点重测成功 -> passed 或 in_pool
```

失败节点不会被自动删除。后续网络恢复或节点可用时，可以重新测速。

## 节点池与端口

节点池是当前实际可用的代理节点集合。只有节点池中的节点才会占用本机代理端口。

### Multi-Port 模式

每个节点一个独立端口：

```text
127.0.0.1:24000 -> 节点 1
127.0.0.1:24001 -> 节点 2
127.0.0.1:24002 -> 节点 3
```

使用示例：

```bash
curl -x http://127.0.0.1:24000 https://ipinfo.io
```

如果配置了 Multi-Port 用户名和密码：

```bash
curl -x http://user:pass@127.0.0.1:24000 https://ipinfo.io
```

### Pool 模式

所有节点共用一个入口：

```text
127.0.0.1:2323
```

使用示例：

```bash
curl -x http://127.0.0.1:2323 https://ipinfo.io
```

### Hybrid 模式

同时启用 Pool 入口和 Multi-Port 入口。

### 端口一致性

项目保证以下三者对齐：

```text
WebUI 节点池数量
= config.yaml 中节点池节点数量
= sing-box 实际监听端口数量
```

节点池变化后，端口会根据 `multi_port.base_port` 重新分配。WebUI 的端口状态页会显示：

- 从哪个起始端口开始寻找端口
- 为多少个节点找到了端口
- 最后使用到哪个端口
- 哪些端口不可用
- 每个可用端口对应哪个节点

如果某些端口被外部程序占用，会自动跳过并继续向后查找可用端口。

## 测速与国家检测

### 测速

测速目标默认是：

```text
https://www.google.com/generate_204
```

可以在设置页修改探测目标。

测速成功会记录延迟，测速失败会记录失败原因。失败原因会显示在失败节点列表中。

### 测试国家

测试国家用于确认代理出口的真实国家或地区。国家检测不会自动包含测速。

推荐流程：

```text
先测速
  -> 测速成功
  -> 再测试国家
  -> 根据国家重命名或筛选
```

### 批量测试

批量测试页面支持选择范围：

- 候选节点
- 节点池
- 失败节点

支持选择操作：

- 测速
- 测试国家
- 测速成功后自动加入节点池

异步批量测试会显示实时进度，包括当前阶段、完成数量、成功数量、失败数量、国家检测结果和入池数量。

### 自动测速

候选节点、节点池、失败节点可以分别开启自动测速。

行为说明：

- 失败节点：测速成功后会自动测试国家，然后按开关进入候选节点或直接加入节点池。
- 候选节点：测速失败会进入失败节点。
- 节点池：测速失败会从节点池移除并进入失败节点。

## 订阅与导入来源管理

设置页包含两个相关概念：

```text
订阅自动刷新
当前导入来源
```

### 订阅自动刷新

订阅自动刷新只对订阅链接生效，不对手动粘贴的 URI、Base64、Clash YAML 内容生效。

刷新间隔由三个输入框配置：

- 天
- 小时
- 分钟

默认是 1 天。

点击 `保存自动刷新设置` 只会保存自动刷新开关和间隔，不会改动当前节点。

点击 `手动刷新全部订阅来源` 或某个来源旁边的 `手动刷新` 会进入完整刷新流程：

1. 弹出阻塞式进度窗口，任务完成前窗口不会自动关闭。
2. 按导入来源逐个刷新；每个来源都会显示标签和订阅组序号。
3. 每个订阅 URL 单独显示一行，即使当前来源只有 1 个订阅链接也会展示。
4. 先拉取订阅并解析最新节点。
5. 解析成功后按“标签快照”替换旧节点。
6. 自动执行 `generate_204` 测速。
7. 测速成功的节点自动加入节点池。
8. 测速失败的节点进入失败节点列表。
9. 弹窗实时刷新 `节点 / 进度 / 成功 / 失败 / 入池` 数量。
10. 全部完成后刷新设置列表、左侧导航数量和节点池状态。

手动刷新不会把新旧订阅结果做并集，而是以本次成功解析和测速后的最新结果为准。

### 当前导入来源

当前导入来源会显示所有通过 WebUI 导入过的来源。订阅链接会按标签聚合显示，同一个标签下可以包含一个或多个 URL：

- 订阅链接
- URI 内容导入
- Base64 内容导入
- Clash YAML 内容导入

每个来源会显示：

- 类型
- 标签前缀
- 总节点数
- 池内数量
- 候选数量
- 失败数量
- 来源 URL 或内容类型
- 更新时间

### 删除单个导入来源

每个来源右侧有 `删除` 按钮。

删除订阅链接来源时，会同时：

- 从 `config.yaml` 的 `subscriptions` 中移除 URL
- 删除该来源导入的全部 managed 节点
- 从节点池移除池内节点
- 触发一次 reload

删除 URI、Base64、Clash YAML 内容来源时，会删除该来源导入的全部 managed 节点。

### 一键删除全部导入

设置页提供 `一键删除全部导入`。

该操作会：

- 删除所有导入来源
- 删除所有 managed 导入节点
- 从节点池移除所有导入节点
- 清空 `config.yaml` 中的 `subscriptions`
- 保留自动刷新开关和刷新间隔
- 保留手动配置节点

这是清空 WebUI 导入数据的最快方式。

## 设置页

设置页可以修改运行参数。

| 配置项 | 说明 |
| --- | --- |
| 运行模式 | `pool`、`multi-port`、`hybrid` |
| 探测目标 | 测速使用的 URL |
| 外部 IP | 导出入口时使用的外部地址 |
| 管理监听 | WebUI 和 REST API 监听地址 |
| Pool 监听地址 | Pool 模式监听地址 |
| Pool 端口 | Pool 模式监听端口 |
| Multi 地址 | Multi-Port 模式监听地址 |
| Multi 起始端口 | Multi-Port 模式分配端口的起点 |
| Multi 用户名 | Multi-Port 代理认证用户名 |
| Multi 密码 | Multi-Port 代理认证密码 |
| 池模式 | 节点池选择策略 |
| 失败阈值 | 连续失败多少次后视为失败 |
| 跳过证书验证 | 是否跳过 TLS 证书校验 |

保存运行设置后，配置会写入 `config.yaml`，并触发 reload。

## 运行日志

日志页面支持：

- 查看最近运行日志
- 刷新日志
- 清空日志

清空日志只影响 WebUI 中的日志缓冲和配置的日志文件，不会删除节点。

## 配置文件

`config.yaml` 示例：

```yaml
mode: multi-port

listener:
  address: 127.0.0.1
  port: 2323
  username: ""
  password: ""

multi_port:
  address: 127.0.0.1
  base_port: 24000
  username: ""
  password: ""

pool:
  mode: sequential
  failure_threshold: 3
  blacklist_duration: 24h

management:
  enabled: true
  listen: 127.0.0.1:9091
  probe_target: https://www.google.com/generate_204
  password: ""

subscription_refresh:
  enabled: true
  interval: 24h
  timeout: 30s
  health_check_timeout: 1m
  drain_timeout: 30s
  min_available_nodes: 1

geoip:
  enabled: true
  database_path: ./GeoLite2-Country.mmdb
  listen: ""
  port: 0
  auto_update_enabled: true
  auto_update_interval: 24h

log:
  output: stdout
  file: logs/easy_proxies.log
  max_size: 50
  max_backups: 3
  max_age: 7
  compress: false

subscriptions: []
nodes: []
skip_cert_verify: false
```

### 重要字段说明

| 字段 | 说明 |
| --- | --- |
| `mode` | 运行模式，支持 `pool`、`multi-port`、`hybrid` |
| `listener` | Pool 模式入口配置 |
| `multi_port` | Multi-Port 模式入口配置 |
| `multi_port.base_port` | 节点池端口分配起始值 |
| `management.listen` | WebUI/API 地址 |
| `management.password` | WebUI 登录密码，空表示不启用登录 |
| `management.probe_target` | 测速目标 |
| `subscription_refresh.enabled` | 是否启用订阅自动刷新 |
| `subscription_refresh.interval` | 自动刷新间隔 |
| `subscriptions` | 订阅 URL 列表 |
| `nodes` | 当前节点池中的节点配置 |
| `skip_cert_verify` | 是否跳过上游 TLS 校验 |

## REST API

WebUI 使用 REST API 与后端通信。默认 base URL：

```text
http://127.0.0.1:9091
```

如果启用了管理密码，需要携带 Bearer Token。

### 认证

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/auth` | 登录，返回 token |

### 设置

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/settings` | 获取运行设置 |
| `PUT` | `/api/settings` | 保存运行设置 |
| `POST` | `/api/reload` | 手动 reload 核心 |

### 订阅

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/subscription/status` | 获取订阅刷新状态 |
| `POST` | `/api/subscription/refresh` | 立即刷新订阅 |
| `GET` | `/api/subscription/config` | 获取订阅配置 |
| `PUT` | `/api/subscription/config` | 保存订阅配置 |
| `POST` | `/api/subscription/delete` | 删除某个订阅 URL 和它导入的节点 |

删除订阅请求：

```json
{
  "url": "https://example.com/sub"
}
```

### 导入

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/import/parse` | 解析导入内容 |
| `POST` | `/api/import/{import_id}/commit` | 提交导入并开始测速 |
| `GET` | `/api/import/jobs/{job_id}` | 查询导入任务进度 |
| `GET` | `/api/import/sources` | 获取所有导入来源 |
| `POST` | `/api/import/sources` | 删除单个来源或全部来源 |

解析订阅链接：

```json
{
  "mode": "url",
  "url": "https://example.com/sub",
  "tag_prefix": "local"
}
```

解析内容：

```json
{
  "mode": "content",
  "content": "vless://...\ntrojan://...",
  "tag_prefix": "local"
}
```

提交导入：

```json
{
  "node_ids": ["node-id-1", "node-id-2"],
  "auto_reload": true,
  "promote_passed": true
}
```

导入任务进度响应示例：

```json
{
  "id": "job-id",
  "status": "running",
  "total": 49,
  "passed": 35,
  "failed": 14,
  "promoted": 35,
  "node_ids": []
}
```

删除单个导入来源：

```json
{
  "key": "url:https://example.com/sub"
}
```

删除全部导入来源：

```json
{
  "all": true
}
```

### Managed Nodes

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/nodes/all` | 所有 managed 节点 |
| `GET` | `/api/nodes/pool` | 节点池节点 |
| `GET` | `/api/nodes/failed` | 失败节点 |
| `PUT` | `/api/nodes/order` | 调整节点顺序 |
| `POST` | `/api/managed-nodes/{id}/retest` | 单节点测速 |
| `POST` | `/api/managed-nodes/{id}/country` | 单节点测试国家 |
| `POST` | `/api/managed-nodes/{id}/promote` | 加入节点池 |
| `POST` | `/api/managed-nodes/{id}/exclude` | 排除节点 |
| `POST` | `/api/managed-nodes/{id}/delete` | 删除节点 |
| `POST` | `/api/managed-nodes/batch-test/start` | 异步批量测试 |
| `GET` | `/api/managed-nodes/batch-test/status?id=` | 查询批量测试进度 |
| `POST` | `/api/managed-nodes/batch-promote` | 批量加入节点池 |
| `POST` | `/api/managed-nodes/batch-delete` | 批量删除节点 |

### 手动节点

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/nodes/config` | 获取手动配置节点 |
| `POST` | `/api/nodes/config` | 新增手动配置节点 |
| `PUT` | `/api/nodes/config/{name}` | 更新手动配置节点 |
| `DELETE` | `/api/nodes/config/{name}` | 删除手动配置节点 |

### 端口与日志

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/ports/status` | 查看端口状态 |
| `GET` | `/api/logs` | 获取日志 |
| `POST` | `/api/logs/clear` | 清空日志 |
| `GET` | `/api/export` | 导出入口信息 |

## 从源码构建

需要 Go 1.24 或更高版本。

Windows：

```powershell
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies.exe ./cmd/easy_proxies
```

Linux/macOS：

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies ./cmd/easy_proxies
```

构建标签：

| 标签 | 说明 |
| --- | --- |
| `with_clash_api` | 启用 sing-box Clash API 相关能力 |
| `with_utls` | 启用 uTLS 指纹能力 |
| `with_quic` | 启用 QUIC 相关协议支持 |

## 支持格式与协议

### 导入格式

| 格式 | 说明 |
| --- | --- |
| 订阅链接 | HTTP/HTTPS URL，内容自动识别 |
| URI 列表 | 每行一个代理 URI |
| Base64 | Base64 编码后的 URI 列表 |
| Clash YAML | 提取 `proxies` 下的节点 |

### 协议

支持协议取决于 sing-box 和当前构建标签，常见包括：

- VLESS
- VMess
- Trojan
- Shadowsocks
- ShadowsocksR
- Hysteria
- Hysteria2
- TUIC
- AnyTLS
- SOCKS5
- HTTP
- HTTPS

## 持久化文件

| 文件 | 说明 |
| --- | --- |
| `config.yaml` | 主配置文件，保存运行模式、端口、订阅、节点池等 |
| `managed_nodes.json` | WebUI managed 节点状态，包含候选、节点池、失败节点 |
| `nodes.txt` | 订阅刷新拉取到的 URI 缓存 |
| `GeoLite2-Country.mmdb` | GeoIP 国家数据库 |
| `logs/easy_proxies.log` | 文件日志 |

不要手动编辑 `managed_nodes.json`。它由 WebUI 和后端自动维护。

## 常见问题

### 为什么没有节点也能启动？

首次使用时通常没有节点。程序会先启动 WebUI，允许用户在浏览器中导入节点。只有节点池中有节点时，sing-box 才需要为节点分配监听端口。

### 重复导入同一个订阅会发生什么？

重复导入同一个订阅 URL 时，会重新拉取内容并解析。如果该 URL 之前已经导入过，会继续使用原来的标签前缀。新内容解析成功后，系统会删除该标签下旧的节点池、候选、失败节点，再写入这次解析出的最新节点。也就是说，订阅导入不是旧节点与新节点的并集，而是以本次导入结果为准。

### 为什么订阅自动刷新没有直接改变节点池？

订阅刷新会重新拉取记录的订阅内容。WebUI 的手动刷新按“标签快照”执行：成功解析最新内容后替换旧节点，并立即执行 `generate_204` 测速；测速成功的节点会自动加入节点池，失败节点进入失败列表。刷新窗口会展示每个订阅链接的实时进度，例如：

```text
节点 44 · 进度 44/44 · 成功 33 · 失败 11 · 入池 33
```

如果某次刷新后出现 `总数 x / 池内 0 / 候选 0 / 失败 0`，说明只完成了解析但没有完成测速入库流程；当前 WebUI 的手动刷新已经改为强制执行测速和成功入池。

### 什么是一键删除全部导入？

设置页的 `一键删除全部导入` 会删除所有通过 WebUI 导入的来源和节点，并清空订阅 URL 列表。它不会删除手动配置节点。

### 端口为什么会跳号？

如果某些端口被其他程序占用，程序会跳过这些端口，继续向后查找可用端口。端口状态页会显示不可用端口列表。

### WebUI 显示的端口和实际监听端口应该一致吗？

应该一致。节点池中的每个节点都会对应实际监听端口。如果不一致，优先查看端口状态页和日志。

### 失败节点会被删除吗？

不会。失败节点会保留下来，可以后续重新测速。测速成功后可以进入候选节点，也可以按设置直接进入节点池。

### 测试国家为什么没有自动测速？

测速和测试国家是两个动作。测试国家默认假设节点已经可用。如果节点当前失败，需要先测速成功。

### 如何完全清空项目导入数据？

进入设置页，点击 `一键删除全部导入`。该操作会清空所有导入来源、managed 节点和订阅 URL。

### 如何清空日志？

进入日志页，点击 `清空日志`。

## License

本项目继承上游项目 [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies) 的许可证。
