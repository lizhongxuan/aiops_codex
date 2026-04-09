# AIOps Codex 前端 UI 升级设计文档

## 背景

当前前端功能完整（16 个页面、24 个 vitest 测试、6 个 playwright e2e 测试），但存在以下问题：

- 全部手写 CSS，没有使用组件库，视觉一致性差
- MessageCard 的 markdown 渲染有 bug（`preprocessForMarkdown` 对已格式化内容的二次处理）
- 管理页面（主机、审批、Skills 等）的表格、表单、弹窗都是手写的，交互粗糙
- 缺少 loading skeleton、toast 通知、确认对话框等基础交互组件

## 目标

在不改动后端的前提下，用 2-3 周完成前端 UI 升级：

1. 引入 Naive UI 组件库，替换手写的基础组件
2. 修复 markdown 渲染问题
3. 统一全局 layout 和设计语言
4. 优化聊天页面的 streaming 体验

## 技术选型

| 项目 | 当前 | 升级后 |
|------|------|--------|
| 组件库 | 无（手写 CSS） | Naive UI |
| Markdown 渲染 | marked + 手写 preprocessor | markdown-it + highlight.js |
| 图标 | lucide-vue-next | lucide-vue-next（保留） |
| 状态管理 | Pinia | Pinia（保留） |
| 路由 | Vue Router | Vue Router（保留） |
| 构建 | Vite | Vite（保留） |

选择 Naive UI 的理由：
- Vue 3 原生，TypeScript 友好
- 内置暗色主题，后续可扩展
- 组件覆盖全面（DataTable、Modal、Notification、Drawer、Menu 等）
- 按需引入，不影响打包体积
- 中文文档完善，社区活跃

## 升级范围

### Phase 1: 基础设施（3 天）

**1.1 安装依赖**

```bash
cd web
npm install naive-ui markdown-it highlight.js
```

**1.2 全局配置**

在 `main.js` 中配置 Naive UI 的全局 provider：

```js
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import naive from 'naive-ui'
import App from './App.vue'
import router from './router'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(naive)
app.mount('#app')
```

**1.3 CSS 变量迁移**

保留 `style.css` 中的 layout 变量（`--sidebar-bg`、`--canvas-bg` 等），删除所有组件级样式（`.ops-button`、`.ops-pill`、`.ops-table` 等），由 Naive UI 组件替代。

保留的 CSS：
- `.app-layout` 三栏布局
- `.app-sidebar` 侧边栏结构
- `.app-main` 主内容区
- `.app-mcp-drawer` 右侧抽屉
- `.chat-container` / `.chat-stream-inner` 聊天流布局
- `.omnibar-dock` / `.omnibar-wrapper` 输入框布局

删除的 CSS（由 Naive UI 替代）：
- `.ops-button` / `.ops-pill` / `.ops-mini-pill` → `<n-button>` / `<n-tag>`
- `.ops-table` / `.ops-table-wrap` → `<n-data-table>`
- `.ops-card` / `.ops-metric-card` → `<n-card>` / `<n-statistic>`
- `.ops-search` → `<n-input>` with search slot
- `.ops-detail-block` / `.ops-subcard` → `<n-card>` nested
- `.header-pill` → `<n-button>` tertiary + round

### Phase 2: Markdown 渲染修复（2 天）

**2.1 替换 marked 为 markdown-it**

修改 `web/src/components/MessageCard.vue`：

```js
// 替换
import { marked } from 'marked'

// 为
import MarkdownIt from 'markdown-it'
import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import json from 'highlight.js/lib/languages/json'
import yaml from 'highlight.js/lib/languages/yaml'
import nginx from 'highlight.js/lib/languages/nginx'
import python from 'highlight.js/lib/languages/python'
import go from 'highlight.js/lib/languages/go'

hljs.registerLanguage('bash', bash)
hljs.registerLanguage('json', json)
hljs.registerLanguage('yaml', yaml)
hljs.registerLanguage('nginx', nginx)
hljs.registerLanguage('python', python)
hljs.registerLanguage('go', go)

const md = new MarkdownIt({
  html: false,
  breaks: true,
  linkify: true,
  highlight(str, lang) {
    if (lang && hljs.getLanguage(lang)) {
      return hljs.highlight(str, { language: lang }).value
    }
    return ''
  }
})
```

