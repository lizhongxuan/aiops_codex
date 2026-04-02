#!/usr/bin/env bash
set -euo pipefail

IMAGE_TAG="${1:-aiops-codex-host-agent:latest}"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required to validate the host-agent image" >&2
  exit 1
fi

if ! docker image inspect "$IMAGE_TAG" >/dev/null 2>&1; then
  echo "image not found: $IMAGE_TAG" >&2
  echo "build it first, for example:" >&2
  echo "  docker build -f deploy/docker/host-agent.Dockerfile -t $IMAGE_TAG ." >&2
  exit 1
fi

echo "[1/4] checking required binaries inside image: /bin/sh, bash, script, host-agent"
docker run --rm --entrypoint /bin/sh "$IMAGE_TAG" -lc '
  set -eu
  command -v /bin/sh >/dev/null
  command -v bash >/dev/null
  command -v script >/dev/null
  test -x /usr/local/bin/host-agent
'

echo "[2/4] checking default runtime env placeholders"
docker run --rm --entrypoint /bin/sh "$IMAGE_TAG" -lc '
  set -eu
  test -n "${AIOPS_SERVER_GRPC_ADDR:-}"
  test -n "${AIOPS_AGENT_HOST_ID:-}"
  test -n "${AIOPS_AGENT_HOSTNAME:-}"
  test -n "${AIOPS_AGENT_BOOTSTRAP_TOKEN:-}"
'

echo "[3/4] checking pseudo-TTY dependency from util-linux"
docker run --rm --entrypoint /bin/sh "$IMAGE_TAG" -lc '
  set -eu
  script --version >/dev/null 2>&1 || script -V >/dev/null 2>&1
'

echo "[4/4] checking host-agent binary starts and fails fast without grpc target"
docker run --rm \
  -e AIOPS_SERVER_GRPC_ADDR=127.0.0.1:1 \
  -e AIOPS_AGENT_HOST_ID=smoke-host \
  -e AIOPS_AGENT_HOSTNAME=smoke-host \
  -e AIOPS_AGENT_BOOTSTRAP_TOKEN=smoke-token \
  --entrypoint /bin/sh \
  "$IMAGE_TAG" -lc '
    set +e
    /usr/local/bin/host-agent >/tmp/host-agent-smoke.log 2>&1
    status=$?
    cat /tmp/host-agent-smoke.log
    if [ "$status" -eq 0 ]; then
      echo "host-agent unexpectedly exited with code 0" >&2
      exit 1
    fi
    exit 0
  '

echo "host-agent image validation passed for $IMAGE_TAG"
