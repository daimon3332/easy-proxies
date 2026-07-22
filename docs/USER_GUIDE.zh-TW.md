# Easy Proxies 使用教學

[English](./USER_GUIDE.md) · [简体中文](./USER_GUIDE.zh-CN.md) · [繁體中文](./USER_GUIDE.zh-TW.md)

本教學介紹一般使用者透過 Release 使用 Easy Proxies 的完整流程。原始碼建置流程請參閱 [CONTRIBUTING.md](../CONTRIBUTING.md)。

## 1. 選擇下載檔案

開啟[最新 Release](https://github.com/daimon3332/easy-proxies/releases/latest)，依照作業系統和 CPU 選擇 ZIP 壓縮包：

| 系統 | 常見 CPU | 檔名後綴 |
| --- | --- | --- |
| Windows 64 位元電腦 | Intel 或 AMD | `windows-amd64.zip` |
| Windows ARM 裝置 | ARM64 | `windows-arm64.zip` |
| Linux 64 位元電腦或伺服器 | Intel 或 AMD | `linux-amd64.zip` |
| Linux ARM 裝置或伺服器 | ARM64 | `linux-arm64.zip` |
| Intel Mac | Intel | `macos-amd64.zip` |
| Apple 晶片 Mac | M1/M2/M3/M4 或更新型號 | `macos-arm64.zip` |

建議下載 ZIP，因為其中已包含執行檔、`config.example.yaml`、說明文件和授權條款。Release 也提供獨立執行檔。

## 2. 解壓縮並建立本機設定

將 ZIP 解壓縮到新目錄。不要直接編輯 `config.example.yaml`，應將它複製為 `config.yaml`，避免日後更新壓縮包時覆蓋本機設定。

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

設定範本不包含訂閱連結或代理節點。預設模式為 `multi-port`，節點連接埠從 `24000` 開始，WebUI 監聽 `127.0.0.1:9091`。

## 3. 啟動 Easy Proxies

Windows PowerShell 或命令提示字元：

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

macOS 檔案未使用 Apple Developer 憑證簽署或公證。如果 Gatekeeper 隔離了檔案，請先使用 `SHA256SUMS.txt` 核對檔案，再執行：

```bash
xattr -d com.apple.quarantine easy_proxies
```

程式執行期間請保持終端機視窗開啟。

## 4. 開啟 WebUI

在瀏覽器中開啟 `http://127.0.0.1:9091`。即使沒有預先設定節點，首次啟動仍可正常開啟 WebUI。

## 5. 匯入訂閱並測試節點

1. 開啟「匯入節點」。
2. 保持「訂閱連結」類型。
3. 每行貼上一個訂閱 URL。
4. 保持「測速成功後自動加入節點池」開啟。
5. 點選「匯入並測試」。
6. 等待解析和並行測速完成。
7. 查看測速成功、失敗和已加入節點池的數量。

開啟自動加入節點池後，測速成功的節點會直接加入節點池。預設 `multi-port` 模式會為每個池內節點分配獨立本機連接埠。

## 6. 使用產生的代理連接埠

開啟連接埠頁面，複製節點實際分配的位址，例如 `127.0.0.1:24000`。被其他程式占用的連接埠會自動略過。

HTTP 代理範例：

```bash
curl -x http://127.0.0.1:24000 https://api.ipify.org
```

SOCKS5 代理範例：

```bash
curl --proxy socks5h://127.0.0.1:24000 https://api.ipify.org
```

實際監聽協定以 WebUI 顯示為準。

## 7. 停止和再次啟動

在終端機按 `Ctrl+C` 停止程式。日後繼續使用相同的 `-config config.yaml` 指令啟動。替換執行檔時，請保留應用程式目錄中的 `config.yaml` 和執行資料。

## 常見問題

### 顯示 `clash api is not included in this build`

請使用官方 Release 檔案。自行建置時，需要加入 [CONTRIBUTING.md](../CONTRIBUTING.md) 中說明的 `with_clash_api` 標籤。

### 測速成功的節點沒有預期連接埠

請開啟連接埠頁面。對應連接埠可能已被其他程式占用，Easy Proxies 會繼續分配下一個可用連接埠。

### 自動加入節點池沒有開啟

瀏覽器會儲存這個選項。下次匯入前，在匯入頁面重新開啟「測速成功後自動加入節點池」。

### WebUI 無法開啟

確認程式仍在執行，並檢查 `config.yaml` 中的 `management.listen` 是否與瀏覽器位址一致，同時確認 `9091` 連接埠未被其他程式占用。
