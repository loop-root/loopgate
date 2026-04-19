**Last updated:** 2026-04-16

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

The checked-in starter policy is intentionally strict. Use
[Policy reference](./POLICY_REFERENCE.md) and
`./bin/loopgate-policy-admin render-template` when you want to review or
customize the policy surface before signing and applying it.

The command examples below use the built binaries under `./bin/`. If you ran
`make install-local`, use the bare command names instead.

## Basic workflow

### 1. Initialize local policy signing

For a first-time local setup:

```bash
make build
./bin/loopgate init
./bin/loopgate-policy-admin validate
```

If you later re-sign policy intentionally, reuse the `key_id` printed by
`loopgate init`.

### 2. Start Loopgate

```bash
./bin/loopgate
```

Expected local socket:

```text
runtime/state/loopgate.sock
```

Current startup summary:

- version
- socket path
- signed policy path and `key_id`
- audit integrity posture

For keychain-backed operator flows, prefer the stable `./bin/...` binaries over
`go run`; a fresh `go run` build changes the executable identity and can cause
repeated macOS approval prompts.

### Run Loopgate in background

If you start `./bin/loopgate` in a terminal, that shell owns the foreground
process. Closing the terminal usually stops Loopgate. From the repo root, a
simple background launch looks like:

```bash
mkdir -p runtime/logs runtime/state
nohup ./bin/loopgate > runtime/logs/loopgate.stdout.log 2> runtime/logs/loopgate.stderr.log < /dev/null &
echo $! > runtime/state/loopgate.pid
```

Stop that background process with:

```bash
kill "$(cat runtime/state/loopgate.pid)"
```

### 3. Install Claude hooks

```bash
./bin/loopgate install-hooks
```

Optional:

```bash
./bin/loopgate install-hooks -repo /path/to/loopgate -claude-dir ~/.claude
```

This updates:
- `~/.claude/settings.json`
- `~/.claude/hooks/`
- source scripts come from the tracked repo bundle at `claude/hooks/scripts/`

If this repo also has local Claude settings under `./.claude/`, `remove-hooks`
will sweep those repo-local Loopgate entries too.

If Claude says a hook script is missing, reinstall first before disabling anything.
After install, run `/hooks` in Claude Code and confirm the Loopgate entries point
at `~/.claude/hooks/loopgate_*.py`.
If Loopgate is unreachable, the installed command hooks exit with Claude Code's
blocking hook status. That means governed events such as `PreToolUse`,
`PermissionRequest`, and `UserPromptSubmit` fail closed instead of silently
bypassing Loopgate, while audit-only lifecycle hooks surface a visible error.
Expected outcomes:
- `PreToolUse`: Claude blocks the tool invocation and shows the Loopgate hook error instead of running the tool.
- `PermissionRequest`: Claude denies the permission request and shows the Loopgate hook error instead of granting the permission.
- `UserPromptSubmit`: Claude blocks prompt submission and shows the Loopgate hook error instead of continuing with governed execution.

### 4. Re-sign policy when you intentionally change it

The `-verify-setup` commands below infer the repo’s current signed-policy
`key_id` by default. Pass `-key-id` only when you intentionally want to verify
or apply against a different signer than the current `core/policy/policy.yaml.sig`.

```bash
./bin/loopgate-policy-admin validate
./bin/loopgate-policy-sign -verify-setup
```

### 5. Hot-apply policy

```bash
./bin/loopgate-policy-admin apply -verify-setup
```

### 6. Exercise the harness

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

Current policy model note:
- `host.folder.*` and `host.plan.apply` currently reuse `tools.filesystem.*`
  enablement flags.
- there is not yet a separate `tools.host` policy block, so host reads/writes
  are not independently toggled from filesystem reads/writes

### When Loopgate asks for approval

List pending approvals from the local control plane:

```bash
./bin/loopgate-policy-admin approvals list
```

Approve a pending request:

```bash
./bin/loopgate-policy-admin approvals approve <approval-id> -reason "reviewed and allowed"
```

Deny a pending request:

```bash
./bin/loopgate-policy-admin approvals deny <approval-id> -reason "outside allowed change window"
```

The approve and deny commands print the resulting audit event hash so you can
correlate the human decision with `loopgate-ledger verify` or a later audit
review.

### Apply a policy change safely

Use this flow:

```bash
./bin/loopgate-policy-admin validate
./bin/loopgate-policy-sign -verify-setup
./bin/loopgate-policy-admin apply -verify-setup
```

With a different signer than the repo’s current signed policy:

```bash
./bin/loopgate-policy-admin validate
./bin/loopgate-policy-sign -key-id "$KEY_ID" -verify-setup
./bin/loopgate-policy-admin apply -key-id "$KEY_ID" -verify-setup
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
./bin/loopgate-ledger verify
./bin/loopgate-doctor report
```

`loopgate-ledger verify` checks the append-only chain across the active audit
file and any sealed segments. In the shipped macOS-first config, audit HMAC
checkpoints are enabled by default, so verify also checks those checkpoints
with the configured Keychain-backed secret.

If you are unsure whether to use `loopgate-ledger` or `loopgate-doctor`, see:
- [Doctor and ledger tools](./DOCTOR_AND_LEDGER.md)

### Know your audit integrity mode

Loopgate has two audit integrity modes. Startup prints which one is active:

```
Audit integrity: hash-chain only (HMAC checkpoints disabled)
```
or
```
Audit integrity: hash-chain + HMAC checkpoints (every N events)
```

**Hash-chain only:** each event commits a SHA-256 digest of its
predecessor. Ordering changes and corruption are detectable on read. A
same-user attacker who controls the log files can replace the entire file
with a new internally-consistent chain that passes verification.

**Hash-chain + HMAC checkpoints:** additionally binds cumulative chain state
to an out-of-band secret stored outside the log. Replacing the file requires
forging a keyed MAC — detectable by anyone who holds the key.

To check mode without restarting:

```bash
./bin/loopgate-doctor report | grep -A6 '"hmac_checkpoints"'
```

Look for `"enabled": true` and `"status": "verified"` to confirm HMAC mode
is active and the key is loading correctly. On a fresh repo before the first
Loopgate start, doctor may report `"status": "bootstrap_pending"`; that means
the default Keychain-backed checkpoint key has not been created yet.

The shipped runtime config already enables HMAC checkpoints with the default
macOS Keychain ref:

```yaml
logging:
  audit_ledger:
    hmac_checkpoint:
      enabled: true
      interval_events: 256
      secret_ref:
        id: audit_ledger_hmac
        backend: macos_keychain
        account_name: loopgate.audit_ledger_hmac
        scope: local
```

On the first successful Loopgate start, that default key is bootstrapped into
Keychain automatically if it does not already exist.
If Keychain access is denied or canceled, Loopgate startup fails closed. Rerun
from an interactive macOS login session and approve the prompt.

See [Ledger and audit integrity](./LEDGER_AND_AUDIT_INTEGRITY.md) for the
full explanation of what each mode proves and does not prove.

## Troubleshooting

### Claude says a hook script is missing

Likely causes:
- broken path in `~/.claude/settings.json`
- hook files were never installed or were overwritten
- hook file exists but the command points at the wrong Claude config directory

Check:
- rerun `./bin/loopgate install-hooks`
- the script file exists under `~/.claude/hooks/`
- the hook command points at the same `~/.claude/hooks/` directory you expect

### Remove Loopgate hooks temporarily

If you need to disable the Loopgate harness without deleting the scripts:

```bash
./bin/loopgate remove-hooks
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

Tracked status:
- [Review closure status](../reports/reviews/2026-04-16/review_status.md)
