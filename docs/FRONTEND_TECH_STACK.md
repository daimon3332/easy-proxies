# FRONTEND_TECH_STACK.md

# Easy Proxies 前端技术栈说明

前端技术栈主要是：

```text
原生 HTML + 原生 CSS + 原生 JavaScript + Go embed 静态资源服务
```

没有使用 Vue、React、Angular、Svelte，也没有使用 Vite、Webpack、Rollup 这类前端构建工具。

也没有使用 Ant Design、Element Plus、Tailwind CSS、shadcn/ui、Bootstrap 这类 UI 框架。当前 WebUI 是纯手写 HTML、CSS、JavaScript 实现的。

---

## 1. 前端形态：单文件 WebUI

当前前端核心文件是：

```text
internal/monitor/assets/index.html
```

这个文件同时包含：

- HTML 页面结构
- CSS 样式
- JavaScript 页面逻辑
- 路由式页面切换
- 表格渲染
- 弹窗渲染
- Toast 提示
- 调用后端 API
- 节点导入、测速、刷新订阅等 WebUI 交互

也就是说，目前前端不是组件化框架项目，而是一个单文件原生 WebUI。

这种形态的特点是：

- 没有 npm 依赖
- 没有 node_modules
- 没有前端构建步骤
- 修改后重新编译 Go 程序即可打包进可执行文件
- 部署简单，运行一个 Go 程序即可提供 WebUI

---

## 2. 构建方式：Go embed

前端静态资源通过 Go 的 `embed` 机制打包进程序。

相关文件：

```text
internal/monitor/server.go
internal/monitor/assets/index.html
internal/monitor/assets/logo.png
```

后端通过：

```go
//go:embed assets/index.html assets/logo.png
```

把 WebUI 页面和 Logo 嵌入到 Go 二进制文件中。

运行时由 Go HTTP 服务读取嵌入资源并返回给浏览器。

特点：

- 不需要单独部署前端 dist 目录
- 不需要 Nginx 托管静态资源
- 不需要前后端分离部署
- `easy_proxies.exe` 一个文件即可携带 WebUI

---

## 3. 前端框架：没有使用框架

当前没有使用：

- React
- Vue
- Angular
- Svelte
- Solid
- Preact

页面状态和渲染全部由原生 JavaScript 控制。

核心状态对象在：

```text
internal/monitor/assets/index.html
```

里面的全局对象大致类似：

```text
S = {
  page,
  nodes,
  viewNodes,
  settings,
  importSources,
  selected,
  filters,
  ...
}
```

页面切换不是 React Router 或 Vue Router，而是通过内部状态控制：

```text
S.page = 'import' / 'nodes' / 'pool' / 'failed' / 'bulk' / 'ports' / 'logs' / 'settings'
```

然后调用对应的渲染函数重新生成页面 HTML。

---

## 4. 页面渲染方式：模板字符串 + DOM API

当前前端主要通过 JavaScript 模板字符串生成 HTML。

常见模式是：

```text
qs('#view').innerHTML = `...`
```

相关渲染函数包括：

- `renderNav()`
- `renderImport()`
- `renderManaged()`
- `renderBulk()`
- `renderPorts()`
- `renderLogs()`
- `renderSettings()`

DOM 查询使用原生 API 封装：

```text
querySelector
querySelectorAll
```

项目中封装为：

```text
qs()
qsa()
```

这意味着当前 WebUI 的本质是：

```text
原生 JavaScript 状态对象 + 手写 render 函数 + innerHTML 模板渲染
```

---

## 5. 样式方案：原生 CSS

样式全部写在：

```text
internal/monitor/assets/index.html
```

也就是 HTML 文件顶部的 `<style>` 标签中。

没有使用：

- CSS Modules
- Sass
- Less
- Tailwind CSS
- UnoCSS
- PostCSS 独立构建
- CSS-in-JS

当前 UI 主要靠 CSS 变量和手写 class 控制。

核心 CSS 变量包括：

```text
--bg
--panel
--panel-soft
--line
--line-strong
--text
--muted
--blue
--blue-deep
--green
--green-soft
--red
--red-soft
--amber
--amber-soft
--shadow
--radius
--mono
--font
```

这些变量控制：

- 浅蓝绿色背景
- 白色卡片
- 淡蓝灰边框
- 主文字和次级文字
- 蓝色主操作
- 绿色成功操作
- 红色失败 / 删除
- 黄色等待 / 测试中
- 柔和阴影
- 圆角体系
- 等宽字体

---

## 6. 当前 UI 风格实现

当前 UI 是手写 CSS 实现的浅蓝绿色轻量控制台风格。

主要实现了：

