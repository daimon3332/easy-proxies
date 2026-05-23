# Easy Proxies

[English](README.md) | 简体中文

Easy Proxies 是一个基于 sing-box 的代理池管理工具。目标是把大量上游节点统一成稳定的本地 HTTP/SOCKS5 代理入口，同时支持按节点独立端口访问。

## 核心功能

### 运行模式
- **`pool`** — 单端口代理池，所有节点共享一个本地 HTTP/SOCKS5 入口，自动负载均衡
- **`multi-port`** — 多端口模式，每个节点独立本地 HTTP/SOCKS5 端口
- **`hybrid`** — 混合模式，同时启用 pool + multi-port

### 协议支持
VMess, VLESS, Trojan, Shadowsocks(SS), Hysteria2(HY2), TUIC, AnyTLS, SOCKS5, HTTP/HTTPS

### 节点来源
- **config.yaml 内联节点** — 直接在 `nodes` 段定义
- **nodes_file** — 每行一个代理 URI 的外部文件
- **订阅 URL** — 支持 Base64 编码、纯文本 URI 列表、Clash YAML（含 JSON inline 格式）
- **WebUI 导入** — 通过管理面板粘贴订阅内容或输入订阅 URL，支持 Tag 前缀，并直接导入测试

### 节点生命周期管理（新增）
```
Import → Parse → Test (generate_204) → GeoIP (ipinfo.io via proxy) → Rename → Add to Pool
```

1. **导入解析** — 支持 URL 拉取和内容粘贴 4 种格式
2. **直接提交测试** — WebUI 解析成功后直接提交全部节点
3. **连通性测试** — 为每个导入节点创建临时 sing-box 实例，通过 `generate_204` 探测可用性
4. **出口 IP 检测** — 通过代理访问 `ipinfo.io` 获取真实出口国家，自动重命名为 `[国家代码] 前缀-节点名`
5. **智能入池** — 测试通过的节点自动加入节点池；失败节点保留但排除在池外，支持后续重新测试

**节点状态机**:
```
parsed → testing → passed → in_pool
                  ↘ failed → testing (retest)
                           → excluded
in_pool → excluded | testing (retest)
```

### 端口管理（新增）
- 自动扫描端口可用性，WebUI 实时展示占用状态
- 按国家排序节点，支持拖拽重排
- 端口冲突自动检测（配置占用 / OS 占用 / 监听冲突）

### WebUI 管理面板
- **导入订阅** — 输入 URL 或粘贴 URI/Base64/Clash YAML 后直接导入并测试，实时进度反馈
- **托管节点** — 搜索/筛选、状态徽章、延迟颜色标识、国家标识
- **节点池和失败节点** — 重新测试、加入池、排除、保存排序
- **节点配置** — CRUD 增删改查
- **端口状态** — 端口占用一览
- **日志查看** — 实时日志控制台
- **系统设置** — 密码、探测目标、外部 IP、订阅配置

### 其他功能
- **自动健康检查** — 可配置故障阈值和黑名单时长
- **GeoIP 地域路由** — 按国家分类节点，通过 `/jp`, `/us`, `/hk` 等路径路由
- **订阅自动刷新** — 定时拉取 + 热重载，无需重启
- **可配置 DNS** — 支持主备 DNS、IPv4/IPv6 策略
- **日志轮转** — 大小限制、备份数量、保留天数、压缩
- **会话认证** — WebUI 密码保护，24 小时 token 过期

## 快速开始

### 1）准备配置

```bash
cp config.example.yaml config.yaml
touch nodes.txt
```

编辑 `config.yaml`，配置节点来源。

### 2）启动

Docker：
```bash
./start.sh
# 或
docker compose up -d
```

本地运行：
```bash
go build -tags with_clash_api -o easy_proxies ./cmd/easy_proxies/
./easy_proxies -config config.yaml
```

> `with_clash_api` 编译标签是 sing-box 必需的，否则启动会报错。

### 3）访问 WebUI

打开 `http://localhost:9091`（默认管理地址）

## 配置参考

### 最小配置（Pool 模式）

```yaml
mode: pool

listener:
  address: 0.0.0.0
  port: 2323
  username: user
  password: pass

pool:
  mode: random
  failure_threshold: 3
  blacklist_duration: 24h

management:
  enabled: true
  listen: 0.0.0.0:9091
  probe_target: http://cp.cloudflare.com/generate_204
  password: ""

nodes_file: nodes.txt
```

### Multi-Port 模式

```yaml
mode: multi-port

multi_port:
  address: 0.0.0.0
  base_port: 24000
  username: user
  password: pass

management:
  enabled: true
  listen: 0.0.0.0:9091
```

### 订阅模式

```yaml
subscriptions:
  - https://your-subscription-url.com/sub?token=xxx

subscription_refresh:
  enabled: true
  interval: 3h
  timeout: 30s
  min_available_nodes: 3
```

### GeoIP 地域路由

```yaml
geoip:
  enabled: true
  database_path: ./GeoLite2-Country.mmdb
  route_listen: 0.0.0.0:1221
  auto_update_enabled: true
  auto_update_interval: 24h
```

### DNS 配置

