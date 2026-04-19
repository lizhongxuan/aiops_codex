# ============================================================
# Stage 1: 构建前端 (Vue 3)
# ============================================================
FROM node:22-bookworm-slim AS frontend

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ============================================================
# Stage 2: 构建 ai-server (Go)
# ============================================================
FROM golang:1.26-bookworm AS backend

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY proto ./proto

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/ai-server ./cmd/ai-server

# ============================================================
# Stage 3: 最终运行镜像
# ============================================================
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       bash ca-certificates tzdata git curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --create-home --uid 10001 aiops

# ai-server 二进制 (Go 静态编译)
COPY --from=backend /out/ai-server /usr/local/bin/ai-server

# 前端构建产物
COPY --from=frontend /src/web/dist /app/web/dist

# 启动脚本
COPY deploy/docker/entrypoint.sh /app/entrypoint.sh

WORKDIR /app

RUN mkdir -p /data/workspace \
    && chown -R aiops:aiops /data /app \
    && chmod +x /app/entrypoint.sh

USER aiops

ENV HOME=/home/aiops \
    AIOPS_HTTP_ADDR=0.0.0.0:8080 \
    AIOPS_GRPC_ADDR=0.0.0.0:18090 \
    DEFAULT_WORKSPACE=/data/workspace \
    APP_STATE_PATH=/data/ai-server-state.json \
    APP_AUDIT_LOG_PATH=/data/ai-audit.log \
    HOST_AGENT_BOOTSTRAP_TOKEN=change-me \
    LANG=C.UTF-8

EXPOSE 8080 18090

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -sf http://127.0.0.1:8080/api/v1/healthz || exit 1

ENTRYPOINT ["/app/entrypoint.sh"]
