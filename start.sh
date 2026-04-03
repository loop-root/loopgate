#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOCKET_PATH="${LOOPGATE_SOCKET:-$REPO_ROOT/runtime/state/loopgate.sock}"
FRONTEND_DIR="$REPO_ROOT/cmd/haven/frontend"
LOOPGATE_LOG_DIR="$REPO_ROOT/runtime/logs"
LOOPGATE_LOG_PATH="$LOOPGATE_LOG_DIR/loopgate.log"
STARTED_LOOPGATE=0
LOOPGATE_PID=""
GO_RUN_TAGS=( -tags production )
REUSE_RUNNING_LOOPGATE="${HAVEN_REUSE_RUNNING_LOOPGATE:-0}"

append_unique_flag() {
  local existing_flags="$1"
  local required_flag="$2"
  if [[ " $existing_flags " == *" $required_flag "* ]]; then
    printf '%s' "$existing_flags"
    return
  fi
  if [[ -n "$existing_flags" ]]; then
    printf '%s %s' "$existing_flags" "$required_flag"
    return
  fi
  printf '%s' "$required_flag"
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

loopgate_ready() {
  curl --silent --fail --unix-socket "$SOCKET_PATH" http://loopgate/v1/health >/dev/null
}

loopgate_socket_pid() {
  lsof -t -U "$SOCKET_PATH" 2>/dev/null | head -n 1
}

stop_existing_loopgate() {
  local existing_pid
  existing_pid="$(loopgate_socket_pid)"
  if [[ -z "$existing_pid" ]]; then
    return
  fi

  echo "Stopping existing Loopgate on $SOCKET_PATH..."
  kill "$existing_pid" >/dev/null 2>&1 || true

  for _ in $(seq 1 25); do
    if ! kill -0 "$existing_pid" >/dev/null 2>&1; then
      break
    fi
    sleep 0.2
  done

  if kill -0 "$existing_pid" >/dev/null 2>&1; then
    echo "Existing Loopgate did not stop cleanly; sending SIGKILL..."
    kill -9 "$existing_pid" >/dev/null 2>&1 || true
  fi
}

cleanup() {
  if [[ "$STARTED_LOOPGATE" == "1" && -n "$LOOPGATE_PID" ]]; then
    kill "$LOOPGATE_PID" >/dev/null 2>&1 || true
    wait "$LOOPGATE_PID" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT INT TERM

require_command go
require_command npm
require_command curl
require_command lsof

mkdir -p "$REPO_ROOT/runtime/state" "$LOOPGATE_LOG_DIR"

# Match the linker flags used when launching Haven (Wails on macOS needs UTType).
HAVEN_CGO_LDFLAGS="${CGO_LDFLAGS:-}"
HAVEN_CGO_CFLAGS="${CGO_CFLAGS:-}"
if [[ "$(uname -s)" == "Darwin" ]]; then
  HAVEN_CGO_LDFLAGS="$(append_unique_flag "$HAVEN_CGO_LDFLAGS" "-framework UniformTypeIdentifiers")"
  HAVEN_CGO_CFLAGS="$(append_unique_flag "$HAVEN_CGO_CFLAGS" "-Wno-deprecated-declarations")"
fi

if [[ ! -d "$FRONTEND_DIR/node_modules" ]]; then
  echo "Installing Haven frontend dependencies..."
  (
    cd "$FRONTEND_DIR"
    npm ci
  )
fi

echo "Building Haven frontend..."
(
  cd "$FRONTEND_DIR"
  npm run build
)

# Compile-check Go before starting Loopgate (go run also builds from source each time;
# this fails fast with a clear error if the tree does not build).
echo "Verifying Go code compiles..."
(
  cd "$REPO_ROOT"
  go build -o /dev/null ./cmd/loopgate
  env \
    CGO_CFLAGS="$HAVEN_CGO_CFLAGS" \
    CGO_LDFLAGS="$HAVEN_CGO_LDFLAGS" \
    go build "${GO_RUN_TAGS[@]}" -o /dev/null ./cmd/haven
)

if loopgate_ready; then
  if [[ "$REUSE_RUNNING_LOOPGATE" == "1" ]]; then
    echo "Loopgate is already running (reusing process — set HAVEN_REUSE_RUNNING_LOOPGATE=0 or stop it to pick up Go code changes)." >&2
  else
    stop_existing_loopgate
  fi
fi

if ! loopgate_ready; then
  if [[ -e "$SOCKET_PATH" ]]; then
    echo "Removing stale Loopgate socket at $SOCKET_PATH"
    rm -f "$SOCKET_PATH"
  fi

  echo "Starting Loopgate..."
  (
    cd "$REPO_ROOT"
    MORPH_REPO_ROOT="$REPO_ROOT" go run ./cmd/loopgate >>"$LOOPGATE_LOG_PATH" 2>&1
  ) &
  LOOPGATE_PID=$!
  STARTED_LOOPGATE=1

  for _ in $(seq 1 50); do
    if loopgate_ready; then
      break
    fi
    if ! kill -0 "$LOOPGATE_PID" >/dev/null 2>&1; then
      echo "Loopgate exited before it became ready. See $LOOPGATE_LOG_PATH" >&2
      exit 1
    fi
    sleep 0.2
  done

  if ! loopgate_ready; then
    echo "Loopgate did not become ready in time. See $LOOPGATE_LOG_PATH" >&2
    exit 1
  fi
fi

echo "Launching Haven..."
cd "$REPO_ROOT"

env \
  MORPH_REPO_ROOT="$REPO_ROOT" \
  LOOPGATE_SOCKET="$SOCKET_PATH" \
  CGO_CFLAGS="$HAVEN_CGO_CFLAGS" \
  CGO_LDFLAGS="$HAVEN_CGO_LDFLAGS" \
  go run "${GO_RUN_TAGS[@]}" ./cmd/haven
