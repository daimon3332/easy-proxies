# Easy Proxies 使用教程

[English](./USER_GUIDE.md) · [简体中文](./USER_GUIDE.zh-CN.md)

## 1. 获取程序

### 方式 A：下载 Release

打开 [最新 Release](https://github.com/daimon3332/easy-proxies/releases/latest)，下载与系统匹配的 ZIP 并解压到新文件夹：

| 系统 | 文件后缀 |
| --- | --- |
| Windows（Intel/AMD） | `windows-amd64.zip` |
| Windows ARM | `windows-arm64.zip` |
| Linux（Intel/AMD） | `linux-amd64.zip` |
| Linux ARM64 | `linux-arm64.zip` |

### 方式 B：从源码构建

需要 Git 和 Go 1.24.4 或兼容的 Go 1.24 工具链。

```bash
git clone https://github.com/daimon3332/easy-proxies.git
cd easy-proxies
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies .
```

Windows 下改用 `-o easy_proxies.exe`。这些 tags 用于启用 uTLS/Reality、基于 QUIC 的协议（Hysteria2、TUIC）以及可选的 Clash API；Clash API 默认关闭，设置环境变量 `EASY_PROXIES_CLASH_API_LISTEN`（如 `127.0.0.1:9092`）后才启用。

## 2. 创建配置

在程序所在目录复制模板：

```bash
cp config.example.yaml config.yaml          # Linux
Copy-Item config.example.yaml config.yaml   # Windows PowerShell
```

## 3. 启动

```bash
./easy_proxies -config config.yaml          # Linux（先执行 chmod +x easy_proxies）
.\easy_proxies.exe -config config.yaml      # Windows
```

保持终端窗口开启，然后在浏览器打开 `management.listen` 地址（默认 `http://127.0.0.1:9091`）。

## 常见问题

**找不到 `config.yaml`**：在包含 `config.yaml` 的目录启动程序，或给 `-config` 传入完整路径。

**提示 `another easy_proxies instance is using this config`**：同一份配置只允许一个进程使用。新进程会最多等待 40 秒让旧进程退出；如果旧进程仍在运行，请先关闭它。

**程序启动后立即退出**：查看终端输出。常见原因是 `config.yaml` 格式错误、管理端口（默认 `9091`）已被占用，或程序与操作系统/CPU 架构不匹配。

**提示 `clash api is not included in this build`**：按上面的 tags 重新构建，或使用官方 Release 包。
