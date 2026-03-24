# design_ui_0324

## 0. 文档定位

本文是当前项目进入“页面优化阶段”后的专项设计文档，目标是把前端 UI 从“能工作”推进到“接近企业级工具可交付”。

本文重点回答 4 件事：

- 企业级 AI 运维工具一共应支持多少种卡片 UI
- 如何新增一个可进入所选主机的终端页面
- 如何把当前页面风格对齐到 `codex app` 的交互气质
- 如何在 `codex app-server` 思考期间给用户明确反馈，而不是空等

本文覆盖：

- 聊天时间线卡片体系
- 终端页面 IA 与协议
- 对话页视觉与信息密度调整
- “正在思考”状态机
- `codex app-server` 重连状态可视化
- 过程摘要条与 plan 工作栏

本文不覆盖：

- 多租户权限模型
- 审计落库存储方案
- Host Agent gRPC 细节实现

## 1. 当前状态简述

当前项目已经具备聊天主链路，但 UI 仍处于 MVP 形态。

当前后端实际产出的卡片类型主要有：

- `MessageCard`
- `PlanCard`
- `StepCard`
- `CommandApprovalCard`
- `FileChangeApprovalCard`
- `ResultCard`

当前前端实际渲染组件主要有：

- `MessageCard.vue`
- `PlanCard.vue`
- `TerminalCard.vue`
- `CodeCard.vue`
- `AuthCard.vue`
- generic fallback

这说明当前系统“逻辑上有卡片”，但还没有形成一套稳定、可精细打磨的企业级卡片体系。主要问题有：

- 普通消息、过程消息、结果消息边界不清
- 会把内部状态直接暴露给用户，例如 `status: completed`
- 缺少独立的“正在思考”卡片
- 缺少真正的终端页面，只是在聊天时间线里嵌了终端输出块
- 工具卡片尺寸、间距、信息层级还不像 `codex app`
- `codex app-server` 网络抖动时没有明确的重连反馈
- 缺少类似 `已浏览 2 个文件，1 个搜索，1 个列表` 的过程摘要
- 缺少 plan 模式下的工作栏

## 2. 企业版卡片体系定版

### 2.1 结论

如果按企业级工具来做，建议把聊天时间线卡片体系定为 **12 种核心卡片 UI**。

这个数量的取舍逻辑是：

- 少于 10 种，会把不同语义硬塞进同一张卡，后续很难精细打磨
- 多于 14 种，V1 维护成本会过高，用户也会感到时间线过碎

建议的 12 种卡片如下：

| 编号 | 卡片名称 | 作用 | 当前状态 |
| --- | --- | --- | --- |
| 1 | `UserMessageCard` | 展示用户输入 | 已有雏形 |
| 2 | `AssistantMessageCard` | 展示 AI 正文回复 | 已有雏形 |
| 3 | `ThinkingCard` | 展示 `codex app-server` 正在思考 | 缺失 |
| 4 | `PlanCard` | 展示计划与进度 | 已有雏形 |
| 5 | `CommandCard` | 展示命令执行过程与输出 | 部分已有 |
| 6 | `FileChangeCard` | 展示文件改动与 diff | 部分已有 |
| 7 | `CommandApprovalCard` | 审批命令执行 | 已有雏形 |
| 8 | `FileChangeApprovalCard` | 审批文件改动 | 已有雏形 |
| 9 | `ChoiceCard` | 承接通用用户选择输入 | 缺失 |
| 10 | `ResultSummaryCard` | 展示结构化结果总结 | 需要重做 |
| 11 | `NoticeCard` | 展示系统提示、线程重建、主机切换等 | 缺失 |
| 12 | `ErrorCard` | 展示可操作错误与恢复建议 | 需要重做 |

### 2.2 每种卡片的详细定义

#### 2.2.1 `UserMessageCard`

用途：

- 展示用户原始输入
- 作为每一轮对话的起点

必需字段：

- `id`
- `role=user`
- `text`
- `createdAt`

可选字段：

- `hostId`
- `mentions`

UI 规则：

- 保持右对齐气泡（或使用现代宽版聊天样式中的右侧轻量气泡）
- 气泡背景色建议使用低饱和浅灰（如 `bg-gray-100` 或 `#F3F4F6`），字体确保高对比度（如纯黑或深灰）
- 不再使用尖角，改为全包围大圆角，建议粗略等同于 `rounded-2xl`（例如 16px 圆角）
- 增加足够宽裕的内边距，使其视觉有呼吸感（例如 `px-5 py-3`）
- 宽度不宜过大，建议最大 `72ch`
- 不显示技术性元信息
- 如果当前轮绑定了主机，可在气泡上方显示一个很小的 host pill

