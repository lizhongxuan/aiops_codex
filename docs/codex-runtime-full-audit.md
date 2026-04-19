# Codex Runtime 完整能力审计报告

审计范围：`codex/codex-rs/core/src/` 全部模块 vs `internal/` 实现
审计时间：2026-04-12

## 审计结论

| 统计 | 数量 |
|------|------|
| 总审计能力项 | ~75 |
| ✅ 完全实现 | 5 |
| ⚠️ 部分实现 | 12 |
| ❌ 完全缺失 | 55+ |

---

## 一、工具处理器（Tool Handlers）

| # | 能力 | Codex 实现 | ai-server | 状态 |
|---|------|-----------|-----------|------|
| 1.1 | shell / local_shell | 本地沙箱化 shell 执行 | execute_readonly_query + execute_command（已支持 server-local） | ⚠️ 有但无沙箱 |
| 1.2 | apply_patch | 统一 diff 文件补丁 + 安全检查 | write_file（仅远程全量写入） | ❌ |
| 1.3 | list_dir | 本地目录列表 | list_files（仅远程） | ❌ 本地缺失 |
| 1.4 | view_image | 图片加载/缩放/注入 prompt | 无 | ❌ |
| 1.5 | js_repl | 内嵌 V8 JS 运行时 | 无 | ❌ |
| 1.6 | unified_exec | TTY/stdin/yield 控制 | 无 | ❌ |
| 1.7 | tool_search | BM25 工具搜索 | 无 | ❌ |
| 1.8 | tool_suggest | 上下文工具推荐 | 无 | ❌ |
| 1.9 | request_user_input | 结构化多问题输入 | ask_user_question（简化版） | ⚠️ |
| 1.10 | request_permissions | 运行时权限请求 | 无 | ❌ |
| 1.11 | plan tool | 结构化计划 checklist | enter/update/exit_plan_mode | ⚠️ |
| 1.12 | mcp handler | MCP 工具调用 + 命名空间 | mcphost bridge（简化版） | ⚠️ |
| 1.13 | mcp_resource | MCP 资源读取 | 无 | ❌ |
| 1.14 | dynamic tools | 运行时动态工具注册 | 无 | ❌ |
| 1.15 | agent_jobs | CSV 批量任务 | 无 | ❌ |
| 1.16 | code_mode | 代码模式执行 | 无 | ❌ |
| 1.17 | shell_command | 可配置 shell 后端 | 无 | ❌ |

## 二、多 Agent 系统

| # | 能力 | ai-server | 状态 |
|---|------|-----------|------|
| 2.1 | V1 spawn/wait/send/close/resume | spawn/wait/send/close/list（缺 resume） | ⚠️ |
| 2.2 | V2 spawn/wait/send_message/close/list/followup_task | 无 | ❌ |
| 2.3 | Agent Registry + 角色定义 | 无 | ❌ |
| 2.4 | Agent Mailbox 通信 | 简单 string 注入 | ❌ |
| 2.5 | Fork 模式（继承历史） | 无 | ❌ |
| 2.6 | 昵称系统 | 无 | ❌ |

## 三、上下文管理

| # | 能力 | ai-server | 状态 |
|---|------|-----------|------|
| 3.1 | Token 感知历史 + 截断策略 | 粗略/精确估算（无逐项追踪） | ⚠️ |
| 3.2 | 历史规范化 | Sanitize()（缺 inter-agent 处理） | ⚠️ |
| 3.3 | 引用上下文项 | 无 | ❌ |
| 3.4 | Token 用量明细 | 无 | ❌ |
| 3.5 | 图片处理 | 无 | ❌ |

## 四、压缩系统

| # | 能力 | ai-server | 状态 |
|---|------|-----------|------|
| 4.1 | 远程压缩任务 | 无 | ❌ |
| 4.2 | 内联自动压缩 | ShouldCompress + Compact | ✅ |
| 4.3 | 压缩历史构建器 | 无初始上下文重注入 | ⚠️ |
| 4.4 | 文件读取去重 | deduplicateFileReads | ✅ |
| 4.5 | 压缩模板 | 硬编码字符串 | ❌ |

## 五、安全与沙箱

| # | 能力 | ai-server | 状态 |
|---|------|-----------|------|
| 5.1 | Patch 安全评估 | 无 | ❌ |
| 5.2 | OS 级沙箱（Landlock/Seatbelt） | 无 | ❌ |
| 5.3 | 沙箱升级 | 无 | ❌ |
| 5.4 | 审批缓存 | 无 | ❌ |
| 5.5 | 网络审批流 | 无 | ❌ |

