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
  <img alt="Platforms" src="https://img.shields.io/badge/Platform-Windows%20%7C%20Linux-blue">
  <a href="https://github.com/daimon3332/easy-proxies/releases/latest"><img alt="最新版本" src="https://img.shields.io/github/v/release/daimon3332/easy-proxies?display_name=tag&sort=semver"></a>
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

## ✨ 核心功能

- 🔗 面向普通用户的订阅优先 WebUI 流程。
- 支持 HTTP/HTTPS 订阅、URI 列表、Base64 内容和 Clash/Mihomo YAML。
- 支持每行一个 `host:port` 或可选 `user:pass@host:port` 的 HTTP/SOCKS5 节点列表。
- ⚡ 并发、异步节点测速和实时进度。
- 🔀 支持使用当前 sing-box 构建可识别的任意代理 URI 配置前置代理，并形成链式路由。
- 订阅拉取固定沿用直连优先、可用池内代理有限兜底的策略；所选前置代理只用于组成节点链路。
- 链式导入分别显示前置基线与完整链路结果，不对后置节点执行无前置的直连测试。
- 站点检测支持任意 TAG，分别检测 Google、GitHub、Outlook 和 ProxySpace，并可按所选站点全部成功的严格交集重新生成端口。
- 🧩 分别保留候选节点、节点池节点和失败节点。
- 导入测速成功后默认自动加入节点池。
- 🔌 默认 `multi-port` 模式下每个节点独立端口。
- 可选 `pool` 和 `hybrid` 模式。
- 支持批量重测、国家检测、订阅刷新、端口查看和运行日志。
- 探测目标只支持 `https://www.gstatic.com/generate_204` 和 `https://cp.cloudflare.com/generate_204`。
- WebUI 与 REST API 共用管理入口。

## ⚙️ 可靠性与性能

- 使用共享测速运行时复用 sing-box 服务，避免每轮重试都创建完整运行时。
- 使用有界异步并发、延迟重试、备用目标和跨任务去重，避免任务无限堆积。
- 节点只有在实际 multi-port 本地端口也能访问探测目标时，才会被判定为测速成功。
- 订阅刷新、本地 URI/YAML/Base64 节点重测、取消任务和节点池更新都具备事务恢复与有界队列。
- 运行时诊断可查看启动阶段、监听器数量、内存、协程和测速队列，不会暴露节点凭据或订阅链接。

## 🖼️ WebUI 预览

<details>
<summary>显示全部界面截图</summary>
<br>

### 导入并生成端口

<img src="./images/webui-import.png" width="960" alt="导入订阅">

### 可用端口

<img src="./images/webui-pool.png" width="960" alt="可用代理端口">

### 候选节点

<img src="./images/webui-nodes.png" width="960" alt="候选节点">

### 失败节点

<img src="./images/webui-failed.png" width="960" alt="失败节点">

### 批量工具

<img src="./images/webui-bulk.png" width="960" alt="批量工具">

### 端口状态

<img src="./images/webui-ports.png" width="960" alt="端口状态">

### 日志

<img src="./images/webui-logs.png" width="960" alt="日志">

### 设置

<img src="./images/webui-settings.png" width="960" alt="设置">

</details>

## 开始使用
请查看 **[简体中文使用教程](./docs/USER_GUIDE.zh-CN.md)**，教程提供两种启动方法：

1. 复制项目源码到本地，自行构建并启动 Easy Proxies。
2. 从 [Releases](https://github.com/daimon3332/easy-proxies/releases/latest) 下载对应版本并启动。

## 导入格式与协议

支持 HTTP/HTTPS 订阅 URL、代理 URI 列表、Base64 编码 URI 列表、Clash/Mihomo YAML 的 `proxies` 部分，以及每行一个 `host:port` 或 `user:pass@host:port` 的纯文本列表。导入 Host:Port 列表时需要选择 HTTP 或 SOCKS5 协议。

常见协议包括 VLESS、VMess、Trojan、Shadowsocks、ShadowsocksR、Hysteria、Hysteria2、TUIC、AnyTLS、HTTP/HTTPS、SOCKS4 和 SOCKS5。实际协议能力取决于 sing-box 版本与构建标签。

链式导入需要先在设置中配置前置代理，再在导入时选择该前置。订阅内容仍按直连优先、可用池内代理有限兜底的策略拉取。测试成功表示前置代理和 `前置代理 -> 导入节点 -> 探测目标` 完整链路都可用。协议组合仍需满足传输兼容性，例如依赖 UDP 的后置节点不能使用仅提供 TCP 隧道的前置代理。

## 运行模式

| 模式 | 行为 |
| --- | --- |
| `multi-port` | 默认模式，每个节点分配一个本地端口。 |
| `pool` | 所有节点共用一个代理入口，由节点池调度。 |
| `hybrid` | 同时启用共享入口和每节点独立端口。 |

配置中的 `multi_port` 写法也受支持，并会自动规范为 `multi-port`。

## 二次开发与贡献

源码环境、构建标签、测试命令、分支规范和 Pull Request 流程请查看 **[CONTRIBUTING.md](./CONTRIBUTING.md)**。

## 常见问题

使用教程包含启动报错、端口分配和浏览器保存的导入选项。测速成功的节点没有使用预期端口时，请查看 WebUI 的端口页面；被其他程序占用的端口会自动跳过。

## 上游项目与致谢

- [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies) — 上游项目
- [SagerNet/sing-box](https://github.com/SagerNet/sing-box) — 代理平台与协议实现

## 🔗 友情链接

- [linux.do](https://linux.do)：**学AI，上L站！！！**

## 许可证

本项目采用 [MIT License](./LICENSE)，并保留对上游项目及其 MIT 许可代码的归属说明。