不应显示：

- `threadId`
- `turnId`
- `sessionId`
- `raw item id`

#### 2.2.2 `AssistantMessageCard`

用途：

- 展示 AI 的自然语言回复
- 承接流式增量输出

必需字段：

- `id`
- `role=assistant`
- `text`
- `status`

可选字段：

- `hostId`
- `citations`
- `references`

状态建议：

- `streaming`
- `completed`
- `interrupted`

UI 规则：

- 左对齐
- 不要做成厚重边框卡片，应该更像正文块
- 支持 Markdown，但默认保持简洁排版
- 与工具卡片形成主从关系，正文优先，工具次之

不应显示：

- `status: completed` 这类内部字样
- “Turn completed” 这样的技术性尾卡

#### 2.2.3 `ThinkingCard`

用途：

- 在用户发消息后立即反馈 “AI 已经接单”
- 在首个可见结果到来前，持续给出正在处理的阶段感知

必需字段：

- `id`
- `phase`
- `startedAt`

状态建议：

- `thinking`
- `planning`
- `waiting_approval`
- `executing`
- `finalizing`

文案建议：

- `正在思考`
- `正在规划步骤`
- `正在等待审批`
- `正在执行命令`
- `正在整理结果`

UI 规则：

- 不做成很重的卡片
- 用轻量 spinner + 文案 + 动态省略号
- 应该出现在用户消息之后 150ms 内
- 一旦 `PlanCard`、`AssistantMessageCard` 或 `CommandCard` 有首屏内容，`ThinkingCard` 要自然让位或转场

#### 2.2.4 `PlanCard`

用途：

- 展示本轮任务计划
- 让用户明确 AI 的下一步动作

必需字段：

- `id`
- `items[]`
- `status`

`items[]` 建议字段：

- `step`
- `status`

状态建议：

- `pending`
- `in_progress`
- `completed`

UI 规则：

- 同一轮内只保留 1 张 `PlanCard`
- 后续计划更新必须原位更新，不新增新卡
- 已完成项删除线
- 进行中项高亮
- 头部信息要紧凑，不要写成“Total 3 tasks, 1 completed”这种偏后台表格味的语言
- 参考 `codex app`，整体应是白底、圆角、轻边框的嵌入式卡片，而不是重色块面板
- 默认头部显示汇总句，如 `共 3 个任务，已经完成 0 个`
- 左侧使用轻量 plan/list 图标，右侧保留折叠/展开按钮
- 卡片主体展示编号步骤列表，支持长文本自动换行
- 列表视觉应更接近“任务清单”，而不是后台表格或项目管理看板
- 默认首次出现时展开，后续允许用户折叠为仅显示头部摘要

建议文案：

- 头部摘要：`共 3 个任务，已经完成 0 个`
- 进行中提示：`正在思考` / `正在规划步骤`

#### 2.2.5 `CommandCard`

用途：

- 展示命令执行过程
- 展示命令输出
- 承接命令执行的开始、流式回显、结束

必需字段：

- `id`
- `hostId`
- `command`
- `status`

可选字段：

- `cwd`
- `output`
- `exitCode`
- `durationMs`
- `startedAt`
- `endedAt`
- `terminalMode`
- `autoCloseOnComplete`

状态建议：

- `queued`
- `running`
- `completed`
- `failed`
- `cancelled`
- `waiting_approval`
- `auto_approved`

UI 规则：

- 默认显示命令头部和摘要
- 输出区域默认折叠或半展开
- 大输出不应一次性撑满整个时间线
- 成功时不需要再额外插一张“Command result: exit code 0”的卡
- 失败时在卡片头部突出错误状态
- 如果命令来自审批后的执行，卡片应进入“临时终端执行态”
- 临时终端执行态使用终端风格 UI，实时追加后续输出日志
- 用户可以看到命令执行过程，而不是只看到最终摘要
- 执行成功后，终端输出区自动关闭或收起，卡片仅保留简要结果摘要
- 执行失败时，终端输出区默认保持展开，方便查看失败原因
- 当前命令结束后，时间线继续后续步骤，不应让已完成的终端卡片长期占据大面积空间
- 聊天时间线里的临时终端卡片不等于独立终端页面，它是“过程回显 UI”，不是可长期驻留的交互终端

临时终端执行态建议：

- 卡片头部显示当前命令摘要
- 卡片主体显示终端输出流
- 终端区域使用等宽字体、深色背景或接近终端的轻量容器
- 支持滚动查看近期输出
- 命令运行时默认展开

执行完成后的时间线状态 UI 规则：

