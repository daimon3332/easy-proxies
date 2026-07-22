# Easy Proxies 使用教程

[English](./USER_GUIDE.md) · [简体中文](./USER_GUIDE.zh-CN.md) · [繁體中文](./USER_GUIDE.zh-TW.md)

本教程介绍普通用户通过 Release 使用 Easy Proxies 的完整流程。源码构建流程请查看 [CONTRIBUTING.md](../CONTRIBUTING.md)。

## 1. 选择下载文件

打开[最新 Release](https://github.com/daimon3332/easy-proxies/releases/latest)，根据操作系统和 CPU 选择 ZIP 压缩包：

| 系统 | 常见 CPU | 文件名后缀 |
| --- | --- | --- |
| Windows 64 位电脑 | Intel 或 AMD | `windows-amd64.zip` |
| Windows ARM 设备 | ARM64 | `windows-arm64.zip` |
| Linux 64 位电脑或服务器 | Intel 或 AMD | `linux-amd64.zip` |
| Linux ARM 设备或服务器 | ARM64 | `linux-arm64.zip` |
| Intel Mac | Intel | `macos-amd64.zip` |
| Apple 芯片 Mac | M1/M2/M3/M4 或更新型号 | `macos-arm64.zip` |

建议下载 ZIP，因为其中已经包含可执行文件、`config.example.yaml`、说明文档和许可证。Release 也提供独立可执行文件。

## 2. 解压并创建本地配置

把 ZIP 解压到一个新目录。不要直接编辑 `config.example.yaml`，应将它复制为 `config.yaml`，避免以后更新压缩包时覆盖本地设置。

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

配置模板不包含订阅链接或代理节点。默认模式是 `multi-port`，节点端口从 `24000` 开始，WebUI 监听 `127.0.0.1:9091`。

## 3. 启动 Easy Proxies

Windows PowerShell 或命令提示符：

```powershell
.easy_proxies.exe -config config.yaml
```

Linux：

```bash
chmod +x easy_proxies
./easy_proxies -config config.yaml
```

macOS：

```bash
chmod +x easy_proxies
./easy_proxies -config config.yaml
```

macOS 文件未使用 Apple Developer 证书签名或公证。如果 Gatekeeper 隔离了文件，请先使用 `SHA256SUMS.txt` 核对文件，再运行：

```bash
xattr -d com.apple.quarantine easy_proxies
```

程序运行期间请保持终端窗口开启。

## 4. 打开 WebUI

在浏览器中打开 `http://127.0.0.1:9091`。即使没有预先配置节点，首次启动也可以正常打开 WebUI。

## 5. 导入订阅并测试节点

1. 打开“导入节点”。
2. 保持“订阅链接”类型。
3. 每行粘贴一个订阅 URL。
4. 保持“测速成功后自动加入节点池”开启。
5. 点击“导入并测试”。
6. 等待解析和并发测速完成。
7. 查看测速成功、失败和已入池节点数量。

开启自动入池后，测速成功的节点会直接加入节点池。默认 `multi-port` 模式会为每个池内节点分配独立本地端口。

## 6. 使用生成的代理端口

打开端口页面，复制节点实际分配的地址，例如 `127.0.0.1:24000`。被其他程序占用的端口会自动跳过。

HTTP 代理示例：

```bash
curl -x http://127.0.0.1:24000 https://api.ipify.org
```

SOCKS5 代理示例：

```bash
curl --proxy socks5h://127.0.0.1:24000 https://api.ipify.org
```

具体监听协议以 WebUI 显示为准。

## 7. 停止和再次启动

在终端按 `Ctrl+C` 停止程序。以后继续使用相同的 `-config config.yaml` 命令启动。替换可执行文件时，请保留应用目录中的 `config.yaml` 和运行数据。

## 常见问题

### 提示 `clash api is not included in this build`

请使用官方 Release 文件。自行构建时，需要加入 [CONTRIBUTING.md](../CONTRIBUTING.md) 中说明的 `with_clash_api` 标签。

### 测速成功的节点没有预期端口

请打开端口页面。对应端口可能已被其他程序占用，Easy Proxies 会继续分配下一个可用端口。

### 自动加入节点池没有开启

浏览器会保存这个选项。下次导入前，在导入页面重新开启“测速成功后自动加入节点池”。

### WebUI 打不开

确认程序仍在运行，并检查 `config.yaml` 中的 `management.listen` 是否与浏览器地址一致，同时确认 `9091` 端口未被其他程序占用。
