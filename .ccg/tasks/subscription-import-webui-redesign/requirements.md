# Requirements: 订阅导入 + WebUI 重塑

## F1: 订阅导入
- 支持 4 种格式：link/txt（每行一 URI）、Clash YAML、Base64、单行 URI
- WebUI 驱动：输入 URL 或粘贴内容 + tag 前缀（默认 "local"）
- 解析后预览节点列表，用户确认后添加
- 已有 `parseSubscriptionContent()` 支持所有格式（config.go:630-665）

## F2: 节点生命周期流水线
- Import → Parse → Test (generate_204) → GeoIP (ipinfo.io via proxy) → Rename (country prefix) → Add to pool
- 失败节点保留但不进入 pool 候选
- 后续可重新测试并添加
- 已有 `CreateNode()` (boxmgr:706)、`Register()`/`Probe()` (monitor/manager.go)

## F3: 端口管理
- 自动检测端口可用性（已有 `IsPortAvailable()`）
- WebUI 显示不可用端口
- 按国家排序 + 拖拽排序

## F4: WebUI 完全重塑
- 三栏侧边栏布局（左导航 → 中列表 → 右详情）
- 左侧导航：所有节点、节点池、分组管理、设置
- 当前 WebUI 为单文件嵌入 HTML (index.html, 1753 行)

## 技术约束
- Go 1.24.1 + sing-box v1.12.12
- 嵌入式 WebUI: `//go:embed assets/index.html`
- 现有 API 结构: `/api/auth`, `/api/nodes`, `/api/subscription/*`, `/api/settings`
- 多端口模式自动端口冲突解决
