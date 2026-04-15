# Claude Code Skills 模块分析

> 源码路径: `claude code/skills/`

## 概述

Skills 是 Claude Code 的**可扩展命令系统**，允许通过 Markdown 文件定义自定义 prompt 命令。Skills 分为两类：bundled（内置编译到二进制）和 disk-based（从文件系统加载）。用户可以通过 `/skill-name` 调用 skill，模型也可以通过 `SkillTool` 自动调用。

## 目录结构

```
skills/
├── bundledSkills.ts       # 内置 skill 注册框架
├── loadSkillsDir.ts       # 从文件系统加载 skill（核心）
├── mcpSkillBuilders.ts    # MCP skill 构建器注册表
└── bundled/               # 内置 skill 实现
    ├── index.ts           # 注册入口
    ├── verify.ts          # /verify — 验证代码变更
    ├── debug.ts           # /debug — 调试辅助
    ├── batch.ts           # /batch — 批量操作
    ├── remember.ts        # /remember — 记忆管理
    ├── simplify.ts        # /simplify — 代码简化
    ├── skillify.ts        # /skillify — 将操作转为 skill
    ├── stuck.ts           # /stuck — 卡住时的帮助
    ├── updateConfig.ts    # /update-config — 配置更新
    ├── keybindings.ts     # /keybindings — 快捷键
    ├── loremIpsum.ts      # /lorem-ipsum — 测试文本
    ├── loop.ts            # /loop — 循环执行
    ├── claudeApi.ts       # /claude-api — API 调用
    └── scheduleRemoteAgents.ts  # 远程 agent 调度
```

## 核心概念

### Skill = Command

```typescript
interface Command {
  type: 'prompt'
  name: string                    // 命令名（如 "verify"）
  description: string             // 描述
  aliases?: string[]              // 别名
  whenToUse?: string              // 模型何时应该使用
  allowedTools?: string[]         // 允许使用的工具列表
  model?: string                  // 指定模型
  disableModelInvocation: boolean // 是否禁止模型自动调用
  userInvocable: boolean          // 用户是否可以手动调用
  source: 'bundled' | 'project' | 'user'  // 来源
  hooks?: HooksSettings           // 关联的 hooks
  skillRoot?: string              // skill 文件根目录
  context?: 'inline' | 'fork'    // 执行上下文（内联 or fork 子 agent）
  agent?: string                  // 关联的 agent
  getPromptForCommand(args, context): Promise<ContentBlockParam[]>  // 生成 prompt
}
```

### BundledSkillDefinition

内置 skill 的定义接口：

```typescript
interface BundledSkillDefinition {
  name: string
  description: string
  aliases?: string[]
  whenToUse?: string
  allowedTools?: string[]
  files?: Record<string, string>  // 附带的参考文件
  getPromptForCommand(args, context): Promise<ContentBlockParam[]>
}
```

## 加载流程

### 内置 Skill
```
initBundledSkills()
  → registerVerifySkill()
  → registerDebugSkill()
  → registerBatchSkill()
  → ... 每个 skill 调用 registerBundledSkill()
  → 写入 bundledSkills[] 数组
```

### 文件系统 Skill
```
loadSkillsFromSkillsDir(paths)
  → 扫描 .claude/skills/ 和 ~/.claude/skills/
  → 解析 Markdown frontmatter（name, description, allowedTools, hooks...）
  → 解析 skill 内容为 prompt
  → 创建 Command 对象
  → 注册到 skill 注册表
```

### MCP Skill
```
MCP server 连接
  → 发现 skill 资源
  → getMCPSkillBuilders() 获取构建器
  → createSkillCommand() 创建 Command
  → 注册到动态 skill 列表
```

## Skill Frontmatter 格式

```markdown
---
name: my-skill
description: 描述
allowed-tools: FileReadTool, BashTool
when-to-use: 当用户需要...
model: claude-sonnet-4-20250514
context: fork
hooks:
  pre-tool-use:
    - tool: BashTool
      command: echo "pre-check"
---

这里是 skill 的 prompt 内容...
```

## 关键设计

### 1. 文件提取机制
内置 skill 可以附带参考文件（`files` 字段），首次调用时提取到磁盘：
```typescript
registerBundledSkill({
  name: 'verify',
  files: SKILL_FILES,  // Record<string, string>
  async getPromptForCommand(args) {
    // prompt 前缀会自动加上 "Base directory: <dir>"
    return [{ type: 'text', text: SKILL_BODY }]
  },
})
```

### 2. 条件激活
Skill 可以根据文件路径条件激活：
```typescript
activateConditionalSkillsForPaths(paths)  // 当读取特定文件时激活相关 skill
```

### 3. 动态 Skill
运行时可以动态添加 skill 目录：
```typescript
addSkillDirectories(dirs)   // 添加新的 skill 扫描目录
getDynamicSkills()          // 获取动态加载的 skill
clearDynamicSkills()        // 清除动态 skill
```

### 4. MCP Skill 桥接
`mcpSkillBuilders.ts` 解决了循环依赖问题——MCP 模块需要 skill 构建函数，但 skill 模块又依赖 MCP。通过一个 write-once 注册表解耦：
```typescript
// loadSkillsDir.ts 启动时注册
registerMCPSkillBuilders({ createSkillCommand, parseSkillFrontmatterFields })

// mcpSkills.ts 使用时获取
const builders = getMCPSkillBuilders()
```

## 与你项目的对应关系

| Claude Code | aiops-codex | 说明 |
|-------------|-------------|------|
| `bundledSkills.ts` | `internal/skills/` | 内置 skill 注册 |
| `loadSkillsDir.ts` | skill 文件扫描 | 从磁盘加载 |
| `Command` 类型 | skill 定义 | skill 数据结构 |
| `mcpSkillBuilders.ts` | MCP skill 集成 | MCP 桥接 |
| `allowedTools` | 工具白名单 | 权限控制 |
| `context: 'fork'` | subagent | 独立执行上下文 |

## 可借鉴的设计

1. **Markdown frontmatter 定义 skill**：用户友好，非开发者也能写 skill
2. **三级来源**：bundled（内置）→ user（用户级）→ project（项目级），优先级清晰
3. **文件提取 + 懒加载**：内置 skill 的参考文件首次使用时才提取，节省启动时间
4. **条件激活**：skill 可以根据上下文自动激活，不需要用户手动选择
5. **MCP 桥接模式**：write-once 注册表解决循环依赖，值得学习
