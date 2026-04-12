#!/usr/bin/env bash
# Start Loopgate for native Haven clients over the Unix-socket HTTP control plane.
#
# IMPORTANT: Run the copy of this script inside the Loopgate repo.
# REPO_ROOT is derived from this file's location unless LOOPGATE_REPO_ROOT or
# MORPH_REPO_ROOT overrides it.
#
# Usage:
#   ./scripts/start-loopgate-swift-dev.sh
#   ./scripts/start-loopgate-swift-dev.sh --accept-policy
#   LOOPGATE_BACKGROUND=1 ./scripts/start-loopgate-swift-dev.sh
#
# Environment (optional):
#   LOOPGATE_REPO_ROOT      — canonical repo root for config/policy
#   MORPH_REPO_ROOT         — compatibility alias for repo root
#   LOOPGATE_SOCKET         — Unix socket path (default: $REPO_ROOT/runtime/state/loopgate.sock)
#   LOOPGATE_REUSE_RUNNING  — if 1, keep an already-ready Loopgate on the socket (default: 0)
#   HAVEN_REUSE_RUNNING_LOOPGATE — alias for LOOPGATE_REUSE_RUNNING
#   LOOPGATE_BACKGROUND     — if 1, run Loopgate in the background and append stdout/stderr to runtime/logs/loopgate.log
#   LOOPGATE_LOG_PATH       — override log path when backgrounding
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${LOOPGATE_REPO_ROOT:-${MORPH_REPO_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}}"
REPO_ROOT="$(cd "$REPO_ROOT" && pwd)"

if [[ ! -f "$REPO_ROOT/config/runtime.yaml" ]]; then
  echo "error: expected Loopgate repo at REPO_ROOT=$REPO_ROOT (missing config/runtime.yaml)." >&2
  echo "  Fix: invoke scripts/start-loopgate-swift-dev.sh from your Loopgate checkout." >&2
  echo "  Or set LOOPGATE_REPO_ROOT to the Loopgate tree that contains config/runtime.yaml." >&2
  exit 1
fi

SOCKET_PATH="${LOOPGATE_SOCKET:-$REPO_ROOT/runtime/state/loopgate.sock}"
export LOOPGATE_SOCKET="$SOCKET_PATH"
LOOPGATE_LOG_DIR="$REPO_ROOT/runtime/logs"
LOOPGATE_LOG_PATH="${LOOPGATE_LOG_PATH:-$LOOPGATE_LOG_DIR/loopgate.log}"
REUSE_RUNNING="${LOOPGATE_REUSE_RUNNING:-${HAVEN_REUSE_RUNNING_LOOPGATE:-0}}"
STARTED_LOOPGATE=0
LOOPGATE_PID=""
ACCEPT_POLICY=0
BACKGROUND="${LOOPGATE_BACKGROUND:-0}"

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
  if [[ "$STARTED_LOOPGATE" == "1" && -n "${LOOPGATE_PID:-}" ]]; then
    kill "$LOOPGATE_PID" >/dev/null 2>&1 || true
    wait "$LOOPGATE_PID" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT INT TERM

for arg in "$@"; do
  case "$arg" in
    --accept-policy)
      ACCEPT_POLICY=1
      ;;
    --background)
      BACKGROUND=1
      ;;
    -h | --help)
      sed -n '1,20p' "$0"
      exit 0
      ;;
    *)
      echo "Unknown option: $arg (try --help)" >&2
      exit 1
      ;;
  esac
done

require_command go
require_command curl
require_command lsof

mkdir -p "$REPO_ROOT/runtime/state" "$LOOPGATE_LOG_DIR" "$(dirname "$LOOPGATE_LOG_PATH")"

echo "Verifying Loopgate compiles..."
(
  cd "$REPO_ROOT"
  go build -o /dev/null ./cmd/loopgate
)

if loopgate_ready; then
  if [[ "$REUSE_RUNNING" == "1" ]]; then
    echo "Loopgate is already running (reuse enabled). Socket: $SOCKET_PATH" >&2
    echo "GET /v1/health: OK"
    trap - EXIT INT TERM
    exit 0
  fi
  stop_existing_loopgate
fi

if [[ -e "$SOCKET_PATH" ]] && ! loopgate_ready; then
  echo "Removing stale socket at $SOCKET_PATH"
  rm -f "$SOCKET_PATH"
fi

if [[ "$BACKGROUND" == "1" ]]; then
  echo "Starting Loopgate in background (shell log: $LOOPGATE_LOG_PATH)..."
  (
    cd "$REPO_ROOT"
    export LOOPGATE_REPO_ROOT="$REPO_ROOT"
    export MORPH_REPO_ROOT="$REPO_ROOT"
    export LOOPGATE_SOCKET="$SOCKET_PATH"
    if [[ "$ACCEPT_POLICY" == "1" ]]; then
      go run ./cmd/loopgate --accept-policy
    else
      go run ./cmd/loopgate
    fi
  ) >>"$LOOPGATE_LOG_PATH" 2>&1 &
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

  echo "Loopgate PID $LOOPGATE_PID — socket $SOCKET_PATH"
  echo "Shell stdout/stderr (sparse): $LOOPGATE_LOG_PATH"
  echo "Structured slog (if logging.diagnostic.enabled in config/runtime.yaml): $LOOPGATE_LOG_DIR/server.log, socket.log, client.log, …"
  echo "Stop: kill $LOOPGATE_PID"
  trap - EXIT INT TERM
  exit 0
fi

echo "Starting Loopgate (foreground; Ctrl+C to stop)..."
echo "Socket: $SOCKET_PATH (set LOOPGATE_SOCKET in Swift to this path)"
echo "Structured slog: $LOOPGATE_LOG_DIR/{server,socket,client}.log when diagnostic enabled"
cd "$REPO_ROOT"
export LOOPGATE_REPO_ROOT="$REPO_ROOT"
export MORPH_REPO_ROOT="$REPO_ROOT"
export LOOPGATE_SOCKET="$SOCKET_PATH"
trap - EXIT INT TERM
if [[ "$ACCEPT_POLICY" == "1" ]]; then
  exec go run ./cmd/loopgate --accept-policy
else
  exec go run ./cmd/loopgate
fi
