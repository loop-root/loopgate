#!/usr/bin/env bash
# Build Haven with an embedded production frontend (no signing).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}/cmd/haven"
echo "==> npm ci + build (frontend)"
npm --prefix frontend ci
npm --prefix frontend run build
echo "==> wails build"
exec wails build -clean
