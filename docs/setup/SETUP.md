**Last updated:** 2026-04-13

# Loopgate Setup

This setup guide is intentionally short and Loopgate-only.

It reflects the current product boundary:
- local-first
- single-user / local operator
- Claude Code hooks as the active harness
- signed policy
- local audit

## Prerequisites

- Go (see `go.mod`)
- macOS for supported production use

Non-macOS hosts require:

```bash
LOOPGATE_ALLOW_NON_DARWIN=1
```

for development and CI only.

## Validate the checkout

```bash
go mod tidy
go test ./...
```

## Start Loopgate

```bash
go run ./cmd/loopgate
```

Default socket:

```text
runtime/state/loopgate.sock
```

## Sign and apply policy

Loopgate requires a valid detached signature for `core/policy/policy.yaml`.

Validate signer setup and sign:

```bash
go run ./cmd/loopgate-policy-sign -verify-setup
```

Validate policy:

```bash
go run ./cmd/loopgate-policy-admin validate
```

If Loopgate is already running, hot-apply the signed on-disk policy:

```bash
go run ./cmd/loopgate-policy-admin apply -verify-setup
```

## Connect Claude Code

The active harness is Claude Code project hooks.

Relevant files:
- `.claude/settings.json`
- `.claude/hooks/loopgate_pretool.py`
- `.claude/hooks/loopgate_posttool.py`

Design and behavior notes:
- [Claude Code hooks MVP](../design_overview/claude_code_hooks_mvp.md)

## Learn the local operator flow

Start here:
- [Operator guide](./OPERATOR_GUIDE.md)
- [Loopgate HTTP API (local clients)](./LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md)

## Configuration

- Runtime config: `config/runtime.yaml`
- Signed policy: `core/policy/policy.yaml` and `core/policy/policy.yaml.sig`

Important current note:
- older runtime and policy fields that mention Haven, morphlings, tenancy, or future enterprise behavior may still exist in the repo as compatibility or cleanup debt
- those are not the current product center

## Current operator commands

```bash
go run ./cmd/loopgate-policy-admin help
go run ./cmd/loopgate-doctor trust-check
```

`trust-check` currently exists because audit export hardening work landed before the repo cleanup pass. It is not the center of the local-first product story.

## Further reading

- [Operator guide](./OPERATOR_GUIDE.md)
- [Policy signing](./POLICY_SIGNING.md)
- [Policy signing rotation](./POLICY_SIGNING_ROTATION.md)
- [Ledger and audit integrity](./LEDGER_AND_AUDIT_INTEGRITY.md)
- [Secrets](./SECRETS.md)
- [Threat model](../loopgate-threat-model.md)
- [Docs index](../README.md)
- [Loopgate cleanup plan](../roadmap/loopgate_cleanup_plan.md)

## Environment

- `LOOPGATE_SOCKET` — override local socket path
- `LOOPGATE_ALLOW_NON_DARWIN=1` — allow development on non-macOS
- `MORPH_REPO_ROOT` — legacy compatibility env var; avoid for new operator setup
