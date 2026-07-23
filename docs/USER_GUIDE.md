# Easy Proxies User Guide

[English](./USER_GUIDE.md) · [简体中文](./USER_GUIDE.zh-CN.md) · [繁體中文](./USER_GUIDE.zh-TW.md)

This guide covers the two ways to prepare and start Easy Proxies. It ends after the process starts; WebUI usage is outside this startup guide.

## Method 1: Build from source

### 1. Requirements

- Git
- Go 1.24.4 or a compatible Go 1.24 toolchain

### 2. Clone the project

```bash
git clone https://github.com/daimon3332/easy-proxies.git
cd easy-proxies
```

### 3. Create the local configuration

Copy the template instead of editing it directly.

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

### 4. Build Easy Proxies

Windows PowerShell or Command Prompt:

```powershell
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies.exe .
```

Linux or macOS:

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies .
```

The `with_clash_api` tag is required for the embedded Clash API. The other tags enable uTLS/Reality-related and QUIC-based protocol support.

### 5. Start the locally built binary

Windows:

```powershell
.\easy_proxies.exe -config config.yaml
```

Linux or macOS:

```bash
chmod +x easy_proxies
./easy_proxies -config config.yaml
```

Keep the terminal open while Easy Proxies is running.

## Method 2: Download a Release

### 1. Choose a package

Open the [latest Release](https://github.com/daimon3332/easy-proxies/releases/latest) and choose the ZIP package matching your system and CPU:

| System | Common CPU | Package suffix |
| --- | --- | --- |
| Windows 64-bit PC | Intel or AMD | `windows-amd64.zip` |
| Windows on ARM | ARM64 | `windows-arm64.zip` |
| Linux 64-bit PC/server | Intel or AMD | `linux-amd64.zip` |
| Linux ARM device/server | ARM64 | `linux-arm64.zip` |
| Intel Mac | Intel | `macos-amd64.zip` |
| Apple silicon Mac | M1/M2/M3/M4 or newer | `macos-arm64.zip` |

The ZIP already contains the executable. No local build is required for this method.

### 2. Extract and create the configuration

Extract the ZIP into a new folder, then copy the configuration template.

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

### 3. Start Easy Proxies

Windows:

```powershell
.\easy_proxies.exe -config config.yaml
```

Linux or macOS:

```bash
chmod +x easy_proxies
./easy_proxies -config config.yaml
```

Keep the terminal open while Easy Proxies is running.

## Troubleshooting

### `clash api is not included in this build`

For Method 1, rebuild with:

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies .
```

For Method 2, download an official Release package instead of using an incomplete binary.

### `config.yaml` cannot be found

Run the configuration copy command from the directory that contains `config.example.yaml`, then start Easy Proxies from that same directory.

### The program exits during startup

Check the terminal output first. Confirm that `config.yaml` is valid, the required ports are available, and the binary matches your operating system and CPU architecture.

### The WebUI does not open

Confirm that the process is still running and that `management.listen` in `config.yaml` matches the address you opened. Also check whether another program already uses port `9091`.

### macOS blocks the binary

macOS builds are not signed or notarized with an Apple Developer certificate. After verifying the downloaded file against `SHA256SUMS.txt`, remove the quarantine attribute:

```bash
xattr -d com.apple.quarantine easy_proxies
```
