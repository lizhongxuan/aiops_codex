# AIOps Codex Docker 部署指南

## 架构概览

```
                    ┌─────────────────────────────────────────┐
                    │         ai-server 容器                   │
                    │                                          │
 浏览器 ──HTTP──►  │  ai-server (Go)                          │
                    │      │                                   │
                    │      │ Bifrost Gateway                   │
                    │      ▼                                   │
                    │  OpenAI / Anthropic / Ollama             │
                    │      │                                   │
                    │      │ HTTPS / OpenAI-compatible         │
                    │      ▼                                   │
                    │  模型提供方 API                          │
                    ├──────────────────────────────────────────┤
                    │  端口:                                    │
                    │    8080  → HTTP + WebSocket + 前端        │
                    │    18090 → gRPC (host-agent 接入)         │
                    └──────────┬───────────────────────────────┘
                               │ gRPC 双向流
                    ┌──────────▼───────────────────────────────┐
                    │  host-agent 容器 (每台目标主机一个)         │
                    │  host-agent (Go) → 终端/执行/文件操作      │
                    └──────────────────────────────────────────┘
```

当前运行时默认由 ai-server 内置的 Bifrost Gateway 对接模型提供方，
通过 `LLM_PROVIDER`、`LLM_API_KEY`、`LLM_BASE_URL` 等环境变量选择 provider。
不需要在宿主机额外安装任何 LLM 客户端二进制。

## 镜像里有什么

ai-server.Dockerfile 是 3 阶段多阶段构建：

```
Stage 1 (frontend)     : Node 22 → npm ci → npm run build → web/dist/
Stage 2 (backend)      : Go 1.26 → go build → ai-server 二进制
Stage 3 (final)        : debian:bookworm-slim + 产物合并
```

最终镜像内容：

```
/usr/local/bin/ai-server   ← Go 静态二进制 (~15MB)
/app/web/dist/             ← Vue 3 前端构建产物
/data/                     ← 运行时数据目录
```

基础镜像是 `debian:bookworm-slim`，不需要 Node.js 运行时。
ai-server 是 `CGO_ENABLED=0` 构建，Bifrost 通过网络访问 provider 接口，不依赖本机安装额外的 LLM 客户端二进制。

## 快速开始

### 1. 准备环境变量

```bash
cd deploy/docker
cp .env.example .env
```

编辑 `.env`：

```bash
# 必填: host-agent 接入 token
HOST_AGENT_BOOTSTRAP_TOKEN=your-secure-token-here

# 必填: LLM provider 配置
LLM_PROVIDER=openai
LLM_API_KEY=sk-xxx
# 可选: 兼容 OpenAI / 私有网关 / Ollama 的自定义地址
LLM_BASE_URL=
```

### 2. 构建并启动

```bash
# 构建镜像 (首次约 3-5 分钟)
docker compose build

# 启动
docker compose up -d

# 查看日志
docker compose logs -f ai-server

# 查看健康状态
docker compose ps
```

### 3. 访问

- 前端: http://127.0.0.1:18080
- 健康检查: http://127.0.0.1:18080/api/v1/healthz
- gRPC: 127.0.0.1:18090 (host-agent 接入)

开发态可以把 `18090` 临时绑定到 `0.0.0.0:18090` 做受控联调，但只限本机或受控测试网段，不能作为长期对外入口。
不要在开发态使用默认 token 或长期不轮转的 token 把这个端口长期开口；如果只是本机验证，优先保留在 `127.0.0.1:18090`。
跨主机测试时必须同时限制来源网段和暴露时长。

## host-agent 必填环境变量

以下 4 个变量是 host-agent 最低接入要求，缺任意一个都无法稳定注册到 ai-server：

```bash
AIOPS_SERVER_GRPC_ADDR=192.168.1.10:18090
AIOPS_AGENT_HOST_ID=linux-01
AIOPS_AGENT_HOSTNAME=linux-01
AIOPS_AGENT_BOOTSTRAP_TOKEN=replace-with-real-token
```

字段说明：

- `AIOPS_SERVER_GRPC_ADDR`: host-agent 回连 ai-server 的 gRPC 地址，必须是 agent 宿主机可达的地址。
- `AIOPS_AGENT_HOST_ID`: 稳定且唯一的主机标识；会进入 UI、审批、审计和会话绑定。
- `AIOPS_AGENT_HOSTNAME`: 展示给用户看的主机名；建议与机器实际 hostname 或 CMDB 名称一致。
- `AIOPS_AGENT_BOOTSTRAP_TOKEN`: 必须和 ai-server 侧 `HOST_AGENT_BOOTSTRAP_TOKEN` 或 `HOST_AGENT_BOOTSTRAP_TOKENS` 匹配。

