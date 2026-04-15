# Claude Code 核心模块学习笔记

> 基于 `claude code/` 源码分析，提取 coordinator、hooks、skills、tools 四个模块的架构设计。

## 模块索引

| 文档 | 模块 | 核心职责 |
|------|------|----------|
| [tools.md](tools.md) | Tools | 工具定义、注册、权限、执行 |
| [coordinator.md](coordinator.md) | Coordinator | 会话模式管理、任务分解与委派 |
| [hooks.md](hooks.md) | Hooks | React hooks、权限队列、IDE 集成、通知 |
| [skills.md](skills.md) | Skills | 可扩展命令系统、Markdown skill、MCP 桥接 |

## 模块间关系

```
用户输入
  │
  ▼
Coordinator ──── 判断模式（普通 / coordinator）
  │                注入对应 system prompt
  ▼
Hooks ─────────── useMergedTools() 合并工具池
  │                useCanUseTool() 权限检查
  │                useCommandQueue() 命令队列
  ▼
Tools ─────────── assembleToolPool() 组装工具
  │                Tool.call() 执行工具
  │                AgentTool → 子 agent 调度
  ▼
Skills ────────── SkillTool 调用 skill
                   loadSkillsDir() 加载自定义 skill
                   bundledSkills 内置 skill
```

## 关键架构决策

### 1. Prompt 驱动而非代码分支
Claude Code 的模式切换（普通/coordinator/plan）主要通过 system prompt 差异实现，agent loop 代码基本不变。这比硬编码 if-else 分支更灵活。

### 2. 工具即目录
每个工具是一个独立目录（实现 + prompt + UI），而不是一个大文件。这让工具的 prompt 调优和 UI 修改互不干扰。

### 3. 权限异步化
工具权限检查通过 PermissionQueue + resolve 回调实现异步确认，不阻塞 agent loop。用户可以在 UI 上逐个审批或批量通过。

### 4. Skill 三级加载
bundled（编译期）→ user（~/.claude/skills/）→ project（.claude/skills/），优先级递增，项目级 skill 可以覆盖全局 skill。

### 5. MCP 工具一等公民
MCP 工具通过 MCPTool 代理后与内置工具完全平等，经过相同的权限检查和 deny 规则过滤。
