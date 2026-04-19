<p align="center">
  <img src="docs/logo.svg" alt="AIOps Codex" width="80" />
</p>

<h1 align="center">AIOps Codex</h1>

<p align="center">
  AI-native 智能运维平台 — 将 LLM 能力深度集成到服务器运维全链路
</p>

<p align="center">
  <a href="#快速开始">快速开始</a> ·
  <a href="#核心特性">核心特性</a> ·
  <a href="#架构概览">架构</a> ·
  <a href="#部署指南">部署</a> ·
  <a href="#开发指南">开发</a>
</p>

---

## 简介

AIOps Codex 是一个面向生产环境的 AI 运维平台，通过 Chat / Workspace / Runner 三种交互模式，让运维工程师以自然语言驱动服务器巡检、故障诊断、变更执行和监控分析。

平台默认通过 **Bifrost LLM Gateway** 连接模型提供方，支持 **OpenAI、Anthropic、Ollama**，再通过 **Host Agent** 安全接入目标主机，所有变更操作经过统一审批审计，确保 AI 辅助运维的可控性和可追溯性。

## 核心特性

### 🤖 AI 驱动的运维交互

- **Chat 模式** — 自然语言对话，AI 自动选择工具执行运维操作
- **Workspace 协作** — 多主机编排，支持 Planner → Worker 任务分解
- **Runner 脚本** — 可复用的自动化脚本，带参数 schema 和 dry-run 预览

### 🔐 治理与审批

- **统一审批流** — 所有变更命令经审批后执行，支持手动审批和自动放行
- **审核流水** — 结构化审计日志，支持按时间/主机/操作人/决策多维筛选
- **主机级授权白名单** — 已授权命令自动通过，支持撤销、禁用、TTL 过期
- **高风险命令拦截** — `rm -rf /`、`sudo su` 等危险命令强制要求 TTL

### 🛡️ 三层能力网关

| 层级 | 说明 | 审批策略 |
|------|------|----------|
| Structured Read | 14 个标准化只读接口 (`host.summary`, `host.process.top` 等) | 免审批 |
| Controlled Mutation | 5 个受控变更接口 (`service.restart`, `config.apply` 等) | 强制审批 |
| Raw Shell | 原始命令执行兜底 | 按策略审批 |

### 📊 Coroot 监控集成

- **服务健康总览** — 嵌入 Coroot 服务视图，实时展示健康/告警/异常状态
- **7 个 MCP 动态工具** — `coroot.list_services`、`coroot.service_metrics`、`coroot.rca_report` 等
- **监控内嵌 AI** — 在监控页面直接调用 AI 分析，支持面板解释、异常归因、修复建议
- **卡片映射** — Coroot 查询结果自动映射为结构化 UI 卡片

### 🧩 能力中心

- **Skills 管理** — 内置 + 自定义技能，支持分类、版本、启用/禁用
- **MCP Servers** — 统一管理 MCP 工具服务，支持探活和权限控制
- **能力绑定** — Skill/MCP 与 Agent Profile、Workspace Preset、UI Card 的绑定关系

### 🎴 UI 卡片系统

- **9 种内置卡片** — 摘要卡、KPI 条、时序图、状态表、控制面板、操作表单、监控聚合、修复聚合
- **卡片管理后台** — 元数据编辑、触发调试器、实时预览
- **Bundle 机制** — Monitor Bundle 和 Remediation Bundle 聚合多卡片

### ⚗️ 沙盒演练

- **Lab 环境** — 创建隔离的沙盒环境，模拟多节点拓扑
- **场景模板** — 预置双层架构、三层微服务、缓存层等拓扑模板
- **故障注入** — 对 mock 节点注入故障，验证告警响应和修复流程
- **v1 Mock 模式** — 前端可正常渲染 bundle 和 control panel，命令返回模拟结果

### 🏭 Generator Workshop

- **自动生成** — 从 MCP Tool → Skill 草稿、Script Config → UI Card 草稿、Coroot → Bundle Preset 草稿
- **4 步流程** — Generate → Lint → Preview → Publish Draft
- **草稿隔离** — 所有生成物默认 draft 状态，不自动上线

## 架构概览