**2.2 删除 `preprocessForMarkdown`**

这个函数是当前格式化问题的根源。它在每个中文句号后插入 `\n\n`，会破坏 LLM 已经格式化好的 markdown 结构。

markdown-it 本身对中文段落的处理已经足够好，不需要预处理。

**2.3 保留 `cleanDisplayText`**

这个函数清理路由元数据（`{"route": "direct"}` 等），是必要的，保留不动。

**2.4 代码块样式**

引入 highlight.js 的主题 CSS：

```js
import 'highlight.js/styles/github.css'
```

在 `.markdown-body` 的 scoped style 中调整 `pre code` 的样式，与 Naive UI 的设计语言对齐。

### Phase 3: 全局 Layout 升级（3 天）

**3.1 侧边栏改造**

将 `App.vue` 中手写的侧边栏替换为 Naive UI 的 `<n-layout-sider>` + `<n-menu>`：

```vue
<n-layout-sider
  bordered
  :collapsed="isSidebarCollapsed"
  :collapsed-width="64"
  :width="240"
  collapse-mode="width"
  show-trigger
>
  <n-menu
    :value="activeMenuKey"
    :collapsed="isSidebarCollapsed"
    :options="menuOptions"
    @update:value="handleMenuSelect"
  />
</n-layout-sider>
```

菜单结构：

```
主导航
├── 单机会话 (/)
├── 协作工作台 (/protocol)
├── Coroot 监控 (/coroot)
└── 主机列表 (/settings/hosts)

设置
├── Agent Profile (/settings/agent)
├── Skills 管理 (/settings/skills)
├── MCP 管理 (/settings/mcp)
├── 审批管理 (/approval-management)
└── 更多设置 (/settings)
```

**3.2 顶部 Header 改造**

将手写的 `.main-header` 替换为 Naive UI 的 `<n-page-header>` 或保留自定义 header，但内部按钮替换为 `<n-button>`：

```vue
<n-space align="center">
  <n-button quaternary circle @click="openHeaderHistoryDrawer">
    <template #icon><HistoryIcon size="16" /></template>
  </n-button>
  <n-button quaternary size="small" @click="isHostModalOpen = true">
    <template #icon><ServerIcon size="14" /></template>
    {{ selectedHostLabel }}
    <n-badge :type="hostStatusType" dot />
  </n-button>
</n-space>
```

**3.3 通知系统**

将 `store.noticeMessage` / `store.errorMessage` 的手写 banner 替换为 Naive UI 的 `useMessage()` / `useNotification()`：

```js
import { useMessage } from 'naive-ui'
const message = useMessage()

// 替换
store.noticeMessage = '已清空当前会话上下文'
// 为
message.success('已清空当前会话上下文')
```

**3.4 确认对话框**

将 `window.confirm()` 替换为 `useDialog()`：

```js
import { useDialog } from 'naive-ui'
const dialog = useDialog()

dialog.warning({
  title: '确认清空',
  content: '确认清空当前会话上下文吗？',
  positiveText: '确认',
  negativeText: '取消',
  onPositiveClick: () => { /* ... */ }
})
```

### Phase 4: 管理页面升级（5 天）

**4.1 主机列表页 (`HostsPage.vue`)**

替换手写表格为 `<n-data-table>`：

```vue
<n-data-table
  :columns="hostColumns"
  :data="hosts"
  :row-key="row => row.id"
  :pagination="{ pageSize: 20 }"
  striped
/>
```

列定义：

| 列 | 字段 | 渲染 |
|----|------|------|
| 状态 | status | `<n-badge>` dot (online=success, offline=default) |
| 主机名 | name | 文本 + 点击跳转到 chat?hostId=xxx |
| ID | id | `<n-tag>` monospace |
| 类型 | kind | `<n-tag>` (agent/server_local/lab) |
| OS | os/arch | 文本 |
| 最后心跳 | lastHeartbeat | 相对时间 |
| 操作 | - | `<n-button-group>` (终端/编辑/删除) |

**4.2 审批管理页 (`ApprovalManagementPage.vue`)**

