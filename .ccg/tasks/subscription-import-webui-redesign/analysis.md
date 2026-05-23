# Analysis: 订阅导入 + WebUI 重塑

## Codex Backend Analysis (Session: 019e4ecd-32b2-7aa1-a036-aec587d759a5)

### 核心发现
当前系统缺少"节点导入与生命周期状态层"。节点从 config.Nodes 直接进入 sing-box pool，没有 staging/failed 持久层。

### 三种方案对比

| 方案 | 做法 | 优点 | 缺点 |
|------|------|------|------|
| **A: 最小侵入** | 新增 staging NodeStore，passed 节点写入 config.Nodes | 风险低、交付快、旧 API 兼容 | 双状态同步 |
| **B: 统一状态中心** | NodeStore 成为权威来源，config.Nodes 由它生成 | 最符合长期目标 | 改动大、迁移复杂 |
| **C: 扩展 NodeConfig** | 在 NodeConfig 增加 Enabled/Status/Country/Order 字段 | 状态配置合一 | 污染配置格式、nodes.txt 丢失字段 |

**Codex 推荐**: 方案 A（预留向 B 演进）

### 推荐新增模块
```
internal/importer/
  service.go    // ImportService: Parse/Commit/Pipeline
  store.go      // NodeStore: imported/failed/active 持久化
  tester.go     // NodeTester: generate_204 + ipinfo.io via proxy
  pipeline.go   // async job + SSE events
```

### 推荐 API
```
POST /api/import/parse          — 预览解析结果
POST /api/import/{id}/commit    — 确认导入并启动测试
GET  /api/import/jobs/{id}      — 查询 job 状态
GET  /api/nodes/all             — 所有节点（含 failed）
GET  /api/nodes/pool            — 仅 pool 内节点
POST /api/nodes/{id}/retest     — 重新测试
POST /api/nodes/{id}/promote    — 手动加入 pool
PUT  /api/nodes/order           — 拖拽排序
GET  /api/ports/status          — 端口可用性
```

### 依赖顺序
F1 Parse → F2 Pipeline/Store → F3 Port/Order → F4 WebUI

### 风险清单
- 订阅刷新可能覆盖导入节点 → 新增 NodeSourceImport
- builder tag 基于 Name，rename 后旧 tag 失效 → 新 API 用 stable node_id
- pool 只排除 blacklist → failed 节点不构建进 pool
- 端口冲突在 reload 才发现 → 创建时先检查 IsPortAvailable()

---

## Claude Frontend Analysis (Antigravity 不可用，降级)

### WebUI 架构决策
- **选择 Vanilla JS** — 单文件 embed 约束下，引入框架增加体积且无构建步骤
- CSS Grid 三栏布局，CSS custom properties 实现主题切换
- 事件驱动状态管理（简单 EventBus + state object）

### 组件层级
```
App
├── Sidebar (左导航, 固定宽度 200px)
│   ├── NavItem: All Nodes
│   ├── NavItem: Node Pool
│   ├── NavItem: Groups
│   └── NavItem: Settings
├── ListView (中列表, flex-grow)
│   ├── SearchBar + FilterDropdown
│   ├── NodeList / PoolList / GroupList / SettingsSections
│   └── ImportButton (触发 modal)
└── DetailPanel (右详情, 固定宽度 400px)
    ├── NodeDetail (状态/延迟/GeoIP/端口/操作按钮)
    ├── PoolDetail (端口排序/拖拽)
    ├── GroupDetail (成员管理)
    └── SettingsForm
```

### 导入交互流程
```
[Import Button] → Modal Dialog
  ├── Tab: URL (输入框 + fetch)
  ├── Tab: Paste (textarea)
  ├── Tag Prefix 输入 (default: "local")
  └── [Parse] → Preview Table
       ├── 节点列表 (name/protocol/server)
       ├── 全选/反选
       └── [Confirm Import] → SSE Progress
            ├── Testing... (generate_204)
            ├── GeoIP detecting...
            ├── Renaming...
            └── Done: X passed, Y failed
```

### 响应式设计
- Desktop: 三栏并排
- Tablet (<1024px): 隐藏右栏，点击展开
- Mobile (<768px): 单栏，底部 tab 导航