- 浅蓝绿色渐变背景
- 左侧 Sidebar 导航
- 品牌区和 Logo
- 卡片式内容分区
- 圆角按钮
- 表格化节点列表
- 表头排序
- 多选框和全选框
- 筛选器
- 状态 Pill / Badge
- Toast 提示
- Modal 弹窗
- 阻塞式进度弹窗
- 日志全屏查看
- 端口状态列表
- 响应式布局

整体没有依赖 UI 组件库，因此所有视觉细节都由当前 CSS 控制。

优点是：

- 样式完全可控
- 依赖极少
- 打包简单
- 不需要维护前端工程链

缺点是：

- 页面逻辑集中在一个文件中
- 后续功能继续变多时，维护成本会上升
- 组件复用能力弱
- 表格、弹窗、表单等都需要手写维护

---

## 7. JavaScript 语言：原生 ES6+

当前 JavaScript 使用浏览器原生能力，写法是现代 ES6+。

使用到的语法和能力包括：

- `const` / `let`
- 箭头函数
- 模板字符串
- `async` / `await`
- `Promise`
- `Set`
- `Map` 风格对象
- 数组方法：`map`、`filter`、`sort`、`reduce`
- 可选链：`?.`
- DOM API

没有使用 TypeScript。

因此当前没有：

- 类型检查
- 前端接口类型定义
- 编译期字段校验

---

## 8. 后端 API 调用：fetch

前端通过浏览器原生 `fetch` 调用 Go 后端 API。

封装函数在：

```text
internal/monitor/assets/index.html
```

核心形式是：

```text
api(path, options)
```

它负责：

- 拼接请求
- 自动带上 Authorization token
- 解析 JSON
- 处理 401 登录状态
- 抛出错误信息

常见 API 类型包括：

- 节点列表
- 节点池
- 失败节点
- 导入解析
- 导入提交
- 导入任务进度
- 批量测试任务
- 订阅来源
- 设置
- 端口状态
- 日志

前端和后端不是 GraphQL，也不是 gRPC，而是普通 REST 风格 HTTP JSON API。

---

## 9. 浏览器本地存储：localStorage

前端使用 `localStorage` 保存少量浏览器本地状态。

主要用于：

- 登录 token
- 导入时是否自动加入节点池
- 自动测速配置
- 失败节点是否自动入池
- 正在运行的导入任务 ID
- 正在运行的批量测试任务 ID

相关 key 包括：

```text
easy_proxies_token
easy_proxies_import_auto_promote
easy_proxies_auto_probe
easy_proxies_failed_auto_promote
easy_proxies_active_import_job
easy_proxies_active_batch_job
```

这样即使用户刷新页面，前端也能恢复部分状态，例如继续轮询正在运行的任务。

---

## 10. 异步任务轮询：setTimeout / setInterval

前端没有使用 WebSocket，也没有 Server-Sent Events。

导入、测速、订阅刷新等任务进度主要通过轮询实现。

使用方式：

- 提交任务后获取 `job_id`
- 每隔一段时间请求任务状态 API
- 更新进度弹窗
- 任务完成后停止轮询

相关浏览器 API：

```text
setTimeout
setInterval
```

这种方式简单稳定，适合当前纯 HTML/JS 架构。

---

## 11. 弹窗系统：原生 DOM + CSS

当前弹窗不是组件库实现的，而是手写 HTML、CSS、JS。

包括：

- 登录弹窗
- 导入格式选择弹窗
- 结果弹窗
- 阻塞式进度弹窗

相关函数包括：

- `openDialog()`
- `openBlockingDialog()`
- `updateBlockingDialog()`
- `closeDialog()`
- `openImportTypeModal()`
- `closeImportTypeModal()`

弹窗通过 CSS class 控制显示隐藏：

```text
modal
modal show
dialog
dialog wide
```

特点：

- 不依赖第三方 Modal 组件
- 样式统一
- 适合显示长任务进度
- 任务完成前可以保持弹窗不关闭

---

## 12. Toast 提示：手写实现

Toast 也是手写实现。

核心函数：

```text
toast(msg, type)
```

实现方式：

- 创建一个 DOM 元素
- 添加到右上角 Toast 容器
- 几秒后自动移除

Toast 适合轻量提示，例如：

- 保存成功
- 小错误
- 操作完成

长任务不会只靠 Toast，而是使用进度弹窗。

---

## 13. 文件下载：Blob + URL.createObjectURL

前端使用浏览器原生 API 实现导出下载。

相关 API：

```text
Blob
URL.createObjectURL
HTMLAnchorElement.click()
URL.revokeObjectURL
```

用于导出代理节点入口信息等文本内容。

实现方式是：

- 请求后端导出内容
- 创建 Blob
- 生成临时 URL
- 创建 `<a>` 标签并触发点击下载
- 释放临时 URL