- 命令完成后，卡片应**退化为一条极其简洁的时间线状态记录**，消除厚重的边框，与上下文语境自然融合。
- 左侧显示动作摘要，例如：`已运行 sysctl -n hw.memsize`。
- 中间或右侧沿分割线展示命令耗时，例如：`已处理 38s`。
- 在页面上仅保留一根轻巧的分割线，表示该命令顺利执行并无缝衔接至下方的结果说明气泡。
- 如果命令执行失败，则完整保留原本包含输出区的卡片以供用户排查原因。

推荐文案：

- 运行中：`Running command`
- 完成后（若不折叠则显示状态，若按前述规则折叠则进入时间线摘要线）：`Command completed`
- 失败时：`Command failed`

额外动作：

- `展开输出`
- `复制命令`
- `在终端中打开`

#### 2.2.6 `FileChangeCard`

用途：

- 展示文件改动过程
- 展示变更文件列表与 diff 摘要

必需字段：

- `id`
- `status`
- `changes[]`

`changes[]` 建议字段：

- `path`
- `kind`
- `diff`

状态建议：

- `running`
- `completed`
- `failed`
- `waiting_approval`

UI 规则：

- 默认只展示文件数和首个 diff 摘要
- 大 diff 默认折叠
- 不要把整份 patch 直接铺满时间线
- 文件级别信息优先于原始 diff 文本

额外动作：

- `展开 diff`
- `复制 patch`

#### 2.2.7 `CommandApprovalCard`

用途：

- 审批任何有副作用的命令执行

必需字段：

- `approvalId`
- `hostId`
- `command`
- `cwd`
- `reason`
- `status`

可选字段：

- `riskLevel`
- `decisions[]`

状态建议：

- `pending`
- `accepted`
- `accepted_for_session`
- `declined`

UI 规则：

- 审批预判断、运行该命令的风险及必要性需作为清晰意图在顶部显示。
- 命令预览应该占据视觉中心，呈现为带有浅灰底色的单行或多行等宽代码块。
- 审批动作从单独的按钮组进化成**结构化的单选项系统（Radio 或 List）**。如：
  1. `是`
  2. `是，且对于以后内容开头的命令不再询问 {命令前缀}`
  3. `否，请告知 Codex 如何调整`
- 提供单独的取消 / 确认提交操作区。底部可设置 `跳过` 和 `提交 ↵`（可通过 Enter 提交，主按钮选用深色底色，无边框小圆角风格）。
- 卡片左下角应当显示环境标签与权限限定项（如支持下拉展开 `本地 v`、`默认权限 v` 的轻量图标按钮），确保用户执行前确认上下文环境。
- 当前审批卡本质上应是一张“内嵌表单”，具有较大的整体外圆角，视觉整体保持连贯。
- 一旦用户提交批准，该操作区即可平稳过渡或退场，在原位置衔接进入命令过程回显 UI 阶段。

按钮及表单动作：

- `跳过`（对应拒绝或略过此步骤）
- `提交（Enter）`（对应已选策略立刻进行处理）
- 全局下拉控制（例如：权限或系统主机范围等）

#### 2.2.8 `FileChangeApprovalCard`

用途：

- 审批文件写入、补丁、配置改动

必需字段：

- `approvalId`
- `hostId`
- `reason`
- `changes[]`
- `status`

可选字段：

- `grantRoot`
- `riskLevel`

UI 规则：

- 先展示摘要，再展示 diff
- 重点突出“改了哪些文件”
- 对超长 diff 只展示前几段摘要

按钮建议：

- `仅本次允许`
- `拒绝`

说明：

- 文件改动类审批不建议在 V1 就给“本会话长期放行”太宽的授权

#### 2.2.9 `ChoiceCard`

用途：

- 承接通用 `tool/requestUserInput`
- 例如：选择环境、选择执行策略、选择一个目录、补充一段简短输入

必需字段：

- `requestId`
- `title`
- `question`
- `options[]`
- `status`

UI 规则：

- 以单选为主
- 一屏内完成理解与操作
- 不与高风险审批卡混用

说明：

- 这是企业工具一定会需要的卡片，因为很多流程不是“批准/拒绝”，而是“请选择下一步”

#### 2.2.10 `ResultSummaryCard`

用途：

- 只在“结构化总结有明显价值”时出现
- 展示检查结论、机器信息、服务状态、关键指标摘要

必需字段：

- `id`
- `title`
- `summary`

可选字段：

- `kvRows[]`
- `highlights[]`
- `nextAction`

UI 规则：

- 用于浓缩结果，不用于回显原始技术状态
- 禁止输出 `status: completed`
- 禁止只写“Turn completed”

适用场景：

- `内存 32 GiB`
- `nginx 正在运行`
- `3 个文件已修改，等待审批`

