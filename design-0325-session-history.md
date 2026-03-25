# design-0325-session-history

## 0. 文档定位

本文是“会话列表 / 历史会话”专项设计文档，目标是在当前 AIOps Codex Web 控制台中补齐一套可落地的会话管理能力：

- 左侧菜单提供“历史会话”入口
- 点击后弹出历史会话列表
- 每次新对话都形成独立会话
- 可以查看并切换历史会话
- 会话记录在刷新页面和服务重启后仍可恢复

本文优先贴合当前仓库结构，不做脱离现状的理想化设计。

## 1. 当前状态与问题

结合当前实现，现状如下：

- 左侧 Sidebar 目前只有一个固定聊天入口，代码在 `web/src/App.vue`
- 顶部主按钮目前叫 `New Thread`，实际调用的是 `POST /api/v1/thread/reset`
- 当前 `thread/reset` 的语义是“清空当前会话上下文”，不是“创建新会话”
- 后端 `SessionState` 既承担浏览器 cookie 会话，又承担聊天会话，两个概念目前耦合在一起
- `internal/store/memory.go` 当前持久化的内容主要是认证、选中主机、审批记忆和时间戳
- 当前不会持久化 `Cards`，也不会持久化完整聊天记录
- `GET /api/v1/state` 和 `/ws` 都只面向“当前 cookie 绑定的单个 session”

这意味着当前系统只能支持“一个浏览器窗口对应一个当前对话”，还不具备以下能力：

- 保留多条历史会话
- 为每次新会话生成独立入口
- 在历史会话之间切换
- 服务重启后恢复聊天时间线

所以这次需求不是简单加一个前端弹窗，而是要补齐“会话索引 + 会话切换 + 会话持久化”三件事。

## 2. 目标与非目标

### 2.1 目标

本次 V1 目标如下：

- 左侧菜单新增“历史会话”按钮，点击后弹出会话列表抽屉
- 当前“New Thread”改为“新建会话”
- 每次点击“新建会话”都创建一个新的逻辑会话，而不是清空当前会话
- 历史会话列表按最近活跃时间倒序展示
- 每条会话展示标题、预览、最近活跃时间、运行状态、绑定主机
- 点击历史项后切换到该会话并恢复聊天时间线
- 页面刷新后历史列表仍在
- `ai-server` 重启后历史列表与聊天记录仍在
- 登录状态在同一浏览器下默认复用，不要求每个会话重新登录

### 2.2 非目标

本次 V1 不做：

- 多用户共享会话
- 会话全文搜索
- 会话删除 / 归档
- 会话手动重命名
- 多标签页同时操作不同会话
- 正在运行中的会话后台实时订阅

这些能力可以在 V2 再补。

## 3. 核心结论

### 3.1 概念必须拆开

当前代码里的 `session` 同时表示两件事：

- 浏览器 cookie 对应的访问上下文
- 一条聊天会话

这个耦合是历史会话功能的最大阻碍。V1 需要把概念拆成两层：

- `BrowserSession`：浏览器访问上下文，由 cookie 标识
- `ChatSession`：一条实际的聊天会话，出现在历史列表里

其中：

- 一个 `BrowserSession` 可以拥有多条 `ChatSession`
- 一个 `BrowserSession` 同一时刻只有一条“当前激活的 ChatSession”
- 认证信息继续通过现有 `AuthSessionState` 复用

### 3.2 “新建会话”与“清空当前上下文”必须分开

当前 `New Thread` 按钮直接调用 `thread/reset`，这会把现有记录清掉，不符合“保留每次会话记录”的目标。

因此需要做产品语义拆分：

- Sidebar 主按钮：`新建会话`
- 当前 `/api/v1/thread/reset`：保留，但改为“清空当前会话上下文”

也就是说：

- “新建会话”产生一条新的历史记录
- “清空上下文”只影响当前会话，不影响历史列表中的其他会话

### 3.3 历史记录不能继续塞在单个总状态文件里

当前 `.data/ai-server-state.json` 是一个总状态文件。如果把全部 `cards` 直接持久化到这个文件里，会有两个问题：

- 每次流式消息增量都会重写整个文件
- 历史会话变多后，单文件体积会快速膨胀

因此建议：

- 总状态文件只保存会话元数据和索引
- 每条会话的完整时间线单独存为一个 transcript 文件

推荐目录结构：

