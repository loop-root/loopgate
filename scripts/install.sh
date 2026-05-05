#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  install.sh [-version VERSION] [-repo OWNER/REPO] [-bin-dir DIR] [-install-root DIR]
             [--archive-file PATH --checksums-file PATH]

Install a published Loopgate release archive without requiring a Go toolchain.

By default this script:
  - detects the current OS and CPU architecture
  - resolves the latest GitHub release tag
  - downloads the matching release archive plus checksums
  - installs versioned binaries under ~/.local/share/loopgate/versions/<version>
  - keeps operator state under ~/.local/share/loopgate/state
  - installs wrapper commands under ~/.local/bin/
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

detect_os() {
  case "$(uname -s)" in
    Darwin) printf 'darwin\n' ;;
    Linux) printf 'linux\n' ;;
    *) die "unsupported operating system: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64\n' ;;
    arm64|aarch64) printf 'arm64\n' ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac
}

latest_release_tag() {
  local repo="$1"
  local effective_url
  effective_url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${repo}/releases/latest")"
  local tag="${effective_url##*/}"
  [[ -n "$tag" ]] || die "could not resolve latest release tag for ${repo}"
  printf '%s\n' "$tag"
}

download_to() {
  local url="$1"
  local output_path="$2"
  curl -fsSL "$url" -o "$output_path"
}

verify_archive_checksum() {
  local archive_path="$1"
  local checksums_path="$2"
  local archive_name
  archive_name="$(basename "$archive_path")"
  local expected_hash
  expected_hash="$(awk -v target="$archive_name" '$2 == target { print $1 }' "$checksums_path")"
  [[ -n "$expected_hash" ]] || die "no checksum entry found for ${archive_name}"
  local actual_hash
  actual_hash="$(sha256_file "$archive_path")"
  [[ "$expected_hash" == "$actual_hash" ]] || die "checksum mismatch for ${archive_name}"
}

write_wrapper() {
  local wrapper_path="$1"
  local tool_name="$2"
  local install_dir="$3"
  local state_root="$4"
  cat > "$wrapper_path" <<EOF
#!/bin/sh
set -eu
LOOPGATE_INSTALL_ROOT="$install_dir"
LOOPGATE_STATE_ROOT="$state_root"
export LOOPGATE_REPO_ROOT="\$LOOPGATE_STATE_ROOT"
exec "\$LOOPGATE_INSTALL_ROOT/bin/$tool_name" "\$@"
EOF
  chmod 755 "$wrapper_path"
}

write_install_marker() {
  local marker_dir="$1"
  local version="$2"
  local repo="$3"
  local os_name="$4"
  local arch_name="$5"
  local install_root="$6"
  local install_dir="$7"
  local state_root="$8"
  mkdir -p "$marker_dir"
  cat > "$marker_dir/.loopgate-install-root" <<EOF
layout_version=2
version=$version
repo=$repo
os=$os_name
arch=$arch_name
install_root=$install_root
binary_root=$install_dir
state_root=$state_root
installed_at_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF
}

assert_managed_path_safe() {
  local path="$1"
  local description="$2"
  [[ -n "$path" ]] || die "$description must not be empty"
  [[ "$path" != "/" ]] || die "$description must not be /"
}

