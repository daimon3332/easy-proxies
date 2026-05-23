# Implementation Plan: 订阅导入 + WebUI 重塑

> Codex Session: 019e4ecd-32b2-7aa1-a036-aec587d759a5 | 方案 A: Staging NodeStore

## 文件级实施计划

### Layer 1 — 无依赖，可并行

| 文件 | 类型 | 行数 | 内容 |
|------|------|------|------|
| `internal/importer/types.go` | 新增 | ~180 | ManagedNode, ImportJob, ParseRequest/Response, CommitRequest/Response, TestResult, 状态常量 |
| `internal/importer/store.go` | 新增 | ~240 | JSON NodeStore: CRUD, ListPool/Failed, SetOrder, Save/Load |
| `internal/builder/node.go` | 新增 | ~20 | 导出 BuildSingleNodeOutbound() |
| `internal/monitor/ports_handlers.go` | 新增 | ~120 | GET /api/ports/status handler |

### Layer 2 — 依赖 Layer 1

| 文件 | 类型 | 行数 | 内容 |
|------|------|------|------|
| `internal/importer/tester.go` | 新增 | ~360 | NodeTester: 临时 sing-box outbound, generate_204, ipinfo.io via proxy |
| `internal/importer/service.go` | 新增 | ~320 | ImportService: Parse/Commit/Retest/Promote/Exclude/Reorder |

### Layer 3 — 最终集成

| 文件 | 类型 | 行数 | 内容 |
|------|------|------|------|
| `internal/monitor/server.go` | 修改 | +50 | ImportService 接口, 字段, setter, 路由注册 |
| `internal/monitor/import_handlers.go` | 新增 | ~260 | 所有 import/managed-nodes API handlers |
| `internal/app/app.go` | 修改 | +40 | 初始化 Store/Tester/Service, 注入 monitor server |
| `internal/monitor/assets/index.html` | 重写 | ~1500 | 三栏布局 WebUI |

## API 设计

```
POST /api/import/parse                    — 预览解析（不测试、不加入 pool）
POST /api/import/{import_id}/commit       — 启动测试+加入 pool 异步任务
GET  /api/import/jobs/{job_id}            — 轮询 job 进度

GET  /api/nodes/all                       — 所有 ManagedNode
GET  /api/nodes/pool                      — 仅 in_pool 节点
GET  /api/nodes/failed                    — 仅 failed 节点
PUT  /api/nodes/order                     — 拖拽排序

POST /api/managed-nodes/{node_id}/retest  — 重新测试
POST /api/managed-nodes/{node_id}/promote — 手动加入 pool
POST /api/managed-nodes/{node_id}/exclude — 排除

GET  /api/ports/status?from=&to=          — 端口可用性
```

## 节点状态模型

```
parsed → testing → passed → in_pool
                 ↘ failed → testing (retest)
                          → excluded
in_pool → excluded | testing (retest)
```

## NodeTester 设计
- 为每个测试节点启动临时 sing-box 实例（127.0.0.1 随机端口 mixed inbound）
- 通过 http.Client Proxy 访问 generate_204 + ipinfo.io
- 并发: min(8, NumCPU*2), 单节点超时 20s

## WebUI 三栏布局
```
┌──────────┬────────────────┬────────────────────┐
│ Left Nav │ Middle List    │ Right Detail       │
├──────────┼────────────────┼────────────────────┤
│All Nodes │ searchable     │ node detail/test   │
│Node Pool │ pool candidates│ pool status/port   │
│Groups    │ country/group  │ group rules/order  │
│Settings  │ settings list  │ forms/subscription │
└──────────┴────────────────┴────────────────────┘
```
- Vanilla JS, CSS Grid, CSS custom properties 主题
- 全局 state object + render() 模式
- 响应式: Desktop 三栏 / Tablet 隐藏右栏 / Mobile 单栏

## 架构决策
- **选择**: 方案 A (staging NodeStore)，预留向方案 B 演进
- **拒绝**: 扩展 NodeConfig (污染配置)、统一状态中心 (改动过大)、blacklist 表示 excluded (pool 会自动 release)
- **持久化**: managed_nodes.json (与 config.yaml 同目录)
- **兼容**: 保留所有现有 API，新 UI 逐步迁移