```text
.data/
  ai-server-state.json
  sessions/
    sess-aaa.json
    sess-bbb.json
    sess-ccc.json
```

这是最适合当前项目的持久化方式。

## 4. 交互设计

### 4.1 Sidebar 调整

左侧 Sidebar 建议改为两级入口：

- 主按钮 1：`新建会话`
- 主按钮 2：`历史会话`

其中：

- `新建会话` 创建一条空白会话并自动切换过去
- `历史会话` 打开左侧抽屉，不离开当前页面

当前固定的 `Codex Assistant` 单项入口不再有意义，后续可替换为“当前会话摘要卡”。

### 4.2 历史会话抽屉

交互形式建议使用左侧抽屉，而不是全屏 modal。

原因：

- 用户已经把“会话入口”认知为左侧导航的一部分
- 抽屉更像工作台，不会打断聊天主视图
- 与现有 `Sidebar + Main Canvas` 布局兼容性更好

抽屉建议信息结构：

- 顶部：标题 `历史会话`
- 顶部右侧：关闭按钮
- 顶部下方：`新建会话` 次级按钮
- 主体：历史会话列表
- 空状态：`还没有历史会话，先开始第一段对话`

### 4.3 会话列表项

每个会话列表项展示：

- 标题
- 最近一条预览
- 最近活跃时间
- 当前绑定主机
- 状态标签

状态标签建议：

- `空白`
- `执行中`
- `待确认`
- `已完成`
- `失败`

标题生成规则：

- 优先取第一条 `UserMessageCard` 的正文前 24 个中文字符
- 去掉换行和多余空格
- 如果还没有用户消息，则显示 `新会话`

预览生成规则：

- 优先取最后一条用户或助手正文
- 截断到 48 到 60 个字符
- 没有内容时显示 `暂无消息`

### 4.4 切换规则

V1 为了控制复杂度，建议限制如下：

- 当前会话如果 `runtime.turn.active = true`，允许打开历史列表
- 但不允许切换到其他会话
- 列表项置灰，并提示 `当前任务执行中，完成后再切换`

这样可以避免以下复杂度：

- 正在运行时切走后 WebSocket 订阅如何迁移
- 后台会话的实时状态如何同步
- 待审批卡片切走后如何恢复

这个限制不影响历史记录能力，但会显著降低实现风险。

## 5. 数据模型设计

### 5.1 BrowserSession

新增浏览器维度状态：

```go
type BrowserSessionState struct {
    ID              string
    ActiveSessionID string
    SessionIDs      []string
    CreatedAt       string
    UpdatedAt       string
}
```

职责：

- 承接 cookie
- 维护当前浏览器拥有的会话列表
- 记录当前激活的会话

### 5.2 ChatSession 元数据

当前 `SessionState` 继续承载聊天会话，但需要补充元数据字段：

```go
type SessionState struct {
    ID             string
    AuthSessionID  string
    Title          string
    Preview        string
    MessageCount   int
    SelectedHostID string
    ThreadID       string
    TurnID         string
    Cards          []model.Card
    ...
    CreatedAt      string
    LastActivityAt string
}
```

说明：

- `Title` 和 `Preview` 用于历史列表
- `MessageCount` 用于后续展示和调试
- `Cards` 仍是当前活跃会话的运行态载体

### 5.3 会话摘要模型

新增前端列表接口返回结构：

```go
type SessionSummary struct {
    ID             string `json:"id"`
    Title          string `json:"title"`
    Preview        string `json:"preview"`
    SelectedHostID string `json:"selectedHostId"`
    Status         string `json:"status"`
    MessageCount   int    `json:"messageCount"`
    CreatedAt      string `json:"createdAt"`
    LastActivityAt string `json:"lastActivityAt"`
}
```

状态字段建议由后端统一派生，前端不自行推断。

## 6. 持久化设计

### 6.1 总状态文件

`ai-server-state.json` 保留并扩展为：

- `browserSessions`
- `sessions` 的轻量元数据
- `authSessions`
- `hosts`
- 其他全局映射

不在总文件中存完整 `cards`。

### 6.2 单会话 transcript 文件

每条 `ChatSession` 的完整聊天记录单独落盘：

```go
type SessionTranscript struct {
    Version   int          `json:"version"`
    SessionID string       `json:"sessionId"`
    Cards     []model.Card `json:"cards"`
}
```

V1 建议只持久化：

- `Cards`

V1 不恢复：

