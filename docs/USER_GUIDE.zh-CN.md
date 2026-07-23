# Easy Proxies 使用教程

[English](./USER_GUIDE.md) · [简体中文](./USER_GUIDE.zh-CN.md) · [繁體中文](./USER_GUIDE.zh-TW.md)

本教程介绍准备并启动 Easy Proxies 的两种方法。教程在程序启动后结束，不包含 WebUI 的使用流程。

## 方法一：从源码构建

### 1. 准备环境

- Git
- Go 1.24.4，或兼容的 Go 1.24 版本

### 2. 复制项目到本地

```bash
git clone https://github.com/daimon3332/easy-proxies.git
cd easy-proxies
```

### 3. 创建本地配置

复制配置模板，不要直接修改模板文件。

Windows PowerShell：

```powershell
Copy-Item config.example.yaml config.yaml
```

Windows 命令提示符：

```batch
copy config.example.yaml config.yaml
```

Linux 或 macOS：

```bash
cp config.example.yaml config.yaml
```

### 4. 自行构建 Easy Proxies

Windows PowerShell 或命令提示符：

```powershell
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies.exe .
```

Linux 或 macOS：

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies .
```

其中 `with_clash_api` 用于启用内置 Clash API，其他 tags 用于启用 uTLS/Reality 相关能力和基于 QUIC 的协议支持。

### 5. 启动自行构建的程序

Windows：

```powershell
.\easy_proxies.exe -config config.yaml
```

Linux 或 macOS：

```bash
chmod +x easy_proxies
./easy_proxies -config config.yaml
```

程序运行期间请保持终端窗口开启。

## 方法二：下载 Release

### 1. 选择下载文件

打开[最新 Release](https://github.com/daimon3332/easy-proxies/releases/latest)，根据操作系统和 CPU 选择对应的 ZIP 压缩包：

| 系统 | 常见 CPU | 文件名后缀 |
| --- | --- | --- |
| Windows 64 位电脑 | Intel 或 AMD | `windows-amd64.zip` |
| Windows ARM 设备 | ARM64 | `windows-arm64.zip` |
| Linux 64 位电脑或服务器 | Intel 或 AMD | `linux-amd64.zip` |
| Linux ARM 设备或服务器 | ARM64 | `linux-arm64.zip` |
| Intel Mac | Intel | `macos-amd64.zip` |
| Apple 芯片 Mac | M1/M2/M3/M4 或更新型号 | `macos-arm64.zip` |

ZIP 压缩包已经包含可执行文件，此方法不需要本地构建。

### 2. 解压并创建配置

把 ZIP 解压到新目录，然后复制配置模板。

Windows PowerShell：

```powershell
Copy-Item config.example.yaml config.yaml
```

Windows 命令提示符：

```batch
copy config.example.yaml config.yaml
```

Linux 或 macOS：

```bash
cp config.example.yaml config.yaml
```

### 3. 启动 Easy Proxies

Windows：

```powershell
.\easy_proxies.exe -config config.yaml
```

Linux 或 macOS：

```bash
chmod +x easy_proxies
./easy_proxies -config config.yaml
```

程序运行期间请保持终端窗口开启。

## 常见问题

### 提示 `clash api is not included in this build`

方法一需要使用以下命令重新构建：

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies .
```

方法二请重新下载官方 Release 压缩包，不要使用缺少构建 tags 的程序。

### 找不到 `config.yaml`

请在包含 `config.example.yaml` 的目录中执行复制命令，并在同一目录启动 Easy Proxies。

### 程序启动后立即退出

先查看终端中的错误信息，确认 `config.yaml` 格式正确、所需端口未被占用，并确认程序与操作系统和 CPU 架构匹配。

### WebUI 无法打开

确认程序仍在运行，并检查 `config.yaml` 中的 `management.listen` 是否与浏览器地址一致，同时确认 `9091` 端口未被其他程序占用。

### macOS 阻止运行程序

macOS 文件未使用 Apple Developer 证书签名或公证。使用 `SHA256SUMS.txt` 核对下载文件后，可以移除隔离属性：

```bash
xattr -d com.apple.quarantine easy_proxies
```
