# Agent Profile Smoke 0328

这份 smoke 资产用于验证 `Agent Profile` 的核心运行闭环，重点覆盖 `14.4`：

1. 修改 `main-agent` 的 `System Prompt` 后，发起新会话并验证行为变化。
2. 修改 `host-agent` 的命令权限后，远端执行被正确拦截或允许。
3. 关闭/开启 skill 后，`/api/v1/state` 里的 `enabledSkills` 能反映真实加载结果。
4. 关闭/开启 MCP 后，`/api/v1/state` 里的 `enabledMCPs` 能反映真实加载结果。

## 执行方式

```bash
GOCACHE=$PWD/.tools/go-build GOMODCACHE=$PWD/.tools/gomodcache go run ./scripts/agent_profile_smoke_0328
```

## 依赖环境

- `AIOPS_BASE_URL`
- `AIOPS_REMOTE_HOST_ID`
- `AIOPS_SMOKE_PROMPT_TRIGGER`
- `AIOPS_SMOKE_PROMPT_EXPECTED`
- `AIOPS_SMOKE_REMOTE_BLOCKED_COMMAND`
- `AIOPS_SMOKE_REMOTE_ALLOWED_COMMAND`
- `AIOPS_SMOKE_HOST_SKILL_ID`
- `AIOPS_SMOKE_HOST_MCP_ID`

## 断言策略

- `main-agent` 的 prompt smoke 通过 `/api/v1/agent-profile/preview` 和聊天结果共同验证。
- `host-agent` 的权限 smoke 通过 `/api/v1/state` 里主机状态和远端命令卡结果共同验证。
- `skill / MCP` smoke 优先使用 `/api/v1/state` 的 `profileSummary`、`enabledSkills`、`enabledMCPs`，避免解析自由文本。

## 说明

- 这个 smoke 只覆盖资产和验证脚本，不修改运行时实现。
- 如果真实远端 host 不存在，脚本会在解析 `AIOPS_REMOTE_HOST_ID` 时直接失败。
