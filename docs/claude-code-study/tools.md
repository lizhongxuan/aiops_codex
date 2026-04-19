# Claude Code Tools 模块分析

> 源码路径: `claude code/tools/`, `claude code/Tool.ts`, `claude code/tools.ts`

## 概述

Tools 是 Claude Code 最核心的模块，定义了模型可以调用的所有工具。每个工具是一个独立目录，包含实现、prompt、UI 渲染三部分。工具系统支持权限控制、并发安全检查、只读模式、破坏性操作警告等。

## 目录结构

```
tools/
├── AgentTool/          # 子 agent 调度（多 agent 协作）
├── BashTool/           # Shell 命令执行
├── FileEditTool/       # 文件编辑（diff-based）
├── FileReadTool/       # 文件读取
├── FileWriteTool/      # 文件写入
├── GlobTool/           # 文件搜索（glob 模式）
├── GrepTool/           # 文本搜索（ripgrep）
├── LSPTool/            # LSP 集成（代码导航）
├── MCPTool/            # MCP 工具代理
├── WebSearchTool/      # 网页搜索
├── WebFetchTool/       # 网页内容获取
├── BriefTool/          # 附件/简报工具
├── ConfigTool/         # 配置管理
├── SkillTool/          # Skill 调用
├── EnterPlanModeTool/  # 进入计划模式
├── ExitPlanModeTool/   # 退出计划模式
├── TaskCreateTool/     # 创建后台任务
├── TaskGetTool/        # 获取任务状态
├── TaskListTool/       # 列出任务
├── TaskStopTool/       # 停止任务
├── TaskUpdateTool/     # 更新任务
├── TaskOutputTool/     # 获取任务输出
├── SendMessageTool/    # 发送消息（agent 间通信）
├── TeamCreateTool/     # 创建 team
├── TeamDeleteTool/     # 删除 team
├── TodoWriteTool/      # TODO 管理
├── ToolSearchTool/     # 工具搜索
├── NotebookEditTool/   # Notebook 编辑
├── PowerShellTool/     # PowerShell（Windows）
├── REPLTool/           # REPL 工具
├── ScheduleCronTool/   # 定时任务
├── SleepTool/          # 等待/延时
├── RemoteTriggerTool/  # 远程触发
├── shared/             # 共享工具函数
├── testing/            # 测试用工具
└── utils.ts            # 工具通用函数
```

## Tool 接口定义 (`Tool.ts`)

每个工具必须实现 `Tool<Input, Output>` 接口：

```typescript
interface Tool<Input, Output> {
  name: string
  async call(input: Input, context: ToolUseContext): Promise<ToolResult<Output>>
  description(context): string
  prompt(options): string              // 给模型的使用说明
  isReadOnly(input): boolean           // 是否只读操作
  isDestructive?(input): boolean       // 是否破坏性操作
  isEnabled(): boolean                 // 是否启用
  isConcurrencySafe(input): boolean    // 是否并发安全
  checkPermissions(input, context): PermissionResult  // 权限检查
  userFacingName(input): string        // UI 显示名
  renderToolUseMessage(input): ReactNode  // UI 渲染
  renderToolResultMessage?(output): ReactNode
  validateInput?(input): ValidationResult  // 输入校验
  getPath?(input): string              // 获取操作路径（用于权限匹配）
}
```

## 工具注册与组装流程

```
tools.ts
├── getAllBaseTools()        → 收集所有内置工具
├── filterToolsByDenyRules() → 应用 deny 规则过滤
├── getTools(permCtx)       → 根据权限上下文过滤
└── assembleToolPool(permCtx, mcpTools) → 合并内置 + MCP 工具
    └── getMergedTools()    → 最终合并，去重，返回给 agent loop
```

## 关键设计模式

### 1. 每个工具一个目录
```
BashTool/
├── BashTool.tsx          # 主实现（call 方法）
├── prompt.ts             # 给模型的 prompt
├── bashPermissions.ts    # 权限检查逻辑
├── bashSecurity.ts       # 安全检查
├── commandSemantics.ts   # 命令语义分析
├── modeValidation.ts     # 模式校验（plan mode 限制）
├── pathValidation.ts     # 路径校验
├── readOnlyValidation.ts # 只读模式校验
├── UI.tsx                # 终端 UI 渲染
└── utils.ts              # 工具内部函数
```

### 2. 权限分层
- `isReadOnly()` — 标记只读操作，plan mode 下只允许只读
- `isDestructive()` — 标记破坏性操作，需要额外确认
- `checkPermissions()` — 细粒度权限检查（路径、命令白名单等）
- `isConcurrencySafe()` — 并发安全标记，决定是否可以并行执行

### 3. AgentTool（子 agent）
AgentTool 是最复杂的工具，支持：
- `forkSubagent.ts` — fork 子 agent 进程
- `runAgent.ts` — 运行 agent 主循环
- `resumeAgent.ts` — 恢复暂停的 agent
- `agentMemory.ts` — agent 间内存共享
- `builtInAgents.ts` — 内置 agent 定义
- `built-in/` — 内置 agent 的 prompt 文件

### 4. MCP 工具代理
`MCPTool` 是一个通用代理，将 MCP server 暴露的工具包装成 Claude Code 的 Tool 接口。

## 与你项目的对应关系

| Claude Code | aiops-codex | 说明 |
|-------------|-------------|------|
| `Tool<I,O>` 接口 | `ToolEntry` + `ToolHandler` | 工具定义 |
| `tools.ts` assembleToolPool | `ToolRegistry.Register()` | 工具注册 |
| `BashTool` | agent loop 的 shell 执行 | 命令执行 |
| `FileEditTool/FileReadTool/FileWriteTool` | 文件操作工具 | 文件系统 |
| `AgentTool` | subagent 机制 | 多 agent |
| `MCPTool` | MCP host 集成 | MCP 代理 |
| `WebSearchTool` | `web_search` handler | 搜索 |
| `checkPermissions()` | approval handler | 权限 |

## 可借鉴的设计

1. **工具目录化**：每个工具独立目录，prompt/实现/UI 分离，便于维护
2. **prompt 与实现分离**：`prompt.ts` 单独文件，方便调优 prompt 而不改代码
3. **权限多层检查**：readOnly → destructive → permissions → concurrency
4. **工具语义分析**：BashTool 会分析命令语义（搜索/读取/写入/破坏性），自动决定权限级别
5. **Plan Mode 限制**：plan mode 下只允许只读工具，防止意外修改
