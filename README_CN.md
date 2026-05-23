# Easy Proxies

[English](README.md)

Easy Proxies 是一个基于 sing-box 的代理池和节点生命周期管理工具。它的目标不是简单启动几个代理节点，而是解决真实使用场景中的完整流程：导入大量订阅，解析不同格式，批量测速，保留失败节点，检测真实出口国家，按标签和国家重命名，把确认可用的节点加入本地代理池，并通过 WebUI 管理端口、日志和自动刷新。

当前版本的主要操作都可以在 WebUI 中完成。WebUI 使用浅蓝色和绿色的简洁界面，不依赖仪表盘，核心页面包括：导入节点、候选节点、节点池、失败节点、端口状态、日志和设置。

## 目录

- [核心功能](#核心功能)
- [工作方式](#工作方式)
- [快速开始](#快速开始)
- [源码构建](#源码构建)
- [Docker 运行](#docker-运行)
- [配置说明](#配置说明)
- [WebUI 使用指南](#webui-使用指南)
- [节点生命周期](#节点生命周期)
- [导入格式](#导入格式)
- [测速和国家检测](#测速和国家检测)
- [端口管理](#端口管理)
- [GeoIP 地域路由](#geoip-地域路由)
- [管理 API](#管理-api)
- [支持协议](#支持协议)
- [文件和持久化](#文件和持久化)
- [常见问题](#常见问题)
- [开发](#开发)

## 核心功能

- **完整 WebUI 流程**：从导入、测速、国家检测、候选节点、节点池、失败节点到端口状态，都可以在浏览器中操作。
- **取消无意义仪表盘**：界面直接围绕节点管理，不展示无用的装饰性统计首页。
- **四种导入入口**：订阅链接、URI 格式、Base64 格式、Clash YAML 格式。
- **订阅链接自动识别内容**：HTTP/HTTPS 链接获取到的内容可以是完整 Clash 配置、Base64 订阅或 URI 列表，后端会按内容解析。
- **Clash 配置只提取节点**：Clash/Mihomo 配置中的规则、策略组、DNS 等内容会被忽略，只导入 `proxies` 中的节点。
- **标签前缀命名**：导入时输入 tag 前缀，默认是 `local`，初始名称为 `标签-原始名称`。
- **真实国家重命名**：测试国家后按真实出口国家重命名，例如 `良心云-JP1`、`良心云-SG2`、`local-US3`。
- **候选节点机制**：测速成功的节点先进入候选节点，不会自动进入节点池，用户选择后再加入。
- **失败节点保留**：测速失败的节点不会丢弃，会进入失败节点列表，之后可以重新测速。
- **失败节点恢复流程**：失败节点测速成功后会自动测试国家，然后根据开关进入候选节点或直接进入节点池。
- **表格多选**：候选节点、节点池、失败节点都是表格视图，支持行选择、一键全选、批量操作。
- **正常表格排序**：点击列头按该列排序，再点击一次反向排序。
- **测速和测试国家分离**：测速只访问 `generate_204`，测试国家只做出口国家检测，不隐式包含测速。
- **按页面自动测速**：候选节点、节点池、失败节点都有独立自动测速配置。
- **节点池端口重排**：节点池支持按国家、标签、延迟重排端口。
- **端口状态扫描**：根据当前节点池数量推荐可用端口段，并汇总不可用端口。
- **日志全屏查看**：日志页面使用更宽的控制台布局，便于查看运行日志。
- **订阅自动刷新**：设置页面可以启用自动刷新，并用天、小时、分钟配置刷新间隔。
- **热重载**：加入节点池、删除池内节点、订阅刷新和部分配置变更可触发 runtime reload。
- **协议覆盖广**：支持 VLESS、VMess、Trojan、Shadowsocks、Hysteria2、TUIC、AnyTLS、SOCKS5、HTTP/HTTPS。
- **GeoIP 地域路由**：可按国家分类节点，并通过 `/jp`、`/us`、`/hk`、`/sg` 等路径使用指定地区节点池。
- **REST API**：WebUI 背后的接口可以直接用于脚本或外部工具。

## 工作方式

Easy Proxies 有两个相关但不同的层：

| 层 | 作用 |
| --- | --- |
| 托管节点存储 | 保存所有导入节点，包括状态、延迟、国家、标签、原始名称、是否在节点池、最后错误等。数据持久化到 `managed_nodes.json`。 |
| sing-box 运行时配置 | 真正被本地代理入口使用的节点。只有进入节点池的节点才会写入运行时配置。 |

因此需要明确几个概念：

- **候选节点**：测速成功，但还没有进入节点池，不会被本地代理入口使用。
- **节点池**：已经加入运行时配置的活动节点，会被代理池或独立端口使用。
- **失败节点**：测速失败，或者池内节点重新测速失败后被移出的节点。
- **删除节点**：从托管节点存储中永久删除；如果它在节点池中，也会从运行时配置中删除。

## 快速开始

### 1. 准备 `config.yaml`

如果你是在当前仓库运行，目录中可能已经有 `config.yaml`。如果没有，可以新建下面的最小配置：

```yaml
mode: pool

listener:
  address: 127.0.0.1
  port: 2323
  username: username
  password: password

multi_port:
  address: 127.0.0.1
  base_port: 24000
  username: mpuser
  password: mppass

pool:
  mode: sequential
  failure_threshold: 3
  blacklist_duration: 24h

management:
  enabled: true
  listen: 127.0.0.1:9091
  probe_target: http://cp.cloudflare.com/generate_204
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
  auto_update_enabled: true
  auto_update_interval: 24h

log:
  output: stdout
  file: logs/easy_proxies.log
  max_size: 50
  max_backups: 3
  max_age: 7
  compress: false

nodes: []
nodes_file: ""
subscriptions: []
external_ip: ""
log_level: info
skip_cert_verify: false
```

### 2. 编译并启动

Windows PowerShell：

```powershell
$env:GOPROXY = "https://goproxy.cn,direct"
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies.exe ./cmd/easy_proxies
.\easy_proxies.exe -config config.yaml
```

Linux 或 macOS：

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies ./cmd/easy_proxies
./easy_proxies -config config.yaml
```

### 3. 打开 WebUI

默认地址：

```text
http://127.0.0.1:9091
```

如果 `management.password` 为空，打开后不需要登录。如果配置了密码，需要在浏览器中登录，或者先调用 `/api/auth` 获取 token。

### 4. 导入节点

在 WebUI 中打开 **导入节点**：

1. 选择导入格式。
2. 输入标签前缀，例如 `local`、`良心云`、`provider`。
3. 粘贴订阅链接、URI 列表、Base64 内容或 Clash YAML。
4. 点击导入并测试。
5. 测速成功的节点进入 **候选节点**。
6. 测速失败的节点进入 **失败节点**。
7. 需要使用某些候选节点时，勾选它们并加入 **节点池**。

## 源码构建

项目使用 Go 1.24。

推荐构建命令：

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies ./cmd/easy_proxies
```

Windows：

```powershell
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies.exe ./cmd/easy_proxies
```

完整功能构建：

```bash
go build -tags "with_clash_api with_utls with_quic with_grpc with_wireguard with_gvisor" -o easy_proxies ./cmd/easy_proxies
```

编译标签说明：

| 标签 | 作用 |
| --- | --- |
| `with_clash_api` | sing-box Clash API 集成需要，用于监控和流量接口。 |
| `with_utls` | 启用 uTLS 指纹能力，很多现代 VLESS、VMess、Trojan 节点需要。 |
| `with_quic` | Hysteria2、TUIC 等 QUIC 协议必须启用。 |
| `with_grpc` | 启用 gRPC 传输支持。 |
| `with_wireguard` | 启用 sing-box WireGuard 支持。 |
| `with_gvisor` | 启用 sing-box gVisor 支持。 |

如果 HY2、TUIC 或其他 QUIC 节点报错 `QUIC is not included in this build`，需要重新用 `with_quic` 编译。

## Docker 运行

仓库包含 `docker-compose.yml` 和 `start.sh`。

Linux：

```bash
chmod +x start.sh
./start.sh
```

手动运行：

```bash
touch config.yaml nodes.txt
docker compose up -d
```

Docker Compose 默认使用 host 网络模式。这对自动端口分配和 multi-port 模式更友好。

Docker 注意事项：

- 容器启动前，`config.yaml` 和 `nodes.txt` 必须是文件。
- 如果挂载源不存在，Docker 可能会把它创建成目录，导致程序异常。
- `start.sh` 会自动处理这个常见问题。
- WebUI 保存设置需要写入 `config.yaml`。
- 如果设置保存失败，先检查宿主机文件权限。

## 配置说明

### 运行模式

```yaml
mode: pool
```

支持模式：

| 模式 | 行为 |
| --- | --- |
| `pool` | 单端口代理池。所有池内节点共享一个本地 HTTP/SOCKS5 混合入口。 |
| `multi-port` | 多端口模式。每个活动节点分配一个独立本地端口。 |
| `hybrid` | 混合模式。同时启用代理池入口和每节点独立端口。 |

### Listener

```yaml
listener:
  address: 127.0.0.1
  port: 2323
  username: username
  password: password
```

这是 `pool` 和 `hybrid` 模式下共享的本地混合代理入口。

示例：

```bash
curl -x http://username:password@127.0.0.1:2323 https://ipinfo.io
```

### Multi-Port

```yaml
multi_port:
  address: 127.0.0.1
  base_port: 24000
  username: mpuser
  password: mppass
```

在 `multi-port` 和 `hybrid` 模式中，节点池中的活动节点可以分配独立端口。如果某个端口不可用，程序会跳过并使用后续可用端口，WebUI 的端口状态页面会展示实际结果。

### Pool

```yaml
pool:
  mode: sequential
  failure_threshold: 3
  blacklist_duration: 24h
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `mode` | 调度模式，常用值是 `sequential` 和 `random`。 |
| `failure_threshold` | 运行时节点连续失败多少次后进入黑名单。 |
| `blacklist_duration` | 黑名单持续时间。 |

### Management

```yaml
management:
  enabled: true
  listen: 127.0.0.1:9091
  probe_target: http://cp.cloudflare.com/generate_204
  password: ""
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `enabled` | 是否启用 WebUI 和管理 API。 |
| `listen` | WebUI/API 监听地址。 |
| `probe_target` | 运行时探测目标。 |
| `password` | WebUI 密码。为空表示不需要登录。 |

### Subscription Refresh

```yaml
subscription_refresh:
  enabled: true
  interval: 24h
  timeout: 30s
  health_check_timeout: 1m
  drain_timeout: 30s
  min_available_nodes: 1
```

设置页面用天、小时、分钟输入框配置自动刷新间隔。自动刷新作用于通过 **导入节点** 页面导入过的订阅链接。设置页面不再提供一个独立的大型订阅 URL 编辑框，避免和导入流程重复。

### GeoIP

```yaml
geoip:
  enabled: true
  database_path: ./GeoLite2-Country.mmdb
  listen: ""
  port: 0
  auto_update_enabled: true
  auto_update_interval: 24h
```

如果 `port` 为 `0`，会使用默认 GeoIP 路由端口，常见默认值是 `1221`。

### 日志

```yaml
log:
  output: stdout
  file: logs/easy_proxies.log
  max_size: 50
  max_backups: 3
  max_age: 7
  compress: false

log_level: info
```

`log.output` 可以是 `stdout` 或 `file`。WebUI 日志页读取内存环形缓冲区中的最近日志。

### 静态节点

可以直接在配置里写节点：

```yaml
nodes:
  - name: example-node
    uri: vless://uuid@example.com:443?security=tls&type=ws&path=/path#example-node
    port: 24000
```

也可以从文件读取：

```yaml
nodes_file: nodes.txt
```

`nodes.txt` 每行一个 URI。

### 订阅链接

```yaml
subscriptions:
  - https://provider.example/api/v1/client/subscribe?token=xxx
```

通过 WebUI 的订阅链接格式导入时，订阅 URL 也会被记录，用于自动刷新。

## WebUI 使用指南

WebUI 页面结构：

| 页面 | 作用 |
| --- | --- |
| 导入节点 | 导入订阅链接、URI 列表、Base64 内容或 Clash YAML。 |
| 候选节点 | 测速成功但尚未加入节点池的节点。 |
| 节点池 | 当前已经生效的运行时节点。 |
| 失败节点 | 测速失败的节点，可后续重新测速。 |
| 端口状态 | 查看池内节点占用端口、不可用端口汇总和推荐端口段。 |
| 日志 | 全屏查看最近运行日志。 |
| 设置 | 配置探测目标、日志、GeoIP、订阅自动刷新等。 |

### 导入节点

导入格式：

| 按钮 | 输入内容 |
| --- | --- |
| 订阅链接格式 | 一个或多个 HTTP/HTTPS 订阅链接，每行一个。 |
| URI 格式 | 每行一个代理 URI。 |
| Base64 格式 | Base64 编码的 V2Ray 风格订阅内容。 |
| Clash YAML 格式 | 包含 `proxies` 的 Clash/Mihomo YAML。 |

订阅链接格式会先请求 URL，然后自动识别响应内容。URL 返回 Clash YAML、Base64 或 URI 列表都可以。

导入行为：

- Tag 前缀默认是 `local`。
- 节点初始名称是 `标签-原始名称`。
- 导入后会进行测速。
- 测速成功进入 **候选节点**。
- 测速失败进入 **失败节点**。
- 不需要二次预览确认。
- 通过订阅链接导入的 URL 会进入订阅自动刷新范围。

### 候选节点

候选节点表示已经通过测速，但还没有加入节点池。

可用操作：

- 用每行前面的方框选择节点。
- 使用表头方框一键选择当前可见节点。
- 点击表格列头排序。
- 再次点击同一个列头反向排序。
- 对选中节点批量测速。
- 对选中节点批量测试国家。
- 把选中节点加入节点池。
- 永久删除节点。
- 开启当前页面的自动测速。

失败行为：

- 候选节点测速失败后，会进入 **失败节点**。
- 测试国家不会自动包含测速。
- 如果需要确认节点仍然可用，先点击测速，再点击测试国家。

### 节点池

节点池中的节点已经写入运行时配置，可以被本地代理入口使用。

可用操作：

- 用每行前面的方框选择节点。
- 对选中节点批量测速。
- 对选中节点批量测试国家。
- 按国家重排端口。
- 按标签重排端口。
- 按延迟重排端口。
- 永久删除节点。
- 开启当前页面的自动测速。

失败行为：

- 节点池中的节点测速失败后，会从节点池移除并进入 **失败节点**。
- 移除池内节点时会更新运行时配置，并在需要时触发 reload。

端口重排行为：

- **按国家重排**：按真实国家代码分组。
- **按标签重排**：按导入 tag 前缀分组。
- **按延迟重排**：低延迟节点排在前面。
- 重排会影响活动节点顺序和端口分配。

### 失败节点

失败节点不会进入节点池，也不会作为候选节点使用，但会保留以便后续恢复。

可用操作：

- 用每行前面的方框选择节点。
- 使用表头方框一键选择当前可见节点。
- 对选中失败节点一键测速。
- 永久删除节点。
- 开启当前页面自动测速。
- 切换“失败节点测速成功后自动加入节点池”。

恢复行为：

- 失败节点测速成功。
- 自动测试国家。
- 如果开启自动加入节点池，则直接进入 **节点池**。
- 如果关闭自动加入节点池，则进入 **候选节点**。

### 端口状态

端口状态页面根据当前节点池数量计算。

它会展示：

- multi-port 监听地址。
- 用户输入的起始端口。
- 当前需要端口的节点数量。
- 推荐可分配端口段。
- 不可用端口数量和具体端口。
- 节点池中每个节点当前占用的端口和节点名称。

不可用原因：

| 原因 | 含义 |
| --- | --- |
| `listener_conflict` | 和共享代理入口端口冲突。 |
| `occupied_by_os` | 被系统或其他进程占用。 |
| `used_by_config` | 已经在配置中被节点使用。 |

### 日志

日志页面是全宽控制台布局，方便查看 sing-box、节点测试、导入、端口、reload 等运行日志。

### 设置

设置页面包含：

- 导出代理时使用的外部 IP。
- 探测目标。
- 是否跳过证书验证。
- GeoIP 开关。
- 日志输出和轮转。
- 订阅自动刷新开关。
- 订阅自动刷新间隔，按天、小时、分钟输入。
- 保存并刷新订阅按钮。

## 节点生命周期

托管节点状态流转：

```text
parsed -> testing -> passed -> in_pool
                  -> failed
in_pool -> failed
failed -> testing -> passed
passed -> excluded
任意可见状态 -> deleted
```

状态含义：

| 状态 | 含义 |
| --- | --- |
| `parsed` | 已解析，但还没完成测试。 |
| `testing` | 正在测试。 |
| `passed` | 测速成功，属于候选节点。 |
| `failed` | 测速失败，或者池内节点测速失败后被移出。 |
| `in_pool` | 已加入节点池，属于活动运行时节点。 |
| `excluded` | 已排除，不参与候选或节点池。 |

命名规则：

- 国家检测前：`标签-原始名称`。
- 国家检测后：`标签-国家代码序号`。
- 示例：`良心云-JP1`、`良心云-SG2`、`local-US3`。
- 节点失败后，名称会回到 `标签-原始名称`，方便在失败节点列表中识别。

## 导入格式

### 订阅链接格式

输入：

```text
https://provider.example/api/v1/client/subscribe?token=xxx
https://another-provider.example/sub
```

行为：

- 每行一个 URL。
- 只接受 `http://` 和 `https://`。
- URL 返回内容自动识别格式。
- Clash 规则和策略组会被忽略，只导入节点。
- 订阅 URL 会保存，用于自动刷新。

### URI 格式

输入：

```text
vless://uuid@example.com:443?security=tls&type=ws&path=/path#node-1
trojan://password@example.com:443?security=tls&type=ws#node-2
ss://method:password@example.com:443#node-3
vmess://base64-json
```

行为：

- 每行一个 URI。
- 支持的 scheme 见 [支持协议](#支持协议)。
- 空行会被忽略。

### Base64 格式

输入：

```text
dmxlc3M6Ly8...
```

行为：

- 常见 V2Ray 订阅格式。
- 解码后内容通常是每行一个 URI。

### Clash YAML 格式

输入：

```yaml
proxies:
  - name: example
    type: vless
    server: example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    tls: true
    network: ws
    ws-opts:
      path: /path
      headers:
        Host: example.com
```

行为：

- 只导入 `proxies` 中的节点。
- 忽略 rules、proxy-groups、dns 等配置。
- 支持 Clash YAML 中常见的 inline JSON 风格节点写法。

## 测速和国家检测

### 测速

测速会通过实际代理访问 `generate_204`，不是只检查 URI 语法。

后端行为：

- 为待测节点创建临时 sing-box 实例。
- 通过这个代理访问测试目标。
- 记录延迟。
- 批量测速并发执行。
- 单节点测试有超时上限，避免大量节点串行等待太久。

### 测试国家

测试国家和测速是分开的。测试国家只检测代理真实出口位置。

后端会依次尝试：

1. `https://ipinfo.io/json`
2. `http://ip-api.com/json/?fields=status,countryCode,country`
3. `https://api.country.is`

国家检测会更新：

- `country_code`
- `country_name`
- 显示名称
- 如果节点已经在节点池中，也会更新运行时节点名称

### 自动测速

每个节点页面都有独立自动测速设置：

| 页面 | 自动测速行为 |
| --- | --- |
| 候选节点 | 重测候选节点。失败的候选节点进入失败节点。 |
| 节点池 | 重测池内节点。失败的池内节点移出节点池并进入失败节点。 |
| 失败节点 | 重测失败节点。恢复成功后自动测试国家，再根据开关进入候选节点或节点池。 |

## 端口管理

端口主要影响 `multi-port` 和 `hybrid` 模式。

端口逻辑：

- 用户输入希望使用的起始端口。
- Easy Proxies 根据当前节点池数量计算需要多少端口。
- 程序扫描端口是否可用。
- 不可用端口会被跳过。
- WebUI 给出足够当前节点池使用的推荐端口段。
- 已经分配给节点池的端口会显示为“端口 + 节点名称”。
- 真正不可用的端口会在上方汇总显示。

API 示例：

```bash
curl "http://127.0.0.1:9091/api/ports/status?from=24000&count=60"
```

返回字段：

| 字段 | 含义 |
| --- | --- |
| `address` | multi-port 绑定地址。 |
| `base_port` | 扫描起始端口。 |
| `target_count` | 需要端口的节点数量。 |
| `recommended` | 推荐端口段和跳过端口。 |
| `ports` | 端口扫描详情。 |

## GeoIP 地域路由

启用 GeoIP 后，可以使用按地区划分的代理路径。

常见路径：

| 路径 | 含义 |
| --- | --- |
| `/jp` | 日本节点 |
| `/kr` | 韩国节点 |
| `/us` | 美国节点 |
| `/hk` | 香港节点 |
| `/tw` | 台湾节点 |
| `/sg` | 新加坡节点 |
| `/other` | 其他地区节点 |

示例：

```bash
curl -x http://username:password@127.0.0.1:1221/jp/ https://ipinfo.io
```

实际监听地址和端口由 `geoip.listen` 与 `geoip.port` 控制。

## 管理 API

如果配置了 `management.password`，除 `/api/auth` 外，其余 API 都需要认证。

认证头：

```http
Authorization: Bearer <token>
```

### 认证

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/auth` | 使用 `{"password":"..."}` 登录并获取 session token。 |

### 导入

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/import/parse` | 解析 URL 或粘贴内容，生成托管节点。 |
| `POST` | `/api/import/{import_id}/commit` | 提交解析出的节点并启动测试。 |
| `GET` | `/api/import/jobs/{job_id}` | 查询导入任务进度。 |

解析订阅 URL：

```bash
curl -X POST http://127.0.0.1:9091/api/import/parse \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"mode":"url","url":"https://provider.example/sub","tag_prefix":"local"}'
```

解析粘贴内容：

```bash
curl -X POST http://127.0.0.1:9091/api/import/parse \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"mode":"content","content":"vless://...","tag_prefix":"local"}'
```

提交全部解析节点：

```bash
curl -X POST http://127.0.0.1:9091/api/import/<import_id>/commit \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"auto_reload":true}'
```

提交指定节点：

```bash
curl -X POST http://127.0.0.1:9091/api/import/<import_id>/commit \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"node_ids":["node-id-1","node-id-2"],"auto_reload":true}'
```

### 托管节点

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/nodes/all` | 获取全部托管节点。 |
| `GET` | `/api/nodes/pool` | 获取节点池中的节点。 |
| `GET` | `/api/nodes/failed` | 获取失败节点。 |
| `PUT` | `/api/nodes/order` | 保存节点池顺序。 |
| `POST` | `/api/managed-nodes/batch-test` | 批量测速、测试国家、可选加入节点池。 |
| `POST` | `/api/managed-nodes/{id}/retest` | 对单个节点测速。 |
| `POST` | `/api/managed-nodes/{id}/country` | 对单个节点测试国家。 |
| `POST` | `/api/managed-nodes/{id}/promote` | 将测速成功的候选节点加入节点池。 |
| `POST` | `/api/managed-nodes/{id}/exclude` | 排除节点。 |
| `POST` | `/api/managed-nodes/{id}/delete` | 永久删除节点。 |
| `DELETE` | `/api/managed-nodes/{id}/delete` | 永久删除节点。 |

批量测速：

```bash
curl -X POST http://127.0.0.1:9091/api/managed-nodes/batch-test \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"node_ids":["id1","id2"],"retest":true,"auto_reload":true}'
```

批量测试国家：

```bash
curl -X POST http://127.0.0.1:9091/api/managed-nodes/batch-test \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"node_ids":["id1","id2"],"country":true,"auto_reload":true}'
```

失败节点恢复后直接入池：

```bash
curl -X POST http://127.0.0.1:9091/api/managed-nodes/batch-test \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"node_ids":["id1","id2"],"retest":true,"country":true,"promote_passed":true,"auto_reload":true}'
```

批量响应示例：

```json
{
  "total": 2,
  "retested": 2,
  "passed": 1,
  "failed": 1,
  "country_ok": 1,
  "country_bad": 0,
  "promoted": 1,
  "nodes": []
}
```

### 端口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/ports/status?from=24000&count=60` | 从指定端口开始扫描，并按节点数量推荐端口段。 |
| `GET` | `/api/ports/status?from=24000&to=24200` | 扫描明确端口范围。 |

### 运行时节点

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/nodes` | 获取运行时节点快照。 |
| `POST` | `/api/nodes/{tag}/probe` | 探测单个运行时节点。 |
| `POST` | `/api/nodes/{tag}/release` | 解除单个节点黑名单。 |
| `POST` | `/api/nodes/{tag}/blacklist` | 拉黑单个运行时节点。 |
| `POST` | `/api/nodes/probe-all` | SSE 方式探测所有运行时节点。 |

### 订阅和设置

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/subscription/config` | 获取订阅刷新配置。 |
| `POST` | `/api/subscription/config` | 保存订阅刷新配置。 |
| `GET` | `/api/subscription/status` | 获取订阅刷新状态。 |
| `POST` | `/api/subscription/refresh` | 手动触发订阅刷新。 |
| `GET` | `/api/settings` | 获取全局设置。 |
| `POST` | `/api/settings` | 保存全局设置。 |
| `POST` | `/api/reload` | 重载运行时核心。 |
| `GET` | `/api/export` | 导出代理链接。 |
| `GET` | `/api/logs` | 获取最近日志。 |

### 配置节点 CRUD

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/nodes/config` | 获取配置节点列表。 |
| `POST` | `/api/nodes/config` | 创建配置节点。 |
| `PUT` | `/api/nodes/config/{name}` | 更新配置节点。 |
| `DELETE` | `/api/nodes/config/{name}` | 删除配置节点。 |

## 支持协议

| 协议 | URI scheme | 说明 |
| --- | --- | --- |
| VLESS | `vless://` | 支持 TLS、Reality、TCP、WS、HTTP/2、gRPC 等常见字段。 |
| VMess | `vmess://` | 支持常见 Base64 JSON VMess URI 和 Clash 输入。 |
| Trojan | `trojan://` | 支持 TLS、WS、SNI 和常见查询参数。 |
| Shadowsocks | `ss://` | 支持 SIP002 风格 URI 和部分插件字段。 |
| Hysteria2 | `hysteria2://`、`hy2://` | 需要 `with_quic` 编译标签。 |
| TUIC | `tuic://` | 需要 `with_quic` 编译标签。 |
| AnyTLS | `anytls://` | 在 sing-box 构建和配置支持时可用。 |
| SOCKS5 | `socks5://`、`socks://` | 上游 SOCKS 代理。 |
| HTTP/HTTPS | `http://`、`https://` | 上游 HTTP 代理。 |

## 文件和持久化

| 文件 | 作用 |
| --- | --- |
| `config.yaml` | 主运行配置。WebUI 设置和节点池变更可能写入此文件。 |
| `managed_nodes.json` | 托管节点存储，保存导入节点、状态、延迟、国家和节点池元数据。 |
| `nodes.txt` | 可选 URI 列表，由 `nodes_file` 加载。 |
| `GeoLite2-Country.mmdb` | GeoIP 地域路由使用的数据库。 |
| `logs/easy_proxies.log` | 启用文件日志时的轮转日志文件。 |

删除行为：

- 删除候选节点：从 `managed_nodes.json` 中移除。
- 删除失败节点：从 `managed_nodes.json` 中移除。
- 删除节点池节点：从托管状态和运行时配置中同时移除，并在需要时重载运行时。

## 常见问题

### `QUIC is not included in this build`

重新带 `with_quic` 编译：

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies ./cmd/easy_proxies
```

### `import service not available`

说明当前监控服务没有挂载导入服务。请用完整应用入口启动：

```bash
./easy_proxies -config config.yaml
```

不要用只启动 WebUI 的测试 harness 来访问导入功能。

### `snap.forEach is not a function`

通常表示前端期望数组，但接口返回了错误对象或旧缓存数据。硬刷新页面，并检查 `/api/nodes/all`、`/api/nodes/pool`、`/api/nodes/failed` 的响应。

### `Cannot read properties of null (reading 'error')`

通常是 API 调用失败，但返回体不是前端预期格式。打开浏览器网络面板或 WebUI 日志页查看具体接口响应。

### 导入节点后 DNS `NXDOMAIN`

节点语法可能能解析，但上游 server、SNI、Reality、DNS 或供应商配置不可用。可以保留在失败节点中后续重测，也可以直接删除。

### 测试国家失败或很慢

测试国家需要通过代理访问外部 IP 查询服务。后端会尝试多个服务，但大量节点测试时仍可能遇到临时限流或目标服务不稳定。

### 日志显示端口不是我输入的起始端口

通常是某些端口被系统、其他程序或共享监听端口占用。打开 **端口状态**，从你的起始端口扫描。WebUI 会显示不可用端口数量、具体端口，并推荐足够当前节点池使用的端口段。

### Docker 中 WebUI 设置保存失败

检查宿主机权限：

```bash
chmod 666 config.yaml nodes.txt
```

同时确认 `config.yaml` 是文件，不是目录。

## 开发

运行测试：

```bash
go test ./...
```

运行 vet：

```bash
go vet ./...
```

构建推荐本地二进制：

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies ./cmd/easy_proxies
```

构建 Docker 镜像：

```bash
docker build -t easy_proxies:local .
```

## 许可证

MIT License