```yaml
dns:
  server: 223.5.5.5
  fallback_servers:
    - 8.8.8.8
    - 1.1.1.1
  port: 53
  strategy: prefer_ipv4
```

`strategy` 可选值：`as_is` / `prefer_ipv4` / `prefer_ipv6` / `ipv4_only` / `ipv6_only`

### 日志配置

```yaml
log:
  output: file          # stdout | file
  file: logs/easy_proxies.log
  max_size: 50          # MB
  max_backups: 3
  max_age: 7            # 天
  compress: false

log_level: info         # debug | info | warn | error
```

## 管理 API

### 认证
| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/auth` | 登录获取 token `{"password":"xxx"}` |

### 运行时节点（健康检查快照）
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/nodes` | 节点运行时状态 |
| POST | `/api/nodes/{tag}/probe` | 探测单个节点 |
| POST | `/api/nodes/{tag}/release` | 解封单个节点 |
| POST | `/api/nodes/{tag}/blacklist` | 拉黑单个节点 |
| POST | `/api/nodes/probe-all` | 全量探测 (SSE) |

### 导入和托管节点（新增）
| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/import/parse` | 解析订阅内容 `{"mode":"url\|content","url":"...","content":"...","tag_prefix":"local"}` |
| POST | `/api/import/{import_id}/commit` | 确认导入并启动测试流水线 |
| GET | `/api/import/jobs/{job_id}` | 查询导入任务进度 |
| GET | `/api/nodes/all` | 所有托管节点（含 failed/excluded） |
| GET | `/api/nodes/pool` | 仅池内节点（state=in_pool） |
| GET | `/api/nodes/failed` | 仅失败节点 |
| PUT | `/api/nodes/order` | 拖拽排序 `{"order":["id1","id2"]}` |
| POST | `/api/managed-nodes/{id}/retest` | 重新测试节点 |
| POST | `/api/managed-nodes/{id}/promote` | 手动加入节点池 |
| POST | `/api/managed-nodes/{id}/exclude` | 排除节点 |

### 端口管理（新增）
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/ports/status?from=&to=` | 端口可用性扫描 |

### 订阅管理
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/subscription/status` | 订阅刷新状态 |
| POST | `/api/subscription/refresh` | 手动刷新 |
| GET | `/api/subscription/config` | 获取订阅配置 |
| POST | `/api/subscription/config` | 更新订阅配置（即时生效） |

### 配置管理
| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/nodes/config` | 获取配置节点列表 |
| POST | `/api/nodes/config` | 添加配置节点 |
| PUT | `/api/nodes/config/{name}` | 更新配置节点 |
| DELETE | `/api/nodes/config/{name}` | 删除配置节点 |
| POST | `/api/reload` | 重载 sing-box 核心 |
| GET | `/api/settings` | 获取全局设置 |
| POST | `/api/settings` | 更新全局设置 |
| GET | `/api/export` | 导出节点配置 |
| GET | `/api/traffic` | 实时流量 (SSE) |
| GET | `/api/logs` | 最近日志 |

### 导入工作流示例

```bash
# 1. 解析订阅
curl -X POST http://localhost:9091/api/import/parse \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"url","url":"https://sub.example.com/sub","tag_prefix":"my"}'

# 返回:
# {
#   "import_id": "a1b2c3d4e5f6",
#   "format": "clash_yaml",
#   "nodes": [
#     {"id": "abc123...", "name": "my-node1", "uri": "trojan://...", "state": "parsed", ...},
#     {"id": "def456...", "name": "my-node2", "uri": "vless://...", "state": "parsed", ...}
#   ]
# }

# 2. 提交导入（WebUI 会自动提交全部解析节点）
curl -X POST http://localhost:9091/api/import/a1b2c3d4e5f6/commit \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"node_ids":["abc123...","def456..."],"auto_reload":true}'

# 返回: {"job_id": "x1y2z3w4v5u6"}

# 3. 轮询进度
curl http://localhost:9091/api/import/jobs/x1y2z3w4v5u6 \
  -H "Authorization: Bearer $TOKEN"

# 返回: {"id":"x1y2z3...","status":"completed","total":2,"passed":2,"failed":0,...}
```

## 订阅格式支持

| 格式 | 说明 | 示例 |
|------|------|------|
| 纯文本 URI | 每行一个代理链接 | `vless://uuid@ip:port?security=reality...` |
| Base64 编码 | V2Ray 订阅常见格式 | `dmxlc3M6Ly91dWlk...` |
| Clash YAML | Clash/Mihomo 订阅 | `proxies:\n  - {name: "xx", type: trojan, ...}` |
| Clash JSON inline | YAML + JSON 混合 | `proxies:\n  - {"name":"xx","type":"trojan",...}` |

## 注意事项

- 重载（`/api/reload` 或订阅刷新）会中断现有连接
- Settings API 会写回 `config.yaml`；部分设置需重载生效
- 托管节点持久化在 `managed_nodes.json`（与 config.yaml 同目录）
- GeoIP 数据库首次启动自动下载（~9MB），下载失败不影响导入功能
- `management.password` 为空时不要求登录

## 开发

```bash
# 编译
go build -tags with_clash_api -o easy_proxies ./cmd/easy_proxies/

# 测试
go test ./...

# 代码检查
go vet ./...
```

## 许可证

MIT License
