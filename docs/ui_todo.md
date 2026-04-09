# AIOps Codex 前端 UI 升级 — 任务清单

基于 [ui_design.md](./ui_design.md) 最新版，含 openwork 借鉴项。

---

## Phase 1: 基础设施（3 天）

- [ ] 1.1 安装 naive-ui、markdown-it、highlight.js 依赖
- [ ] 1.2 在 `main.js` 中注册 Naive UI，包裹 `<n-message-provider>` / `<n-dialog-provider>` / `<n-notification-provider>`
- [ ] 1.3 添加 `<n-config-provider>` 设置 `cls-prefix` 防止全局样式冲突
- [ ] 1.4 清理 `style.css`：删除组件级样式（`.ops-button`、`.ops-pill`、`.ops-table`、`.ops-card`、`.ops-metric-card`、`.ops-search`、`.ops-detail-block`、`.header-pill` 等），保留 layout 级样式（`.app-layout`、`.app-sidebar`、`.app-main`、`.chat-container`、`.omnibar-dock`）
- [ ] 1.5 验证：所有现有 vitest 测试通过
- [ ] 1.6 验证：所有现有 playwright e2e 测试通过

## Phase 2: Markdown 渲染修复 + 性能优化（3 天）

- [ ] 2.1 在 `MessageCard.vue` 中用 markdown-it 替换 marked，配置 `html: false, breaks: true, linkify: true`
- [ ] 2.2 配置 highlight.js 按需加载（bash、json、yaml、nginx、python、go）
- [ ] 2.3 引入 `highlight.js/styles/github.css` 代码高亮主题
- [ ] 2.4 删除 `preprocessForMarkdown` 函数及其调用
- [ ] 2.5 保留 `cleanDisplayText` 函数不动
- [ ] 2.6 调整 `.markdown-body` scoped 样式中 `pre code` 的样式，对齐 Naive UI 设计语言
- [ ] 2.7 （借鉴 3）添加 markdown 渲染 LRU 缓存（Map，上限 80 条）
- [ ] 2.8 （借鉴 3）streaming 状态下用 `watchThrottled` 节流渲染，间隔 80ms
- [ ] 2.9 （借鉴 3）超过 8000 字符的消息自动折叠，显示前 2000 字符 + "展开全部" 按钮
- [ ] 2.10 验证：headers/lists/tables/code blocks/inline formatting 正确渲染
- [ ] 2.11 验证：路由元数据（`{"route": "direct"}` 等）仍被正确清理

## Phase 3: 全局 Layout 升级 + 状态栏（4 天）

### 侧边栏

- [ ] 3.1 将 `App.vue` 中手写侧边栏替换为 `<n-layout-sider>` + `<n-menu>`
- [ ] 3.2 定义菜单结构：主导航（单机会话、协作工作台、Coroot 监控、主机列表）+ 设置组
- [ ] 3.3 实现折叠/展开（`collapsed-width="64"`）
- [ ] 3.4 保留"新建会话"和"新建工作台"按钮在侧边栏顶部

### 顶部 Header

- [ ] 3.5 将 `.main-header` 内手写按钮替换为 `<n-button>` quaternary/tertiary
- [ ] 3.6 主机选择按钮用 `<n-button>` + `<n-badge>` dot 显示在线状态
- [ ] 3.7 认证状态按钮用 `<n-button>` + 条件样式

### 通知系统

- [ ] 3.8 将所有 `store.noticeMessage = '...'` 替换为 `useMessage().success('...')`
- [ ] 3.9 将所有 `store.errorMessage = '...'` 替换为 `useMessage().error('...')`
- [ ] 3.10 删除 `App.vue` 中手写的 notice banner 和 noticeTimer 逻辑

### 确认对话框

- [ ] 3.11 将 `window.confirm()` 替换为 `useDialog().warning()`

### 弹窗

- [ ] 3.12 `LoginModal.vue` 内部替换为 `<n-modal>`
- [ ] 3.13 `HostModal.vue` 替换为 `<n-modal>` + `<n-form>`
- [ ] 3.14 `SettingsModal.vue` 替换为 `<n-modal>`
- [ ] 3.15 `HostEditorModal.vue` / `HostBatchTagModal.vue` 替换为 `<n-modal>` + `<n-form>`

### Session 历史抽屉