- 审计日志表格 → `<n-data-table>` + `<n-date-picker>` 筛选
- 授权白名单 → `<n-data-table>` + `<n-switch>` 启用/禁用
- 审批详情 → `<n-drawer>` 侧滑面板

**4.3 Agent Profile 页 (`AgentProfilePage.vue`)**

- System Prompt 编辑 → `<n-input>` type="textarea" + 字数统计
- 权限配置 → `<n-form>` + `<n-select>` + `<n-switch>`
- Skills/MCP 列表 → `<n-transfer>` 或 `<n-checkbox-group>`

**4.4 Coroot 监控页 (`CorootOverviewPage.vue`)**

- 健康统计 → `<n-statistic>` + `<n-grid>`
- 服务表格 → `<n-data-table>` + 状态 badge
- Tab 切换 → `<n-tabs>`
- AI 抽屉 → `<n-drawer>`

**4.5 其他管理页面**

| 页面 | 主要替换 |
|------|---------|
| SkillCatalogPage | 列表 → `<n-data-table>`，开关 → `<n-switch>` |
| McpCatalogPage | 列表 → `<n-data-table>`，权限 → `<n-select>` |
| ScriptConfigPage | 表单 → `<n-form>`，代码编辑保留 Monaco |
| UICardManagementPage | 卡片列表 → `<n-data-table>`，预览 → `<n-card>` |
| LabEnvironmentPage | 环境列表 → `<n-data-table>`，模板 → `<n-card>` grid |
| GeneratorWorkshopPage | 步骤 → `<n-steps>`，表单 → `<n-form>` |
| CapabilityCenterPage | 绑定列表 → `<n-data-table>` |
| SettingsPage | 入口卡片 → `<n-card>` grid |
| ExperiencePacksPage | 列表 → `<n-list>` + `<n-card>` |

### Phase 5: 聊天体验优化（3 天）

**5.1 消息 Streaming 优化**

- 添加 skeleton loading：assistant 消息开始时显示 `<n-skeleton>` 占位
- 平滑滚动：用 `scrollIntoView({ behavior: 'smooth' })` 替换 `jumpToLatest`
- 打字机效果：CSS animation 在最后一个字符后显示闪烁光标

**5.2 审批 Overlay 优化**

将手写的审批卡片替换为 Naive UI 组件：

```vue
<n-card
  :bordered="true"
  :segmented="{ content: true }"
  size="small"
>
  <template #header>
    <n-space align="center">
      <n-icon color="#f59e0b"><AlertTriangleIcon /></n-icon>
      命令审批
    </n-space>
  </template>
  <n-code :code="command" language="bash" />
  <template #action>
    <n-space>
      <n-button type="success" @click="approve">允许执行</n-button>
      <n-button @click="deny">拒绝</n-button>
    </n-space>
  </template>
</n-card>
```

**5.3 ThinkingCard 优化**

- 用 `<n-spin>` 替换手写的 CSS spinner
- 添加 phase 对应的图标和颜色
- 活动详情（浏览文件、搜索）用 `<n-collapse>` 折叠

**5.4 输入框（Omnibar）优化**

保留当前的自定义 Omnibar 设计（它的交互已经很好），但优化：
- 发送按钮用 `<n-button>` circle
- 状态提示用 `<n-text>` depth="3"
- 附件/粘贴提示用 `<n-tag>` closable

### Phase 6: 右侧 MCP 抽屉优化（1 天）

- 抽屉容器 → `<n-drawer>` placement="right"
- 固定面板列表 → `<n-list>` + `<n-list-item>`
- 面板切换 → `<n-tabs>` type="segment"
- 空状态 → `<n-empty>`

## 不改动的部分

以下组件/逻辑保持不变：

- `store.js` — Pinia 状态管理，WebSocket 连接，API 调用
- `router.js` — 路由结构
- `composables/` — 所有 composable（useChatScrollState、useVirtualTurnList 等）
- `lib/` — 所有工具库（chatTurnFormatter、mcpBundleResolver 等）
- `components/chat/ChatTurnGroup.vue` — 聊天 turn 分组逻辑
- `components/chat/ChatComposerDock.vue` — 输入框 dock 逻辑
- `components/chat/ChatProcessFold.vue` — 进程折叠逻辑
- Monaco Editor 集成（ScriptConfigPage 等）
- xterm.js 终端集成（TerminalPage）