推荐额外补充：

- `AIOPS_AGENT_LABELS`: 如 `env=prod,role=web`，便于筛选和审计。
- `AIOPS_AGENT_TLS_CA_FILE` / `AIOPS_AGENT_TLS_CERT_FILE` / `AIOPS_AGENT_TLS_KEY_FILE`: 生产态启用 TLS / mTLS 时必填。

## Bifrost LLM 配置

Bifrost 默认启用，当前代码支持的 provider 是 `openai`、`anthropic`、`ollama`。
通用配置项如下：

```bash
USE_BIFROST=true
LLM_PROVIDER=openai
LLM_MODEL=gpt-4o-mini
LLM_API_KEY=sk-xxx
LLM_BASE_URL=
LLM_API_KEYS=
LLM_FALLBACK_PROVIDER=
LLM_FALLBACK_MODEL=
LLM_FALLBACK_API_KEY=
LLM_COMPACT_MODEL=gpt-4o-mini
```

配置方式：

```yaml
# docker-compose.yml
services:
  ai-server:
    environment:
      - USE_BIFROST=true
      - LLM_PROVIDER=openai
      - LLM_API_KEY=sk-xxx
      - LLM_BASE_URL=
```

常见 provider 示例：

```bash
# OpenAI
LLM_PROVIDER=openai
LLM_API_KEY=sk-xxx
LLM_BASE_URL=https://api.openai.com/v1

# Anthropic
LLM_PROVIDER=anthropic
LLM_API_KEY=ant-xxx
LLM_BASE_URL=https://api.anthropic.com

# Ollama
LLM_PROVIDER=ollama
LLM_BASE_URL=http://127.0.0.1:11434/v1
```

说明：

- `LLM_BASE_URL` 可指向 OpenAI-compatible 服务，也可指向本地 Ollama。
- `LLM_PROVIDER=ollama` 时通常不需要 API key。
- 需要回退模型时，填 `LLM_FALLBACK_PROVIDER`、`LLM_FALLBACK_MODEL`、`LLM_FALLBACK_API_KEY`。

## 多架构支持

Dockerfile 支持 amd64 和 arm64：

```bash
# 构建 amd64 (默认)
docker compose build

# 构建 arm64 (如部署到 ARM 服务器)
docker compose build --build-arg TARGETARCH=arm64

# 或者用 buildx 同时构建两个架构
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -f deploy/docker/ai-server.Dockerfile \
  -t aiops-codex:latest \
  --push .
```

## 数据持久化

所有运行时数据在 `/data` volume 里：

```
/data/
├── workspace/              ← 运行时工作区 (agent 读写文件的地方)
├── ai-server-state.json    ← 会话/主机/认证状态持久化
└── ai-audit.log            ← 审计日志 (JSONL)
```

`Agent Profile` 也持久化在同一个 `APP_STATE_PATH` 状态文件里，默认就是 `/data/ai-server-state.json`。  
这意味着 profile 的修改、恢复默认、以及 `main-agent` / `host-agent-default` 的默认值回填，都会跟着这份状态文件一起保存，不需要再单独挂载一份 profile 配置文件。

接口和恢复默认的运维步骤见 [docs/agent_profile_api.md](/Users/lizhongxuan/Desktop/aiops-codex/docs/agent_profile_api.md)。

备份：

```bash
# 备份整个数据目录
docker compose exec ai-server tar czf - /data > backup.tar.gz

# 或者直接备份 volume
docker run --rm -v ai-data:/data -v $(pwd):/backup \
  debian:bookworm-slim tar czf /backup/ai-data-backup.tar.gz /data
```

## host-agent 部署

### Docker 方式（测试环境）

docker-compose.yml 里已经包含一个 `host-agent-local` 服务用于测试。

### host-agent 镜像最低运行要求校验

`host-agent` 镜像至少要满足 3 组要求：

- Shell 能力：镜像里必须有 `/bin/sh`，并最好提供 `bash`，否则只读命令和交互终端的 shell 选择会退化。
- Pseudo-TTY 能力：镜像里必须提供 `script`（来自 `util-linux`），否则远程终端无法稳定建立伪终端。
- 回连能力：运行时必须显式提供 `AIOPS_SERVER_GRPC_ADDR / AIOPS_AGENT_HOST_ID / AIOPS_AGENT_HOSTNAME / AIOPS_AGENT_BOOTSTRAP_TOKEN`，否则 agent 无法稳定注册回 ai-server。

