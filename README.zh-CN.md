<p align="center">
  <img src="./internal/monitor/assets/logo.png" width="128" alt="Easy Proxies Logo">
</p>

<h1 align="center">Easy Proxies</h1>

<p align="center">基于 sing-box 的订阅优先代理节点导入、测速、节点池管理与多端口网关。</p>

<p align="center">
  <a href="./README.md">English</a> ·
  <a href="./README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <img alt="Go 1.24+" src="https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white">
  <img alt="License MIT" src="https://img.shields.io/badge/License-MIT-green.svg">
  <img alt="Powered by sing-box" src="https://img.shields.io/badge/Powered%20by-sing--box-4B5563">
  <img alt="Platforms" src="https://img.shields.io/badge/Platform-Windows%20%7C%20Linux-blue">
  <a href="https://github.com/daimon3332/easy-proxies/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/daimon3332/easy-proxies?display_name=tag&sort=semver"></a>
</p>

> 本项目基于 [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies) 二次开发，重做了 WebUI，改进了订阅导入、节点测速和多端口使用体验。

## 项目用途

粘贴订阅 URL，Easy Proxies 会解析并测试全部节点，把成功节点加入节点池，并从 `24000` 开始为每个节点分配独立的本地 HTTP/SOCKS5 端口。

## 核心功能

- 支持 HTTP/HTTPS 订阅、URI 列表、Base64、Clash/Mihomo YAML，以及 `host:port` / `user:pass@host:port` 列表。
- 并发多轮测速并实时显示进度；候选、池内和失败节点分别保留。
- 可配置前置代理组成链式路由，修改后先重新检测再应用。
- 站点检测覆盖 Google、GitHub、Outlook 和 ProxySpace，可按所选站点全部成功的节点重建端口。
- 订阅定时刷新，失败自动回滚；支持批量重测与国家检测。
- 仪表盘集中展示节点池、订阅刷新和运行状态；WebUI 与 REST API 共用管理入口。
- 支持本地与 WebDAV 备份、按 Tag 导出数据。

## 界面预览

<img src="./images/webui-dashboard.png" width="960" alt="仪表盘">

<details>
<summary>更多截图</summary>
<br>
<img src="./images/webui-pool.png" width="960" alt="可用端口">
<br><br>
<img src="./images/webui-import.png" width="960" alt="导入节点">
</details>

## 快速开始

从 [Releases](https://github.com/daimon3332/easy-proxies/releases/latest) 下载对应平台的压缩包，或从源码构建；把 `config.example.yaml` 复制为 `config.yaml` 后运行：

```bash
./easy_proxies -config config.yaml
```

浏览器打开 `management.listen` 地址（默认 `http://127.0.0.1:9091`）。安装包选择、构建命令和常见问题见 **[使用教程](./docs/USER_GUIDE.zh-CN.md)**。

## 支持的协议

VLESS、VMess、Trojan、Shadowsocks、ShadowsocksR、Hysteria、Hysteria2、TUIC、AnyTLS、HTTP/HTTPS、SOCKS4 和 SOCKS5。实际可用性取决于 sing-box 版本和构建 tags。

## 运行模式

| 模式 | 行为 |
| --- | --- |
| `multi-port` | 默认。每个池内节点一个本地端口。 |
| `pool` | 一个共享入口，在池内节点间调度。 |
| `hybrid` | 同时启用共享入口和独立端口。 |

## 参与开发

见 **[CONTRIBUTING.md](./CONTRIBUTING.md)**。

## 致谢

- [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies) — 上游项目
- [SagerNet/sing-box](https://github.com/SagerNet/sing-box) — 代理平台与协议实现
- [linux.do](https://linux.do) 社区

## 许可证

[MIT](./LICENSE)，保留上游项目及 MIT 授权部分的署名。
