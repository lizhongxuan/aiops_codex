# Claude Code Hooks 模块分析

> 源码路径: `claude code/hooks/`

## 概述

Hooks 是 Claude Code 的 **React hooks 集合**，驱动整个终端 UI 的状态管理和副作用处理。这里的 "hooks" 不是 agent hooks（那个在 `utils/hooks/` 里），而是 React 组件层面的 hooks，负责工具权限、会话管理、IDE 集成、通知系统等。

## 目录结构

```
hooks/
├── toolPermission/           # 工具权限系统（最核心）
│   ├── PermissionContext.ts  # 权限上下文和队列管理
│   ├── permissionLogging.ts  # 权限日志
│   └── handlers/             # 各工具的权限处理器
├── notifs/                   # 通知系统
│   ├── useAutoModeUnavailableNotification.ts
│   ├── useRateLimitWarningNotification.tsx
│   ├── useMcpConnectivityStatus.tsx
│   ├── usePluginInstallationStatus.tsx
│   └── ...
├── useMergedTools.ts         # 工具合并（内置 + MCP）
├── useCanUseTool.tsx         # 工具权限检查 hook
├── useCommandQueue.ts        # 命令队列
├── useSettings.ts            # 设置管理
├── useIDEIntegration.tsx     # IDE 集成
├── useVoice.ts               # 语音输入
├── useTextInput.ts           # 文本输入处理
├── useVimInput.ts            # Vim 模式输入
├── useHistorySearch.ts       # 历史搜索
├── useDiffData.ts            # Diff 数据
├── useSwarmInitialization.ts # Swarm（多 agent）初始化
└── ...（80+ hooks）
```

## 核心 Hooks 分类

### 1. 工具权限系统 (`toolPermission/`)

这是最关键的子系统，控制模型是否可以执行某个工具调用。

```typescript
// PermissionContext.ts 核心接口
interface PermissionQueueOps {
  push(item: ToolUseConfirm): void    // 添加待确认项
  remove(toolUseID: string): void     // 移除已处理项
  update(toolUseID: string, patch): void  // 更新确认状态
}

interface ResolveOnce<T> {
  resolve(value: T): void    // 解决权限请求
  isResolved(): boolean      // 是否已解决
  claim(): boolean           // 声明处理权（防止重复处理）
}
```

权限流程：
1. 模型请求调用工具 → `checkPermissions()` 检查
2. 如果需要用户确认 → 加入 `PermissionQueue`
3. UI 渲染确认对话框 → 用户选择允许/拒绝
4. `resolve()` 回调 → 工具执行或跳过

### 2. 工具合并 (`useMergedTools.ts`)

```typescript
function useMergedTools(
  initialTools: Tools,      // 内置工具
  mcpTools: Tools,          // MCP 动态工具
  permissionContext: ToolPermissionContext
): Tools
```

调用 `assembleToolPool()` 合并内置工具和 MCP 工具，应用 deny 规则和去重。

### 3. 会话管理
- `useAssistantHistory.ts` — 对话历史
- `useSessionBackgrounding.ts` — 会话后台化
- `useRemoteSession.ts` — 远程会话
- `useCancelRequest.ts` — 取消请求

### 4. IDE 集成
- `useIDEIntegration.tsx` — VS Code/JetBrains 集成
- `useIdeConnectionStatus.ts` — IDE 连接状态
- `useIdeSelection.ts` — IDE 选区同步
- `useDiffInIDE.ts` — 在 IDE 中显示 diff
- `useLspPluginRecommendation.tsx` — LSP 插件推荐

### 5. 输入处理
- `useTextInput.ts` — 文本输入
- `useVimInput.ts` — Vim 模式
- `usePasteHandler.ts` — 粘贴处理
- `useArrowKeyHistory.tsx` — 方向键历史
- `useTypeahead.tsx` — 自动补全

### 6. 通知系统 (`notifs/`)
每个通知是一个独立 hook，检测特定条件并显示通知：
- 速率限制警告
- MCP 连接状态
- 插件安装状态
- 模型迁移通知
- 弃用警告

### 7. 多 Agent（Swarm）
- `useSwarmInitialization.ts` — Swarm 初始化
- `useSwarmPermissionPoller.ts` — Swarm 权限轮询
- `useTeammateViewAutoExit.ts` — 队友视图自动退出

## 设计模式

### 权限队列模式
```
Tool Call → checkPermissions() → 需要确认?
  ├── 不需要 → 直接执行
  └── 需要 → push 到 PermissionQueue
              → UI 渲染确认框
              → 用户操作
              → resolve() → 执行/跳过
```

### 外部存储订阅模式
```typescript
// useCommandQueue.ts — 典型的 useSyncExternalStore 模式
function useCommandQueue(): readonly QueuedCommand[] {
  return useSyncExternalStore(subscribeToCommandQueue, getCommandQueueSnapshot)
}
```

## 与你项目的对应关系

| Claude Code | aiops-codex | 说明 |
|-------------|-------------|------|
| `toolPermission/` | `ApprovalHandler` 接口 | 工具执行审批 |
| `PermissionQueue` | approval 队列 | 待审批队列 |
| `useMergedTools` | `ToolRegistry` | 工具合并 |
| `useIDEIntegration` | 无（Web UI） | IDE 集成 |
| `notifs/` | 无 | 通知系统 |
| `useSwarm*` | subagent 机制 | 多 agent |

## 可借鉴的设计

1. **权限队列 + resolve 回调**：异步权限确认模式，不阻塞 agent loop
2. **通知 hook 化**：每个通知条件独立 hook，组合灵活
3. **外部存储订阅**：`useSyncExternalStore` 模式，状态变更精确触发重渲染
4. **工具合并管道**：内置 → deny 过滤 → MCP 合并 → 去重，管道清晰
