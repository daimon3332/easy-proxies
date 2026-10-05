# Easy Proxies User Guide

[English](./USER_GUIDE.md) · [简体中文](./USER_GUIDE.zh-CN.md)

## 1. Get the program

### Option A: download a Release

Open the [latest Release](https://github.com/daimon3332/easy-proxies/releases/latest) and download the ZIP for your system, then extract it into a new folder:

| System | Package suffix |
| --- | --- |
| Windows (Intel/AMD) | `windows-amd64.zip` |
| Windows on ARM | `windows-arm64.zip` |
| Linux (Intel/AMD) | `linux-amd64.zip` |
| Linux ARM64 | `linux-arm64.zip` |

### Option B: build from source

Requires Git and Go 1.24.4 or a compatible Go 1.24 toolchain.

```bash
git clone https://github.com/daimon3332/easy-proxies.git
cd easy-proxies
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies .
```

On Windows, use `-o easy_proxies.exe`. The tags enable uTLS/Reality, QUIC-based protocols (Hysteria2, TUIC), and the optional Clash API, which stays off unless `EASY_PROXIES_CLASH_API_LISTEN` (for example `127.0.0.1:9092`) is set.

## 2. Create the configuration

In the program folder, copy the template:

```bash
cp config.example.yaml config.yaml          # Linux
Copy-Item config.example.yaml config.yaml   # Windows PowerShell
```

## 3. Start

```bash
./easy_proxies -config config.yaml          # Linux (run chmod +x easy_proxies first)
.\easy_proxies.exe -config config.yaml      # Windows
```

Keep the terminal open, then open the WebUI at the `management.listen` address (default `http://127.0.0.1:9091`).

## Troubleshooting

**`config.yaml` cannot be found** — start the program from the folder that contains `config.yaml`, or pass its full path to `-config`.

**`another easy_proxies instance is using this config`** — only one process may use a given config. The new process waits up to 40 seconds for the previous one to exit; stop the old process if it is still running.

**The program exits during startup** — read the terminal output. Common causes are an invalid `config.yaml`, the management port (`9091` by default) already in use, or a binary that does not match your OS/CPU.

**`clash api is not included in this build`** — rebuild with the tags shown above, or use an official Release package.
