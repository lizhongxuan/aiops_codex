# Claude-Style Unified Tool Contract With Vue-Compatible Display Descriptor

## 背景

用户希望参考 `claude code/` 的源码，把 `aiops-codex` 的所有工具统一到一套接口，并迁移这些核心工具的契约与 prompt：

- `AgentTool`
- `BashTool`
- `FileReadTool`
- `FileEditTool`
- `FileWriteTool`
- `GlobTool`
- `GrepTool`
- `WebFetchTool`
- `WebSearchTool`
- `NotebookEditTool`
- `SkillTool`

Claude Code 的核心契约见 [claude code/Tool.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/Tool.ts)，它把三类能力揉在一个 `Tool<Input, Output, Progress>` 里：

1. 执行契约
   - `call`
   - `inputSchema`
   - `checkPermissions`
   - `isConcurrencySafe`
   - `isReadOnly`
   - `isDestructive`
2. 提示词契约
   - `description()`
3. 展示契约
   - `renderToolUseMessage`
   - `renderToolResultMessage`
   - `renderToolUseProgressMessage`

`aiops-codex` 当前已经有部分相近能力，但分散在多层：

- 工具执行与注册：
  - [internal/server/tool_handler.go](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/tool_handler.go)
  - [internal/server/tool_handler_registry.go](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/tool_handler_registry.go)
  - [internal/server/tool_dispatcher.go](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/tool_dispatcher.go)
- tool prompt：
  - [internal/server/tool_prompts.go](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/tool_prompts.go)
- 生命周期投影：
  - [internal/server/tool_projection_cards.go](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/tool_projection_cards.go)
  - [internal/server/tool_projection_snapshot.go](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/tool_projection_snapshot.go)
- 前端展示：
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
  - [web/src/pages/ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)

关键结论：

- **后端执行契约和 prompt 契约应该向 Claude 靠拢**
- **前端不能直接照搬 Claude 的 `ReactNode` 渲染契约**
- **必须保留 `snapshot/cards/approvals/evidence/orchestrator` 作为产品读模型**

因此，本方案不是“把 Claude 的 Tool 类型逐字段翻译成 Go”，而是：

- 建立一套统一的工具契约
- 让 tool prompt 归 tool 自己拥有
- 为展示层增加一个 **Vue-compatible display descriptor**
- 仍由服务端生命周期投影把工具展示落成 durable snapshot/card

## 为什么不能直接照搬 Claude 的前端渲染契约

Claude Code 的工具展示是 React-first：

- `renderToolUseMessage()` 直接返回 React 组件
- `renderToolResultMessage()` 直接返回 React 组件
- `renderToolUseProgressMessage()` 直接返回 React 组件

例如：

- [claude code/tools/WebSearchTool/UI.tsx](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/tools/WebSearchTool/UI.tsx)
- [claude code/tools/FileReadTool/UI.tsx](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/tools/FileReadTool/UI.tsx)
- [claude code/tools/BashTool/UI.tsx](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/tools/BashTool/UI.tsx)

而 `aiops-codex` 的展示架构是 server-owned projection：

- 后端发生命周期事件
- projection 落成 `ProcessLineCard` / `ResultCard` / `ApprovalCard`
- 前端从 snapshot/cards 重建 chat view

直接照搬 Claude 的问题：

1. `ReactNode` 无法直接跨 Go/Vue 边界复用
2. 会绕开当前 durable snapshot/card 模型
3. 会削弱刷新、回放、审批、evidence、workspace mission 的一致性
4. 会把“工具内部展示”和“产品级读模型”混成一层

所以前端不能直接变成“每个工具自己渲染 ReactNode”，而应该变成：

- 工具提供 **结构化 display descriptor**
- 服务端把 descriptor 投影到 durable snapshot/card
- Vue 前端只负责渲染 descriptor schema，而不是承接工具内部 React 组件

## 目标

### 目标

- 所有工具遵循统一的执行/权限/安全/提示词接口
- 让 tool prompt 完整回收到 tool 自己维护
- 引入统一 display descriptor，减少前端手写分支
- 保留当前 `snapshot/cards/approvals/evidence/orchestrator` 架构
- 支持逐步迁移 Claude Code 核心工具

### 非目标

- 不直接把 `claude code/` 的 React UI 搬进 `web/`
- 不推翻现有 lifecycle + projection + snapshot 架构
- 不在第一阶段一次性重写所有前端聊天视图
- 不要求所有工具都立刻拥有丰富自定义结果渲染

## 设计原则

### 1. Tool-owned, Projection-owned, Frontend-rendered

职责分离：

- Tool-owned：能力、权限、输入 schema、prompt、显示意图
- Projection-owned：生命周期落库、审批卡、过程卡、结果卡、evidence、orchestrator
- Frontend-rendered：把结构化显示模型渲染成 Vue 组件

### 2. 展示描述是结构化 schema，不是组件

后端输出的不是 React/Vue 组件，而是结构化 block：

- `text`
- `kv_list`
- `command`
- `file_preview`
- `file_diff_summary`
- `search_queries`
- `link_list`
- `result_stats`
- `warning`