- [ ] 3.16 `SessionHistoryDrawer.vue` 替换为 `<n-drawer>`
- [ ] 3.17 会话列表用 `<n-list>` + `<n-list-item>` 渲染

### 底部状态栏（借鉴 4）

- [ ] 3.18 在 `App.vue` 的 `<main>` 底部添加 48px 状态栏组件
- [ ] 3.19 左侧：`<n-badge>` dot 连接状态灯 + Codex 状态文字 + 分隔线 + 主机状态
- [ ] 3.20 右侧：当前 turn phase 状态文字 + 设置入口按钮
- [ ] 3.21 状态灯动画：connected = 绿色脉冲，disconnected = 红色静态，reconnecting = 琥珀色

## Phase 4: 管理页面升级（5 天）

### SettingsPage

- [ ] 4.1 入口卡片替换为 `<n-grid>` + `<n-card>` 布局

### HostsPage

- [ ] 4.2 主机表格替换为 `<n-data-table>`（列：状态 badge、主机名、ID tag、类型 tag、OS、心跳时间、操作按钮组）
- [ ] 4.3 搜索框替换为 `<n-input>` with search prefix icon
- [ ] 4.4 操作按钮替换为 `<n-button-group>`（终端、编辑、删除）

### ApprovalManagementPage

- [ ] 4.5 审计日志表格替换为 `<n-data-table>` + `<n-date-picker>` 时间筛选
- [ ] 4.6 授权白名单表格替换为 `<n-data-table>` + `<n-switch>` 启用/禁用
- [ ] 4.7 审批详情替换为 `<n-drawer>` 侧滑面板

### AgentProfilePage

- [ ] 4.8 System Prompt 编辑替换为 `<n-input>` type="textarea" + 字数统计
- [ ] 4.9 Runtime 配置替换为 `<n-form>` + `<n-select>`
- [ ] 4.10 Command Permissions 替换为 `<n-form>` + `<n-switch>` + `<n-select>`
- [ ] 4.11 Skills/MCP 列表替换为 `<n-data-table>` + `<n-switch>`

### CorootOverviewPage

- [ ] 4.12 健康统计替换为 `<n-grid>` + `<n-statistic>`
- [ ] 4.13 服务表格替换为 `<n-data-table>` + 状态 `<n-badge>`
- [ ] 4.14 Tab 切换替换为 `<n-tabs>`
- [ ] 4.15 AI 助手抽屉替换为 `<n-drawer>`

### 其他管理页面

- [ ] 4.16 SkillCatalogPage：列表 → `<n-data-table>`，开关 → `<n-switch>`
- [ ] 4.17 McpCatalogPage：列表 → `<n-data-table>`，权限 → `<n-select>`
- [ ] 4.18 ScriptConfigPage：表单 → `<n-form>`，保留 Monaco Editor
- [ ] 4.19 UICardManagementPage：卡片列表 → `<n-data-table>`，tab → `<n-tabs>`
- [ ] 4.20 LabEnvironmentPage：环境列表 → `<n-data-table>`，模板 → `<n-grid>` + `<n-card>`，操作 → `<n-button-group>`
- [ ] 4.21 GeneratorWorkshopPage：步骤 → `<n-steps>`，表单 → `<n-form>`
- [ ] 4.22 CapabilityCenterPage：绑定列表 → `<n-data-table>`
- [ ] 4.23 ExperiencePacksPage：列表 → `<n-list>` + `<n-card>`

## Phase 5: 聊天体验优化（4 天）

### Streaming 体验

- [ ] 5.1 assistant 消息开始时显示 `<n-skeleton>` 占位
- [ ] 5.2 优化滚动：`scrollIntoView({ behavior: 'smooth' })` 改进 `jumpToLatest`
- [ ] 5.3 添加打字机光标效果（CSS animation 闪烁光标）

### Tool 折叠时间线（借鉴 1）

- [ ] 5.4 在 `ChatTurnGroup.vue` 中将 PlanCard 步骤列表改为 `<n-timeline>` + `<n-collapse>`
- [ ] 5.5 每个 step 默认显示一行摘要（命令名 + 目标），点击展开看完整 output
- [ ] 5.6 正在执行的 step 自动展开，已完成的自动折叠
- [ ] 5.7 用 `<n-timeline-item>` type 区分状态：success/warning/error/info