仓库已提供一个最低要求自检脚本：

```bash
chmod +x deploy/docker/validate_host_agent_image.sh
deploy/docker/validate_host_agent_image.sh aiops-codex-host-agent:latest
```

脚本会检查：

- 镜像内存在 `/bin/sh`、`bash`、`script`、`/usr/local/bin/host-agent`
- 镜像默认环境变量占位已写入
- `script` 可执行，满足 pseudo-TTY 依赖
- `host-agent` 二进制能在缺少真实 gRPC 目标时快速启动并失败退出，而不是因为镜像依赖缺失直接崩掉

对应到当前 `deploy/docker/host-agent.Dockerfile`：

- `bash` 满足命令执行和 shell 选择回退需求
- `util-linux` 提供 `script`，满足终端伪 TTY
- `ca-certificates` 满足 TLS / mTLS 回连场景
- `tzdata` 便于日志和审计时间与 Linux 宿主机一致

### 二进制方式（生产环境，推荐）

host-agent 应该直接装在目标主机上，不要装在容器里，
否则它只能操作容器内部而不是宿主机。

```bash
# 在目标主机上构建
docker build -f deploy/docker/host-agent.Dockerfile -o type=local,dest=./out .

# 或者直接 go build
CGO_ENABLED=0 go build -o host-agent ./cmd/host-agent

# 运行
AIOPS_SERVER_GRPC_ADDR=ai-server-ip:18090 \
AIOPS_AGENT_HOST_ID=web-01 \
AIOPS_AGENT_HOSTNAME=web-01 \
AIOPS_AGENT_BOOTSTRAP_TOKEN=your-token \
AIOPS_AGENT_LABELS=env=prod,role=web \
./host-agent
```

### systemd 服务（Linux 生产环境）

```ini
# /etc/systemd/system/aiops-host-agent.service
[Unit]
Description=AIOps Host Agent
After=network.target

[Service]
Type=simple
User=ops
ExecStart=/usr/local/bin/host-agent
Restart=always
RestartSec=3
Environment=AIOPS_SERVER_GRPC_ADDR=ai-server-ip:18090
Environment=AIOPS_AGENT_HOST_ID=web-01
Environment=AIOPS_AGENT_HOSTNAME=web-01
Environment=AIOPS_AGENT_BOOTSTRAP_TOKEN=your-token
Environment=AIOPS_AGENT_LABELS=env=prod,role=web

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now aiops-host-agent
```

### Linux 主机升级步骤

```bash
# 1. 拉取新二进制
sudo install -m 0755 ./host-agent /usr/local/bin/host-agent

# 2. 重启服务
sudo systemctl restart aiops-host-agent

# 3. 确认重启成功
sudo systemctl status aiops-host-agent --no-pager
```

如果你是用容器方式运行 host-agent：

```bash
docker compose build host-agent-local
docker compose up -d host-agent-local
docker compose logs --tail=100 host-agent-local
```

### 最小 smoke 检查

建议每次新部署或升级后至少跑完下面 6 步：

```bash
# 1. ai-server 健康
curl -fsSL http://127.0.0.1:18080/api/v1/healthz

# 2. host-agent 进程或容器在线
sudo systemctl status aiops-host-agent --no-pager
# 或 docker compose ps host-agent-local

# 3. UI 状态里能看到目标主机上线
curl -fsSL http://127.0.0.1:18080/api/v1/state | jq '.hosts[] | {id, name, status, executable, terminalCapable}'

# 4. 在页面里选中该主机，执行一次只读命令
#    示例：uptime / df -h / systemctl status nginx

# 5. 执行一次需要审批的命令
#    示例：systemctl restart nginx

# 6. 进入同一台主机终端，确认聊天与终端指向一致
```

通过标准：

- 主机在 `/api/v1/state` 里是 `online`，且 `executable=true`。
- 只读命令能返回输出，不会静默回退到本地。
- 变更命令会弹审批，审批后能继续执行。
- 终端页显示的主机和聊天当前选中的主机一致。

## 生产环境安全加固

生产态必须把 host-agent 接入面放在私网或 VPN 内，不允许直接暴露到公网。
建议全链路启用 TLS / mTLS，并且同时配置 host allowlist、CIDR allowlist 和 bootstrap token 轮转。
`HOST_AGENT_SECURITY_PROFILE=production` 只是生产配置开关，不代表已经满足网络隔离、证书校验和身份约束。

