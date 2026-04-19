#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATA_DIR="${CLIPROXYAPI_DATA_DIR:-$ROOT_DIR/.data/cliproxyapi}"
CONFIG_PATH="${CLIPROXYAPI_CONFIG_PATH:-$DATA_DIR/config.yaml}"

if ! command -v cliproxyapi >/dev/null 2>&1; then
  echo "Missing required command: cliproxyapi" >&2
  exit 1
fi

mkdir -p "$DATA_DIR/auth" "$DATA_DIR/logs"

exec cliproxyapi -config "$CONFIG_PATH" "$@"