### 子 Agent 嵌套线程（借鉴 2）

- [ ] 5.8 在 `ProtocolWorkspacePage.vue` 的 worker 卡片中添加内联展开功能
- [ ] 5.9 用 `border-left: 2px solid var(--primary)` + `padding-left: 16px` 做视觉缩进
- [ ] 5.10 折叠状态显示：agent 类型 + 状态标签 + "展开详情"
- [ ] 5.11 展开后显示 worker 最近 3-5 条消息摘要
- [ ] 5.12 保留 "在新页面打开" 链接

### 批量渲染节流（借鉴 7）

- [ ] 5.13 在 `store.js` 的 WebSocket handler 中对 streaming snapshot 做 48ms 节流
- [ ] 5.14 非 streaming 状态（turn completed/failed/waiting_approval）立即更新

### 审批 Overlay

- [ ] 5.15 CommandApprovalCard 替换为 `<n-card>` + `<n-code>` + approve/deny 按钮
- [ ] 5.16 FileChangeApprovalCard 替换为 `<n-card>` + diff 展示 + approve/deny 按钮
- [ ] 5.17 审批队列用 `<n-badge>` 显示待审批数量

### ThinkingCard

- [ ] 5.18 手写 CSS spinner 替换为 `<n-spin>`
- [ ] 5.19 添加 phase 对应图标（thinking=脑、planning=列表、executing=终端、waiting_approval=锁）
- [ ] 5.20 活动详情用 `<n-collapse>` 折叠展示

### Omnibar 输入框

- [ ] 5.21 发送按钮替换为 `<n-button>` circle type="primary"
- [ ] 5.22 状态提示用 `<n-text>` depth="3"

### 其他聊天组件

- [ ] 5.23 ErrorCard 替换为 `<n-alert>` type="error"
- [ ] 5.24 NoticeCard 替换为 `<n-alert>` type="info"
- [ ] 5.25 PlanCard 步骤列表用 `<n-timeline>` 渲染
- [ ] 5.26 ChoiceCard 选项用 `<n-radio-group>` 或 `<n-button-group>` 渲染

## Phase 6: MCP 抽屉 + Session 列表 + Slash 命令（2 天）

### MCP 抽屉优化

- [ ] 6.1 `.app-mcp-drawer` 替换为 `<n-drawer>` placement="right" :width="320"
- [ ] 6.2 固定面板列表替换为 `<n-list>` + `<n-list-item>`
- [ ] 6.3 分区用 `<n-divider>` 分隔，空状态用 `<n-empty>`
- [ ] 6.4 固定/移除按钮替换为 `<n-button>` text 类型

### Session 树形列表（借鉴 5）

- [ ] 6.5 改造 `SessionHistoryDrawer.vue`，按主机分组展示 session
- [ ] 6.6 每个主机组用颜色圆点 + 主机名作为分组头
- [ ] 6.7 workspace session 下的 worker session 用缩进展示
- [ ] 6.8 活跃 session（turn running）显示琥珀色脉冲点
- [ ] 6.9 默认每组显示最近 5 个 session，底部 "加载更多"

### Slash 命令（借鉴 6）

- [ ] 6.10 Omnibar 输入 `/` 时弹出命令列表（`<n-auto-complete>`）
- [ ] 6.11 实现 `/hosts` 命令：列出所有主机及状态
- [ ] 6.12 实现 `/switch <host>` 命令：切换当前主机
- [ ] 6.13 实现 `/approve` 命令：批量审批当前 pending 项
- [ ] 6.14 实现 `/status` 命令：显示系统状态摘要
- [ ] 6.15 底部显示当前主机名和 model 信息（只读）

## Phase 7: 收尾验证

- [ ] 7.1 全部 vitest 测试通过
- [ ] 7.2 全部 playwright e2e 测试通过
- [ ] 7.3 手动走查所有 16 个页面的基本交互
- [ ] 7.4 检查打包体积影响（`npm run build` 对比前后 dist 大小）
- [ ] 7.5 暗色主题预留验证（`<n-config-provider>` 切换 theme 不报错）
- [ ] 7.6 streaming 性能验证：长对话（50+ 条消息）时 CPU 占用合理
- [ ] 7.7 markdown 渲染验证：中文密集段落、代码块、表格、列表、内联格式全部正确
