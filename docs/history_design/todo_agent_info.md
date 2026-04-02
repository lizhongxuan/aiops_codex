# Agent Profile 详细实现任务清单

状态标记：

- `[ ]` 未开始
- `[~]` 进行中
- `[x]` 已完成
- `[!]` 阻塞 / 需先决策

## 0. 前置约束与设计收口

### 0.1 范围确认

- [x] 确认 `Agent Profile` 页面管理的是“静态配置”，不再混入主机运行态、thread/turn、heartbeat 等信息
- [x] 确认该页面入口放在左下角设置按钮弹出菜单中，页面路由为 `/settings/agent`
- [x] 确认第一版先支持“统一配置 + 明确类型区分”，不把所有运行时状态带进来

### 0.2 双 Agent 模型收口

- [x] 把“主 agent”和“host-agent”统一建模为可配置的 Agent Profile 对象
- [x] 明确第一版至少支持两个 profile 类型：
  - `main-agent`
  - `host-agent-default`
- [x] 明确第一版暂不支持“单 host-agent 覆盖配置”
- [x] 明确主 agent 与 host-agent 的公共配置维度完全一致：
  - `System Prompt`
  - `Command Permissions`
  - `Capability Permissions`
  - `Skills`
  - `MCP`

### 0.3 技术路线决策

- [x] 决策 `host-agent` 如何真正消费 `System Prompt / Skills / MCP`
  - 方案 A：把 `host-agent` 升级成真正的本地 agent runtime
  - 方案 B：保留现有 exec daemon，只先接收统一 schema，但部分字段映射为执行策略
- [x] 基于上面决策，更新 [design_agent_info.md](/Users/lizhongxuan/Desktop/aiops-codex/design_agent_info.md) 为最终版本

## 1. 数据模型与配置 Schema

### 1.1 统一 Profile Schema

- [x] 新增 `AgentProfile` 数据结构
- [x] 定义基础字段：
  - `id`
  - `name`
  - `type`
  - `description`
  - `updatedAt`
  - `updatedBy`
- [x] 定义 `type` 枚举：
  - `main-agent`
  - `host-agent-default`
  - `host-agent-override`（若第一版支持）

### 1.2 System Prompt Schema

- [x] 定义 `systemPrompt.content`
- [x] 定义 `systemPrompt.preview`
- [x] 定义 `systemPrompt.version` 或 `hash`
- [x] 定义默认值与恢复默认逻辑

### 1.3 Command Permissions Schema

- [x] 定义 `enabled`
- [x] 定义 `defaultMode`
- [x] 定义 `allowShellWrapper`
- [x] 定义 `allowSudo`
- [x] 定义 `defaultTimeoutSeconds`
- [x] 定义 `allowedWritableRoots`
- [x] 定义 `categoryPolicies`
- [x] 定义命令类别枚举：
  - `system_inspection`
  - `service_read`
  - `network_read`
  - `file_read`
  - `service_mutation`
  - `filesystem_mutation`
  - `package_mutation`

### 1.4 Capability Permissions Schema

- [x] 定义 capability 枚举：
  - `commandExecution`
  - `fileRead`
  - `fileSearch`
  - `fileChange`
  - `terminal`
  - `webSearch`
  - `webOpen`
  - `approval`
  - `multiAgent`
  - `plan`
  - `summary`
- [x] 定义 capability 状态枚举：
  - `enabled`
  - `approval_required`
  - `disabled`

### 1.5 Skills Schema

- [x] 定义 `skills[].id`
- [x] 定义 `skills[].name`
- [x] 定义 `skills[].description`
- [x] 定义 `skills[].source`
- [x] 定义 `skills[].enabled`
- [x] 定义 `skills[].activationMode`

### 1.6 MCP Schema

- [x] 定义 `mcps[].id`
- [x] 定义 `mcps[].name`
- [x] 定义 `mcps[].type`
- [x] 定义 `mcps[].source`
- [x] 定义 `mcps[].enabled`
- [x] 定义 `mcps[].permission`
- [x] 定义 `mcps[].requiresExplicitUserApproval`