## 迁移策略

逐页面替换，每个页面独立 PR，确保不破坏现有功能：

1. 先装 naive-ui，配置全局 provider，跑通所有现有测试
2. 从最简单的页面开始（SettingsPage → HostsPage → SkillCatalogPage）
3. 管理页面全部完成后，再改 ChatPage 和 ProtocolWorkspacePage
4. 最后改 App.vue 的全局 layout

每个 PR 必须：
- 通过所有现有 vitest 测试
- 通过所有现有 playwright e2e 测试
- 视觉回归检查（截图对比）

## 工期估算

| Phase | 内容 | 工期 |
|-------|------|------|
| 1 | 基础设施（安装、配置、CSS 清理） | 3 天 |
| 2 | Markdown 渲染修复 + 缓存/节流机制（借鉴 3） | 3 天 |
| 3 | 全局 Layout 升级 + 底部状态栏（借鉴 4） | 4 天 |
| 4 | 管理页面升级（10 个页面） | 5 天 |
| 5 | 聊天体验优化 + Tool 折叠时间线（借鉴 1）+ 子 Agent 嵌套（借鉴 2）+ 批量渲染节流（借鉴 7） | 4 天 |
| 6 | MCP 抽屉优化 + Session 树形列表（借鉴 5）+ Slash 命令（借鉴 6） | 2 天 |
| **总计** | | **~21 天（4 周）** |

## 从 openwork 借鉴的 UI 设计（审核通过项）

以下是逐项审核 openwork 前端后，确认能提升用户体验和美观度的设计模式。每项都标注了借鉴理由和在 aiops-codex 中的落地方式。

### 借鉴 1: Tool 执行步骤的折叠时间线（✅ 采纳）

openwork 的 `message-list.tsx` 中，tool call 不是平铺展示，而是折叠成一行摘要，点击展开看 input/output 详情。每个 step 有一个 `toolHeadline()` 函数生成人类可读的摘要（如 "Reviewed nginx.conf"、"Ran systemctl status nginx"）。

当前 aiops-codex 的问题：CommandCard、PlanCard 的步骤是平铺的，长任务时聊天流会被撑得很长。

落地方式：
- 在 `ChatTurnGroup.vue` 中，将 PlanCard 的步骤列表改为可折叠的 `<n-timeline>` + `<n-collapse>`
- 每个 step 默认显示一行摘要（命令名 + 目标文件/服务），点击展开看完整 output
- 正在执行的 step 自动展开，已完成的自动折叠
- 用 `<n-timeline-item>` 的 type 属性区分状态：success/warning/error/info

```vue
<n-timeline>
  <n-timeline-item
    v-for="step in planSteps"
    :key="step.id"
    :type="stepStatusType(step)"
    :title="stepHeadline(step)"
  >
    <n-collapse v-if="step.output">
      <n-collapse-item :title="'查看详情'">
        <n-code :code="step.output" language="bash" />
      </n-collapse-item>
    </n-collapse>
  </n-timeline-item>
</n-timeline>
```

### 借鉴 2: 子 Agent 嵌套线程展示（✅ 采纳）

openwork 的 `SubagentThread` 组件在主消息流中嵌套展示子 agent 的对话，用左侧竖线 (`border-l`) 缩进，带折叠/展开控制和 "Open session" 链接。

当前 aiops-codex 的问题：Workspace 模式下的 Worker session 是跳转到独立页面查看的，上下文切换成本高。

落地方式：
- 在 `ProtocolWorkspacePage.vue` 的 worker 卡片中，添加内联展开功能
- 用 `border-left: 2px solid var(--primary)` + `padding-left: 16px` 做视觉缩进
- 折叠状态显示：agent 类型 + 状态标签 + "展开详情" 按钮
- 展开后显示 worker 的最近 3-5 条消息摘要
- 保留 "在新页面打开" 链接作为完整查看入口

