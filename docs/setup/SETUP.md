**Last updated:** 2026-04-14

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

On the first successful Loopgate start, the default Keychain-backed audit HMAC
checkpoint key is bootstrapped automatically for the shipped macOS-first
runtime config.

## Sign and apply policy

Loopgate requires a valid detached signature for `core/policy/policy.yaml`.
If you are using your own signer, install its public key into the operator trust directory first. See [Policy signing](./POLICY_SIGNING.md).
If you use a custom `key_id`, pass it to both `loopgate-policy-sign -verify-setup` and `loopgate-policy-admin apply -verify-setup`.

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

Install the Loopgate hook bundle into Claude's config directory:

```bash
go run ./cmd/loopgate install-hooks
```

Useful flags:

```bash
go run ./cmd/loopgate install-hooks -repo /path/to/loopgate -claude-dir ~/.claude
go run ./cmd/loopgate remove-hooks
```

This command:
- creates `~/.claude/hooks/` if needed
- copies the Loopgate Python hook scripts there
- updates `~/.claude/settings.json`
- wires the 7 supported hook events without duplicating entries on rerun

Relevant files after install:
- `~/.claude/settings.json`
- `~/.claude/hooks/loopgate_pretool.py`
- `~/.claude/hooks/loopgate_posttool.py`
- `~/.claude/hooks/loopgate_posttoolfailure.py`
- `~/.claude/hooks/loopgate_sessionstart.py`
- `~/.claude/hooks/loopgate_sessionend.py`
- `~/.claude/hooks/loopgate_userpromptsubmit.py`
- `~/.claude/hooks/loopgate_permissionrequest.py`

Design and behavior notes:
- [Claude Code hooks MVP](../design_overview/claude_code_hooks_mvp.md)

## Learn the local operator flow

Start here:
- [Getting started](./GETTING_STARTED.md)
- [Operator guide](./OPERATOR_GUIDE.md)
- [Doctor and ledger tools](./DOCTOR_AND_LEDGER.md)
- [Loopgate HTTP API (local clients)](./LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md)

## Configuration

- Runtime config: `config/runtime.yaml`
- Signed policy: `core/policy/policy.yaml` and `core/policy/policy.yaml.sig`

Important current note:
- some compatibility-oriented names and future-facing fields still exist in the repo as cleanup debt
- those are not the current product center

## Current operator commands

```bash
go run ./cmd/loopgate install-hooks
go run ./cmd/loopgate remove-hooks
go run ./cmd/loopgate-policy-admin help
go run ./cmd/loopgate-doctor trust-check
```

`trust-check` currently exists because audit export hardening work landed before the repo cleanup pass. It is not the center of the local-first product story.

## Further reading

- [Getting started](./GETTING_STARTED.md)
- [Operator guide](./OPERATOR_GUIDE.md)
- [Doctor and ledger tools](./DOCTOR_AND_LEDGER.md)
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
