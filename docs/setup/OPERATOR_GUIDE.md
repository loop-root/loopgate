**Last updated:** 2026-04-20

# Loopgate Operator Guide

This guide is for the current product:

**Loopgate** as a local-first governance layer for AI-assisted engineering work.

If you are setting up Loopgate for the first time, start with:
- [Getting started](./GETTING_STARTED.md)
- [Setup](./SETUP.md)

For most operators, the recommended first command is:

```bash
curl -fsSL https://raw.githubusercontent.com/loop-root/loopgate/main/scripts/install.sh | sh
```

Then run:

```bash
loopgate setup
```

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

The guided setup path is intentionally opinionated:
- `balanced` is the recommended daily-driver for normal local engineering work
- `strict` is the higher-sensitivity option for repos that should stay read-first until you widen policy deliberately
- `read-only` is the lowest-friction evaluation option when you want governed reads without any write path

The command examples below show both paths:
- published-install operators can use the bare command names such as `loopgate`
- source-checkout operators can use `./bin/loopgate`

## Basic workflow

### 1. Run guided setup

Fastest macOS path without Go:

```bash
curl -fsSL https://raw.githubusercontent.com/loop-root/loopgate/main/scripts/install.sh | sh
loopgate setup
```

That installs the latest published Loopgate release under
`~/.local/share/loopgate/<version>`, installs wrapper commands under
`~/.local/bin`, and then runs the guided setup flow from the installed
binaries.

Fastest source-checkout path:

```bash
make quickstart
```

That builds the local binaries and runs `./bin/loopgate quickstart`, which uses
the same signed-policy setup path as `loopgate setup` but accepts the
recommended defaults.

If you want to choose the profile or skip hooks / LaunchAgent installation, use
the guided path instead:

Installed-binary path:

```bash
loopgate setup
```

Source-checkout path:

```bash
make build
./bin/loopgate setup
```

`loopgate setup` is the fastest supported path for operators. It:
- initializes or reuses the local signer
- lets you choose `balanced`, `strict`, or `read-only`
- shows the setup plan before applying it
- signs the selected policy
- checks for `python3` before Claude hook install
- installs Claude Code hooks
- can install and load a macOS LaunchAgent
- prints a deterministic operator summary with `operator_mode`, policy, signer, socket, audit ledger, derived `readiness_state`, a `next_steps:` block, and next commands

Profile intent:
- `balanced`
  - allows Claude `Read`, `Glob`, `Grep`, `Edit`, and `MultiEdit` inside the repo root
  - keeps Claude `Write` and allowed Bash commands behind approval
  - keeps HTTP disabled
- `strict`
  - allows repo reads and search
  - keeps all Claude file edits behind approval
  - keeps Bash and HTTP disabled
- `read-only`
  - allows Claude `Read`, `Glob`, and `Grep` inside the repo root
  - keeps Claude writes and edits disabled
  - keeps Bash and HTTP disabled

If you need the broader `developer` template, render it manually with
`./bin/loopgate-policy-admin render-template -preset developer` and review it
before signing. It remains an experimental escape hatch, not part of the main
v1 setup flow.

Important:
- hook install writes into your user-level Claude config under `~/.claude/`
- until you remove those hooks, Claude Code will keep routing governed hook events through Loopgate on this machine

If you later re-sign policy intentionally through the manual path, reuse the
`key_id` printed by `loopgate init`.

### 2. Start Loopgate

Installed-binary path:

```bash
loopgate
```

Source-checkout path:

```bash
./bin/loopgate
```

If setup installed and loaded the LaunchAgent, Loopgate should already be
running in the background.

Quick operator checks:

```bash
loopgate status
loopgate test
```

Source-checkout equivalents:

```bash
./bin/loopgate status
./bin/loopgate test
```

`loopgate test` reports whether it reused a running daemon or had to start a
temporary one for the smoke test, so the proof output matches the actual
operator posture before you rely on Claude Code.

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
process. Closing the terminal usually stops Loopgate.

Recommended macOS path:

```bash
./bin/loopgate install-launch-agent -load
```

This writes a per-repo LaunchAgent plist under `~/Library/LaunchAgents/`,
points it at the current Loopgate binary, and starts it with launchd.
Use the built `./bin/loopgate` or an installed `loopgate` binary rather than
`go run`, because the LaunchAgent pins that executable path directly.

Simple shell-managed fallback from the repo root:

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

Installed-binary path:

```bash
loopgate install-hooks
```

Source-checkout path:

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

### Remove Loopgate from this machine

If you want to stop using Loopgate on this machine or repo:

Installed-binary path:

```bash
loopgate uninstall
```

Source-checkout equivalent:

```bash
./bin/loopgate uninstall
```

That command removes Loopgate-managed Claude hook entries, removes the copied
Loopgate hook scripts from `~/.claude/hooks/`, and unloads/removes the per-repo
macOS LaunchAgent when present. It deliberately leaves runtime/audit state and
tracked repo content in place so evidence removal is always explicit. The
command output includes a compact `offboarding_state` summary.

If you also want to remove repo-scoped runtime state, current signer material,
and default installed binaries, use:

Installed-binary path:

```bash
loopgate uninstall --purge
```

Source-checkout equivalent:

```bash
./bin/loopgate uninstall --purge
```

Lower-level offboarding commands:

```bash
./bin/loopgate remove-hooks
./bin/loopgate remove-launch-agent
make uninstall-local
```

`make uninstall-local` only removes binaries copied into your local install
directory. It does not remove policy files or runtime/audit state.

For source checkouts, `uninstall --purge` still leaves tracked files such as
`core/policy/policy.yaml` and `core/policy/policy.yaml.sig`, so deleting the
repo itself is still a separate manual step. For published installs,
`uninstall --purge` also removes the managed install root.

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

If you need the operator-facing explanation for one approval id from the
verified ledger, run:

```bash
./bin/loopgate-doctor explain-denial -approval-id <approval-id>
```

This prints the current approval status plus the denial code, operator reason,
and short related-event timeline when the request was actually denied.

For a direct capability denial or execution failure keyed by `request_id`, use:

```bash
./bin/loopgate-doctor explain-denial -request-id <request-id>
```

This prints the current request status plus the denial code or execution-failure
class and the related request timeline from the verified ledger.

For a Claude hook block that never became an approval or capability request, use:

```bash
./bin/loopgate-doctor explain-denial -hook-session-id <session-id> -tool-use-id <tool-use-id>
```

If you only know the hook session id, you can omit `-tool-use-id`; doctor will
select the latest blocked hook event recorded for that session.

### Add or revoke a permanent operator grant

Permanent operator grants are signed operator policy records. They can only narrow
within the root policy ceiling. If the root policy says
`max_delegation: session` or `none`, the CLI refuses to write a permanent
grant. The signed policy still serializes this ceiling as
`max_delegation: persistent`; operator-facing CLI output calls that scope
`permanent`.

Preview a grant before writing it:

```bash
./bin/loopgate-policy-admin grants add repo_edit_safe -path docs -dry-run
```

Write and hot-apply a permanent path-scoped grant:

```bash
./bin/loopgate-policy-admin grants add repo_edit_safe -path docs
```

Supported path-scoped classes are `repo_read_search`, `repo_edit_safe`,
`repo_write_safe`, and `repo_bash_safe`. List active grants with:

```bash
./bin/loopgate-policy-admin grants list
```

Revoke by grant id:

```bash
./bin/loopgate-policy-admin grants revoke <grant-id>
```

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
- [Glossary](./GLOSSARY.md)

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
- [Active product gaps](../roadmap/loopgate_v1_product_gaps.md)