```vue
<div class="worker-inline-thread" v-if="workerExpanded">
  <div style="border-left: 2px solid var(--primary); padding-left: 16px;">
    <n-tag :type="workerStatusType">{{ worker.status }}</n-tag>
    <span class="worker-agent-label">{{ worker.agentType || 'Worker' }}</span>
    <div v-for="msg in workerRecentMessages" :key="msg.id" class="worker-message-preview">
      <MessageCard :card="msg" compact />
    </div>
    <n-button text type="primary" @click="openWorkerSession(worker.sessionId)">
      在新页面查看完整会话
    </n-button>
  </div>
</div>
```

### 借鉴 3: Markdown 渲染的缓存和节流机制（✅ 采纳）

openwork 的 `part-view.tsx` 实现了：
- markdown HTML 缓存（LRU，最多 100 条），避免重复解析
- streaming 时的节流渲染（`useThrottledValue`，默认 100ms），减少 DOM 更新频率
- 大文本折叠（超过 12000 字符自动折叠，显示前 3200 字符 + "展开全部"）

当前 aiops-codex 的问题：每次 snapshot broadcast 都会重新计算 `renderedMarkdown`，streaming 时性能差。

落地方式：
- 在 `MessageCard.vue` 中添加 markdown 渲染缓存（用 `Map` 做 LRU）
- streaming 状态下（`card.status === 'inProgress'`）用 `watchThrottled` 节流渲染，间隔 80ms
- 超过 8000 字符的消息自动折叠，显示前 2000 字符 + "展开全部" 按钮

```js
const markdownCache = new Map();
const MAX_CACHE = 80;

function cachedMarkdownRender(text) {
  if (markdownCache.has(text)) return markdownCache.get(text);
  const html = md.render(text);
  markdownCache.set(text, html);
  if (markdownCache.size > MAX_CACHE) {
    const oldest = markdownCache.keys().next().value;
    markdownCache.delete(oldest);
  }
  return html;
}
```

### 借鉴 4: 底部状态栏（✅ 采纳）

openwork 的 `status-bar.tsx` 在页面底部有一个 12px 高的状态栏，显示：
- 连接状态指示灯（绿色脉冲动画 = 已连接，琥珀色 = 受限，红色 = 断开）
- 连接摘要（"2 providers connected · 3 MCP connected"）
- 反馈按钮和设置入口

当前 aiops-codex 的问题：WebSocket 连接状态只在侧边栏底部有一个小圆点，不够醒目。codex 连接状态和主机状态没有统一展示。

落地方式：
- 在 `App.vue` 的 `<main>` 底部添加一个 48px 高的状态栏
- 左侧：连接状态灯 + "Codex 已连接" / "Codex 断开" + 当前主机状态
- 右侧：当前 session 的 turn 状态（idle/thinking/executing）+ 设置入口
- 用 Naive UI 的 `<n-badge>` dot 做状态灯，`<n-text>` 做文字

```vue
<footer class="app-status-bar">
  <div class="status-left">
    <n-badge :type="codexStatusType" dot />
    <n-text depth="2">{{ codexStatusLabel }}</n-text>
    <n-divider vertical />
    <n-text depth="3">{{ hostStatusLabel }}</n-text>
  </div>
  <div class="status-right">
    <n-text depth="3">{{ turnPhaseLabel }}</n-text>
  </div>
</footer>
```

### 借鉴 5: Workspace 侧边栏的树形 Session 列表（✅ 采纳）

openwork 的 `workspace-session-list.tsx` 实现了：
- workspace 分组，每个 workspace 有颜色标识圆点
- session 支持 parent-child 树形结构（子 agent session 缩进展示）
- 活跃 session 有琥珀色小圆点标识
- hover 时显示操作按钮（新建任务、更多选项）
- 渐进加载（默认显示 6 个，"Show more" 加载更多）

当前 aiops-codex 的问题：`SessionHistoryDrawer` 是扁平列表，没有分组，没有树形结构，没有活跃状态标识。

落地方式：
- 改造 `SessionHistoryDrawer.vue`，按主机分组展示 session
- 每个主机组用颜色圆点 + 主机名作为分组头
- workspace session 下的 worker session 用缩进展示
- 活跃 session（有 turn running）显示琥珀色脉冲点
- 默认显示每组最近 5 个 session，底部 "加载更多"

### 借鉴 6: Composer 输入框的 @ 提及和 / 命令（✅ 采纳，简化版）