#### 2.2.11 `NoticeCard`

用途：

- 展示系统级提示
- 展示不会阻断用户、但又值得知道的信息

适用场景：

- 线程自动重建
- 主机切换成功
- WebSocket 已重连
- 已应用本会话授权
- 终端断开后自动恢复

UI 规则：

- 低饱和、低干扰
- 更像时间线注记，而不是主卡片
- 对于重连中的提示，允许短时原位更新同一张卡，而不是连续插入多张新卡

说明：

- 现在很多 `ResultCard` 实际更适合改成 `NoticeCard`
- `Reconnecting... 1/5` 到 `Reconnecting... 5/5` 这类连接恢复提示，应优先落在 `NoticeCard` 或顶部状态条，而不是普通消息正文

#### 2.2.12 `ErrorCard`

用途：

- 展示明确错误
- 给出恢复建议

必需字段：

- `id`
- `title`
- `message`

可选字段：

- `actionLabel`
- `actionType`
- `retryable`

适用场景：

- `codex app-server` 不可用
- 主机离线
- 命令执行失败
- 审批回传失败
- `codex app-server` 在 5 次重试后仍无法恢复连接

UI 规则：

- 错误信息必须说人话
- 需要可恢复建议
- 不要只输出底层 RPC 字符串

## 3. 卡片映射原则

企业版 UI 不能再用“一个后端事件对应一张原样卡片”的方式渲染，而应该做事件归一化。

建议映射如下：

| 后端事件 | 目标 UI |
| --- | --- |
| 用户 `POST /api/v1/chat/message` | `UserMessageCard` |
| 首个事件到来前 | `ThinkingCard` |
| `turn/plan/updated` | `PlanCard` |
| `item/agentMessage/delta` | `AssistantMessageCard` |
| `item/started` type=`commandExecution` | `CommandCard` |
| `item/commandExecution/outputDelta` | 更新同一张 `CommandCard` |
| `item/started` type=`fileChange` | `FileChangeCard` |
| `item/fileChange/outputDelta` | 更新同一张 `FileChangeCard` |
| `item/commandExecution/requestApproval` | `CommandApprovalCard` |
| 命令审批通过后 | 同位置切换为 `CommandCard` 的临时终端执行态 |
| 命令执行中的流式日志 | 持续追加到该终端执行态 |
| 命令执行完成 | 自动关闭终端 body，保留摘要后继续下一步 |
| `item/fileChange/requestApproval` | `FileChangeApprovalCard` |
| `tool/requestUserInput` | `ChoiceCard` |
| 结构化业务结果 | `ResultSummaryCard` |
| 线程重建、重连、授权命中 | `NoticeCard` |
| 不可恢复或需明确提示的错误 | `ErrorCard` |
| `codex app-server` 重试耗尽 | `ErrorCard` |

明确禁止：

- 每轮固定插入一张 `Turn completed`
- 成功命令后固定插入 `exit code: 0`
- 把原始内部 `status` 当成最终给用户看的正文

## 4. 终端页面设计

### 4.1 设计目标

新增一个独立终端页面，让用户可以进入“当前选择主机”的真实终端，而不是只能在聊天卡片里看命令输出。

终端页面目标：

- 可直接进入所选主机
- 支持低延迟输入输出
- 支持窗口缩放
- 支持中断信号
- 支持断线重连
- 能从聊天页一键跳转

### 4.2 页面入口

建议提供 3 个入口：

- 顶部主机 pill 下拉菜单中的 `打开终端`
- 主机选择弹窗中的 `进入终端`
- `CommandCard` 内的 `在终端中打开`

### 4.3 路由建议

建议路由：

- `/terminal/:hostId`

示例：

- `/terminal/server-local`

这样 URL 本身就能表达当前终端目标主机，也方便后续做多标签页恢复。

### 4.4 页面结构

建议采用“单主终端 + 轻量工具栏”的结构。

页面布局：

1. 顶部栏
2. 主终端区
3. 右侧可折叠信息抽屉

顶部栏建议包含：

- 主机名
- 主机在线状态
- 当前 shell
- 当前工作目录
- 连接状态
- `重新连接`
- `Ctrl+C`
- `清屏`
- `返回聊天`

主终端区：

- 使用 `xterm.js`
- 占满主要可视高度
- 优先保证输入延迟和字号可读性

右侧抽屉建议 V1 只放：

- 会话 ID
- 启动时间
- 最近一次重连时间
- 简短说明

V2 再补：

- 会话历史
- 最近命令
- 环境变量白名单

### 4.5 交互原则