## 六、Guardian 安全守卫

| # | 能力 | ai-server | 状态 |
|---|------|-----------|------|
| 6.1 | LLM 驱动的审批审查 | 无 | ❌ |
| 6.2 | Guardian 转录构建 | 无 | ❌ |
| 6.3 | 结构化风险评估 | 无 | ❌ |
| 6.4 | Guardian 会话管理 | 无 | ❌ |

## 七、执行环境

| # | 能力 | ai-server | 状态 |
|---|------|-----------|------|
| 7.1 | 环境变量策略 | 无过滤 | ❌ |
| 7.2 | Shell 检测 | 检测但不配置工具 | ⚠️ |
| 7.3 | 命令规范化 | 无 | ❌ |
| 7.4 | ExecPolicy（Starlark） | 基础规则引擎（无 Starlark） | ⚠️ |

## 八、文件与补丁

| # | 能力 | ai-server | 状态 |
|---|------|-----------|------|
| 8.1 | Apply patch + 安全检查 | 无 | ❌ |
| 8.2 | Turn diff tracker | 无 | ❌ |
| 8.3 | File watcher | 无 | ❌ |

## 九、Hook 系统

| # | 能力 | ai-server | 状态 |
|---|------|-----------|------|
| 9.1 | Session start hooks | 无 | ❌ |
| 9.2 | Pre/post tool use hooks | 无 | ❌ |
| 9.3 | User prompt submit hooks | 无 | ❌ |

## 十、Skills 系统

| # | 能力 | ai-server | 状态 |
|---|------|-----------|------|
| 10.1 | Skills manager | 基础发现和解析 | ⚠️ |
| 10.2 | Skills 热重载 | 无 | ❌ |
| 10.3 | Skill 依赖解析 | 无 | ❌ |
| 10.4 | Skill 注入 prompt | 无 | ❌ |

## 十一、MCP 集成

| # | 能力 | ai-server | 状态 |
|---|------|-----------|------|
| 11.1 | MCP server 管理 | mcphost.Manager | ⚠️ |
| 11.2 | 工具命名空间 | 无 | ❌ |
| 11.3 | MCP 资源访问 | 无 | ❌ |
| 11.4 | 延迟加载工具 | 无 | ❌ |

## 十二、记忆系统

| # | 能力 | ai-server | 状态 |
|---|------|-----------|------|
| 12.1 | 记忆追踪处理 | 无 | ❌ |
| 12.2 | Phase 1 启动提取 | 无 | ❌ |
| 12.3 | Phase 2 合并 | 无 | ❌ |
| 12.4 | 记忆引用 | 无 | ❌ |

## 十三、环境上下文

| # | 能力 | ai-server | 状态 |
|---|------|-----------|------|
| 13.1 | XML 序列化 | SerializeToXML() | ✅ |
| 13.2 | Diff 注入 | Diff() | ✅ |
| 13.3 | 网络上下文 | NetworkAllowed/Denied | ✅ |
| 13.4 | Subagent 上下文 | Subagents 字段 | ✅ |

## 十四、其他

| # | 能力 | ai-server | 状态 |
|---|------|-----------|------|
| 14.1 | 网络策略执行 | 无 | ❌ |
| 14.2 | Git 提交归属 | 无 | ❌ |
| 14.3 | Web 搜索 | 无 | ❌ |
| 14.4 | 实时对话 | 无 | ❌ |
| 14.5 | Shell 快照 | 无 | ❌ |
| 14.6 | 项目文档注入 | 无 | ❌ |
| 14.7 | @mention 语法 | 无 | ❌ |
| 14.8 | OpenTelemetry | 无 | ❌ |
| 14.9 | 插件系统 | 无 | ❌ |
| 14.10 | 分层配置 | 扁平 config.go | ❌ |

---

## 优先级建议

### P0（核心功能，影响日常使用）
1. ~~本地命令执行~~ ✅ 已修复
2. ~~API Key 持久化~~ ✅ 已修复
3. Apply patch / diff 系统
4. Guardian 安全审查
5. Hook 系统（pre/post tool use）

### P1（重要增强）
6. 记忆系统（跨会话学习）
7. Tool search（大量 MCP 工具时必需）
8. V2 多 Agent 系统
9. 审批缓存
10. 环境变量策略

### P2（锦上添花）
11. JS REPL
12. 图片处理
13. Web 搜索
14. Git 归属
15. OpenTelemetry
