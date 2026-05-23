# Easy Proxies

基于 [sing-box](https://github.com/SagerNet/sing-box) 的代理池与节点生命周期管理器。导入大批订阅、自动测速、保留失败节点便于重试，将可用节点通过统一池入口或独立端口对外暴露。WebUI 是主要工作流。

> 致谢：本项目脱胎于 [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies)，在其基础上进行了大量重构与功能扩展（节点池过滤、端口对齐、批量异步测试、订阅删除、Multi-Port 凭证 WebUI 编辑等）。

---

## 目录

- [如何使用](#如何使用)
- [运行模式](#运行模式)
- [WebUI 详解](#webui-详解)
- [节点生命周期](#节点生命周期)
- [配置文件](#配置文件)
- [REST API](#rest-api)
- [从源码编译](#从源码编译)
- [支持的协议](#支持的协议)
- [文件与持久化](#文件与持久化)
- [常见问题](#常见问题)

---

## 如何使用

### 1. 启动

确保当前目录有 `easy_proxies.exe`、`config.yaml`、`GeoLite2-Country.mmdb` 三个文件，然后运行：

```powershell
.\easy_proxies.exe -config config.yaml
```

或者直接双击 `easy_proxies.exe`（默认会找同目录下的 `config.yaml`）。

启动后会看到：

```
✅ Monitor server started on http://127.0.0.1:9091
🔌 Multi-Port Entry Points (197 nodes):
   [24000] node-A    HTTP/SOCKS5: 127.0.0.1:24000
   [24001] node-B    HTTP/SOCKS5: 127.0.0.1:24001
   ...
```

### 2. 打开 WebUI

浏览器访问 [http://127.0.0.1:9091](http://127.0.0.1:9091)。

### 3. 导入订阅

进入 **导入节点** 页面：

- **订阅链接格式**：粘贴 `http://...` / `https://...`，每行一个，自动识别 Clash YAML / Base64 / URI 格式
- **URI 格式**：粘贴 `vless://`、`vmess://`、`trojan://` 等节点 URI，每行一个
- **Base64 格式**：粘贴 Base64 编码的节点列表
- **Clash YAML 格式**：粘贴 Clash 配置中的 `proxies:` 部分

填写 **标签前缀**（默认 `local`），用于生成节点名（`local-JP1`、`local-HK2` 这样）。

点击 **解析** → **提交** 完成导入。

### 4. 测速 + 测国家 + 加入节点池

新导入的节点处于 **候选节点** 页面。两种方式处理：

**方式 A：批量测试子页面（推荐）**

进入 **批量测试** 页面：

1. 勾选 **测试范围**（多选）：候选节点 / 节点池 / 失败节点
2. 勾选 **操作**（多选）：测速 / 测试国家
3. 可选勾选 **测速成功后自动加入节点池**
4. 点击 **开始** → 弹出实时进度条（阶段、完成数、成功/失败计数）

**方式 B：单独操作**

- **候选节点** 页：勾选若干节点 → 点 `测速`、`测试国家`、`加入节点池`
- **失败节点** 页：勾选若干节点 → 点 `一键测速`（自动补测国家）
- **节点池** 页：勾选若干节点 → 点 `测速` / `测试国家`，失败的会自动降级到失败节点

> **速度优化**：测速使用 64 并发 + 5 秒超时 + probe/国家并发请求；197 节点全量测速约 30 秒。

### 5. 使用代理

**Multi-Port 模式**（默认）：每个池中节点一个端口，从 `multi_port.base_port`（默认 24000）开始连续编号。

```bash
# 不需要认证（config.yaml 默认 username/password 为空）
curl -x http://127.0.0.1:24000 https://ipinfo.io

# 启用认证后（在 设置 → Multi 用户名/密码 中配置）
curl -x http://mpuser:mppass@127.0.0.1:24000 https://ipinfo.io
```

**Pool 模式**：单一统一入口（默认 `127.0.0.1:2323`），sing-box 内部路由到池中可用节点。

```bash
curl -x http://127.0.0.1:2323 https://ipinfo.io
```

### 6. 管理节点

- **节点池** 页：查看所有在用节点 + 端口；可按国家/标签筛选；支持 `删除选中`
- **候选节点** 页：测速成功但未入池的节点；同样支持筛选 + 删除
- **失败节点** 页：测速失败的节点；可重试，也可批量删除
- **端口状态** 页：扫描本机端口占用情况，节点池占用 / 外部进程占用 / 空闲分别统计

### 7. 订阅管理

进入 **设置** 页：

- **订阅自动刷新**：开关 + 间隔（天/小时/分钟），保存后立即触发一次刷新
- **当前订阅**：显示已记录的所有订阅 `名称：URL`，每条带 `删除` 按钮
  - 删除会同时：① 从 `config.yaml` 移除 URL；② 删除该订阅导入的所有节点（候选+失败+池中）；③ 自动重写 `nodes.txt`

### 8. 关闭

`Ctrl+C` 优雅退出，会等待已有连接 drain 完。

---

## 运行模式

| 模式 | 说明 | 适用场景 |
|------|------|----------|
| `multi-port` | 每个池中节点一个独立端口（24000、24001、...） | 需要每个出口绑定固定端口（比如 IP 业务隔离） |
| `pool` | 一个统一入口（默认 2323），内部按池策略路由 | 普通代理使用，无需关心具体节点 |
| `hybrid` | 同时启用以上两种 | 兼顾灵活性与统一入口 |

在 `config.yaml` 中：

```yaml
mode: multi-port   # 或 pool / hybrid
```

也可在 WebUI **设置** 页热修改并自动 reload。

---

## WebUI 详解

| 页面 | 功能 |
|------|------|
| **导入节点** | 四种格式导入（订阅链接 / URI / Base64 / Clash） |
| **候选节点** | 测速成功但未入池；按国家/标签下拉筛选；批量测速 / 测国家 / 加入池 / 删除 |
| **节点池** | 当前在用节点；显示分配端口；按国家/标签筛选；测速 / 测国家 / 删除 |
| **失败节点** | 测速失败节点；批量重测（成功后自动测国家），可设置成功自动入池 |
| **批量测试** | 范围多选 + 操作多选 + 自动入池开关，弹出实时进度 |
| **端口状态** | 扫描 base_port 起 200 个端口，区分节点池占用 / 其他进程 / 空闲 |
| **日志** | 浏览运行时日志 |
| **设置** | 运行模式 / 监听 / Multi-Port 凭证 / 池策略 / 管理端 / GeoIP / 订阅刷新 / 当前订阅 |

WebUI 修改后会自动持久化到 `config.yaml`，必要时触发 sing-box reload，不需要手动重启。

---

## 节点生命周期

```
导入(parsed) ──测速──► passed ──手动入池──► in_pool ──健康检查失败──► failed
                       │                       ▲                       │
                       └─ 测国家 ──┐           │  ┌──测速成功──────────┘
                                   │           │  │
                                   └────► 测国家成功 (可入池)
```

- 新导入：`state=parsed`
- 测速成功：`state=passed`（加入"候选节点"）
- 加入节点池：`state=in_pool`、`InPool=true`、分配端口
- 健康检查失败：从池移除，`state=failed`
- 失败节点重测成功：回到 `passed`；若选择"自动入池"则自动补测国家并入池

> **重要不变量**：sing-box 实际监听端口数 == WebUI 节点池数 == `config.yaml` 中 pool 节点数。运行期间 pool 增减时，端口会从 `base_port` 重新连续编排。

---

## 配置文件

`config.yaml` 主要字段：

```yaml
mode: multi-port          # pool / multi-port / hybrid

listener:                  # pool 模式的统一入口
  address: 127.0.0.1
  port: 2323
  username: ""             # 空 = 无需认证
  password: ""

multi_port:                # multi-port / hybrid 模式
  address: 127.0.0.1
  base_port: 24000         # 起始端口；池里第 N 个节点的端口 = base_port + N
  username: ""             # 空 = 无需认证；可在 WebUI 设置页修改
  password: ""

pool:                      # 池路由策略
  mode: sequential         # sequential / random
  failure_threshold: 3     # 节点连续失败几次进入黑名单
  blacklist_duration: 24h

management:                # WebUI + REST API 服务
  enabled: true
  listen: 127.0.0.1:9091
  probe_target: http://cp.cloudflare.com/generate_204
  password: ""             # 设置后 WebUI 需要登录

subscription_refresh:
  enabled: true
  interval: 24h            # 自动刷新间隔（最小 5 分钟）
  timeout: 30s
  min_available_nodes: 1

geoip:                     # 可选：按国家路由
  enabled: true
  database_path: ./GeoLite2-Country.mmdb
  listen: ""               # 空 = 不开启分国家路由入口
  port: 0
  auto_update_enabled: true
  auto_update_interval: 24h

log:
  output: stdout           # stdout / file / both
  file: logs/easy_proxies.log
  max_size: 50             # MB
  max_backups: 3
  max_age: 7               # 天
  compress: false

subscriptions:             # 订阅链接列表（WebUI 可增删）
  - https://example.com/sub?token=xxx
  - https://another.com/api/v1/client/subscribe?token=yyy

skip_cert_verify: false    # 跳过上游 TLS 校验（不推荐）
```

---

## REST API

WebUI 背后的全部接口（base URL `http://127.0.0.1:9091`）：

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/settings` | 读取运行设置 |
| `PUT` | `/api/settings` | 修改运行设置（含 multi_port 凭证） |
| `GET` | `/api/subscription/config` | 订阅列表 + 名称映射 + 自动刷新设置 |
| `PUT` | `/api/subscription/config` | 更新订阅列表 / 自动刷新 |
| `POST` | `/api/subscription/delete` | 删除某条订阅及其全部节点 `{url}` |
| `POST` | `/api/subscription/refresh` | 立即刷新 |
| `POST` | `/api/import/parse` | 解析订阅 / URI / Base64 / Clash |
| `POST` | `/api/import/{job}/commit` | 确认导入 |
| `GET` | `/api/nodes/all` / `/api/nodes/pool` / `/api/nodes/failed` | 列出节点 |
| `POST` | `/api/managed-nodes/{id}/{retest\|country\|promote\|exclude\|delete}` | 单节点操作 |
| `POST` | `/api/managed-nodes/batch-test` | 同步批量测试（旧） |
| `POST` | `/api/managed-nodes/batch-test/start` | **异步批量测试，返回 `job_id`** |
| `GET` | `/api/managed-nodes/batch-test/status?id=` | **轮询测试进度** |
| `GET` | `/api/ports/status?from=&to=` | 扫描端口占用 |
| `POST` | `/api/reload` | 手动 reload sing-box |
| `GET` | `/api/logs` | 拉取最近日志 |

异步批量测试响应 schema：

```json
{
  "id": "abcdef123456",
  "status": "running",
  "phase": "probe",
  "total": 197,
  "done": 87,
  "passed": 65,
  "failed": 22,
  "country_ok": 0,
  "country_bad": 0,
  "promoted": 0
}
```

---

## 从源码编译

需要 Go 1.24+：

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies.exe ./cmd/easy_proxies
```

Linux：

```bash
go build -tags "with_clash_api with_utls with_quic" -o easy_proxies ./cmd/easy_proxies
```

构建标签说明：

| Tag | 作用 |
|-----|------|
| `with_clash_api` | sing-box 流量统计 / Clash 兼容 API（管理端需要） |
| `with_utls` | uTLS 指纹伪装（部分协议需要） |
| `with_quic` | QUIC / Hysteria2 / TUIC 支持 |

---

## 支持的协议

VLESS / VMess / Trojan / Shadowsocks / Hysteria2（含端口跳跃）/ TUIC / AnyTLS / SOCKS5 / HTTP / HTTPS

URI 格式遵循 v2rayN URI scheme。

---

## 文件与持久化

| 文件 | 内容 | 是否需要手动管理 |
|------|------|------------------|
| `config.yaml` | 全部配置 | 通常通过 WebUI 修改 |
| `nodes.txt` | 订阅刷新拉到的全部节点 URI | 自动生成，候选库 |
| `managed_nodes.json` | 池中 / 候选 / 失败节点完整状态 | 自动管理，不要手改 |
| `GeoLite2-Country.mmdb` | MaxMind GeoIP 数据库 | 自动定期更新 |
| `logs/easy_proxies.log` | 运行日志（如启用 file output） | 按 max_size/max_backups 滚动 |

**设计原则**：sing-box 只为节点池中的节点开监听。订阅自动刷新只更新 `nodes.txt`（候选库），不会注入 sing-box。只有手动 / 自动 Promote 才会让节点真正占用端口。

---

## 常见问题

**Q: WebUI 显示的端口数 / netstat 看到的监听端口数 / config.yaml 节点数 不一致？**
A: 不应该发生。本项目核心不变量就是三者相等。如果你看到不一致，请重启进程，并提 issue。

**Q: 启动时报 "Port 24000 is in use, trying next port"？**
A: 24000 被其他程序占用了，会自动顺延到下一个可用端口。WebUI **端口状态** 页会标出哪些端口被外部进程占了。

**Q: 重复导入同一个订阅会怎样？**
A: 同 URI 的节点会被覆盖（按 sha256(URI) 主键），**状态会被重置为 parsed**。原本在池里的节点会被降级到候选。

**Q: 测速很慢？**
A: 检查你的网络。默认 probe target 是 `www.gstatic.com/generate_204`（5 秒超时，64 并发）。197 节点全量约 30s。如果远超这个时间，多半是上游订阅给的节点本身大量超时。

**Q: 失败节点测速成功后会自动入池吗？**
A: 默认不会，需要在 **失败节点** 页打开"自动加入到节点池"开关，或在 **批量测试** 页勾选"测速成功后自动加入节点池"。失败节点入池前会自动补测国家。

**Q: 怎么完全删除一条订阅？**
A: **设置 → 当前订阅 → 删除** 按钮。会原子地从 `config.yaml` 移除 URL + 删除所有 `ImportSource` 匹配的节点（候选 + 失败 + 池中）+ 重写 `nodes.txt`。

**Q: 想给管理端加密码？**
A: 编辑 `config.yaml` 里 `management.password`，重启。访问 WebUI 时浏览器会提示 Bearer Token 输入。

---

## License

继承自上游项目 [jasonwong1991/easy_proxies](https://github.com/jasonwong1991/easy_proxies) 的许可证。
