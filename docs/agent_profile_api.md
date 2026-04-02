# Agent Profile API 与运维说明

本文记录 `Agent Profile` 的接口、默认配置落点，以及恢复默认 profile 的操作方式。

## 1. Profile 模型

当前系统内置两类默认 profile：

- `main-agent`
- `host-agent-default`

两类 profile 的公共配置维度一致：

- `System Prompt`
- `Command Permissions`
- `Capability Permissions`
- `Skills`
- `MCP`

第一版暂不支持单主机覆盖配置。

## 2. 存储位置

Agent Profile 与其他运行态状态一起持久化到 `APP_STATE_PATH` 指向的 JSON 文件。

默认部署场景下通常是：

- Docker: `/data/ai-server-state.json`
- 本地开发: `.data/ai-server-state.json`

这份状态文件里会保存 `agentProfiles`，因此 profile 修改后重启服务仍会保留。

## 3. API

### 3.1 主 profile

- `GET /api/v1/agent-profile`
- `PUT /api/v1/agent-profile`
- `POST /api/v1/agent-profile/reset`
- `GET /api/v1/agent-profile/preview?profileId=main-agent&hostId=...`

### 3.2 多 profile

- `GET /api/v1/agent-profiles`
- `GET /api/v1/agent-profiles/:id`
- `PUT /api/v1/agent-profiles/:id`
- `POST /api/v1/agent-profiles/:id/reset`

### 3.3 更新请求

`PUT` 请求会携带 profile 本体，以及一个高风险确认标记：

```json
{
  "riskConfirmed": true,
  "id": "main-agent",
  "type": "main-agent",
  "name": "Primary Agent"
}
```

如果修改涉及高风险项，例如：

- `allowSudo` 从关闭改为开启
- `sandboxMode` 切到 `danger-full-access`
- 某些命令/能力从受限切到 `allow` / `enabled`
- MCP 从只读切到 `readwrite`

后端会要求显式确认，否则返回字段级错误。

### 3.4 预览返回

`GET /api/v1/agent-profile/preview` 会返回：

- 最终生效的 `systemPrompt`
- 命令权限摘要
- capability 摘要
- 已启用的 skills
- 已启用的 MCP
- runtime 参数摘要

这用于前端右侧预览区和运维核对。

## 4. 默认值与恢复默认

### 4.1 默认回填

如果状态文件里缺少 profile，服务启动时会自动补齐：

- `main-agent`
- `host-agent-default`

如果 profile 缺失部分字段，也会按默认值自动回填。

### 4.2 恢复默认的推荐方式

优先使用 API：

- 恢复主 profile 默认值: `POST /api/v1/agent-profile/reset`
- 恢复指定 profile 默认值: `POST /api/v1/agent-profiles/:id/reset`

这是最安全的恢复方式，不会影响其他会话、主机或认证状态。

### 4.3 恢复默认的运维流程

如果需要在故障排查时回退到默认 profile，建议按下面顺序操作：

1. 先导出或备份当前 `APP_STATE_PATH` 文件。
2. 调用对应的 `reset` endpoint 恢复默认 profile。
3. 重新打开页面或重启 `ai-server`，确认预览区与保存后的 profile 一致。

只有在 API 不可用且状态文件已损坏时，才考虑直接编辑 `APP_STATE_PATH`，并且只修改 `agentProfiles` 相关内容。

## 5. 交互约束

- `main-agent` 的 `model`、`reasoningEffort`、`approvalPolicy`、`sandboxMode` 会直接参与新会话和新 turn 的指令构造。
- `host-agent-default` 第一版先统一建模和持久化，后续再接入 host-agent runtime 消费。
- `skills` 和 `MCP` 的启用状态会进入主 agent 的指令预览和权限判断。

## 6. 相关配置

Docker 部署时，状态文件路径由 `APP_STATE_PATH` 控制，默认示例见 `deploy/docker/ai-server.env.example`。

如果要从运维角度快速定位 profile 配置，只需要检查：

- `APP_STATE_PATH`
- `agentProfiles`
- `main-agent`
- `host-agent-default`

