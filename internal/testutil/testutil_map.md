# Testutil Package Map

This file maps `internal/testutil/`, shared helpers for tests that need signed
Loopgate policy fixtures.

Use it when changing:

- test policy signing setup
- signed policy fixture creation
- environment variables used by policy-signing tests

## Core Role

`testutil/` contains narrow test-only helpers. It exists to avoid duplicating
policy signing setup across packages while keeping production code free of test
shortcuts.

## Key Files

- `policy_signing.go`
  - `PolicyTestSigner`
  - ephemeral Ed25519 signing key generation
  - test environment configuration for trusted public keys
  - signed `core/policy/policy.yaml` fixture writer

## Relationship Notes

- Production policy parsing and signing lives in `internal/config/`.
- CLI policy tests live under `cmd/loopgate-policy-admin/`,
  `cmd/loopgate-policy-sign/`, and `cmd/loopgate/`.

## Important Watchouts

- Keep this package test-only. Do not import it from production code.
- Test signers are not operator trust anchors.
- If production signing behavior changes, update helpers and tests together so
  fixtures still exercise the real signature path.
