---
name: loopgate-operator
description: Use when running or explaining the main loopgate CLI for setup, server start, status, smoke tests, Claude hook install/removal, macOS LaunchAgent lifecycle, console, explain, or uninstall workflows.
---

# Loopgate Operator CLI

Use the main `loopgate` command for first-run setup, starting the local
authority boundary, checking readiness, and managing local hook/background
lifecycle.

## Guardrails

- Loopgate is the authority boundary. Claude, an IDE, or an assisting agent is
  not the authority source.
- Prefer `loopgate setup` for guided first-run setup and `loopgate quickstart`
  only when the operator explicitly wants recommended defaults.
- Prefer stable installed binaries or `./bin/loopgate`; do not use `go run` for
  operator flows that may touch Keychain-backed material.
- Hook install writes into user-level Claude config. Explain that this affects
  Claude Code behavior on the machine until hooks are removed.
- `uninstall --purge` is destructive. Do not run it unless the operator
  explicitly asks to remove local runtime/signing/install state.
- Startup flags such as `--accept-policy` are intentionally unsupported.

## Command choice

First-run and setup:

- Guided setup: `loopgate setup`
- Source quick path: `make quickstart`
- Non-interactive recommended defaults: `./bin/loopgate quickstart`
- Manual signer init: `./bin/loopgate init`

Run and inspect:

- Start foreground server: `./bin/loopgate`
- Quick readiness summary: `./bin/loopgate status`
- Live readiness summary: `./bin/loopgate status -live`
- Governed smoke test: `./bin/loopgate test`
- Local console snapshot: `./bin/loopgate console -once`
- Explain a policy decision without Claude: `./bin/loopgate explain`

Lifecycle:

- Install Claude hooks: `./bin/loopgate install-hooks`
- Remove Claude hooks: `./bin/loopgate remove-hooks`
- Install/load macOS LaunchAgent:
  `./bin/loopgate install-launch-agent -load`
- Remove macOS LaunchAgent: `./bin/loopgate remove-launch-agent`
- Offboard hooks/background wiring: `./bin/loopgate uninstall`
- Destructive local purge: `./bin/loopgate uninstall --purge`

## Recommended workflow

1. Use `loopgate status` or `loopgate-doctor setup-check --json` to inspect the
   current state.
2. If setup is missing, run `loopgate setup` and follow the printed
   `next_steps`.
3. Start or load the daemon, then run `loopgate status` and `loopgate test`.
4. If Claude hooks are missing, run `loopgate install-hooks`.
5. After lifecycle changes, rerun `loopgate status` and `loopgate test`.
6. For audit trust, use `loopgate-ledger verify`.

## Interpreting results

- `setup OK` means local setup artifacts were created according to selected
  options; it does not mean every later runtime check is healthy.
- `status: ok` means the operator summary sees the expected ready state.
- `loopgate test` may start a temporary daemon when none is running; read its
  `daemon_source` and next steps before saying the background setup is done.
- `Loopgate started` means the foreground server is running until interrupted or
  the owning terminal exits.
- `offboarding_state` in uninstall output explains what was removed and what
  remains.

## Failure posture

If startup or setup fails because signing, Keychain, audit integrity, hook
install, or socket checks fail, keep the failure closed. Do not suggest policy
bypass, plaintext secret fallback, audit disablement, or manual hook edits as
the shortcut.