### 1.7 预览与 DTO

- [x] 定义 `AgentProfilePreview` 数据结构
- [x] 定义“最终 system prompt 预览”返回结构
- [x] 定义“命令权限摘要”返回结构
- [x] 定义“能力权限摘要”返回结构
- [x] 定义“启用中的 skills / MCP 摘要”返回结构

## 2. 存储层与配置持久化

### 2.1 Store 层

- [x] 选择配置持久化位置：
  - 现有 `.data` 状态文件
  - 独立 `agent-profile.json`
- [x] 新增 Agent Profile 的读写接口
- [x] 新增默认值装载逻辑
- [x] 新增配置版本号，便于后续迁移

### 2.2 配置迁移

- [x] 从现有硬编码配置生成初始默认 Profile
- [x] 把当前 `model / reasoningEffort` 迁移到主 profile
- [x] 把当前硬编码 `developerInstructions` 迁移为默认 `systemPrompt`
- [x] 为历史状态缺失字段提供自动补全

### 2.3 审计

- [x] 所有 profile 修改写审计日志
- [x] 记录修改前 / 修改后摘要
- [x] 记录 profile 类型与 profile ID
- [x] 记录操作者与时间

## 3. 后端 API

### 3.1 基础接口

- [x] `GET /api/v1/agent-profile`
- [x] `PUT /api/v1/agent-profile`
- [x] `POST /api/v1/agent-profile/reset`
- [x] `GET /api/v1/agent-profile/preview`

### 3.2 多 Profile 接口

- [x] `GET /api/v1/agent-profiles`
- [x] `GET /api/v1/agent-profiles/:id`
- [x] `PUT /api/v1/agent-profiles/:id`
- [x] `POST /api/v1/agent-profiles/:id/reset`

### 3.3 校验逻辑

- [x] 校验 system prompt 非空 / 长度上限
- [x] 校验命令类别枚举合法
- [x] 校验 capability 状态合法
- [x] 校验 skill / MCP ID 存在
- [x] 校验 writable roots 路径格式
- [x] 校验高风险权限变更是否需要确认标记

## 4. 主 agent 集成

### 4.1 替换硬编码 developerInstructions

- [x] 抽出 `renderMainAgentDeveloperInstructions(profile, context)` 渲染函数
- [x] 替换 `thread/start` 里的硬编码 `developerInstructions`
- [x] 替换 `turn/start` 里的硬编码 `developerInstructions`
- [x] 保证渲染结果与页面预览一致

### 4.2 主 agent 基础参数接入

- [x] 用 profile 驱动 `model`
- [x] 用 profile 驱动 `reasoningEffort`
- [x] 用 profile 驱动 `approvalPolicy`
- [x] 用 profile 驱动 `sandboxPolicy`

### 4.3 主 agent 能力开关接入

- [x] 根据 `capabilityPermissions` 控制工具暴露
- [x] 禁用时从主 agent 可见能力集中移除
- [x] `approval_required` 时走审批流程
- [x] `disabled` 时由服务端返回明确错误

### 4.4 主 agent 命令权限接入

- [x] 让命令分类器读取 `categoryPolicies`
- [x] `allow` 类命令按正常流程执行
- [x] `approval_required` 类命令强制审批
- [x] `readonly_only` 类命令只允许只读语义
- [x] `deny` 类命令直接拒绝
- [x] 接入 `allowShellWrapper / allowSudo / timeout / writableRoots`

### 4.5 Skills / MCP 暴露控制

- [x] 主 agent 只暴露启用的 skills
- [x] 主 agent 只暴露启用的 MCP
- [x] 对“需显式允许”的 MCP 增加额外 gate

## 5. host-agent 集成

### 5.1 Profile 下发机制

- [x] 设计 `host-agent` 接收 profile 的协议
- [x] 明确是注册后拉取还是由 server 主动推送
- [x] 加入 profile 版本 / hash，避免重复下发

### 5.2 host-agent 基础参数接入

