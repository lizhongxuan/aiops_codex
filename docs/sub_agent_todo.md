# 协作工作台 AI Chat 子 Agent 架构改造 TODO

## 目标

将协作工作台的 AI Chat 改造为严格的主/子 Agent 分离架构：
- 主 Agent 只做规划、分发、汇总，绝不直接操作任何主机
- 所有主机操作必须由子 Agent 执行
- 用户可批量选择多台主机
- UI 实时显示每个子 Agent 的工作状态

---

## 一、去除"当前主机"隐式 fallback，改为主动询问

### 现状
- `workspaceDirectTargetHost()` (`orchestrator_integration.go:398`) 在 HostID 和 SelectedHostID 都为空时，fallback 到 `model.ServerLocalHostID`
- `handleWorkspaceChatMessage()` 直接使用该 fallback 结果，不会询问用户

### 改造项

- [ ] 修改 `workspaceDirectTargetHost()`：当 req.HostID 和 session.SelectedHostID 都为空时，返回空字符串而非 `ServerLocalHostID`
- [ ] 在 `handleWorkspaceChatMessage()` 中，当 hostID 为空时，创建一个 ChoiceRequest 询问用户要操作哪些主机（列出所有在线主机供选择）
- [ ] 新增 `buildHostSelectionQuestions()` 函数，从 `a.store.Hosts()` 获取在线主机列表，生成 ChoiceQuestion
- [ ] ChoiceQuestion 支持多选模式（需要扩展 `model.ChoiceQuestion` 增加 `MultiSelect bool` 字段）
- [ ] 用户选择后，将选中的主机 ID 列表存入 session（新增 `session.TargetHostIDs []string` 字段）

涉及文件：
- `internal/server/orchestrator_integration.go` — workspaceDirectTargetHost, handleWorkspaceChatMessage
- `internal/model/session.go` 或 `internal/store/` — session 结构扩展
- `internal/model/choice.go` — ChoiceQuestion 多选支持

---

## 二、支持多主机批量操作

### 现状
- `handleWorkspaceChatMessage()` 只解析出单个 `hostID`
- `reActLoopRequest` 只有 `HostID string` 单值字段
- 批量操作依赖 AI 在 planning 阶段自行拆分，用户无法直接选择多台

### 改造项

- [ ] `chatRequest` 结构增加 `HostIDs []string` 字段，前端可传入多台主机
- [ ] `reActLoopRequest` 增加 `HostIDs []string` 字段
- [ ] `handleWorkspaceChatMessage()` 解析多主机列表，为每台主机创建子任务
- [ ] 前端 `ChatComposerDock` 增加多主机选择器组件（checkbox 列表或 tag 选择器）
- [ ] 前端发送消息时，将选中的主机 ID 列表放入请求体的 `hostIds` 字段

涉及文件：
- `internal/server/orchestrator_integration.go` — handleWorkspaceChatMessage
- `internal/server/react_loop.go` — reActLoopRequest 结构
- `web/src/pages/ChatPage.vue` — ChatComposerDock 多主机选择 UI
- API 请求/响应结构

---

## 三、主 Agent 禁止直接操作主机，强制走子 Agent 分发

### 现状
- 主 Agent 的 ReAct 循环在 `handleCodexServerRequest()` 中处理 tool 调用时，会直接调用 `runRemoteExec()` 对远程主机执行命令
- `runRemoteExec()` (`remote_exec.go:304`) 通过 gRPC `sendAgentEnvelope()` 直接向 host-agent 发送 `exec/start`
- readonly 和 direct 模式下，主 Agent 直接操作目标主机，不经过 orchestrator 分发

### 改造项

- [ ] 在 workspace 模式下，`handleCodexServerRequest()` 中拦截所有 `execute_command` / `execute_*` 类 tool 调用，禁止主 Agent 直接执行
- [ ] 主 Agent 的 workspace thread 指令中移除远程执行类工具，只保留规划和分发类工具
- [ ] 将当前 readonly 场景也改为：主 Agent 创建 readonly 子任务 → orchestrator 分发给子 Agent → 子 Agent 执行 → 结果回传
- [ ] 将当前 direct 场景也改为：主 Agent 创建 direct 子任务 → orchestrator 分发 → 子 Agent 执行
- [ ] `buildWorkspaceReActThreadStartSpec()` 中的 DynamicTools 列表，去除 `execute_command`、`execute_readonly_command` 等远程执行工具
- [ ] 新增 `dispatch_task` 动态工具，供主 Agent 调用来分发任务给子 Agent
- [ ] 修改 `reActAttachmentInjectStage` 中 workspace 分支的指令，明确告知模型"你不能直接执行命令，必须通过 dispatch_task 分发"

涉及文件：
- `internal/server/server.go` — handleCodexServerRequest 拦截逻辑
- `internal/server/react_loop.go` — reActAttachmentInjectStage workspace 分支
- `internal/server/react_loop_instructions.go`（如有）— workspace 模式指令模板
- `internal/server/orchestrator_integration.go` — 新增 readonly/direct 子任务分发路径
- `internal/server/remote_exec.go` — 增加 workspace 模式下的调用拦截断言

---

## 四、子 Agent 状态实时显示在 backupgroup hosts 栏

### 现状
- `buildProtocolBackgroundAgents()` (`protocolWorkspaceVm.js:514`) 从 hostRows 提取子 Agent 信息
- 状态有 running / waiting_approval / queued / idle / pending / dispatched 六种
- 主 Agent 直接操作主机时不会出现在 hostRows 中
- 没有明确的"工作中"/"休息中"二元状态

### 改造项

- [ ] 统一状态映射：将所有状态归类为两大类
  - 工作中（active）：running, waiting_approval, dispatched, pending
  - 休息中（idle）：idle, queued, completed, failed
- [ ] `buildProtocolBackgroundAgents()` 增加 `activeLabel` 字段，值为 "工作中" 或 "休息中"
- [ ] 前端 backupgroup hosts 栏 UI 改造：
  - 每个子 Agent 行显示：主机名 + 当前任务摘要 + 状态标签（工作中/休息中）
  - 工作中用绿色指示灯，休息中用灰色指示灯
- [ ] 确保所有主机操作（包括改造后的 readonly/direct）都经过 orchestrator，使 hostRows 数据完整
- [ ] 子 Agent 开始执行时，orchestrator 更新 worker 状态为 running → UI 显示"工作中"
- [ ] 子 Agent 执行完毕时，orchestrator 更新 worker 状态为 idle → UI 显示"休息中"

涉及文件：
- `web/src/lib/protocolWorkspaceVm.js` — buildProtocolBackgroundAgents 状态映射
- `web/src/pages/ChatPage.vue` — backupgroup hosts 栏 UI 渲染
- `internal/server/orchestrator_integration.go` — syncWorkspaceMissionRuntime 状态同步

---

## 改造顺序建议

1. 先做第三项（主 Agent 禁止直接操作），这是架构核心变更
2. 再做第一项（去除 fallback，主动询问主机），依赖第三项的子 Agent 分发通道
3. 然后做第四项（UI 状态显示），因为第三项完成后所有操作都走 orchestrator，hostRows 数据自然完整
4. 最后做第二项（多主机批量），在前三项基础上扩展
