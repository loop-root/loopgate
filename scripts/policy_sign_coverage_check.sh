#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

coverage_file="$(mktemp "${TMPDIR:-/tmp}/loopgate-policy-sign-coverage.XXXXXX")"
cleanup() {
  rm -f "$coverage_file"
}
trap cleanup EXIT

go test -coverprofile="$coverage_file" ./cmd/loopgate-policy-sign ./internal/config
go tool cover -func="$coverage_file" | awk '
  /^total:/ {
    if ($3 + 0 < 60) {
      print "policy-sign coverage below 60%: " $3
      exit 1
    }
    print "policy-sign coverage OK: " $3
  }
'
