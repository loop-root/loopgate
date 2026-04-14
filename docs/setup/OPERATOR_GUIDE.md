**Last updated:** 2026-04-13

# Loopgate Operator Guide

This guide is for the current product:

**Loopgate** as a local-first governance layer for AI-assisted engineering work.

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

### 2. Make sure Claude hooks point at the repo

The current hook harness lives under:
- `.claude/settings.json`
- `.claude/hooks/`

If Claude says a hook script is missing, check the hook command path first before disabling anything.

### 3. Validate and sign policy

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

### Check MCP broker state

The governed MCP path is still request-driven and intentionally narrow.

Useful current surfaces:
- `GET /v1/mcp-gateway/inventory`
- `GET /v1/mcp-gateway/server/status`

These help answer:
- which servers/tools are declared
- which server is currently launched
- whether a launched server was pruned after process death

## Troubleshooting

### Claude says a hook script is missing

Likely causes:
- broken path in `.claude/settings.json`
- hook file exists but the command expands to the wrong directory

Check:
- the script file exists under `.claude/hooks/`
- the hook command resolves relative to the repo when `CLAUDE_PROJECT_DIR` is missing

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

- The repo still contains legacy and forward-looking code and docs.
- Multi-tenant/admin-node material exists in tree but is not the current product story.
- Some operator/troubleshooting flows are still documented better in architecture docs than they should be.
- Ledger replacement / wholesale state rollback hardening is not fully solved yet.

Tracked cleanup:
- [Loopgate cleanup plan](../roadmap/loopgate_cleanup_plan.md)