---

## 14. 表格实现：CSS Grid + 原生事件

当前节点表格不是 HTML 原生 `<table>`，也不是第三方 DataGrid。

它主要使用 CSS Grid 实现表头和表格行对齐。

相关 class 包括：

```text
node-table
table-head
table-row
th-btn
value-pill
check
```

表格能力包括：

- 多选
- 全选
- 点击表头排序
- 状态颜色
- 行内操作按钮
- 横向滚动
- 响应式下改为单列布局

因为没有使用 DataGrid 组件库，所以排序、选择、过滤等逻辑都是手写 JavaScript 实现。

---

## 15. 响应式布局：原生 CSS Media Query

当前响应式布局使用原生 CSS `@media`。

主要断点是：

```text
max-width: 980px
```

窄屏下会调整：

- 左侧 Sidebar 变为顶部区域
- 导航变为两列
- 表格头隐藏
- 表格行变为单列
- 筛选器变为单列
- 导入格式卡片纵向排列
- 顶部标题和工具栏纵向排列

没有使用响应式框架或栅格库。

---

## 16. 静态资源

当前前端静态资源主要包括：

```text
internal/monitor/assets/index.html
internal/monitor/assets/logo.png
```

Logo 在 CSS 中作为背景图使用：

```text
background: url('/logo.png') center/cover no-repeat
```

对应后端路由：

```text
/logo.png
```

当前项目没有单独的 `public` 目录，也没有前端构建后的 `dist` 目录。

---

## 17. 前端目录结构

当前前端相关目录结构大致是：

```text
internal/
  monitor/
    assets/
      index.html     WebUI 页面、样式、脚本
      logo.png       WebUI Logo
    server.go        嵌入并提供 WebUI / API 路由
    import_handlers.go
    manager.go
```

其中：

- `index.html` 是前端主体。
- `server.go` 负责提供静态页面和 API。
- `import_handlers.go` 等 Go 文件提供 WebUI 调用的后端接口。

---

## 18. 部署形态

当前部署形态是：

```text
HTML/CSS/JS -> Go embed -> easy_proxies.exe -> 浏览器访问 WebUI
```

不是：

```text
React/Vue -> npm build -> dist -> Nginx/Cloudflare Pages
```

也就是说，WebUI 随 Go 程序一起发布。

用户只需要运行：

```text
easy_proxies.exe -config config.yaml
```

然后浏览器访问管理地址，例如：

```text
http://127.0.0.1:9091
```

---

## 19. 当前技术栈优点

当前前端技术栈的优点：

- 极轻量
- 没有 npm 依赖
- 没有前端构建链
- Go 程序单文件携带 WebUI
- 部署简单
- 样式完全可控
- 适合本地工具和轻量管理面板
- 修改 CSS / JS 直观
- 不需要学习大型前端框架

---

## 20. 当前技术栈缺点

当前前端技术栈的缺点：

- 单文件越来越大后维护困难
- 没有组件化拆分
- 没有 TypeScript 类型保护
- 没有成熟 UI 组件库
- 表格、弹窗、表单都需要手写维护
- 复杂状态管理容易混乱
- 后续大型重构成本会升高

如果后续 WebUI 继续变复杂，可以考虑逐步迁移到：

```text
Vite + React + TypeScript + 原生 CSS
```

或者：

```text
Vite + Vue + TypeScript + 原生 CSS / Element Plus
```

但当前项目为了保持简单和单文件部署，仍然适合继续使用原生 HTML/CSS/JS。

---

## 21. 和 React / Vue 项目的区别

当前项目不是这种结构：

```text
src/
  App.tsx
  main.tsx
  components/
  styles.css
package.json
vite.config.ts
```

而是：

```text
internal/monitor/assets/index.html
```

一个文件承担页面结构、样式和逻辑。

因此：

- 没有 npm run dev
- 没有 npm run build
- 没有 dist 输出目录
- 没有 React Router
- 没有 Vue Router
- 没有组件库
- 没有前端类型编译

前端是否更新，取决于 Go 程序是否重新编译并运行。

---

## 22. 总结

当前 Easy Proxies 前端技术栈可以总结为：

```text
原生 HTML
原生 CSS
原生 JavaScript
浏览器原生 API
Go embed
Go net/http REST API
```

核心特点是：

- 技术栈极轻量
- 无前端框架
- 无 UI 框架
- 无前端构建工具
- WebUI 嵌入 Go 二进制
- 样式完全手写
- 适合本地代理工具和轻量控制台

如果用一句话描述：

```text
这是一个由 Go 后端直接嵌入和服务的纯 HTML/CSS/JavaScript 单页 WebUI。
```