- 运行中的 `TurnID`
- pending 的 `Approvals`
- pending 的 `Choices`

原因是：

- 服务重启后这些中间态本来就无法安全恢复
- 历史展示真正需要的是“已生成的卡片记录”

重启恢复时统一处理为：

- `Runtime` 置回 `idle`
- 未完成流程不尝试接续
- 如有需要，补一张 `NoticeCard` 说明“上次任务因服务重启中断”

### 6.3 落盘时机

为了避免每个 token 都重写文件，建议：

- 卡片 append/update 后标记 transcript dirty
- 使用 300ms 到 500ms debounce 合并写盘
- 在以下时机强制 flush：
  - 会话切换前
  - `thread/reset` 后
  - turn 结束后
  - 服务退出前

这样既能保留历史，也不会造成过高 IO 压力。

## 7. API 设计

### 7.1 新增接口

### `GET /api/v1/sessions`

返回当前浏览器下的历史会话列表：

```json
{
  "activeSessionId": "sess-123",
  "sessions": [
    {
      "id": "sess-123",
      "title": "帮我排查今天的构建失败",
      "preview": "我先查看 CI 配置和最近报错日志。",
      "selectedHostId": "server-local",
      "status": "completed",
      "messageCount": 8,
      "createdAt": "2026-03-25T10:00:00Z",
      "lastActivityAt": "2026-03-25T10:12:00Z"
    }
  ]
}
```

### `POST /api/v1/sessions`

创建新会话，并自动切换为当前激活会话。

返回：

- 新会话 summary
- 新会话 snapshot

### `POST /api/v1/sessions/:id/activate`

切换当前激活会话。

约束：

- 若当前会话仍在执行中，返回 `409`

### 7.2 现有接口的语义变化

以下接口继续保留原 URL，但作用对象从“cookie 对应的单个 session”改为“BrowserSession 当前激活的 ChatSession”：

- `GET /api/v1/state`
- `POST /api/v1/chat/message`
- `POST /api/v1/chat/stop`
- `POST /api/v1/thread/reset`
- `POST /api/v1/approvals/:id/decision`
- `POST /api/v1/choices/:id/answer`
- `GET /ws`

这样前端聊天页几乎不用重写，只需要在切换会话时：

- 调用 activate 接口
- 重新拉取 state
- 重连 websocket

### 7.3 认证复用

新建会话时，默认复用当前浏览器下最近一次有效的 `AuthSessionID`，这样可以保证：

- 同一浏览器下多条历史会话共用登录态
- 新会话不需要重新登录 GPT

这部分可以直接复用当前已经存在的 `AuthSessionState` 思路，不必另起一套认证模型。

## 8. 前端实现设计

### 8.1 Store 改造

`web/src/store.js` 需要新增：

- `sessionList`
- `activeSessionId`
- `historyDrawerOpen`
- `historyLoading`

新增 action：

- `fetchSessions()`
- `createSession()`
- `activateSession(id)`
- `refreshActiveSession()`

切换会话时的标准流程：

1. `POST /api/v1/sessions/:id/activate`
2. 关闭当前 websocket
3. `GET /api/v1/state`
4. 重新 `connectWs()`
5. `GET /api/v1/sessions` 刷新左侧列表

### 8.2 App.vue 改造

`web/src/App.vue` 主要改动：

- 将 `New Thread` 按钮改成 `新建会话`
- 在 Sidebar 增加 `历史会话` 按钮
- 引入 `SessionHistoryDrawer.vue`
- 当前固定 `Codex Assistant` 条目改为“当前会话”展示

### 8.3 新组件

建议新增：

- `web/src/components/SessionHistoryDrawer.vue`
- `web/src/components/SessionListItem.vue`

职责划分：

- `SessionHistoryDrawer.vue` 负责抽屉层、列表加载态、空态、新建按钮
- `SessionListItem.vue` 负责单条会话摘要渲染

### 8.4 ChatPage 改造范围

`web/src/pages/ChatPage.vue` 不需要大改。

仅建议补两点：

- 空白会话时展示 `新会话` 空态
- 后续在页头或更多菜单中提供“清空当前上下文”，映射到现有 `thread/reset`

也就是说，这个功能的主体改造在 Sidebar、Store 和后端，不在聊天流本身。

## 9. 后端实现设计

### 9.1 Store 层

`internal/store/memory.go` 需要补的核心能力：

