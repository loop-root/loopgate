**Last updated:** 2026-04-24

# Loopgate Setup

This is the manual and advanced setup companion to
[Getting started](./GETTING_STARTED.md).

If you want an AI assistant to help you through setup interactively, start with
[Agent-assisted setup](./AGENT_ASSISTED_SETUP.md) and the copy-paste
[agent setup prompt](./agent_assisted_prompt.md).

If you want the shortest supported operator path on macOS, use:

```bash
curl -fsSL https://raw.githubusercontent.com/loop-root/loopgate/main/scripts/install.sh | sh

loopgate setup
loopgate status
loopgate test
```

That installs the latest published Loopgate release binaries under
`~/.local/share/loopgate/versions/<version>`, keeps operator state under
`~/.local/share/loopgate/state`, and installs wrapper commands under
`~/.local/bin`.

If you prefer to work from a source checkout instead, use:

```bash
make quickstart
```

That builds the binaries and runs `./bin/loopgate quickstart`, which applies
the recommended defaults on top of the same signed-policy setup path as
`loopgate setup`.

If you want the guided first-run flow instead, use:

```bash
make build
./bin/loopgate setup
```

Use this page when you need one of these instead:
- the published-install versus source-checkout tradeoffs
- the fully manual policy-signing path
- a manual background-run choice
- explicit hook install details
- status / smoke-test / uninstall details
- config and environment reference

## Prerequisites

- macOS for supported production use
- Python 3 on `PATH` for Claude hook scripts
- Claude Code for the active hook-based harness

Published release install:
- no Go toolchain required
- current published archives target macOS `arm64` and `amd64`

Source checkout:
- Go 1.25 or newer to build from source

Linux note:
- Linux remains source-first and experimental today; the published install script is only a supported path for macOS release archives right now

## Build from source

```bash
make build
# optional: copy the binaries into ~/.local/bin
make install-local
```

If you ran `make install-local`, replace `./bin/...` below with the bare
command names such as `loopgate` and `loopgate-policy-admin`.

If you are validating a contributor checkout rather than just operating
Loopgate locally, also run:

```bash
go test ./...
```

## Manual first-time setup

### 1. Initialize local policy signing

Installed-binary path:

```bash
loopgate init
loopgate-policy-admin validate
```

Source-checkout path:

```bash
./bin/loopgate init
./bin/loopgate-policy-admin validate
```

`loopgate init` creates a local Ed25519 signer for this operator, installs the
matching public key in the operator trust directory, and signs the checked-in
policy.

If you later re-sign intentionally with `loopgate-policy-sign`, reuse the
printed `key_id`.

### 2. Start Loopgate

Installed-binary foreground path:

```bash
loopgate
```

Source-checkout foreground path:

```bash
./bin/loopgate
```

Recommended macOS background path:

```bash
./bin/loopgate install-launch-agent -load
```

This LaunchAgent pins the current Loopgate executable path, so use the built
`./bin/loopgate` or an installed `loopgate` binary rather than `go run`.

Shell-managed fallback from the repo root:

```bash
mkdir -p runtime/logs runtime/state
nohup ./bin/loopgate > runtime/logs/loopgate.stdout.log 2> runtime/logs/loopgate.stderr.log < /dev/null &
echo $! > runtime/state/loopgate.pid
```

Stop that fallback process with:

```bash
kill "$(cat runtime/state/loopgate.pid)"
```

Default socket:

```text
runtime/state/loopgate.sock
```

For a published install, that resolves under the stable operator state root, for
example `~/.local/share/loopgate/state/runtime/state/loopgate.sock`.

On the first successful Loopgate start, the default Keychain-backed audit HMAC
checkpoint key is bootstrapped automatically for the shipped macOS-first
runtime config.

If Keychain access is denied or canceled, startup fails closed. Rerun from an
interactive macOS login session and allow the prompt rather than expecting an
insecure fallback.

For keychain-backed commands, prefer the stable `./bin/...` binaries over
`go run`; a fresh `go run` build changes the executable identity and can
trigger repeated macOS approval prompts.

