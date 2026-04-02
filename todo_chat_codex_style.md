# AI Chat 改造成 Codex 原生风格实施清单

基于 [chat_codex_style_plan.md](/Users/lizhongxuan/Desktop/aiops-codex/chat_codex_style_plan.md) 的方案，整理出以下可直接落地的实施清单。

本轮改造只聚焦 `AI chat` 页面，不改协作工作台的右侧审批栏逻辑。

收敛目标：

- 把当前 `AI chat` 从“原始卡片流”改成“turn 级结果线程”
- 把一轮内容拆成 `过程层` 与 `结果层`
- `过程层` 默认折叠，`结果层` 默认展开
- `plan` 与 `background agents` 收到输入框上方，像 Codex composer 的一部分
- 输入框、消息比例、字体、间距整体向 Codex app 原生风格靠拢

状态说明：

- `[x]` 已完成
- `[ ]` 未完成
- `（部分完成）` 表示已有基础或兼容逻辑，但还没达到本轮目标

## 1. 实施目标

1. AI chat 主线程只突出用户消息、最终回答、当前运行状态和关键决策点。
2. 文件浏览、搜索、命令痕迹、后台 agent 过程统一进入 `process group`，默认折叠。
3. `plan` 不再以传统卡片孤立显示，而是作为输入框上方轻量 widget。
4. `background agents` 不再像大卡片，而是作为输入框上方折叠 widget。
5. `Omnibar` 改造成更像 Codex 原生的底部主输入器。
6. 避免在主线程中暴露实现细节，例如“正在判断路由”“这是简单对话”。
7. 前端具备 turn formatter 这一层，UI 不再直接等于存储卡片结构。

## 2. 当前基线

- [x] 2.1 当前 [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue) 已具备消息流、thinking、approval overlay、plan dock、terminal dock。
- [x] 2.2 当前 [MessageCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/MessageCard.vue) 已支持 markdown、结构化文本识别和文件预览。
- [x] 2.3 当前 [Omnibar.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/Omnibar.vue) 已有 stop/send 状态与 follow-up 模式。
- [x] 2.4 当前 [PlanCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/PlanCard.vue) 已支持 compact 形态。
- [ ] 2.5 当前主页面仍按“原始卡片 + 辅助区块”拼装，缺少 turn formatter。
- [ ] 2.6 当前过程日志和最终回答没有分层，导致主线程不够像 Codex 原生线程。
- [ ] 2.7 当前 plan dock、approval overlay、terminal dock、activity summary 分散在多个区域，视觉重心不统一。

## 3. 实施原则

- [ ] 3.1 不再让 `card.type` 直接决定最终 UI 结构。
- [ ] 3.2 先做 `turn formatter`，再做视觉细节；不能只修 CSS。
- [ ] 3.3 最终回答必须常驻展开，过程日志必须可以折叠。
- [ ] 3.4 plan 和 background agents 必须靠近 composer，而不是漂浮在消息流中间。
- [ ] 3.5 过程摘要只展示“对用户有帮助的信息”，不展示内部实现解释。
- [ ] 3.6 样式统一优先于组件各自为战，文本节奏优先于卡片装饰。

## 4. 交付物清单

- [ ] 4.1 新增 `chatTurnFormatter`，把 raw cards 重组为 turn 级 view model
- [ ] 4.2 新增 `ChatTurnGroup` 组件
- [ ] 4.3 新增 `ChatProcessGroup` 组件
- [ ] 4.4 新增 `ChatComposerDock` 组件
- [ ] 4.5 `ChatPage` 改成基于 turn formatter 渲染
- [ ] 4.6 `MessageCard` 重做成更接近 Codex 原生的文本线程样式
- [ ] 4.7 `Omnibar` 重做成更接近 Codex 原生的 composer
- [ ] 4.8 `PlanCard` 改造成输入框上方轻量 widget
- [ ] 4.9 `background agents` 收敛成轻量折叠 widget
- [ ] 4.10 自动化测试覆盖“过程折叠 / 最终消息展开 / composer dock / plan widget”

## 5. 里程碑与任务分解

### P0. 结构重构：turn formatter 与折叠模型

- [ ] TASK-CHAT-CODEX-001 新增 `chatTurnFormatter`
  文件：
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
  目标：
  - 把原始 `cards`、`runtime.activity`、`runtime.turn`、`pending approvals`、`active plan` 重组为 turn 级结构
  完成标准：
  - formatter 输出 `turns / activeWidgets / composerContext`

