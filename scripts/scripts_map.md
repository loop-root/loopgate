# Scripts Map

This file maps `scripts/`, local shell helpers for release, install, and
verification workflows.

Use it when changing:

- release packaging
- installer behavior
- install smoke coverage
- vulnerability scanning
- policy-signing coverage checks

## Core Role

`scripts/` contains operational helpers that support the local-first product
but are not part of the Loopgate authority runtime.

The scripts exist to make release and installation behavior reproducible from a
checkout and in CI.

## Key Files

- `package_release.sh`
  - builds self-contained release archives
  - includes Loopgate binaries, starter signed policy files, and Claude hook
    scripts
  - writes checksums

- `install.sh`
  - installs published release archives without requiring a Go toolchain
  - supports local archive/checksum inputs for smoke tests
  - installs wrapper commands under the selected bin directory

- `install_smoke_test.sh`
  - builds a local archive
  - installs into a temporary home
  - runs setup/status/hook install and optional governed smoke test
  - verifies uninstall/purge behavior

- `policy_sign_coverage_check.sh`
  - enforces minimum coverage for policy signing code paths

- `govulncheck.sh`
  - runs the pinned Go vulnerability scanner

- `demo/`
  - demo-only helper scripts; do not treat as production authority paths

## Relationship Notes

- CI invokes many of these through `Makefile` targets.
- Release assets are written under `dist/`, which is generated output.
- Installer behavior is documented in `docs/setup/GETTING_STARTED.md` and
  `docs/setup/SETUP.md`.

## Important Watchouts

- Keep scripts deterministic and fail-fast (`set -euo pipefail`).
- Do not add secret-bearing output to logs or release artifacts.
- Installer changes should preserve checksum verification and managed install
  markers.
- Destructive cleanup must remain explicit and scoped to managed install paths
  or temporary smoke-test directories.