find_legacy_repo_root() {
  local install_root="$1"
  local newest=""
  local candidate
  for candidate in "$install_root"/*; do
    [[ -d "$candidate" ]] || continue
    case "$(basename "$candidate")" in
      state|versions) continue ;;
    esac
    [[ -f "$candidate/.loopgate-install-root" ]] || continue
    if [[ -z "$newest" || "$candidate/.loopgate-install-root" -nt "$newest/.loopgate-install-root" ]]; then
      newest="$candidate"
    fi
  done
  printf '%s\n' "$newest"
}

migrate_legacy_state_if_needed() {
  local legacy_root="$1"
  local state_root="$2"
  [[ -n "$legacy_root" ]] || return 0
  [[ -d "$legacy_root" ]] || return 0

  mkdir -p "$state_root"
  if [[ ! -d "$state_root/runtime" && -d "$legacy_root/runtime" ]]; then
    cp -pR "$legacy_root/runtime" "$state_root/runtime"
  fi
  if [[ ! -d "$state_root/core" && -d "$legacy_root/core" ]]; then
    cp -pR "$legacy_root/core" "$state_root/core"
  fi
  if [[ ! -d "$state_root/config" && -d "$legacy_root/config" ]]; then
    cp -pR "$legacy_root/config" "$state_root/config"
  fi
}

seed_state_root() {
  local payload_dir="$1"
  local state_root="$2"
  local policy_path="$state_root/core/policy/policy.yaml"
  local signature_path="$state_root/core/policy/policy.yaml.sig"
  local hooks_dir="$state_root/claude/hooks/scripts"

  mkdir -p "$state_root"
  chmod 700 "$state_root"

  if [[ ! -e "$policy_path" && ! -e "$signature_path" ]]; then
    mkdir -p "$state_root/core/policy"
    cp "$payload_dir/core/policy/policy.yaml" "$policy_path"
    cp "$payload_dir/core/policy/policy.yaml.sig" "$signature_path"
  fi

  rm -rf "$hooks_dir"
  mkdir -p "$hooks_dir"
  cp -R "$payload_dir/claude/hooks/scripts/." "$hooks_dir/"
}

REPO="loop-root/loopgate"
BIN_DIR="${HOME}/.local/bin"
INSTALL_ROOT="${HOME}/.local/share/loopgate"
VERSION=""
ARCHIVE_FILE=""
CHECKSUMS_FILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -version|--version)
      VERSION="$2"
      shift 2
      ;;
    -repo|--repo)
      REPO="$2"
      shift 2
      ;;
    -bin-dir|--bin-dir)
      BIN_DIR="$2"
      shift 2
      ;;
    -install-root|--install-root)
      INSTALL_ROOT="$2"
      shift 2
      ;;
    --archive-file)
      ARCHIVE_FILE="$2"
      shift 2
      ;;
    --checksums-file)
      CHECKSUMS_FILE="$2"
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

TARGET_OS="$(detect_os)"
TARGET_ARCH="$(detect_arch)"
if [[ "$TARGET_OS" == "linux" ]]; then
  if [[ -z "$ARCHIVE_FILE" ]]; then
    die "published Linux release archives are not available yet; use a source checkout or pass --archive-file/--checksums-file for a local experimental build"
  fi
  printf 'warning: Loopgate is macOS-first today; Linux install remains experimental.\n' >&2
fi

if [[ -z "$VERSION" ]]; then
  VERSION="$(latest_release_tag "$REPO")"
fi

ARCHIVE_BASENAME="loopgate_${VERSION}_${TARGET_OS}_${TARGET_ARCH}"
ARCHIVE_NAME="${ARCHIVE_BASENAME}.tar.gz"
CHECKSUM_NAME="loopgate_${VERSION}_checksums.txt"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT
ARCHIVE_PATH="$TMPDIR/$ARCHIVE_NAME"
CHECKSUMS_PATH="$TMPDIR/$CHECKSUM_NAME"

if [[ -n "$ARCHIVE_FILE" ]]; then
  cp "$ARCHIVE_FILE" "$ARCHIVE_PATH"
else
  download_to "https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE_NAME}" "$ARCHIVE_PATH"
fi

if [[ -n "$CHECKSUMS_FILE" ]]; then
  cp "$CHECKSUMS_FILE" "$CHECKSUMS_PATH"
else
  download_to "https://github.com/${REPO}/releases/download/${VERSION}/${CHECKSUM_NAME}" "$CHECKSUMS_PATH"
fi

verify_archive_checksum "$ARCHIVE_PATH" "$CHECKSUMS_PATH"

EXTRACT_DIR="$TMPDIR/extracted"
mkdir -p "$EXTRACT_DIR"
tar -C "$EXTRACT_DIR" -xzf "$ARCHIVE_PATH"

PAYLOAD_DIR="$EXTRACT_DIR/$ARCHIVE_BASENAME"
[[ -d "$PAYLOAD_DIR" ]] || die "release archive did not contain expected root directory ${ARCHIVE_BASENAME}"

VERSIONS_ROOT="${INSTALL_ROOT}/versions"
INSTALL_DIR="${VERSIONS_ROOT}/${VERSION}"
STATE_ROOT="${INSTALL_ROOT}/state"
assert_managed_path_safe "$INSTALL_ROOT" "install root"
assert_managed_path_safe "$INSTALL_DIR" "versioned install root"
assert_managed_path_safe "$STATE_ROOT" "state root"
rm -rf "$INSTALL_DIR"
mkdir -p "$VERSIONS_ROOT" "$BIN_DIR"
LEGACY_REPO_ROOT="$(find_legacy_repo_root "$INSTALL_ROOT")"
mv "$PAYLOAD_DIR" "$INSTALL_DIR"
migrate_legacy_state_if_needed "$LEGACY_REPO_ROOT" "$STATE_ROOT"
seed_state_root "$INSTALL_DIR" "$STATE_ROOT"
write_install_marker "$INSTALL_DIR" "$VERSION" "$REPO" "$TARGET_OS" "$TARGET_ARCH" "$INSTALL_ROOT" "$INSTALL_DIR" "$STATE_ROOT"
write_install_marker "$STATE_ROOT" "$VERSION" "$REPO" "$TARGET_OS" "$TARGET_ARCH" "$INSTALL_ROOT" "$INSTALL_DIR" "$STATE_ROOT"

for tool_name in loopgate loopgate-doctor loopgate-ledger loopgate-policy-admin loopgate-policy-sign; do
  write_wrapper "$BIN_DIR/$tool_name" "$tool_name" "$INSTALL_DIR" "$STATE_ROOT"
done

printf 'install OK\n'
printf 'version: %s\n' "$VERSION"
printf 'install_root: %s\n' "$INSTALL_ROOT"
printf 'binary_root: %s\n' "$INSTALL_DIR"
printf 'state_root: %s\n' "$STATE_ROOT"
if [[ -n "$LEGACY_REPO_ROOT" ]]; then
  printf 'migrated_legacy_root: %s\n' "$LEGACY_REPO_ROOT"
fi
printf 'bin_dir: %s\n' "$BIN_DIR"
printf 'next_steps:\n'
printf '  - ensure %s is on PATH\n' "$BIN_DIR"
printf '  - run loopgate setup\n'
printf '  - run loopgate status\n'
printf '  - run loopgate test\n'