- [ ] TASK-CHAT-CODEX-002 定义 turn view model
  文件：
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
  目标：
  - 明确一轮 turn 的字段：
    - `userMessage`
    - `processGroup`
    - `finalMessage`
    - `status`
    - `widgets`
  完成标准：
  - 不再需要在页面层直接分析 raw card 序列

- [ ] TASK-CHAT-CODEX-003 过程类型归类
  文件：
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
  目标：
  - 按语义归类：
    - `user`
    - `assistant_final`
    - `assistant_progress`
    - `thinking`
    - `plan`
    - `agent_status`
    - `search_trace`
    - `file_trace`
    - `command_trace`
    - `approval`
    - `error`
    - `system_notice`
  完成标准：
  - formatter 可按类型决定哪些进入折叠区

- [ ] TASK-CHAT-CODEX-004 过程摘要归并
  文件：
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
  目标：
  - 把重复日志归并成摘要：
    - `已浏览 N 个文件`
    - `已运行 N 个搜索`
    - `已执行 N 个命令`
  完成标准：
  - 主线程不再出现成堆重复 trace

- [ ] TASK-CHAT-CODEX-005 折叠策略落地
  文件：
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
  目标：
  - 让 `process group` 根据 lifecycle 自动决定默认展开/折叠
  完成标准：
  - running turn 默认展开
  - completed turn 默认折叠
  - failed turn 保留错误展开、过程折叠

### P1. 页面重组：从卡片流变成线程

- [ ] TASK-CHAT-CODEX-006 新增 `ChatTurnGroup`
  文件：
  - [web/src/components/chat/ChatTurnGroup.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatTurnGroup.vue)
  目标：
  - 渲染单轮：
    - 用户消息
    - 过程折叠区
    - 最终回答
  完成标准：
  - turn 结构清晰，主线程不再是原始卡片平铺

- [ ] TASK-CHAT-CODEX-007 新增 `ChatProcessGroup`
  文件：
  - [web/src/components/chat/ChatProcessGroup.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatProcessGroup.vue)
  目标：
  - 渲染折叠标题如：
    - `已处理 53s`
    - `已浏览 2 个文件，运行 1 个搜索`
  完成标准：
  - 展开后可查看过程明细

- [ ] TASK-CHAT-CODEX-008 新增 `ChatComposerDock`
  文件：
  - [web/src/components/chat/ChatComposerDock.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatComposerDock.vue)
  目标：
  - 统一承接：
    - plan widget
    - background agents widget
    - Omnibar
  完成标准：
  - 输入区成为统一底部栈，不再是散开的几个区块

