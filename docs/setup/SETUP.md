**Last updated:** 2026-04-19

# Loopgate Setup

This is the manual and advanced setup companion to
[Getting started](./GETTING_STARTED.md).

If you want the shortest supported operator path, use:

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
- the fully manual policy-signing path
- a manual background-run choice
- explicit hook install details
- config and environment reference

## Prerequisites

- Go 1.25 or newer to build from source
- Python 3 on `PATH` for Claude hook scripts
- Claude Code for the active hook-based harness
- macOS for supported production use

## Build local binaries

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

Foreground:

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

```bash
./bin/loopgate install-hooks
```

Important:
- this writes into your user-level Claude config under `~/.claude/`
- until you remove those hooks, Claude Code will keep routing governed hook
  events through Loopgate on this machine

Useful flags:

```bash
./bin/loopgate install-hooks -repo /path/to/loopgate -claude-dir ~/.claude
./bin/loopgate remove-hooks
./bin/loopgate remove-launch-agent
./bin/loopgate uninstall
```

Quick validation:
- run `/hooks` inside Claude Code and confirm the Loopgate hook entries are present
- if your home directory has spaces, confirm the installed command paths remain quoted in Claude's hook view

Removal notes:
- `remove-hooks` removes Loopgate-managed hook entries but leaves the copied hook scripts in place
- `remove-launch-agent` unloads/removes the per-repo macOS LaunchAgent
- `uninstall` performs both steps and also removes the copied Loopgate hook scripts under `~/.claude/hooks/`
- `make uninstall-local` only removes locally installed binaries such as `~/.local/bin/loopgate`

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