- 终端页与聊天页是并列一级页面，不嵌在聊天时间线里
- 聊天仍用于“让 AI 执行工作”
- 终端页用于“人工接管、核查、临时操作”
- 两者必须共享当前选中主机上下文

### 4.6 Browser <-> ai-server 协议建议

终端场景仍建议使用 `WebSocket`。

建议接口：

- `POST /api/v1/terminal/sessions`
- `GET /api/v1/terminal/sessions/:id`
- `WS /api/v1/terminal/ws?sessionId=...`

`POST /api/v1/terminal/sessions` 请求建议：

```json
{
  "hostId": "server-local",
  "cwd": "~",
  "shell": "/bin/zsh",
  "cols": 140,
  "rows": 36
}
```

返回建议：

```json
{
  "sessionId": "term_xxx",
  "hostId": "server-local",
  "status": "connecting"
}
```

WebSocket 客户端消息建议：

```json
{ "type": "input", "data": "ls -la\r" }
{ "type": "resize", "cols": 140, "rows": 36 }
{ "type": "signal", "signal": "SIGINT" }
{ "type": "close" }
```

WebSocket 服务端消息建议：

```json
{ "type": "ready", "sessionId": "term_xxx" }
{ "type": "output", "data": "total 12\r\n" }
{ "type": "exit", "code": 0 }
{ "type": "status", "status": "reconnecting" }
{ "type": "error", "message": "host offline" }
```

### 4.7 ai-server <-> Host Agent 对接建议

终端页背后最终应走：

- `pty/open`
- `pty/input`
- `pty/resize`
- `pty/signal`
- `pty/close`

对于 `server-local`，可以先允许一个本地 PTY fallback，加快 V1 联调，但最终抽象必须和远程主机一致。

### 4.8 终端页验收标准

- 进入页面后 1 秒内看到连接状态
- 主机在线时可完成输入、回显、缩放
- 刷新页面后可恢复当前 session 或明确提示重建
- 离线主机不能进入假终端
- 聊天页和终端页的主机上下文保持一致

补充说明：

- 独立终端页面用于“人工接管和长期操作”
- 聊天时间线中的终端卡片用于“命令审批通过后的短时过程回显”
- 两者都可以复用终端视觉语言，但产品语义不同，不应混为同一个组件状态

## 5. 对齐 `codex app` 的样式原则

### 5.1 总体原则

要对齐的不是“像不像某个截图”，而是这 4 个核心气质：

- 对话优先
- 工具过程次级
- 内部状态不打扰
- 用户始终知道系统在做什么

### 5.2 信息层级调整

建议把时间线信息分成 3 层：

1. 主叙事层
2. 过程层
3. 系统层

主叙事层只放：

- 用户消息
- AI 正文回复

过程层放：

- 计划
- 命令
- 文件变更
- 审批

系统层放：

- 重连
- 线程重建
- 会话授权命中
- 轻量错误提示

系统层必须视觉弱化，不能和 AI 正文抢注意力。

### 5.3 明确哪些字段不要输出到对话框

以下字段不应直接显示在聊天正文里：

- `threadId`
- `turnId`
- `itemId`
- `approvalId`
- `requestIdRaw`
- `status: completed`
- `Turn completed`
- `Command execution`
- `File change`
- 完整 `cwd` 的重复展示
- 完整时间戳
- 全量 diff
- 全量 stdout

这些字段可以：

- 放在 hover tooltip
- 放在折叠详情区
- 放在开发调试模式

### 5.4 哪些字段应该保留但变小

以下信息建议保留，但降级为次要元信息：

- `hostId`
- `cwd`
- `exitCode`
- `duration`
- `riskLevel`
- `changed files count`

展示规则：

- 用 11px 到 12px 的 metadata 样式
- 放在卡片 header 右侧或正文下方
- 默认一行内截断

### 5.5 尺寸建议

对话主内容列建议：

- 为适应更为丰富的结构化单选表单、代码展示块与时间线条，最大宽度建议进一步放宽至 `900px` 甚至 `1024px`，确保宽屏设备的高利用率与清晰排版。

正文文字：

- 整体对话正文文本建议维持在 `15px` 到 `16px`。
- 加大 `line-height`，如升至 `1.7` 及左右，给予充足的段落间呼吸感和高度。
- 选项列表 / 表单控件字体部分，建议尺寸 `14px` 到 `15px` 不等。

元信息及时间线标注：

- 常规记录点文字（如：`已处理 38s`）缩小到 `12px` 左右。
- 色彩选用浅灰弱化层级（如类似 `text-gray-400` / `text-gray-500`）。

工具及审批卡片交互组件尺寸：