### 3. snapshot/cards 仍是唯一 durable read model

工具内部 UI 不是持久化事实。

持久化事实仍然应是：

- tool lifecycle event
- projected card
- approval state
- evidence binding
- orchestrator runtime

### 4. 先统一接口，再迁提示词，再提升展示

迁移顺序必须是：

1. 执行接口统一
2. prompt ownership 统一
3. 权限/安全元数据统一
4. display descriptor 接入 projection
5. 前端按 descriptor 渲染

而不是先重写 UI。

## 目标接口

Go 里不应强行复制 TypeScript 泛型签名，而应定义一套运行时可注册的统一契约。

### 核心接口

建议新增：

- `internal/server/unified_tool.go`
- `internal/server/tool_display.go`

接口建议如下：

```go
type UnifiedTool interface {
	Name() string
	Aliases() []string

	Description(ctx ToolDescriptionContext) string
	InputSchema() map[string]any

	Call(ctx context.Context, req ToolCallRequest) (ToolCallResult, error)
	CheckPermissions(ctx context.Context, req ToolCallRequest) (PermissionResult, error)

	IsConcurrencySafe(req ToolCallRequest) bool
	IsReadOnly(req ToolCallRequest) bool
	IsDestructive(req ToolCallRequest) bool

	Display() ToolDisplayAdapter
}
```

### 执行结果

```go
type ToolCallResult struct {
	Output            any
	DisplayOutput     *ToolDisplayPayload
	StructuredContent map[string]any
	Metadata          map[string]any
}
```

### 权限结果

```go
type PermissionResult struct {
	Allowed           bool
	RequiresApproval  bool
	Reason            string
	ApprovalType      string
	ApprovalDecisions []string
	Metadata          map[string]any
}
```

### 展示适配器

```go
type ToolDisplayAdapter interface {
	RenderUse(req ToolCallRequest) *ToolDisplayPayload
	RenderProgress(progress ToolProgressEvent) *ToolDisplayPayload
	RenderResult(result ToolCallResult) *ToolDisplayPayload
}
```

### 显示载荷

```go
type ToolDisplayPayload struct {
	Summary    string
	Activity   string
	Blocks     []ToolDisplayBlock
	FinalCard  *ToolFinalCardDescriptor
	SkipCards  bool
	Metadata   map[string]any
}
```

### 显示 block

```go
type ToolDisplayBlock struct {
	Kind     string
	Title    string
	Text     string
	Items    []map[string]any
	Metadata map[string]any
}
```

## 为什么 display descriptor 比直接前端重构更合适

### 优点

1. 能保留当前 product read model
2. 工具可表达 richer UI，而不是只靠 `ProcessLineCard.Text`
3. Vue 前端可以逐步支持新的 block kind
4. descriptor 可以被 snapshot 持久化、回放、刷新恢复
5. 审批/证据/orchestrator 仍然是统一 projection，不卡在某个工具组件内部

### 缺点

1. 没有 Claude 那种“工具直接拥有 React 组件”的灵活度
2. 需要设计一套跨后端和前端都稳定的 display schema
3. 某些高度个性化 UI 需要新增 block kind，而不是随手写 JSX

### 结论

这是最符合 `aiops-codex` 当前架构的折中：

- **保留 Claude 的工具内聚思想**
- **不照搬 Claude 的 React UI 所有权模型**

## 与现有代码的映射关系

### 当前后端执行层

继续复用并重构这些对象：

- [internal/server/tool_handler.go](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/tool_handler.go)
- [internal/server/tool_handler_registry.go](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/tool_handler_registry.go)
- [internal/server/tool_dispatcher.go](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/tool_dispatcher.go)

重构目标：

- `ToolHandlerDescriptor` 扩展为统一 `UnifiedToolDescriptor`
- registry 注册对象从 handler 扩展为 full tool definition
- dispatcher 统一调用 `CheckPermissions`、安全属性和 display adapter

### 当前 prompt 层

继续复用：

- [internal/server/tool_prompts.go](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/tool_prompts.go)

重构目标：

- `Description()` 作为 tool 真源
- `tool_prompts.go` 退化为 descriptor registry 或生成器
- 从 `dynamic_tools.go` 中删除散落的 schema description prose

### 当前 projection 层

继续复用：

- [internal/server/tool_projection_cards.go](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/tool_projection_cards.go)
- [internal/server/tool_projection_snapshot.go](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/tool_projection_snapshot.go)
- [internal/server/tool_projection_runtime.go](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/tool_projection_runtime.go)

重构目标：

- lifecycle event payload 中增加 `display`
- `projectToolLifecycleFinalCard()` 支持从 `display.finalCard` 和 `display.blocks` 落卡
- 对 `skipCardProjection` 这类临时布尔标记，逐步迁移到 display contract

### 当前前端层

继续复用：

