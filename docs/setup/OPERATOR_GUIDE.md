**Last updated:** 2026-04-14

# Loopgate Operator Guide

This guide is for the current product:

**Loopgate** as a local-first governance layer for AI-assisted engineering work.

If you are setting up Loopgate for the first time, start with:
- [Getting started](./GETTING_STARTED.md)
- [Setup](./SETUP.md)

## What an operator does

In the current product, the operator:
- runs Loopgate locally
- connects Claude Code hooks to it
- tunes signed policy
- reviews denials and approvals
- inspects local audit when behavior is surprising

Loopgate is not a chat UI. It is the local authority layer.

## Basic workflow

### 1. Start Loopgate

```bash
go run ./cmd/loopgate
```

Expected local socket:

```text
runtime/state/loopgate.sock
```

### 2. Install Claude hooks

```bash
go run ./cmd/loopgate install-hooks
```

Optional:

```bash
go run ./cmd/loopgate install-hooks -repo /path/to/loopgate -claude-dir ~/.claude
```

This updates:
- `~/.claude/settings.json`
- `~/.claude/hooks/`

If this repo also has local Claude settings under `./.claude/`, `remove-hooks`
will sweep those repo-local Loopgate entries too.

If Claude says a hook script is missing, reinstall first before disabling anything.

### 3. Validate and sign policy

If you use a custom signer `key_id`, pass it to both `-verify-setup` commands below.

```bash
go run ./cmd/loopgate-policy-admin validate
go run ./cmd/loopgate-policy-sign -verify-setup
```

### 4. Hot-apply policy

```bash
go run ./cmd/loopgate-policy-admin apply -verify-setup
```

### 5. Exercise the harness

Run a normal Claude Code task and watch for:
- low-risk reads that should be allow + audit
- writes or shell actions that should require approval
- hard denials that indicate policy or path issues

## Common operator tasks

### Reduce approval friction

Tune signed policy so common low-risk actions are allowed with audit.

Typical examples:
- `Read`
- `Glob`
- `Grep`
- narrow safe shell prefixes

Do not reduce friction by broadly widening shell or write authority.

### Apply a policy change safely

Use this flow:

```bash
go run ./cmd/loopgate-policy-admin validate
go run ./cmd/loopgate-policy-sign -verify-setup
go run ./cmd/loopgate-policy-admin apply -verify-setup
```

With a custom signer:

```bash
go run ./cmd/loopgate-policy-admin validate
go run ./cmd/loopgate-policy-sign -key-id "$KEY_ID" -verify-setup
go run ./cmd/loopgate-policy-admin apply -key-id "$KEY_ID" -verify-setup
```

### Check MCP broker state

The governed MCP path is still request-driven and intentionally narrow.

Useful current surfaces:
- `GET /v1/mcp-gateway/inventory`
- `GET /v1/mcp-gateway/server/status`

These help answer:
- which servers/tools are declared
- which server is currently launched
- whether a launched server was pruned after process death

### Verify the local audit ledger

Use the built-in verifier when you want to trust the local audit history after
an incident, a demo reset, or suspicious local behavior:

```bash
go run ./cmd/loopgate-ledger verify
go run ./cmd/loopgate-doctor report
```

`loopgate-ledger verify` checks the append-only chain across the active audit
file and any sealed segments. If audit HMAC checkpoints are configured, it also
verifies those checkpoints with the configured secret backend.

If you are unsure whether to use `loopgate-ledger` or `loopgate-doctor`, see:
- [Doctor and ledger tools](./DOCTOR_AND_LEDGER.md)

## Troubleshooting

### Claude says a hook script is missing

Likely causes:
- broken path in `~/.claude/settings.json`
- hook files were never installed or were overwritten
- hook file exists but the command points at the wrong Claude config directory

Check:
- rerun `go run ./cmd/loopgate install-hooks`
- the script file exists under `~/.claude/hooks/`
- the hook command points at the same `~/.claude/hooks/` directory you expect

### Remove Loopgate hooks temporarily

If you need to disable the Loopgate harness without deleting the scripts:

```bash
go run ./cmd/loopgate remove-hooks
```

This removes only the Loopgate-managed hook entries from the Claude settings
files Loopgate manages and leaves the copied Python scripts in place.

### Policy changes do not seem to take effect

Check:
- `core/policy/policy.yaml.sig` matches the current policy
- `loopgate-policy-admin apply -verify-setup` succeeded
- the running Loopgate instance is the one tied to the socket you expect

### Claude gets too many approvals

That usually means policy is still too conservative for common safe actions.

Use real usage to tune:
- allow + audit for read-heavy flows
- approval for mutation and higher-risk shell/network actions
- hard deny for things that should never run

### Claude is denied outside the repo

That usually means `allowed_roots` in policy are too narrow.

For read-only external content, widen only the read-tool roots:
- `Read`
- `Glob`
- `Grep`

Do not widen write roots unless you actually want that authority.

## Current known limitations

- Local hash chains and HMAC checkpoints improve tamper evidence, but they are
  still local-machine evidence rather than remote notarization.
- The audit export path exists, but the product is still centered on local
  authoritative audit rather than remote aggregation.
- There is no stable compatibility promise yet for external clients beyond the
  current documented local-first operator surface.

Tracked cleanup:
- [Loopgate cleanup plan](../roadmap/loopgate_cleanup_plan.md)
