<p align="center">
  <img src="./internal/monitor/assets/logo.png" width="128" alt="Easy Proxies logo">
</p>

<h1 align="center">Easy Proxies</h1>

<p align="center">
  A subscription-first proxy node importer, tester, pool manager, and multi-port gateway powered by sing-box.
</p>

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

The default runtime mode is `multi-port`, so every passed node receives its own local port. The first-use import option **Automatically add passed nodes to the pool** is enabled by default.

## Key features

- Subscription-first WebUI workflow for ordinary users.
- Imports HTTP/HTTPS subscriptions, URI lists, Base64 content, and Clash/Mihomo YAML.
- Supports multiple subscription URLs in one import.
- Concurrent and asynchronous node testing with live progress.
- Keeps candidate, pooled, and failed nodes instead of silently dropping failures.
- Automatically promotes passed imports to the node pool by default.
- One local port per node in the default `multi-port` mode.
- Optional `pool` and `hybrid` runtime modes.
- Batch retest, country detection, subscription refresh, port inspection, and logs.
- Probe target selection between:
  - `https://www.gstatic.com/generate_204`
  - `https://cp.cloudflare.com/generate_204`
- WebUI and REST API served from the same management endpoint.

## Quick start

### Requirements

- Go 1.24 or newer
- Windows, Linux, or macOS

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

Open:

```text
http://127.0.0.1:9091
```

An empty installation starts the WebUI without requiring preconfigured nodes.

## Most common workflow

1. Open **Import Nodes** in the WebUI.
2. Keep **Subscription URL** selected.
3. Paste one subscription URL per line.
4. Keep **Automatically add passed nodes to the pool** enabled.
5. Click **Import and Test**.
6. Wait for parsing and testing to finish.
7. Copy a generated address such as `127.0.0.1:24000`.

Example with an HTTP proxy:

```bash
curl -x http://127.0.0.1:24000 https://api.ipify.org
```

Example with SOCKS5:

```bash
curl --proxy socks5h://127.0.0.1:24000 https://api.ipify.org
```

Ports occupied by other programs are skipped automatically. Use the WebUI port page as the source of truth for actual assignments.

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

## Configuration

Copy `config.example.yaml` to `config.yaml` before starting. The example contains no subscription or node data and defaults to:

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

The settings page can change the runtime mode, listeners, ports, credentials, pool strategy, probe target, blacklist seconds, and rotation seconds.

## Build tags

| Tag | Purpose |
| --- | --- |
| `with_clash_api` | Required by the embedded Clash API integration. |
| `with_utls` | Enables uTLS/Reality-related capabilities. |
| `with_quic` | Enables QUIC-based protocols such as Hysteria2 and TUIC. |

The Windows build command above includes all three tags.

## Data and privacy

The following files may contain subscription URLs, credentials, node URIs, runtime state, or local logs and are ignored by Git:

```text
config.yaml
nodes.txt
managed_nodes.json
node_ports.json
*.log
*.mmdb
*.exe
```

Use `config.example.yaml` for documentation and commits. Before publishing a fork, inspect the complete Git history because adding a file to `.gitignore` does not remove old commits.

## Troubleshooting

### `clash api is not included in this build`

Rebuild with the Windows command above or include `with_clash_api` in the Go build tags.

### A passed node has no expected port

Open the port status page. Easy Proxies skips ports already occupied by another process.

### A previous import option is still disabled

The WebUI remembers that option in browser storage. Enable it again on the import page to restore automatic promotion.

## Upstream and acknowledgements

- [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies) — upstream project
- [SagerNet/sing-box](https://github.com/SagerNet/sing-box) — proxy platform and protocol implementation

## License

Distributed under the [MIT License](./LICENSE). This project retains attribution for the upstream project and MIT-licensed portions on which it is based.
