#!/usr/bin/env bash
# Local vulnerability scan (stdlib module); run before releases. Requires network on first
# fetch of golang.org/x/vuln. Pin the tool version so CI results stay reproducible instead of
# drifting with @latest. See docs/reports/security-hardening-plan-2026-04.md.
set -euo pipefail
cd "$(dirname "$0")/.."
GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.2.0}"
exec go run "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}" ./...