- BrowserSession 的创建 / 查询 / 激活
- ChatSession 的创建 / 列表 / 排序
- session summary 的派生与更新
- transcript 的加载 / 保存 / debounce flush

建议新增方法：

- `EnsureBrowserSession(browserID string) ...`
- `CreateChatSession(browserID string) ...`
- `ActivateChatSession(browserID, sessionID string) error`
- `ChatSessionSummaries(browserID string) []model.SessionSummary`
- `SaveSessionTranscript(sessionID string) error`
- `LoadSessionTranscript(sessionID string) error`

### 9.2 Server 层

`internal/server/server.go` 需要补的核心能力：

- cookie 不再直接等价于 ChatSession
- 新增 BrowserSession 解析逻辑
- 会话列表 / 创建 / 激活接口
- `/ws` 基于当前激活会话订阅

同时需要把当前这段语义从“新建线程”校正为“清空当前会话上下文”：

- `handleThreadReset`

### 9.3 元数据更新规则

以下时机需要更新 `Title / Preview / MessageCount / LastActivityAt`：

- 新增用户消息卡片后
- 助手消息完成后
- 出现错误卡片后
- `thread/reset` 后

推荐规则：

- `Title` 一旦由首条用户消息生成，默认不再自动覆盖
- `Preview` 始终取最后一条可读卡片内容
- `MessageCount` 只统计用户和助手正文卡片

## 10. 兼容性与迁移

### 10.1 旧 cookie 迁移

当前 cookie 里放的是旧的 `SessionID`。升级后要避免把老用户直接登出或丢失当前记录。

迁移策略建议：

- 如果 cookie 命中的不是 `BrowserSession`，但命中了旧 `ChatSession`
- 则自动创建一个新的 `BrowserSession`
- 把这个旧 `ChatSession` 作为它的第一条历史会话
- 并设置为当前激活会话

这样可以平滑升级。

### 10.2 旧状态文件迁移

旧的 `ai-server-state.json` 中没有 transcript 文件。

升级时建议：

- 对已有旧 session，若内存中已有 `Cards`，则首次启动时补写 transcript
- 若旧持久化里本来没有 `Cards`，则只迁移元数据，不伪造历史内容

这符合真实情况，也不会产生虚假记录。

## 11. 风险与取舍

本方案的主要取舍如下：

- V1 不支持“执行中切换会话”，换来更小的协议改动面
- V1 不恢复 pending 审批和 pending choice，换来更稳的重启恢复语义
- V1 先做自动标题，不做手动重命名，换来更快交付
- V1 采用“总索引 + 单会话 transcript 文件”，换来更好的持久化性能

这些取舍都符合当前仓库阶段。

## 12. 分阶段实施建议

### Phase 1：后端会话索引

- 引入 `BrowserSession`
- 新增 `GET /api/v1/sessions`
- 新增 `POST /api/v1/sessions`
- 新增 `POST /api/v1/sessions/:id/activate`
- 修正 `thread/reset` 的语义边界

### Phase 2：前端会话抽屉

- `store.js` 增加 sessions 状态与 action
- `App.vue` 增加“历史会话”按钮
- 完成抽屉、列表、切换、新建交互

### Phase 3：会话持久化

- 落地 transcript 文件
- 做 debounce flush
- 启动恢复历史时间线

### Phase 4：体验打磨

- 状态标签优化
- 空态与 loading 态
- 最近时间格式化
- 当前会话摘要展示

## 13. 验收标准

完成后应满足以下验收标准：

- 点击 `新建会话` 不会清掉旧聊天记录，而是新增一条历史会话
- 点击 `历史会话` 可以看到按最近活跃排序的会话列表
- 点击某条历史会话可以恢复其聊天时间线
- 页面刷新后，历史会话列表与当前会话仍能恢复
- `ai-server` 重启后，历史会话列表与时间线仍能恢复
- 当前执行中时，历史列表可查看但不可切换
- `清空当前上下文` 只清空当前会话，不影响其他历史会话

## 14. 最终建议

这次功能建议按“会话系统”来做，而不要按“左侧再加一个弹窗”来做。

如果只补一个前端列表，不重构会话索引与持久化，结果会是：

- 看起来有历史列表
- 实际刷新或重启后记录丢失
- 新建会话和清空上下文语义混乱
- 后续再做切换与恢复时还要返工

因此建议直接按本文方案推进，先把抽象层纠正，再做 UI。