- [ ] TASK-CHAT-CODEX-009 `ChatPage` 改为基于 formatted turns 渲染
  文件：
  - [web/src/pages/ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  目标：
  - 从直接遍历 `cards` 改成遍历 `formattedTurns`
  完成标准：
  - `activity summary / thinking / active plan / approval overlay` 的结构改成新的 turn 模型与 composer dock

- [ ] TASK-CHAT-CODEX-010 迁移 activity summary
  文件：
  - [web/src/pages/ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  目标：
  - 当前 `activity-summary` 不再顶在主线程中间
  完成标准：
  - activity 内容进入 process group

### P2. 视觉重做：更像 Codex 原生

- [ ] TASK-CHAT-CODEX-011 重做 assistant 消息样式
  文件：
  - [web/src/components/MessageCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/MessageCard.vue)
  目标：
  - assistant 更像纯文本线程，不像厚重卡片
  完成标准：
  - 字号、行高、段距明显提升
  建议指标：
  - 字号 `17px - 18px`
  - 行高 `1.8`

- [ ] TASK-CHAT-CODEX-012 重做 user bubble
  文件：
  - [web/src/components/MessageCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/MessageCard.vue)
  目标：
  - user bubble 更像 Codex 原生浅灰圆角气泡
  完成标准：
  - 宽度更窄，圆角更大，视觉更轻

- [ ] TASK-CHAT-CODEX-013 assistant markdown 排版优化
  文件：
  - [web/src/components/MessageCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/MessageCard.vue)
  目标：
  - 优化列表、引用、代码块、内联 code 的可读性
  完成标准：
  - 最终回答更像正式产品内容，而不是技术日志

- [ ] TASK-CHAT-CODEX-014 重做 Omnibar 尺寸与视觉
  文件：
  - [web/src/components/Omnibar.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/Omnibar.vue)
  目标：
  - 更像 Codex 原生底部 composer
  完成标准：
  - 更大的圆角、更高的输入框、更统一的底部工具条
  建议指标：
  - `max-width: 920px - 980px`
  - `min-height: 110px - 130px`
  - `border-radius: 28px - 32px`

- [ ] TASK-CHAT-CODEX-015 轻量化 PlanCard
  文件：
  - [web/src/components/PlanCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/PlanCard.vue)
  目标：
  - 把当前 plan dock 收敛成更轻的 composer 上方 widget
  完成标准：
  - 只保留：
    - 总任务数
    - 已完成数
    - 简短步骤列表

- [ ] TASK-CHAT-CODEX-016 收敛 background agents widget
  文件：
  - 可复用现有 background agents 组件，或新增 chat 专用组件
  目标：
  - 让后台 agents 展示更像 Codex 的轻量折叠区
  完成标准：
  - 每个 agent 一行，右侧有 `Open`

### P3. 内容去噪：让页面更像产品而不是调试台

- [ ] TASK-CHAT-CODEX-017 清理实现细节提示
  文件：
  - [web/src/components/MessageCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/MessageCard.vue)
  - [web/src/pages/ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - 后端相关会话文案生成位置
  目标：
  - 减少主线程中对内部实现的解释
  完成标准：
  - 主线程只保留用户能理解且关心的信息

- [ ] TASK-CHAT-CODEX-018 压缩 NoticeCard 噪音
  文件：
  - [web/src/components/CardItem.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/CardItem.vue)
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
  目标：
  - 把非关键 notice 归入 process group
  完成标准：
  - 主线程不再被 `NoticeCard` 打散

- [ ] TASK-CHAT-CODEX-019 优化空态与起始态
  文件：
  - [web/src/pages/ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  目标：
  - 页面初始态更像一个“活着的线程”
  完成标准：
  - 不再出现大块说明文案式空白页

### P4. 自动化测试与视觉验收

- [ ] TASK-CHAT-CODEX-020 测试 turn formatter
  文件：
  - 新增 formatter 单测文件
  目标：
  - 验证 turn grouping、summary 归并、折叠策略
  完成标准：
  - 核心 view model 输出稳定

- [ ] TASK-CHAT-CODEX-021 测试主线程与折叠区
  文件：
  - `web/tests/*` 新增或扩展 chat 页面测试
  目标：
  - 验证：
    - final message 展开
    - process group 折叠
    - 运行中的 turn 默认展开
  完成标准：
  - UI 行为有回归保护

- [ ] TASK-CHAT-CODEX-022 测试 composer dock
  文件：
  - `web/tests/*`
  目标：
  - 验证 plan widget、background agents widget、omnibar 的组合关系
  完成标准：
  - composer 栈不回退成旧布局

- [ ] TASK-CHAT-CODEX-023 自动化截图验收
  文件：
  - `output/playwright/*`
  目标：
  - 对比新旧 chat 页视觉效果
  完成标准：
  - 页面更接近 Codex 原生：
    - 主线程更干净
    - 输入框更像主控件
    - 过程信息更克制

## 6. 建议实施顺序

推荐严格按下面顺序推进：

1. 先做 `P0`：turn formatter
2. 再做 `P1`：页面结构切换到 turns
3. 再做 `P2`：消息、composer、widget 视觉重做
4. 最后做 `P3 / P4`：去噪与验收

不要先只改 CSS，否则很容易继续出现：

- 改了一点
- 页面还是不像 Codex
- 结构仍然没变

## 7. 验收标准

以下都满足，才算这轮达到目标：

- [ ] 7.1 用户第一眼看到的是最终回答，而不是过程日志
- [ ] 7.2 一轮处理过程可以折叠成一条摘要
- [ ] 7.3 运行中的 turn 仍能展示轻量状态，不让用户丢上下文
- [ ] 7.4 plan widget 和 background agents widget 贴在输入框上方
- [ ] 7.5 输入框尺寸、间距、按钮、气质明显更接近 Codex 原生
- [ ] 7.6 assistant 回复更像“文本线程”，不再像“技术卡片列表”
- [ ] 7.7 自动化截图能看出明显提升

## 8. 备注

- 本清单默认不涉及协作工作台右侧审批栏重构。
- 本清单默认优先改 `AI chat` 主页面。
- 若后续要让 `AI chat` 与 `workspace` 使用统一的视觉与 turn formatter，可以在本轮完成后再抽公共层。