- [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
- [web/src/pages/ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)

新增建议：

- `web/src/lib/toolDisplayModel.js`
- `web/src/components/chat/tool-display/ToolDisplayRenderer.vue`
- `web/src/components/chat/tool-display/blocks/*.vue`

重构目标：

- `chatTurnFormatter` 不再依赖大量 tool-specific 正则和文本推断
- 优先渲染来自 snapshot/card 的 `detail.display`
- 保留旧 card 字段兼容窗口

## 工具迁移范围

### 第一批：只读低风险工具

优先迁移：

- `WebSearchTool`
- `WebFetchTool`
- `FileReadTool`
- `GlobTool`
- `GrepTool`
- `SkillTool`

原因：

- 权限和破坏性简单
- 结果适合落成结构化 display block
- 最容易验证与 Claude 的 prompt/summary 行为一致

### 第二批：执行类与文件改写类工具

- `BashTool`
- `FileWriteTool`
- `FileEditTool`
- `NotebookEditTool`

重点：

- 权限校验和审批桥接
- 结果 display 必须与 `CommandCard` / `FileChangeCard` 对齐
- 不能破坏 evidence / verification 路径

### 第三批：高阶编排工具

- `AgentTool`

重点：

- 与现有 worker/orchestrator/task dispatch 语义对齐
- 不把子 agent 展示简化成普通 process block

## Prompt 迁移原则

tool prompt 必须完整迁移，但归 tool 自己拥有。

### Claude 里值得借的点

1. tool 描述归 tool 自己维护
2. system prompt 不重复写 tool 使用说明
3. display summary / activity / result summary 与 prompt 同源

### aiops-codex 里应做的调整

1. `Description()` 成为 tool schema description 真源
2. `ToolDisplayAdapter.RenderUse()` 与 `Description()` 使用同一组输入语义
3. 不再在 session prompt 里重复解释具体 tool 行为

## 需要删除或收缩的旧代码

### 可逐步删除

1. `dynamic_tools.go` 里散落的 tool description prose
2. 只靠 `Message`/`Label` 拼接展示的旧路径
3. `skipCardProjection` 这类为历史兼容引入的局部布尔分支
4. 前端 `chatTurnFormatter.js` 里与单个工具强绑定的文本启发式逻辑

### 需要保留到迁移完成后再删

1. lifecycle event / projection 主干
2. approval coordinator
3. evidence/orchestrator subscriber
4. card/snapshot store

## 风险

### 1. 工具内聚增强后，前后端耦合可能反而变高

缓解：

- descriptor schema 必须稳定
- 工具不能直接输出前端组件

### 2. 旧卡片与新 descriptor 双轨期会增加复杂度

缓解：

- 只允许一段兼容窗口
- 每迁完一批工具就删旧映射

### 3. Bash / FileEdit 这类工具的 rich output 很容易和现有卡片冲突

缓解：

- 第二批迁移时必须先锁回归测试
- 先做 display shadow path，再切主路径

## 推荐方案

推荐采用：

**方案 B：统一工具契约 + tool-owned prompt + Vue-compatible display descriptor + 保留 snapshot/cards**

不推荐：

- 方案 A：只统一后端执行，不动展示
  - 解决不了“工具接口统一后，前端仍到处猜工具含义”的问题
- 方案 C：直接把 Claude 的 React 渲染契约搬进来
  - 会破坏当前 durable projection 架构

## 迁移阶段

### Phase 0: 基线与审计

- 审计当前工具注册点
- 审计当前 tool-specific 前端逻辑
- 建立 Claude 工具映射表

### Phase 1: 统一契约

- 新增 `UnifiedTool`
- registry/dispatcher 接入
- prompt ownership 接入

### Phase 2: 展示描述层

- 引入 `ToolDisplayPayload`
- projection 落 `detail.display`
- Vue renderer 支持 block schema

### Phase 3: 迁移第一批工具

- `WebSearch`
- `WebFetch`
- `FileRead`
- `Glob`
- `Grep`
- `Skill`

### Phase 4: 迁移第二批工具

- `Bash`
- `FileWrite`
- `FileEdit`
- `NotebookEdit`

### Phase 5: 迁移高阶工具

- `AgentTool`

### Phase 6: 删除旧路径

- 删除旧 tool prose
- 删除前端启发式分支
- 删除旧 display 兼容字段

## 验收标准

### 后端

- 所有目标工具都通过统一接口注册
- prompt description 只存在一个真源
- permission/safety 都经统一接口输出

### 前端

- 新工具展示可由 `detail.display` 渲染
- 刷新后工具过程和结果仍可恢复
- 不破坏现有审批、evidence、workspace 视图

### 产品一致性

- `snapshot/cards` 仍然是唯一 durable read model
- tool-specific richer UI 不绕过 projection

## 最终结论

如果要参考 Claude Code 的源码，正确做法不是“把 ReactNode 搬进 Go/Vue”，而是：

- 借它的 **统一工具契约**
- 借它的 **tool-owned prompt**
- 借它的 **tool-owned display intent**
- 但把 display intent 落成 `aiops-codex` 自己的 **Vue-compatible descriptor + durable snapshot/card projection**

这条路既能统一所有工具接口，也不会打碎 `aiops-codex` 已经建好的审批、证据、工作台和回放体系。
