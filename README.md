<p align="center">
  <img src="./internal/monitor/assets/logo.png" width="128" alt="Easy Proxies logo">
</p>

<h1 align="center">Easy Proxies</h1>

<p align="center">A subscription-first proxy node importer, tester, pool manager, and multi-port gateway powered by sing-box.</p>

<p align="center">
  <a href="./README.md">English</a> ·
  <a href="./README.zh-CN.md">简体中文</a> ·
  <a href="./README.zh-TW.md">繁體中文</a>
</p>

<p align="center">
  <img alt="Go 1.24+" src="https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white">
  <img alt="License MIT" src="https://img.shields.io/badge/License-MIT-green.svg">
  <img alt="Powered by sing-box" src="https://img.shields.io/badge/Powered%20by-sing--box-4B5563">
  <img alt="Platforms" src="https://img.shields.io/badge/Platform-Windows%20%7C%20Linux%20%7C%20macOS-blue">
</p>

> Easy Proxies is a community-maintained fork of [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies). This fork focuses on a redesigned WebUI, subscription importing, reliable node testing, node lifecycle management, and an easier multi-port workflow.

## What it does

Easy Proxies turns one or more proxy subscription URLs into local HTTP/SOCKS5 ports:

```text
Paste subscription URLs
  -> parse nodes
  -> test every node
  -> automatically add passed nodes to the pool
  -> assign local ports starting at 24000
  -> copy and use the generated ports
```

The default runtime mode is `multi-port`, so every passed node receives its own local port. **Automatically add passed nodes to the pool** is enabled by default for first-time use.

## Key features

- Subscription-first WebUI workflow.
- Imports HTTP/HTTPS subscriptions, URI lists, Base64 content, and Clash/Mihomo YAML.
- Concurrent and asynchronous node testing with live progress.
- Keeps candidate, pooled, and failed nodes instead of silently dropping failures.
- Automatically promotes passed imports to the node pool by default.
- One local port per node in the default `multi-port` mode.
- Optional `pool` and `hybrid` runtime modes.
- Batch retest, country detection, subscription refresh, port inspection, and logs.
- Probe target selection between `https://www.gstatic.com/generate_204` and `https://cp.cloudflare.com/generate_204`.
- WebUI and REST API served from the same management endpoint.

## Getting started

For ordinary use, download the ZIP package matching your operating system and CPU architecture from [Releases](https://github.com/daimon3332/easy-proxies/releases/latest).

```text
download a release
  -> copy config.example.yaml to config.yaml
  -> start Easy Proxies
  -> open the WebUI
  -> import subscription URLs and test
  -> use the generated local ports
```

See the **[English User Guide](./docs/USER_GUIDE.md)** for download selection, startup commands, subscription importing, node testing, and troubleshooting.

## Import formats and protocols

Import formats:

- HTTP/HTTPS subscription URL
- Proxy URI list
- Base64-encoded URI list
- Clash/Mihomo YAML `proxies` section

Common protocols include VLESS, VMess, Trojan, Shadowsocks, ShadowsocksR, Hysteria, Hysteria2, TUIC, AnyTLS, HTTP/HTTPS, SOCKS4, and SOCKS5. Actual protocol availability depends on the sing-box version and build tags.

## Runtime modes

| Mode | Behavior |
| --- | --- |
| `multi-port` | Default. Assign one local port to every pooled node. |
| `pool` | Expose one shared proxy entry and schedule across pooled nodes. |
| `hybrid` | Enable the shared pool entry and per-node ports together. |

`multi_port` is accepted in configuration files and normalized to `multi-port`.

## Data and privacy

Files that may contain subscription URLs, credentials, node URIs, runtime state, or local logs are ignored by Git, including:

```text
config.yaml
nodes.txt
managed_nodes.json
node_ports.json
*.log
*.mmdb
```

Use `config.example.yaml` for documentation and commits. Before publishing a fork, inspect the complete Git history because adding a file to `.gitignore` does not remove old commits.

## Development and contributing

For source setup, build tags, tests, branch conventions, and the pull request workflow, see **[CONTRIBUTING.md](./CONTRIBUTING.md)**.

## Troubleshooting

The user guide covers startup errors, missing ports, browser-saved import options, and macOS Gatekeeper handling. When a passed node does not use the expected port, check the WebUI port page because occupied ports are skipped automatically.

## Upstream and acknowledgements

- [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies) — upstream project
- [SagerNet/sing-box](https://github.com/SagerNet/sing-box) — proxy platform and protocol implementation

## License

Distributed under the [MIT License](./LICENSE). This project retains attribution for the upstream project and MIT-licensed portions on which it is based.
