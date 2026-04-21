#!/usr/bin/env bash
set -euo pipefail

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

require_contains() {
  local haystack="$1"
  local needle="$2"
  local description="$3"
  if [[ "$haystack" != *"$needle"* ]]; then
    die "expected ${description} to contain ${needle}"
  fi
}

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${VERSION:-vtest-local}"
TARGET_OS="${GOOS:-$(go env GOOS)}"
TARGET_ARCH="${GOARCH:-$(go env GOARCH)}"
RUN_LOOPGATE_TEST="${LOOPGATE_INSTALL_SMOKE_RUN_TEST:-0}"

TMPDIR="$(mktemp -d)"
BASE_DIR="/tmp/lg-smoke-$$"
ORIGINAL_HOME="${HOME}"
ORIGINAL_PATH="${PATH}"
cleanup() {
  rm -rf "$BASE_DIR"
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

DIST_DIR="$TMPDIR/dist"
WORK_DIR="$BASE_DIR/work"
HOME_DIR="$BASE_DIR/home"
CLAUDE_DIR="$HOME_DIR/.claude"
mkdir -p "$DIST_DIR" "$WORK_DIR" "$CLAUDE_DIR"

"$ROOT_DIR/scripts/package_release.sh" -version "$VERSION" -dist-dir "$DIST_DIR"

ARCHIVE_PATH="$DIST_DIR/loopgate_${VERSION}_${TARGET_OS}_${TARGET_ARCH}.tar.gz"
CHECKSUMS_PATH="$DIST_DIR/loopgate_${VERSION}_checksums.txt"
INSTALL_DIR="$HOME_DIR/.local/share/loopgate/$VERSION"
BIN_DIR="$HOME_DIR/.local/bin"

[[ -f "$ARCHIVE_PATH" ]] || die "expected release archive at $ARCHIVE_PATH"
[[ -f "$CHECKSUMS_PATH" ]] || die "expected checksums file at $CHECKSUMS_PATH"

export HOME="$HOME_DIR"
export PATH="$BIN_DIR:$ORIGINAL_PATH"

"$ROOT_DIR/scripts/install.sh" \
  --version "$VERSION" \
  --archive-file "$ARCHIVE_PATH" \
  --checksums-file "$CHECKSUMS_PATH"

[[ -f "$INSTALL_DIR/.loopgate-install-root" ]] || die "missing managed install marker at $INSTALL_DIR/.loopgate-install-root"

cd "$WORK_DIR"

version_output="$(loopgate version)"
require_contains "$version_output" "$VERSION" "loopgate version output"

setup_output="$(loopgate setup -yes -profile balanced -skip-hooks -skip-launch-agent)"
require_contains "$setup_output" "setup OK" "setup output"
require_contains "$setup_output" "profile: balanced" "setup output"

status_output="$(loopgate status)"
require_contains "$status_output" "status:" "status output"
require_contains "$status_output" "policy_profile: balanced" "status output"
require_contains "$status_output" "signer_verified: true" "status output"

loopgate install-hooks -claude-dir "$CLAUDE_DIR"
[[ -f "$CLAUDE_DIR/hooks/loopgate_pretool.py" ]] || die "expected installed hook script in $CLAUDE_DIR/hooks"
require_contains "$(cat "$CLAUDE_DIR/settings.json")" "loopgate_pretool.py" "Claude settings after install-hooks"

loopgate remove-hooks -claude-dir "$CLAUDE_DIR"
if [[ -f "$CLAUDE_DIR/settings.json" ]] && grep -q 'loopgate_pretool.py' "$CLAUDE_DIR/settings.json"; then
  die "expected remove-hooks to remove Loopgate hook entries from $CLAUDE_DIR/settings.json"
fi

loopgate install-hooks -claude-dir "$CLAUDE_DIR"
[[ -f "$CLAUDE_DIR/hooks/loopgate_pretool.py" ]] || die "expected reinstall to restore hook script"

if [[ "$RUN_LOOPGATE_TEST" == "1" ]]; then
  loopgate test
fi

uninstall_output="$(loopgate uninstall --purge -claude-dir "$CLAUDE_DIR")"
require_contains "$uninstall_output" "uninstall OK" "uninstall output"
require_contains "$uninstall_output" "removed_managed_install_root: true" "uninstall output"

[[ ! -d "$INSTALL_DIR" ]] || die "expected managed install root to be removed"
[[ ! -e "$BIN_DIR/loopgate" ]] || die "expected installed loopgate wrapper to be removed"
[[ ! -e "$CLAUDE_DIR/hooks/loopgate_pretool.py" ]] || die "expected uninstall --purge to remove Loopgate hook script"
if [[ -f "$CLAUDE_DIR/settings.json" ]] && grep -q 'loopgate_pretool.py' "$CLAUDE_DIR/settings.json"; then
  die "expected uninstall --purge to remove Loopgate hook entries from $CLAUDE_DIR/settings.json"
fi

printf 'install smoke OK\n'
printf 'version: %s\n' "$VERSION"
printf 'install_root_removed: true\n'
printf 'home_dir: %s\n' "$HOME_DIR"
printf 'loopgate_test_ran: %s\n' "$RUN_LOOPGATE_TEST"

export HOME="$ORIGINAL_HOME"
export PATH="$ORIGINAL_PATH"
