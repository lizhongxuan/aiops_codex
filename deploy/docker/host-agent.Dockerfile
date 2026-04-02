# syntax=docker/dockerfile:1.7

FROM golang:1.26-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd ./cmd
COPY internal ./internal

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/host-agent ./cmd/host-agent

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends bash ca-certificates tzdata util-linux \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --create-home --uid 10001 agent

WORKDIR /home/agent

COPY --from=build /out/host-agent /usr/local/bin/host-agent

USER agent

ENV AIOPS_SERVER_GRPC_ADDR=127.0.0.1:18090 \
    AIOPS_AGENT_HOST_ID=linux-agent \
    AIOPS_AGENT_HOSTNAME=linux-agent \
    AIOPS_AGENT_VERSION=0.1.0 \
    AIOPS_AGENT_LABELS=env=dev \
    AIOPS_AGENT_BOOTSTRAP_TOKEN=change-me

ENTRYPOINT ["/usr/local/bin/host-agent"]