- [x] 让 `host-agent` 识别自己的 profile
- [x] 支持读取 `System Prompt`
- [x] 支持读取 `Command Permissions`
- [x] 支持读取 `Capability Permissions`
- [x] 支持读取 `Skills`
- [x] 支持读取 `MCP`

### 5.3 host-agent 命令权限接入

- [x] 在 `host-agent` 本地执行前再次校验命令类别
- [x] 在 `host-agent` 本地校验 `allowSudo`
- [x] 在 `host-agent` 本地校验 shell wrapper
- [x] 在 `host-agent` 本地校验 timeout 上限
- [x] 在 `host-agent` 本地校验 writable roots

### 5.4 host-agent 能力权限接入

- [x] `commandExecution` 关闭时拒绝 exec
- [x] `fileRead / fileSearch / fileChange` 关闭时拒绝对应 RPC
- [x] `terminal` 关闭时拒绝 terminal/open
- [x] 对 `approval_required` 能力定义与 server 的协同方式

### 5.5 host-agent 的 Skills / MCP 运行时

- [x] 在方案 B 下，把 `skills` 映射为 host-agent 本地 runtime gate，并在命令 / terminal / file flow 中实际生效
- [x] 在方案 B 下，把 `MCP` 配置映射为 host-agent 本地 I/O gate，并在文件 / 日志 / 写入权限中实际生效
- [x] 明确 host-agent 不支持的能力如何降级与报错

## 6. 前端入口与页面框架

### 6.1 设置入口改造

- [x] 把左下角设置按钮从“直接打开 modal”改成“弹出菜单”
- [x] 菜单中新增 `Agent Profile`
- [x] 保留 `通用设置`

### 6.2 路由与页面骨架

- [x] 新增路由 `/settings/agent`
- [x] 新增页面组件 `AgentProfilePage.vue`
- [x] 页面布局支持左侧导航 / 中央表单 / 右侧预览

### 6.3 Profile 选择器

- [x] 顶部增加 profile selector
- [x] 至少支持切换：
  - `主 agent`
  - `host-agent 默认`
- [ ] 若支持单 host-agent 覆盖，再增加实例选择器

## 7. System Prompt 编辑器

### 7.1 文本编辑

- [x] 提供大文本编辑区
- [x] 提供恢复默认按钮
- [x] 提供保存前校验
- [x] 提供 dirty state 提示

### 7.2 结构化辅助

- [x] 提供建议分段：
  - `角色定义`
  - `执行原则`
  - `安全约束`
  - `输出风格`
  - `工具使用规则`
- [x] 提供折叠 / 展开
- [x] 提供字数统计

### 7.3 预览

- [x] 展示最终生效的 system prompt
- [x] 高亮与默认值的 diff
- [x] 提示高风险内容，如“完全放开执行”

## 8. Command Permissions UI

### 8.1 基础控制项

- [x] `是否允许执行命令`
- [x] `是否允许 sudo`
- [x] `是否允许 shell wrapper`
- [x] `默认超时`
- [x] `允许写入路径`

### 8.2 分类权限矩阵

- [x] 用表格展示命令类别
- [x] 每类支持选择：
  - `allow`
  - `approval_required`
  - `readonly_only`
  - `deny`
- [x] 高风险项增加说明文案

### 8.3 UX 提示

- [x] 当选择危险组合时展示 warning
- [x] 保存前二次确认高风险变更

## 9. Capability Permissions UI

### 9.1 能力矩阵

- [x] 列出全部 capability
- [x] 每项支持：
  - `enabled`
  - `approval_required`
  - `disabled`

### 9.2 依赖提示

- [x] 若 `commandExecution` 关闭，提示相关子能力影响
- [x] 若 `fileChange` 为 disabled，提示写文件相关流程不可用
- [x] 若 `multiAgent` 为 disabled，提示无法并行子 agent

## 10. Skills UI

### 10.1 Skills 列表

- [x] 展示全部可用 skills
- [x] 展示来源、简介、当前状态
- [x] 支持搜索与筛选

### 10.2 Skills 配置

- [x] 启用 / 禁用
- [x] activation mode 选择：
  - `default_enabled`
  - `explicit_only`
  - `disabled`