openwork 的 `composer.tsx` 支持：
- `@agent` 提及切换 agent
- `@file` 提及附加文件上下文
- `/` 斜杠命令触发快捷操作
- 模型选择器（底部显示当前模型，点击切换）
- 附件上传（图片压缩、PDF 支持）

当前 aiops-codex 的问题：Omnibar 只有纯文本输入 + 发送按钮，没有快捷操作。

落地方式（简化版，只做最有价值的部分）：
- 添加 `/` 命令支持：`/hosts`（列出主机）、`/switch <host>`（切换主机）、`/approve`（批量审批）、`/status`（系统状态）
- 输入 `/` 时弹出命令列表（用 `<n-auto-complete>` 实现）
- 底部显示当前主机名和 model 信息（只读展示，不做切换）
- 不做 `@` 提及和附件上传（运维场景不需要）

```vue
<n-auto-complete
  v-if="showSlashMenu"
  :options="slashCommandOptions"
  @select="handleSlashCommand"
/>
```

### 借鉴 7: 消息流的批量渲染节流（✅ 采纳）

openwork 的 `session.tsx` 中有一个 `STREAM_RENDER_BATCH_MS = 48` 的批量渲染机制：streaming 时不是每个 delta 都触发 DOM 更新，而是攒 48ms 的 delta 一次性渲染。

当前 aiops-codex 的问题：每次 `broadcastSnapshot` 都触发完整的 Vue reactivity 更新，streaming 时 CPU 占用高。

落地方式：
- 在 `store.js` 的 WebSocket message handler 中，对 streaming 状态的 snapshot 做 48ms 节流
- 用 `requestAnimationFrame` 或 `setTimeout` 攒批更新
- 非 streaming 状态（turn completed/failed）立即更新

```js
let streamBatchTimer = null;
let pendingSnapshot = null;

function handleWsMessage(snapshot) {
  if (snapshot.runtime.turn.active && snapshot.runtime.turn.phase !== 'waiting_approval') {
    pendingSnapshot = snapshot;
    if (!streamBatchTimer) {
      streamBatchTimer = setTimeout(() => {
        applySnapshot(pendingSnapshot);
        streamBatchTimer = null;
        pendingSnapshot = null;
      }, 48);
    }
  } else {
    if (streamBatchTimer) {
      clearTimeout(streamBatchTimer);
      streamBatchTimer = null;
    }
    applySnapshot(snapshot);
  }
}
```

### 未采纳项（审核不通过）

| openwork 特性 | 不采纳理由 |
|---------------|-----------|
| Radix UI 颜色系统（3000 行 CSS 变量） | 过于复杂，Naive UI 自带主题系统足够 |
| Tauri 桌面壳集成 | aiops-codex 是纯 web 应用，不需要桌面壳 |
| 文件浏览器和 diff viewer | 运维场景不需要代码 diff，保留现有的文件预览 modal |
| Todo timeline（execution plan） | aiops-codex 已有 PlanCard，用折叠时间线改造即可 |
| 多 workspace 切换 | aiops-codex 用 session + host 的模型，不需要 workspace 抽象 |
| Provider auth modal（OAuth 流程） | aiops-codex 通过 codex app-server 处理认证，不需要前端 OAuth |
| i18n 国际化 | 当前阶段只需要中文，不需要多语言框架 |
| FlyoutItem 动画（文件飞入效果） | 视觉花哨但对运维场景无实际价值 |
| Command palette（⌘K 全局搜索） | 功能好但实现复杂，可以作为后续迭代 |
| 图片附件上传和压缩 | 运维场景极少需要上传图片 |

## 风险

1. Naive UI 的 DataTable 和现有的虚拟滚动（useVirtualTurnList）可能冲突 — ChatPage 的消息流不用 DataTable，保留自定义虚拟滚动
2. Naive UI 的全局样式可能影响现有的 scoped CSS — 用 `n-config-provider` 的 `cls-prefix` 隔离
3. highlight.js 的语言包体积 — 只注册运维常用语言（bash/json/yaml/nginx/python/go），按需加载
4. 借鉴项的工期影响 — 7 个借鉴项预计额外增加 3-4 天工作量，总工期从 17 天调整为 20-21 天（约 4 周）