- 单行或列表选项高度：加大到 `40px` 至 `48px`，提供舒适的大面积点击热区以及列表间的 Hover 设计。
- 底部操作栏（例如跳过/提交按钮组）：高度保持在紧凑的 `32px` 到 `36px` 之间。
- 卡片边框与圆角：外圈容器使用极浅极细的边线（如 `border-gray-200` 或低透明阴影线），转角保持现代感的大圆角 `rounded-2xl`（约16px圆角半径）。

图标尺寸：

- 标准环境为 `14px` 到 `16px`。

用户气泡：

- 背景色块选用干净明亮或低饱和的浅灰。
- 不再使用对话尖角设计，全包围圆角形态保持与上方组件的一致体验（同样为 `rounded-2xl` 风格）。
- 横向纵向保留更加富余的内补距。

Assistant 正文：

- 不加大面积浅色底
- 保持像自然阅读区

命令卡 / 文件卡：

- 允许更宽
- 但必须在统一内容列内

### 5.6 需要直接删掉的现有表现

建议直接删除或停止显示：

- 对话结束时的 `status: completed`
- 只为了显示状态而存在的 `ResultCard`
- 与用户无关的技术标题
- 把每个过程都做成同样重量的卡片

### 5.7 过程摘要条

参考 `codex app` 的业务逻辑，时间线顶部或当前轮次头部需要一条轻量过程摘要条，用来告诉用户这轮工作已经做了哪些“浏览型动作”。

目标效果示例：

- `已浏览 2 个文件，1 个搜索，1 个列表`
- `现在浏览 design_ui_0324.md`
- `已浏览 1 个文件`
- `已搜索网页（site:developers.openai.com/codex/auth codex app-server chatgpt oauth...）`
- `现在搜索网页（site:developers.openai.com/codex/auth codex app-server chatgpt oauth...）`

建议摘要维度：

- `filesViewed`
- `searchCount`
- `listCount`
- `commandsRun`
- `currentReadingFile`
- `viewedFiles[]`
- `currentWebSearchQuery`
- `searchedWebQueries[]`

展示规则：

- 这是过程摘要，不是正文卡片
- 默认使用一行轻量 metadata 文案
- 紧跟在 `ThinkingCard`、`PlanCard` 或当前轮头部区域下方
- 只在计数大于 0 时展示对应项
- 同一轮内原位更新，不新增多条
- 参考 `codex app`，过程摘要区允许由多行轻量状态组成，而不是只保留一行
- 当 AI 正在查看某个文件时，优先显示进行中状态：`现在浏览 xxxxx`
- 文件查看完成后，恢复为汇总状态：`已浏览 1 个文件`
- 汇总状态可点击展开，显示本轮已查看文件明细
- 明细默认折叠，避免把完整文件列表铺满主界面
- 当 AI 正在做网页搜索时，摘要区第二行显示：`现在搜索网页（query）`
- 搜索完成后，第二行更新为：`已搜索网页（query）`
- 若本轮出现多个搜索，主汇总行显示 `N 个搜索`，第二行默认展示最近一次搜索 query
- `已搜索网页（query）` 这一行应支持省略号截断，避免超长 query 撑坏布局
- 用户点击 `N 个搜索` 或 `已搜索网页（query）` 后，可展开本轮搜索明细

建议快照字段：

```json
{
  "runtime": {
    "activity": {
      "filesViewed": 2,
      "searchCount": 1,
      "listCount": 1,
      "commandsRun": 0,
      "currentReadingFile": "design_ui_0324.md",
      "currentWebSearchQuery": "site:developers.openai.com/codex/auth codex app-server chatgpt oauth",
      "viewedFiles": [
        {
          "label": "Read design_ui_0324.md",
          "path": "design_ui_0324.md"
        }
      ],
      "searchedWebQueries": [
        {
          "label": "Search the web: site:developers.openai.com/codex/auth codex app-server chatgpt oauth",
          "query": "site:developers.openai.com/codex/auth codex app-server chatgpt oauth"
        }
      ]
    }
  }
}
```

文案生成规则：

- 如果 `currentReadingFile` 非空，优先显示 `现在浏览 {filename}`
- 如果 `currentWebSearchQuery` 非空，额外显示第二行：`现在搜索网页（{query}）`
- 优先输出 `已浏览 X 个文件`
- 其次输出 `Y 个搜索`
- 再输出 `Z 个列表`
- 有命令执行时追加 `N 个命令`
- 用户点击 `已浏览 X 个文件` 后，展开 `viewedFiles[]` 明细
- 搜索完成后，第二行切换为 `已搜索网页（{query}）`
- 用户点击 `Y 个搜索` 或 `已搜索网页（{query}）` 后，展开 `searchedWebQueries[]` 明细

示例：

