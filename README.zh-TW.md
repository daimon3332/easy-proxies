<p align="center">
  <img src="./internal/monitor/assets/logo.png" width="128" alt="Easy Proxies Logo">
</p>

<h1 align="center">Easy Proxies</h1>

<p align="center">以 sing-box 為基礎的訂閱匯入、節點測速、節點池管理與多連接埠代理工具。</p>

<p align="center">
  <a href="./README.md">English</a> ·
  <a href="./README.zh-CN.md">简体中文</a> ·
  <a href="./README.zh-TW.md">繁體中文</a>
</p>

<p align="center">
  <img alt="Go 1.24+" src="https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white">
  <img alt="MIT License" src="https://img.shields.io/badge/License-MIT-green.svg">
  <img alt="Powered by sing-box" src="https://img.shields.io/badge/Powered%20by-sing--box-4B5563">
  <img alt="Platforms" src="https://img.shields.io/badge/Platform-Windows%20%7C%20Linux%20%7C%20macOS-blue">
</p>

> 本專案基於 [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies) 二次開發，重點改善 WebUI、訂閱匯入、節點測速、節點生命週期管理與多連接埠使用體驗。

## 專案用途

Easy Proxies 可以把一個或多個代理訂閱 URL 轉換成本機 HTTP/SOCKS5 代理連接埠：

```text
貼上訂閱 URL
  -> 解析節點
  -> 測試全部節點
  -> 成功節點自動加入節點池
  -> 從 24000 開始分配本機連接埠
  -> 複製連接埠並直接使用
```

預設執行模式是 `multi-port`，每個測速成功並進入節點池的節點都會取得獨立本機連接埠。首次使用時，「測速成功後自動加入節點池」預設開啟。

## 核心功能

- 面向一般使用者的訂閱優先 WebUI 流程。
- 支援 HTTP/HTTPS 訂閱、URI 清單、Base64 內容和 Clash/Mihomo YAML。
- 一次匯入多個訂閱 URL。
- 並行、非同步節點測速和即時進度。
- 分別保留候選節點、節點池節點和失敗節點。
- 匯入測速成功後預設自動加入節點池。
- 預設 `multi-port` 模式下每個節點使用獨立連接埠。
- 可選 `pool` 和 `hybrid` 模式。
- 支援批次重測、國家檢測、訂閱重新整理、連接埠檢視和執行日誌。
- 探測目標僅支援：
  - `https://www.gstatic.com/generate_204`
  - `https://cp.cloudflare.com/generate_204`
- WebUI 與 REST API 共用管理入口。

## 快速開始

### 環境需求

- Go 1.24 或更新版本
- Windows、Linux 或 macOS

### Windows

```powershell
Copy-Item config.example.yaml config.yaml
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies.exe ./cmd/easy_proxies
.\easy_proxies.exe -config config.yaml
```

### Linux / macOS

```bash
cp config.example.yaml config.yaml
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies ./cmd/easy_proxies
./easy_proxies -config config.yaml
```

瀏覽器開啟：

```text
http://127.0.0.1:9091
```

首次啟動不需要預先設定節點；空節點狀態下 WebUI 仍可開啟。

## 最常見的使用流程

1. 在 WebUI 開啟「匯入節點」。
2. 保持「訂閱連結」格式。
3. 每行貼上一個訂閱 URL。
4. 保持「測速成功後自動加入節點池」開啟。
5. 點選「匯入並測試」。
6. 等待解析和測速完成。
7. 複製產生的位址，例如 `127.0.0.1:24000`。

HTTP 代理範例：

```bash
curl -x http://127.0.0.1:24000 https://api.ipify.org
```

SOCKS5 範例：

```bash
curl --proxy socks5h://127.0.0.1:24000 https://api.ipify.org
```

被其他程式占用的連接埠會自動略過，實際分配以 WebUI 的連接埠狀態頁面為準。

## 匯入格式與協定

支援的匯入格式：

- HTTP/HTTPS 訂閱 URL
- 代理 URI 清單
- Base64 編碼 URI 清單
- Clash/Mihomo YAML 的 `proxies` 區段

常見協定包括 VLESS、VMess、Trojan、Shadowsocks、ShadowsocksR、Hysteria、Hysteria2、TUIC、AnyTLS、HTTP/HTTPS、SOCKS4 和 SOCKS5。實際協定能力取決於 sing-box 版本與建置標籤。

## 執行模式

| 模式 | 行為 |
| --- | --- |
| `multi-port` | 預設模式，每個節點分配一個本機連接埠。 |
| `pool` | 所有節點共用一個代理入口，由節點池排程。 |
| `hybrid` | 同時啟用共用入口和每節點獨立連接埠。 |

設定中的 `multi_port` 寫法也受支援，並會自動正規化為 `multi-port`。

## 設定

啟動前將 `config.example.yaml` 複製為 `config.yaml`。範本不包含訂閱或節點資訊，預設設定為：

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

設定頁面可以修改執行模式、監聽位址、連接埠、驗證資訊、節點池策略、探測目標、黑名單秒數和輪換秒數。

## 建置標籤

| 標籤 | 用途 |
| --- | --- |
| `with_clash_api` | Clash API 整合所需。 |
| `with_utls` | 啟用 uTLS/Reality 相關能力。 |
| `with_quic` | 啟用 Hysteria2、TUIC 等 QUIC 協定。 |

上面的 Windows 建置命令已包含這三個標籤。

## 資料與隱私

以下檔案可能包含訂閱 URL、憑證、節點 URI、執行狀態或本機日誌，已被 Git 忽略：

```text
config.yaml
nodes.txt
managed_nodes.json
node_ports.json
*.log
*.mmdb
*.exe
```

提交程式碼時請使用 `config.example.yaml`。公開既有儲存庫前還需要檢查完整 Git 歷史，因為加入 `.gitignore` 不會刪除舊提交中的檔案。

## 常見問題

### 啟動提示 `clash api is not included in this build`

使用上面的 Windows 命令重新建置，或在 Go 建置參數中加入 `with_clash_api`。

### 節點沒有使用預期連接埠

開啟連接埠狀態頁面。被其他程序占用的連接埠會自動略過。

### 自動加入節點池沒有開啟

WebUI 會在瀏覽器中保存使用者選擇；在匯入頁面重新勾選即可恢復自動加入節點池。

## 上游專案與致謝

- [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies) — 上游專案
- [SagerNet/sing-box](https://github.com/SagerNet/sing-box) — 代理平台與協定實作

## 授權條款

本專案採用 [MIT License](./LICENSE)，並保留對上游專案及其 MIT 授權程式碼的歸屬說明。
