**Last updated:** 2026-04-16

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

## Validate the checkout

```bash
go mod tidy
go test ./...
```

## Initialize local policy signing

For the default first-time local setup:

```bash
go run ./cmd/loopgate init
go run ./cmd/loopgate-policy-admin validate
```

`loopgate init` creates a local Ed25519 signer for this operator, installs the
matching public key in the operator trust directory, and signs the checked-in
policy.

The checked-in `core/policy/policy.yaml` is an intentionally strict starter
policy. For field-by-field semantics and template guidance, see
[Policy reference](./POLICY_REFERENCE.md).

If you later re-sign intentionally with `loopgate-policy-sign`, reuse the
printed `key_id`.

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
If Keychain access is denied or canceled, startup fails closed. Rerun from an
interactive macOS login session and allow the prompt rather than expecting an
insecure fallback.

## Re-sign and apply policy

Loopgate requires a valid detached signature for `core/policy/policy.yaml`.
If you intentionally use your own signer instead of `loopgate init`, install its
public key into the operator trust directory first. See
[Policy signing](./POLICY_SIGNING.md).
`loopgate-policy-sign -verify-setup` and `loopgate-policy-admin apply
-verify-setup` infer the repo’s current signed-policy `key_id` by default.
Pass `-key-id` only when you intentionally want to verify or apply against a
different signer than the current `core/policy/policy.yaml.sig`.

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
- [Policy reference](./POLICY_REFERENCE.md)
- [Doctor and ledger tools](./DOCTOR_AND_LEDGER.md)
- [Loopgate HTTP API (local clients)](./LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md)

## Configuration

- Runtime config: `config/runtime.yaml`
- Signed policy: `core/policy/policy.yaml` and `core/policy/policy.yaml.sig`

Important current note:
- some compatibility-oriented names and future-facing fields still exist in the repo as cleanup debt
- those are not the current product center
- host-category tools currently reuse `tools.filesystem.*` policy enablement;
  there is not yet a separate `tools.host` policy block

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
- [Policy reference](./POLICY_REFERENCE.md)
- [Policy signing rotation](./POLICY_SIGNING_ROTATION.md)
- [Ledger and audit integrity](./LEDGER_AND_AUDIT_INTEGRITY.md)
- [Secrets](./SECRETS.md)
- [Threat model](../loopgate-threat-model.md)
- [Docs index](../README.md)
- [Review closure status](../reports/reviews/2026-04-16/review_status.md)

## Environment

- `LOOPGATE_SOCKET` — override local socket path
- `LOOPGATE_REPO_ROOT` — override repo root detection for `cmd/loopgate`