- `现在浏览 design_ui_0324.md`
- `现在搜索网页（site:developers.openai.com/codex/auth codex app-server chatgpt oauth...）`
- `已浏览 1 个文件`
- `已搜索网页（site:developers.openai.com/codex/auth codex app-server chatgpt oauth...）`
- `已浏览 2 个文件，1 个搜索，1 个列表`
- `已浏览 4 个文件，2 个搜索，3 个命令`

点击展开后的明细示例：

- `Read design_ui_0324.md`
- `Read todo_mvp_0324.md`
- `Search the web: site:developers.openai.com/codex/auth codex app-server chatgpt oauth`
- `Search the web: codex app-server approval flow`

说明：

- 这条摘要的目标是让用户快速感知 AI “已经做了什么侦查工作”
- 不应显示底层 item 类型、原始 tool name、文件完整路径列表
- 文件明细建议显示为接近 `codex app` 的英文动作短语，例如 `Read design_ui_0324.md`
- 若同一文件在同一轮被重复查看，摘要计数可去重，明细中也应默认去重
- 搜索明细建议显示为接近 `codex app` 的英文动作短语，例如 `Search the web: ...`
- 搜索 query 默认显示最近一次，历史查询放在展开明细里

### 5.8 Plan 模式工作栏

当 `codex app-server` 进入 plan 模式时，页面需要参考 `codex app` 显示一个 plan 工作栏，而不是只在时间线里塞一张普通 `PlanCard`。

目标：

- 让用户一眼知道当前回合已进入 plan 模式
- 让计划、执行、审批三种状态切换更加明显
- 让 `PlanCard` 从“内容卡片”升级为“当前工作态的一部分”

建议位置：

- 放在当前回合的内容流里
- 位于 `正在思考` 文案下方、当前轮 assistant 正文上方
- 不做成全局吸顶 banner，不抢聊天主叙事

工作栏建议包含：

- 左侧 plan/list 图标
- 当前计划完成度摘要
- 右侧折叠/展开按钮
- 展开后的步骤列表

建议展示结构：

- 第一层：一行轻量状态文案，如 `正在思考`
- 第二层：plan 卡片头部摘要，如 `共 3 个任务，已经完成 0 个`
- 第三层：可展开的步骤列表，复用 `PlanCard` 数据

参考样式约束：

- 整体为白色卡片，细边框，圆角较大，阴影很轻
- 头部信息左对齐，图标与摘要在同一行
- 右上角保留一个轻量展开/收起图标
- 步骤列表按 `1. 2. 3.` 的阅读顺序展示
- 每个步骤前保留空心圆或轻量状态点，视觉上接近参考图里的 checklist
- 已完成项可弱化或加删除线，但不要使用过重的成功绿
- 卡片内边距要大，行距要松，保持“像思考中的工作清单”而不是“表格”

交互规则：

- 进入 plan 模式时工作栏立即出现
- 退出 plan 模式或 turn 完成时工作栏收起
- 若用户折叠工作栏，仍保留一行模式状态
- 工作栏与 `ThinkingCard` 可同时存在，但应以工作栏为主、思考文案为辅
- 如果当前仍在 planning 阶段，头部上方保留单独一行 `正在思考`
- 若计划项发生更新，应原位刷新，不允许闪烁或重建整张卡
- 展开/收起状态应尽量在本轮内保持，避免每次增量更新都重置用户视图

建议快照字段：

```json
{
  "runtime": {
    "plan": {
      "active": true,
      "mode": "plan",
      "phase": "planning",
      "summary": "共 3 个任务，已经完成 0 个",
      "hint": "正在思考",
      "expanded": true
    }
  }
}
```

## 6. “正在思考”设计

### 6.1 问题定义

当前发送消息后，前端只知道 HTTP 请求已返回，但不知道 `codex app-server` 是否仍在思考，因此用户会出现“页面像卡住了”的感受。

### 6.2 目标体验

用户发送消息后，应立即看到：

- `正在思考`

后续根据事件推进，文案可变成：

- `正在规划步骤`
- `正在等待审批`
- `正在执行命令`
- `正在整理结果`

### 6.3 推荐状态机

建议新增一套 turn 级 UI 状态：

- `idle`
- `thinking`
- `planning`
- `waiting_approval`
- `executing`
- `finalizing`
- `completed`
- `failed`

同时建议新增一套 codex 连接级状态：

- `connected`
- `reconnecting`
- `disconnected`
- `stopped`

建议新增到快照中的字段：

