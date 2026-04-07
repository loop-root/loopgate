#!/usr/bin/env bash
# Local vulnerability scan (stdlib module); run before releases. Requires network on first
# fetch of golang.org/x/vuln. See docs/reports/security-hardening-plan-2026-04.md.
set -euo pipefail
cd "$(dirname "$0")/.."
exec go run golang.org/x/vuln/cmd/govulncheck@latest ./...
