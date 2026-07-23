# Easy Proxies 使用教學

[English](./USER_GUIDE.md) · [简体中文](./USER_GUIDE.zh-CN.md) · [繁體中文](./USER_GUIDE.zh-TW.md)

本教學介紹準備與啟動 Easy Proxies 的兩種方法。教學在程式啟動後結束，不包含 WebUI 的使用流程。

## 方法一：從原始碼建置

### 1. 準備環境

- Git
- Go 1.24.4，或相容的 Go 1.24 版本

### 2. 複製專案到本機

```bash
git clone https://github.com/daimon3332/easy-proxies.git
cd easy-proxies
```

### 3. 建立本機設定

複製設定範本，不要直接修改範本檔案。

Windows PowerShell：

```powershell
Copy-Item config.example.yaml config.yaml
```

Windows 命令提示字元：

```batch
copy config.example.yaml config.yaml
```

Linux 或 macOS：

```bash
cp config.example.yaml config.yaml
```

### 4. 自行建置 Easy Proxies

Windows PowerShell 或命令提示字元：

```powershell
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies.exe .
```

Linux 或 macOS：

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies .
```

其中 `with_clash_api` 用於啟用內建 Clash API，其他 tags 用於啟用 uTLS/Reality 相關能力和基於 QUIC 的協定支援。

### 5. 啟動自行建置的程式

Windows：

```powershell
.\easy_proxies.exe -config config.yaml
```

Linux 或 macOS：

```bash
chmod +x easy_proxies
./easy_proxies -config config.yaml
```

程式執行期間請保持終端機視窗開啟。

## 方法二：下載 Release

### 1. 選擇下載檔案

開啟[最新 Release](https://github.com/daimon3332/easy-proxies/releases/latest)，依照作業系統和 CPU 選擇對應的 ZIP 壓縮包：

| 系統 | 常見 CPU | 檔名後綴 |
| --- | --- | --- |
| Windows 64 位元電腦 | Intel 或 AMD | `windows-amd64.zip` |
| Windows ARM 裝置 | ARM64 | `windows-arm64.zip` |
| Linux 64 位元電腦或伺服器 | Intel 或 AMD | `linux-amd64.zip` |
| Linux ARM 裝置或伺服器 | ARM64 | `linux-arm64.zip` |
| Intel Mac | Intel | `macos-amd64.zip` |
| Apple 晶片 Mac | M1/M2/M3/M4 或更新型號 | `macos-arm64.zip` |

ZIP 壓縮包已經包含執行檔，此方法不需要本機建置。

### 2. 解壓縮並建立設定

將 ZIP 解壓縮到新目錄，然後複製設定範本。

Windows PowerShell：

```powershell
Copy-Item config.example.yaml config.yaml
```

Windows 命令提示字元：

```batch
copy config.example.yaml config.yaml
```

Linux 或 macOS：

```bash
cp config.example.yaml config.yaml
```

### 3. 啟動 Easy Proxies

Windows：

```powershell
.\easy_proxies.exe -config config.yaml
```

Linux 或 macOS：

```bash
chmod +x easy_proxies
./easy_proxies -config config.yaml
```

程式執行期間請保持終端機視窗開啟。

## 常見問題

### 顯示 `clash api is not included in this build`

方法一需要使用以下指令重新建置：

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies .
```

方法二請重新下載官方 Release 壓縮包，不要使用缺少建置 tags 的程式。

### 找不到 `config.yaml`

請在包含 `config.example.yaml` 的目錄中執行複製指令，並在同一目錄啟動 Easy Proxies。

### 程式啟動後立即退出

先查看終端機中的錯誤訊息，確認 `config.yaml` 格式正確、所需連接埠未被占用，並確認程式與作業系統和 CPU 架構相符。

### WebUI 無法開啟

確認程式仍在執行，並檢查 `config.yaml` 中的 `management.listen` 是否與瀏覽器位址一致，同時確認 `9091` 連接埠未被其他程式占用。

### macOS 阻止執行程式

macOS 檔案未使用 Apple Developer 憑證簽署或公證。使用 `SHA256SUMS.txt` 核對下載檔案後，可以移除隔離屬性：

```bash
xattr -d com.apple.quarantine easy_proxies
```