## 11. MCP UI

### 11.1 MCP 列表

- [x] 展示全部可用 MCP
- [x] 展示来源、类型、当前状态
- [x] 支持搜索与筛选

### 11.2 MCP 配置

- [x] 启用 / 禁用
- [x] 权限级别：
  - `readonly`
  - `readwrite`
- [x] 是否需要显式用户确认

## 12. 预览与保存

### 12.1 预览区

- [x] 展示最终 system prompt
- [x] 展示命令权限摘要
- [x] 展示 capability 摘要
- [x] 展示启用中的 skills
- [x] 展示启用中的 MCP

### 12.2 保存流程

- [x] 明确 `取消 / 恢复默认 / 保存`
- [x] 未保存离开时提示
- [x] 保存成功后刷新预览
- [x] 保存失败时展示字段级错误

## 13. 后端强校验与安全收口

### 13.1 服务端兜底

- [x] 即使前端配置错误，后端也要再次校验
- [x] 高风险命令不得仅依赖 prompt
- [x] `disabled` 能力必须由后端硬拦截

### 13.2 权限冲突处理

- [x] 若 capability 关闭而 command category 允许，定义冲突优先级
- [x] 若 MCP 开启但 capability 关闭，定义冲突优先级
- [x] 若主 agent 与 host-agent 配置冲突，定义执行优先级

## 14. 测试清单

### 14.1 单元测试

- [x] Profile schema 解析测试
- [x] 默认值回填测试
- [x] prompt 渲染测试
- [x] command policy 判定测试
- [x] capability gate 测试
- [x] skills / MCP 过滤测试

### 14.2 集成测试

- [x] 主 agent profile 修改后新会话生效
- [x] 主 agent profile 修改后新 turn 生效
- [x] host-agent profile 下发与加载生效
- [x] host-agent 命令权限实际拦截生效
- [x] capability 关闭后相关入口不可用

### 14.3 前端测试

- [x] 页面加载与保存
- [x] 脏状态提示
- [x] 高风险确认弹窗
- [x] profile 切换
- [x] 预览与实际保存值一致

### 14.4 E2E / Smoke

- [x] 修改主 agent prompt 后发起会话，确认行为变化
- [x] 修改 host-agent 权限后，在远端执行验证被正确拦截 / 允许
- [x] skills 开关后验证是否可用
- [x] MCP 开关后验证是否可访问

## 15. 文档与上线

### 15.1 文档

- [x] 更新产品设计文档
- [x] 更新 API 文档
- [x] 更新部署说明
- [x] 更新运维说明：如何恢复默认 profile

### 15.2 回滚预案

- [x] 保留一份默认 profile 快照
- [x] 提供 reset endpoint
- [x] 提供 profile 导出 / 导入能力（可放第二版）

## 16. 建议实施顺序

### Phase A：打基础

- [x] 0.x 设计收口
- [x] 1.x Schema
- [x] 2.x 存储层
- [x] 3.1 ~ 3.2 主 agent 基础接入
- [x] 6.x 页面入口与骨架

### Phase B：先完成主 agent

- [x] 7.x System Prompt UI
- [x] 8.x Command Permissions UI
- [x] 9.x Capability UI
- [x] 12.x 预览与保存
- [x] 14.1 / 14.2 主 agent 相关测试

### Phase C：接 Skills / MCP

- [x] 10.x Skills
- [x] 11.x MCP
- [x] 3.5 主 agent 暴露控制

### Phase D：完成 host-agent

- [x] 5.x host-agent profile 下发与执行
- [x] 14.2 / 14.4 host-agent 集成验证

## 17. 第一版完成标准

- [x] 可以在页面里编辑并保存主 agent 的 system prompt
- [x] 可以编辑命令权限与能力权限
- [x] 可以启用 / 禁用 skills 和 MCP
- [x] 主 agent 新会话可按 profile 生效
- [x] host-agent 至少能加载统一 schema 并对权限项生效
- [x] 所有高风险权限变更有服务端兜底校验
