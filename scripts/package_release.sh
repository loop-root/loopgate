#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  package_release.sh [-version VERSION] [-os GOOS] [-arch GOARCH] [-dist-dir DIR]

Build a self-contained Loopgate release archive containing:
  - Loopgate binaries
  - starter signed policy files
  - Claude hook scripts

The output archive name is:
  loopgate_<version>_<os>_<arch>.tar.gz

The output checksum file name is:
  loopgate_<version>_checksums.txt
EOF
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

sha256_file() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  die "no sha256 tool found (expected shasum or sha256sum)"
}

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${VERSION:-$(git -C "$ROOT_DIR" describe --tags --always --dirty 2>/dev/null || echo dev)}"
TARGET_OS="${GOOS:-$(go env GOOS)}"
TARGET_ARCH="${GOARCH:-$(go env GOARCH)}"
DIST_DIR="$ROOT_DIR/dist"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -version|--version)
      VERSION="$2"
      shift 2
      ;;
    -os|--os)
      TARGET_OS="$2"
      shift 2
      ;;
    -arch|--arch)
      TARGET_ARCH="$2"
      shift 2
      ;;
    -dist-dir|--dist-dir)
      DIST_DIR="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

COMMIT="$(git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-X main.buildVersion=${VERSION} -X main.buildCommit=${COMMIT} -X main.buildDate=${BUILD_DATE}"
ARCHIVE_BASENAME="loopgate_${VERSION}_${TARGET_OS}_${TARGET_ARCH}"
CHECKSUMS_FILE="$DIST_DIR/loopgate_${VERSION}_checksums.txt"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT
STAGE_DIR="$TMPDIR/$ARCHIVE_BASENAME"

mkdir -p "$STAGE_DIR/bin" "$STAGE_DIR/claude/hooks/scripts" "$STAGE_DIR/core/policy" "$DIST_DIR"

build_binary() {
  local output_path="$1"
  local package_path="$2"
  GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" \
    go build -ldflags "$LDFLAGS" -o "$output_path" "$package_path"
}

build_binary "$STAGE_DIR/bin/loopgate" ./cmd/loopgate
build_binary "$STAGE_DIR/bin/loopgate-doctor" ./cmd/loopgate-doctor
build_binary "$STAGE_DIR/bin/loopgate-ledger" ./cmd/loopgate-ledger
build_binary "$STAGE_DIR/bin/loopgate-policy-admin" ./cmd/loopgate-policy-admin
build_binary "$STAGE_DIR/bin/loopgate-policy-sign" ./cmd/loopgate-policy-sign

cp -R "$ROOT_DIR/claude/hooks/scripts/." "$STAGE_DIR/claude/hooks/scripts/"
cp "$ROOT_DIR/core/policy/policy.yaml" "$STAGE_DIR/core/policy/policy.yaml"
cp "$ROOT_DIR/core/policy/policy.yaml.sig" "$STAGE_DIR/core/policy/policy.yaml.sig"

ARCHIVE_PATH="$DIST_DIR/${ARCHIVE_BASENAME}.tar.gz"
tar -C "$TMPDIR" -czf "$ARCHIVE_PATH" "$ARCHIVE_BASENAME"

ARCHIVE_HASH="$(sha256_file "$ARCHIVE_PATH")"
TMP_CHECKSUMS="$(mktemp)"
trap 'rm -rf "$TMPDIR" "$TMP_CHECKSUMS"' EXIT

if [[ -f "$CHECKSUMS_FILE" ]]; then
  grep -v "  ${ARCHIVE_BASENAME}\.tar\.gz$" "$CHECKSUMS_FILE" > "$TMP_CHECKSUMS" || true
fi
printf '%s  %s\n' "$ARCHIVE_HASH" "${ARCHIVE_BASENAME}.tar.gz" >> "$TMP_CHECKSUMS"
sort "$TMP_CHECKSUMS" > "$CHECKSUMS_FILE"

printf 'release archive OK\n'
printf 'archive: %s\n' "$ARCHIVE_PATH"
printf 'checksums: %s\n' "$CHECKSUMS_FILE"