### 3. Install Claude Code hooks

Installed-binary path:

```bash
loopgate install-hooks
```

Source-checkout path:

```bash
./bin/loopgate install-hooks
```

Important:
- this writes into your user-level Claude config under `~/.claude/`
- until you remove those hooks, Claude Code will keep routing governed hook
  events through Loopgate on this machine

Useful flags:

```bash
loopgate status
loopgate test
loopgate uninstall
loopgate uninstall --purge
./bin/loopgate status
./bin/loopgate test
./bin/loopgate install-hooks -repo /path/to/loopgate -claude-dir ~/.claude
./bin/loopgate remove-hooks
./bin/loopgate remove-launch-agent
./bin/loopgate uninstall
./bin/loopgate uninstall --purge
```

`loopgate status` is the quick operator summary. It reports the signed-policy
posture plus `operator_mode`, `daemon_mode`, `launch_agent_state`, Claude hook
state, LaunchAgent state on macOS, socket health, and optional live UI-safe
runtime details when you pass `-live`.

`loopgate test` is the governed smoke proof. It reports whether it reused a
running daemon or started a temporary one, confirms UI plus audit evidence, and
prints the next steps required before Claude Code can rely on the same path.

Quick validation:
- run `/hooks` inside Claude Code and confirm the Loopgate hook entries are present
- if your home directory has spaces, confirm the installed command paths remain quoted in Claude's hook view

Removal notes:
- `remove-hooks` removes Loopgate-managed hook entries but leaves the copied hook scripts in place
- `remove-launch-agent` unloads/removes the per-repo macOS LaunchAgent
- `uninstall` performs both steps and also removes the copied Loopgate hook scripts under `~/.claude/hooks/`; its output now includes a compact `offboarding_state`
- `uninstall --purge` additionally removes repo-scoped `runtime/` state, current signer material, and default installed binaries such as `~/.local/bin/loopgate`
- for a published install, `uninstall --purge` also removes the managed install root under `~/.local/share/loopgate/<version>`
- `make uninstall-local` only removes locally installed binaries such as `~/.local/bin/loopgate`
- tracked repo policy files such as `core/policy/policy.yaml` and `core/policy/policy.yaml.sig` remain in place either way, so deleting a source checkout is still a separate manual step

### 4. Re-sign and apply policy

Loopgate requires a valid detached signature for `core/policy/policy.yaml`.

Validate signer setup and sign:

```bash
./bin/loopgate-policy-sign -verify-setup
```

Validate policy:

```bash
./bin/loopgate-policy-admin validate
```

If Loopgate is already running, hot-apply the signed on-disk policy:

```bash
./bin/loopgate-policy-admin apply -verify-setup
```

## Configuration reference

- Runtime config: `config/runtime.yaml`
- Signed policy: `core/policy/policy.yaml` and `core/policy/policy.yaml.sig`

For a published install those paths live under
`~/.local/share/loopgate/state` so upgrades replace binaries without orphaning
audit history or local policy state.

Important current note:
- some compatibility-oriented names and future-facing fields still exist in the repo as cleanup debt
- those are not the current product center
- host-category tools currently reuse `tools.filesystem.*` policy enablement;
  there is not yet a separate `tools.host` policy block

## Environment

- `LOOPGATE_SOCKET` — override local socket path
- `LOOPGATE_REPO_ROOT` — override repo root detection for `cmd/loopgate`

## Read next

- [Getting started](./GETTING_STARTED.md)
- [Operator guide](./OPERATOR_GUIDE.md)
- [Policy reference](./POLICY_REFERENCE.md)
- [Glossary](./GLOSSARY.md)
- [Doctor and ledger tools](./DOCTOR_AND_LEDGER.md)
- [Policy signing](./POLICY_SIGNING.md)
- [Policy signing rotation](./POLICY_SIGNING_ROTATION.md)
- [Ledger and audit integrity](./LEDGER_AND_AUDIT_INTEGRITY.md)
- [Secrets](./SECRETS.md)
- [Threat model](../loopgate-threat-model.md)
- [Docs index](../README.md)