```
┌─────────────────────────────────────────────────────────────┐
│                        Web Dashboard                         │
│   Vue 3 + Pinia + Vue Router + Lucide Icons                  │
│   Chat │ Workspace │ Terminal │ 监控 │ 管理后台               │
└────────────────────────┬────────────────────────────────────┘
                         │ HTTP / WebSocket
┌────────────────────────▼────────────────────────────────────┐
│                     AI Server (Go)                           │
│                                                              │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────────────┐  │
│  │ Session  │ │ Approval │ │ Dynamic  │ │  Orchestrator │  │
│  │ Manager  │ │ & Audit  │ │  Tools   │ │  (Workspace)  │  │
│  └──────────┘ └──────────┘ └──────────┘ └───────────────┘  │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────────────┐  │
│  │  Agent   │ │  Coroot  │ │ UI Card  │ │   Generator   │  │
│  │ Profile  │ │  Client  │ │  Store   │ │   Service     │  │
│  └──────────┘ └──────────┘ └──────────┘ └───────────────┘  │
│                         │                                    │
│              Bifrost provider routing                        │
│                         ▼                                    │
│               Bifrost LLM Gateway (Go)                        │
│                         │                                    │
│        OpenAI / Anthropic / Ollama / OpenAI-compatible       │
├──────────────────────────────────────────────────────────────┤
│  HTTP :8080    gRPC :18090    Coroot Proxy :8080/coroot      │
└────────────┬─────────────────────────┬───────────────────────┘
             │ gRPC 双向流              │ HTTP Reverse Proxy
┌────────────▼──────────┐    ┌─────────▼──────────────────────┐
│    Host Agent (Go)    │    │     Coroot Server (外部)        │
│  每台目标主机一个实例   │    │   服务监控 / 拓扑 / RCA         │
│  终端 / 执行 / 文件    │    └────────────────────────────────┘
└───────────────────────┘
```

### 技术栈

| 层级 | 技术 |
|------|------|
| 前端 | Vue 3, Pinia, Vue Router, Lucide Icons, Monaco Editor, xterm.js |
| 后端 | Go 1.26, gRPC, net/http |
| AI 引擎 | Bifrost LLM Gateway, 支持 OpenAI / Anthropic / Ollama |
| 监控集成 | Coroot (HTTP Client + Reverse Proxy) |
| 持久化 | JSON 文件 (内存 + 异步写盘, sync.RWMutex) |
| 测试 | Vitest, Playwright, Go testing |
| 部署 | Docker, Docker Compose, systemd |

## 项目结构

```
aiops-codex/
├── cmd/
│   ├── ai-server/          # AI Server 主入口
│   └── host-agent/         # Host Agent 主入口
├── internal/
│   ├── bifrost/            # 多 provider LLM Gateway
│   ├── agentloop/          # Bifrost ReAct loop / context / workspace runtime
│   ├── server/             # HTTP/gRPC 服务、API 路由、审批流程
│   ├── store/              # 内存存储 + JSON 持久化
│   ├── model/              # 数据模型定义
│   ├── config/             # 配置管理
│   ├── coroot/             # Coroot HTTP 客户端
│   ├── generator/          # Skill/Card/Bundle 自动生成服务
│   ├── orchestrator/       # Workspace 编排引擎
│   └── agentrpc/           # gRPC 协议定义
├── web/
│   ├── src/
│   │   ├── pages/          # 16 个 Vue 页面
│   │   ├── components/     # 可复用组件 (Coroot Embed, Monitor AI 等)
│   │   ├── lib/            # 工具库 (MCP Bundle Resolver 等)
│   │   └── store.js        # Pinia 全局状态
│   └── tests/
│       ├── *.spec.js       # Vitest 组件测试 (24 个文件, 151 测试)
│       └── e2e/            # Playwright E2E 测试 (6 个文件, 55 测试)
├── deploy/docker/          # Docker 构建和编排
├── proto/                  # Protobuf 定义
├── docs/                   # 架构文档
└── scripts/                # 运维脚本
```

## 快速开始

### 前置条件

- Go 1.26+
- Node.js 22+
- Docker & Docker Compose (部署用)
- LLM provider 凭证或本地模型服务

### 本地开发

```bash
# 克隆仓库
git clone https://github.com/lizhongxuan/aiops-codex.git
cd aiops-codex

# 启动后端
export LLM_PROVIDER=openai
export LLM_API_KEY=sk-xxx
go run ./cmd/ai-server

# 启动前端 (另一个终端)
cd web
npm install
npm run dev

# 访问 http://localhost:5173
```

### Docker 部署

```bash
cd deploy/docker
cp .env.example .env
# 编辑 .env 填入 LLM_PROVIDER / LLM_API_KEY / LLM_BASE_URL 和 HOST_AGENT_BOOTSTRAP_TOKEN

# OpenAI-compatible 服务或 Ollama 可通过 LLM_BASE_URL 指定自定义地址

docker compose build
docker compose up -d

# 访问 http://localhost:18080
```

详细部署文档见 [deploy/docker/README.md](deploy/docker/README.md)。

## API 概览

## Prompt 规则

为避免 prompt 规则再次散落到多个文件，当前仓库遵循这组约束：