### 启用 TLS

```yaml
# docker-compose.yml
services:
  ai-server:
    volumes:
      - ai-data:/data
      - ./certs:/certs:ro
    environment:
      - HOST_AGENT_SECURITY_PROFILE=production
      - AIOPS_GRPC_TLS_CERT_FILE=/certs/server.pem
      - AIOPS_GRPC_TLS_KEY_FILE=/certs/server-key.pem
      - AIOPS_GRPC_TLS_CLIENT_CA_FILE=/certs/ca.pem
      - HOST_AGENT_ALLOWED_HOST_IDS=web-01,web-02,db-01
      - HOST_AGENT_ALLOWED_CIDRS=10.0.0.0/8,172.16.0.0/12
```

host-agent 端对应配置：

```bash
AIOPS_AGENT_TLS_CA_FILE=/certs/ca.pem
AIOPS_AGENT_TLS_CERT_FILE=/certs/agent.pem
AIOPS_AGENT_TLS_KEY_FILE=/certs/agent-key.pem
AIOPS_AGENT_TLS_SERVER_NAME=aiops-server.internal
```

生产态里，`AIOPS_AGENT_HOST_ID` 必须保持固定、稳定、全局唯一，不能因为重装、迁移或容器重建而变化。
`AIOPS_AGENT_HOSTNAME` 只是展示名，不应作为身份判断依据。
启用 mTLS 时，证书 SAN / CN 需要与 `AIOPS_AGENT_TLS_SERVER_NAME` 和实际访问域名一致。

### 更换 bootstrap token

```bash
# 生成安全 token
openssl rand -hex 32

# 支持多 token 轮转
HOST_AGENT_BOOTSTRAP_TOKENS=new-token-2026-04,old-token-2026-03
```

生产态不要长期只保留单一 bootstrap token。
滚动更新时应先下发新 token、完成 agent 切换，再移除旧 token。
开发态如果临时开放 `0.0.0.0:18090`，也必须使用短期可回收的测试 token，不能直接沿用生产 token。

## 故障排查

### Bifrost runtime 没起来

```bash
# 查看日志
docker compose logs ai-server | grep -i bifrost

# 检查环境变量
docker compose exec ai-server env | grep -E '^(USE_BIFROST|LLM_)'

# 检查健康
curl http://127.0.0.1:18080/api/v1/healthz
```

常见原因：
- `LLM_PROVIDER` 拼错或不在支持列表里。
- `LLM_API_KEY` 缺失，但当前 provider 需要认证。
- `LLM_BASE_URL` 指向的 provider 不可达，或者不是对应 provider 的兼容接口。
- `USE_BIFROST=false`，导致运行时没有走 Bifrost 网关。

### host-agent 连不上

```bash
# 检查 gRPC 端口
docker compose exec ai-server curl -v telnet://127.0.0.1:18090

# 检查 host-agent 日志
docker compose logs host-agent-local

# 检查主机列表
curl http://127.0.0.1:18080/api/v1/state | jq '.hosts'
```

常见原因：
- bootstrap token 不匹配
- gRPC 端口没暴露或被防火墙拦截
- TLS 配置不一致

### 升级后主机不在线

```bash
# 看 systemd 或容器日志
sudo journalctl -u aiops-host-agent -n 100 --no-pager
# 或 docker compose logs --tail=100 host-agent-local

# 核对 4 个必填变量
env | grep '^AIOPS_'
```

优先检查：

- `AIOPS_SERVER_GRPC_ADDR` 是否写成了 agent 宿主机可访问的地址，而不是容器内部地址。
- `AIOPS_AGENT_HOST_ID` 是否和旧实例冲突，导致 UI 上看起来像“同一台主机反复上下线”。
- `AIOPS_AGENT_BOOTSTRAP_TOKEN` 是否和 ai-server 当前允许的 token 集合一致。

## 文件清单

```
deploy/docker/
├── ai-server.Dockerfile       ← ai-server + 前端 + Bifrost runtime (多阶段构建)
├── host-agent.Dockerfile      ← host-agent (独立镜像)
├── docker-compose.yml         ← 编排文件
├── .env.example               ← 环境变量模板
├── ai-server.env.example      ← ai-server 完整环境变量参考
├── host-agent.env.example     ← host-agent 完整环境变量参考
└── README.md                  ← 本文档
```
