# Easy Proxies User Guide

[English](./USER_GUIDE.md) · [简体中文](./USER_GUIDE.zh-CN.md) · [繁體中文](./USER_GUIDE.zh-TW.md)

This guide covers the normal Release-based workflow. Building from source is documented in [CONTRIBUTING.md](../CONTRIBUTING.md).

## 1. Choose a download

Open the [latest Release](https://github.com/daimon3332/easy-proxies/releases/latest) and choose the ZIP package matching your system and CPU:

| System | Common CPU | Package suffix |
| --- | --- | --- |
| Windows 64-bit PC | Intel or AMD | `windows-amd64.zip` |
| Windows on ARM | ARM64 | `windows-arm64.zip` |
| Linux 64-bit PC/server | Intel or AMD | `linux-amd64.zip` |
| Linux ARM device/server | ARM64 | `linux-arm64.zip` |
| Intel Mac | Intel | `macos-amd64.zip` |
| Apple silicon Mac | M1/M2/M3/M4 or newer | `macos-arm64.zip` |

The ZIP package is recommended because it includes the executable, `config.example.yaml`, documentation, and license. Raw binaries are also available.

## 2. Extract and create your configuration

Extract the ZIP into a new folder. Do not edit `config.example.yaml` directly; copy it to `config.yaml` so future package updates do not overwrite local settings.

Windows PowerShell:

```powershell
Copy-Item config.example.yaml config.yaml
```

Windows Command Prompt:

```batch
copy config.example.yaml config.yaml
```

Linux or macOS:

```bash
cp config.example.yaml config.yaml
```

The template contains no subscription URLs or proxy nodes. The default mode is `multi-port`, the first node port is `24000`, and the WebUI listens on `127.0.0.1:9091`.

## 3. Start Easy Proxies

Windows PowerShell or Command Prompt:

```powershell
.easy_proxies.exe -config config.yaml
```

Linux:

```bash
chmod +x easy_proxies
./easy_proxies -config config.yaml
```

macOS:

```bash
chmod +x easy_proxies
./easy_proxies -config config.yaml
```

macOS builds are not signed or notarized with an Apple Developer certificate. If Gatekeeper quarantines the file, first verify it against `SHA256SUMS.txt`, then run:

```bash
xattr -d com.apple.quarantine easy_proxies
```

Keep the terminal open while the program is running.

## 4. Open the WebUI

Open `http://127.0.0.1:9091` in a browser. An empty installation can open the WebUI without preconfigured nodes.

## 5. Import subscriptions and test nodes

1. Open **Import Nodes**.
2. Keep **Subscription URL** selected.
3. Paste one subscription URL per line.
4. Keep **Automatically add passed nodes to the pool** enabled.
5. Select **Import and Test**.
6. Wait for parsing and concurrent testing to finish.
7. Review passed, failed, and pooled node counts.

Passed nodes are added to the pool automatically when the option is enabled. In the default `multi-port` mode, every pooled node receives a separate local port.

## 6. Use a generated proxy port

Open the port page and copy the actual address assigned to a node, for example `127.0.0.1:24000`. Ports already occupied by other applications are skipped automatically.

HTTP proxy example:

```bash
curl -x http://127.0.0.1:24000 https://api.ipify.org
```

SOCKS5 proxy example:

```bash
curl --proxy socks5h://127.0.0.1:24000 https://api.ipify.org
```

Use the protocol shown in the WebUI for each listener.

## 7. Stop and restart

Press `Ctrl+C` in the terminal to stop the program. Start it again with the same `-config config.yaml` command. Keep `config.yaml` and runtime data in the application folder when replacing the executable.

## Troubleshooting

### `clash api is not included in this build`

Use an official Release package. Source builds must include the `with_clash_api` tag described in [CONTRIBUTING.md](../CONTRIBUTING.md).

### A passed node has no expected port

Open the port page. Another application may already use that port, so Easy Proxies assigns the next available port.

### Automatic pool promotion is disabled

The browser remembers this selection. Enable **Automatically add passed nodes to the pool** on the import page before the next import.

### The WebUI does not open

Confirm that the process is still running and that `management.listen` in `config.yaml` matches the address you opened. Also check whether another program already uses port `9091`.