```json
{
  "runtime": {
    "turn": {
      "active": true,
      "phase": "thinking",
      "hostId": "server-local",
      "startedAt": "2026-03-24T12:00:00Z",
      "updatedAt": "2026-03-24T12:00:02Z"
    },
    "codex": {
      "status": "reconnecting",
      "retryAttempt": 2,
      "retryMax": 5,
      "lastError": "network timeout"
    }
  }
}
```

### 6.4 状态推进规则

建议规则：

1. 用户点击发送后，本地立即创建 `ThinkingCard`
2. `POST /api/v1/chat/message` 返回 `202` 后，保持 `ThinkingCard`
3. 收到 `turn/plan/updated`，`phase -> planning`
4. 收到 `requestApproval`，`phase -> waiting_approval`
5. 收到 `item/started` 且是命令或文件改动，`phase -> executing`
6. 收到首个 assistant 正文 delta 后，`ThinkingCard` 可以淡出
7. 收到 `turn/completed`，清理 turn 状态
8. 收到错误，转为 `ErrorCard`
9. 若 `codex app-server` 连接中断，转入连接重试状态，不清空当前 turn UI

### 6.5 文案原则

- 用中文
- 简短
- 不说底层协议
- 不出现“turn started”“item completed”这类工程术语

推荐文案：

- `正在思考`
- `正在分析你的请求`
- `正在检查主机状态`
- `正在执行命令`
- `正在等待你的确认`

### 6.6 验收标准

- 用户发送后 150ms 内能看到反馈
- 在 AI 无正文输出但后台仍在处理时，界面不显得“假死”
- 一轮执行结束后，`ThinkingCard` 自动退出，不残留空卡

### 6.7 `codex app-server` 重连设计

当 `codex app-server` 因网络原因断连时，页面需要参考 `codex app` 的逻辑进行可视化重试。

要求：

- 固定最多重试 5 次
- 每次重试都要显示当前进度
- 文案使用英文重连提示，保持和 `codex app` 感知接近
- 第 5 次后仍失败，页面必须明确显示“已停止连接 / 当前不可用”

重试文案要求：

- `Reconnecting... 1/5`
- `Reconnecting... 2/5`
- `Reconnecting... 3/5`
- `Reconnecting... 4/5`
- `Reconnecting... 5/5`

展示位置建议：

- 优先在顶部全局状态条展示
- 同时可在时间线中用 1 张可更新的 `NoticeCard` 镜像当前状态

状态流建议：

1. `connected`
2. 连接异常
3. `reconnecting(1/5)`
4. `reconnecting(2/5)`
5. `reconnecting(3/5)`
6. `reconnecting(4/5)`
7. `reconnecting(5/5)`
8. 成功恢复则回到 `connected`
9. 若失败则进入 `stopped`

失败后的页面要求：

- 顶部状态明确显示 `codex app-server stopped` 或等价中文说明
- 输入框禁用
- 已有时间线内容保留，不清空
- 提供显式操作按钮：
  - `Retry`
  - `Refresh`

设计原则：

- 重连状态属于系统层，不应冒充 assistant 回复
- 5 次重试应原位更新同一状态区域，而不是堆叠 5 张卡片
- 重试期间如果当前 turn 还未结束，`ThinkingCard` 保持但要附带“连接恢复中”的弱提示

## 7. 实施顺序建议

建议分 4 个小阶段做，而不是一次性重构全部 UI。

### Phase 1：卡片归一化

- 拆分当前 `StepCard` 和 `ResultCard`
- 停止输出技术性尾卡
- 建立 12 种卡片的前端类型与渲染器映射

### Phase 2：对齐 `codex app` 样式

- 重做对话主列宽度、间距、层级
- 把系统层信息弱化
- 调整卡片大小和元信息展示规则

### Phase 3：补“正在思考”

- 增加 turn 级 runtime state
- 增加 codex 连接级 runtime state
- 新增 `ThinkingCard`
- 完成状态机与转场动画

### Phase 4：终端页面

- 新增 `/terminal/:hostId`
- 打通终端 session 协议
- 增加从聊天页跳转到终端页的入口

## 8. 最终定版结论

本次 UI 优化阶段，建议正式定版为：

- 聊天时间线支持 **12 种核心卡片 UI**
- 新增独立终端页面 `/terminal/:hostId`
- 对话页整体风格向 `codex app` 对齐
- 停止把内部技术状态直接抛给用户
- 引入 `ThinkingCard`，明确表达 `codex app-server` 正在处理
- 增加 5 次重试的 `codex app-server` 重连可视化
- 增加 `已浏览 2 个文件，1 个搜索，1 个列表` 这一类过程摘要条
- 增加 plan 模式工作栏

如果按这个文档执行，后续每一张卡片都可以进入“精细打磨”阶段，而不是继续在 MVP 卡片上打补丁。