- `BuildSystemPrompt()` 只负责通用身份、安全边界、审批与沙箱说明，不承载垂类业务规则。
- tool 的能力、输入限制、host/path/shell/approval 约束只写在 tool-owned prompt spec，不在 developer instructions 里重复展开。
- `TurnPolicy` 只描述结构化合同：
  - `KnowledgeFreshness`
  - `EvidenceContract`
  - `AnswerContract`
  - `MinimumIndependentSources`
  - `RequireSourceAttribution`
  - `RequiredCitationKinds`
  - `AllowEarlyStop`
- 当前/外部 factual query 的要求按通用 freshness/evidence contract 判断，不允许再维护行业或市场专用关键词白名单。
- completion/final-gate repair 只允许说明“缺了什么、下一步该调用什么工具”，不允许再注入 market-specific answer style，例如：
  - `compact snapshot`
  - `1-2 sources`
  - `answer now`
- 搜索类最终回答如命中 `sourced_snapshot` 或 `external_facts` 合同，必须满足独立来源和来源归因要求；这类要求由 `TurnPolicy` 和 final gate 保证，不靠 tool prose 临时兜底。

如果后续需要新增 prompt 规则，请优先判断它属于：

1. 通用 system rule
2. tool-owned contract
3. `TurnPolicy` 结构化约束
4. 极少数 runtime repair

不要把同一条规则同时写进 `system prompt`、`developer instructions`、`tool description` 和 `loop nudge`。

### 核心 API

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/v1/healthz` | GET | 健康检查 |
| `/api/v1/state` | GET | 全局状态快照 |
| `/api/v1/chat/message` | POST | 发送聊天消息 |
| `/api/v1/approvals/{id}` | POST | 审批决策 |

### 审计与授权

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/v1/approval-audits` | GET | 审核流水列表 (支持筛选/分页) |
| `/api/v1/approval-audits/{id}` | GET | 审核流水详情 |
| `/api/v1/approval-grants` | GET/POST | 授权白名单管理 |
| `/api/v1/approval-grants/{id}/revoke` | POST | 撤销授权 |
| `/api/v1/approval-grants/{id}/disable` | POST | 禁用授权 |
| `/api/v1/approval-grants/{id}/enable` | POST | 启用授权 |

### 资源管理

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/v1/capability-bindings` | CRUD | 能力绑定管理 |
| `/api/v1/ui-cards` | CRUD | UI 卡片定义管理 |
| `/api/v1/ui-cards/{id}/preview` | POST | 卡片预览 |
| `/api/v1/script-configs` | CRUD | 脚本配置管理 |
| `/api/v1/script-configs/{id}/dry-run` | POST | 脚本 Dry-Run |
| `/api/v1/lab-environments` | CRUD | 沙盒环境管理 |
| `/api/v1/lab-environments/{id}/start` | POST | 启动沙盒 |
| `/api/v1/lab-environments/{id}/inject` | POST | 故障注入 |
| `/api/v1/lab-environments/{id}/reset` | POST | 重置沙盒 |

### 监控与生成

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/v1/coroot/*` | GET | Coroot 反向代理 (只读) |
| `/api/v1/generator/generate` | POST | 生成 Skill/Card 草稿 |
| `/api/v1/generator/lint` | POST | 校验草稿 |
| `/api/v1/generator/preview` | POST | 预览草稿 |
| `/api/v1/generator/publish-draft` | POST | 发布草稿 |

## 测试

```bash
# Go 后端测试
go test ./internal/... -count=1

# 前端组件测试 (Vitest)
cd web && npm test

# 前端 E2E 测试 (Playwright)
cd web && npx playwright test
```

### 测试覆盖

| 类别 | 文件数 | 测试数 |
|------|--------|--------|
| Go 单元测试 | 20+ | 80+ |
| Vitest 组件测试 | 24 | 151 |
| Playwright E2E | 6 | 55 |

## Agent Profile 策略

Agent Profile 控制 AI 的权限边界，分为两级：

- **main-agent** — 全局策略，控制所有会话的基线权限
- **host-agent-default** — 远程主机默认策略，与 main-agent 取交集

策略维度：

```yaml
capabilityPermissions:
  commandExecution: enabled | approval_required | disabled
  fileRead: enabled
  fileChange: approval_required
  terminal: enabled

commandPermissions:
  defaultMode: allow | approval_required | readonly_only | deny
  allowSudo: false
  categoryPolicies:
    service_mutation: approval_required
    package_mutation: deny
```

## 安全设计

- 所有变更命令经统一审批流程
- Host Agent 通过 Bootstrap Token + 可选 mTLS 认证
- 结构化接口参数校验，拒绝 shell 注入 (`;`, `&&`, `` ` ``, `$(` 等)
- 高风险命令 (`rm -rf /`, `sudo su`, `iptables -F`) 禁止创建无 TTL 授权
- Coroot 代理仅允许只读路径，GET 方法
- 沙盒环境与生产完全隔离，mock host Kind="lab"
- 所有生成物默认 draft 状态，不自动加载执行

## License

MIT
