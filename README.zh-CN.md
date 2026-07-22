<p align="center">
  <img src="./internal/monitor/assets/logo.png" width="128" alt="Easy Proxies Logo">
</p>

<h1 align="center">Easy Proxies</h1>

<p align="center">基于 sing-box 的订阅导入、节点测速、节点池管理与多端口代理工具。</p>

<p align="center">
  <a href="./README.md">English</a> ·
  <a href="./README.zh-CN.md">简体中文</a> ·
  <a href="./README.zh-TW.md">繁體中文</a>
</p>

<p align="center">
  <img alt="Go 1.24+" src="https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white">
  <img alt="MIT License" src="https://img.shields.io/badge/License-MIT-green.svg">
  <img alt="Powered by sing-box" src="https://img.shields.io/badge/Powered%20by-sing--box-4B5563">
  <img alt="Platforms" src="https://img.shields.io/badge/Platform-Windows%20%7C%20Linux%20%7C%20macOS-blue">
</p>

> 本项目基于 [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies) 二次开发，重点改进了 WebUI、订阅导入、节点测速、节点生命周期管理与多端口使用体验。

## 项目用途

Easy Proxies 可以把一个或多个代理订阅 URL 转换为本地 HTTP/SOCKS5 代理端口：

```text
粘贴订阅 URL
  -> 解析节点
  -> 测试全部节点
  -> 成功节点自动加入节点池
  -> 从 24000 开始分配本地端口
  -> 复制端口并直接使用
```

默认运行模式是 `multi-port`，每个测速成功并进入节点池的节点都会获得独立本地端口。首次使用时，“测速成功后自动加入节点池”默认开启。

## 核心功能

- 面向普通用户的订阅优先 WebUI 流程。
- 支持 HTTP/HTTPS 订阅、URI 列表、Base64 内容和 Clash/Mihomo YAML。
- 一次导入多个订阅 URL。
- 并发、异步节点测速和实时进度。
- 分别保留候选节点、节点池节点和失败节点。
- 导入测速成功后默认自动加入节点池。
- 默认 `multi-port` 模式下每个节点独立端口。
- 可选 `pool` 和 `hybrid` 模式。
- 支持批量重测、国家检测、订阅刷新、端口查看和运行日志。
- 探测目标只支持：
  - `https://www.gstatic.com/generate_204`
  - `https://cp.cloudflare.com/generate_204`
- WebUI 与 REST API 共用管理入口。

## 快速开始

### 环境要求

- Go 1.24 或更高版本
- Windows、Linux 或 macOS

### Windows

```powershell
Copy-Item config.example.yaml config.yaml
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies.exe .
.\easy_proxies.exe -config config.yaml
```

### Linux / macOS

```bash
cp config.example.yaml config.yaml
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies .
./easy_proxies -config config.yaml
```

浏览器打开：

```text
http://127.0.0.1:9091
```

首次启动不需要预先配置节点；空节点状态下 WebUI 仍然可以打开。

## 最常见的使用流程

1. 在 WebUI 打开“导入节点”。
2. 保持“订阅链接”格式。
3. 每行粘贴一个订阅 URL。
4. 保持“测速成功后自动加入节点池”开启。
5. 点击“导入并测试”。
6. 等待解析和测速完成。
7. 复制生成的地址，例如 `127.0.0.1:24000`。

HTTP 代理示例：

```bash
curl -x http://127.0.0.1:24000 https://api.ipify.org
```

SOCKS5 示例：

```bash
curl --proxy socks5h://127.0.0.1:24000 https://api.ipify.org
```

被其他程序占用的端口会自动跳过，实际端口分配以 WebUI 的端口状态页面为准。

## 导入格式与协议

支持的导入格式：

- HTTP/HTTPS 订阅 URL
- 代理 URI 列表
- Base64 编码 URI 列表
- Clash/Mihomo YAML 的 `proxies` 部分

常见协议包括 VLESS、VMess、Trojan、Shadowsocks、ShadowsocksR、Hysteria、Hysteria2、TUIC、AnyTLS、HTTP/HTTPS、SOCKS4 和 SOCKS5。实际协议能力取决于 sing-box 版本与构建标签。

## 运行模式

| 模式 | 行为 |
| --- | --- |
| `multi-port` | 默认模式，每个节点分配一个本地端口。 |
| `pool` | 所有节点共用一个代理入口，由节点池调度。 |
| `hybrid` | 同时启用共享入口和每节点独立端口。 |

配置中的 `multi_port` 写法也受支持，并会自动规范为 `multi-port`。

## 配置

启动前把 `config.example.yaml` 复制为 `config.yaml`。模板不包含订阅或节点信息，默认配置为：

```yaml
mode: multi-port

multi_port:
  address: 127.0.0.1
  base_port: 24000

management:
  enabled: true
  listen: 127.0.0.1:9091
  probe_target: https://www.gstatic.com/generate_204

subscriptions: []
nodes: []
```

设置页面可以修改运行模式、监听地址、端口、认证信息、节点池策略、探测目标、黑名单秒数和轮换秒数。

## 构建标签

| 标签 | 用途 |
| --- | --- |
| `with_clash_api` | Clash API 集成所需。 |
| `with_utls` | 启用 uTLS/Reality 相关能力。 |
| `with_quic` | 启用 Hysteria2、TUIC 等 QUIC 协议。 |

上面的 Windows 构建命令已经包含这三个标签。

## 数据与隐私

以下文件可能包含订阅 URL、凭据、节点 URI、运行状态或本地日志，已被 Git 忽略：

```text
config.yaml
nodes.txt
managed_nodes.json
node_ports.json
*.log
*.mmdb
*.exe
```

提交代码时使用 `config.example.yaml`。公开已有仓库前还需要检查完整 Git 历史，因为加入 `.gitignore` 不会删除旧提交中的文件。

## 常见问题

### 启动提示 `clash api is not included in this build`

使用上面的 Windows 命令重新构建，或者在 Go 构建参数中加入 `with_clash_api`。

### 节点没有使用预期端口

打开端口状态页面。被其他进程占用的端口会自动跳过。

### 自动加入节点池没有开启

WebUI 会在浏览器中保存用户选择；在导入页面重新勾选即可恢复自动入池。

## 上游项目与致谢

- [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies) — 上游项目
- [SagerNet/sing-box](https://github.com/SagerNet/sing-box) — 代理平台与协议实现

## 许可证

本项目采用 [MIT License](./LICENSE)，并保留对上游项目及其 MIT 许可代码的归属说明。
