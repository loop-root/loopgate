# Loopgate

**Last updated:** 2026-04-20

**Loopgate** is a local-first governance layer for AI-assisted engineering work.

The current product contract is here:
- [Loopgate V1 product contract](./docs/loopgate_v1_product_contract.md)

Current product scope:
- govern what your AI tools are allowed to do
- require approval for higher-risk actions
- record a durable local audit trail of what happened
- provide a local control plane for Claude Code hook governance
- verify local audit integrity with a hash-linked ledger plus default-on HMAC checkpoints on macOS

**Current MVP harness:** **Claude Code + project hooks + Loopgate**

This repository documents and ships a single product: **Loopgate**.

## Project status

- security-sensitive and experimental
- local-first
- governance-focused
- not yet a stable compatibility target

## What Loopgate does

Most AI harnesses give the model broad ambient authority and rely on prompt discipline to keep it safe.

Loopgate takes the opposite position:

- natural language is never authority
- model output is untrusted input
- policy decides what can run
- approvals are explicit
- audit is local, durable, and append-only

For the current local-first product, that means:

- Claude Code hooks can be governed before tool execution
- risky actions can be denied or approval-gated
- low-risk actions can be allowed with audit
- governed MCP execution can run through Loopgate’s own broker path when explicitly enabled
- local audit stays authoritative even if later export or aggregation changes
- setup should lead operators toward two intentional starter policies: `strict` and `balanced`

## Active product surface

What is active now:
- Loopgate server on a local Unix socket
- signed policy
- Claude Code hook governance
- approval and denial recording
- append-only local audit ledger
- request-driven governed MCP broker work
- local operator CLI flows

What is not the active product story right now:
- a new desktop UI surface
- a separate assistant product
- legacy worker/runtime experiments
- distributed enterprise deployment
- automatic memory or continuity inside Loopgate
- provider-backed OAuth/PKCE setup as part of the main v1 operator flow
- secret brokerage as a top-level product feature

## Quick start

Requirements:
- Go 1.25 or newer to build from source
- Python 3 on `PATH` for Claude hook scripts
- Claude Code for the active hook-based harness

Fastest source-checkout path:

```bash
make quickstart
```

`make quickstart` builds the local binaries and runs `./bin/loopgate quickstart`,
which applies the recommended defaults:
- starter policy profile: `balanced`
- Claude Code hook install into `~/.claude/`
- macOS LaunchAgent install and load so Loopgate keeps running in the background

If you want to choose options interactively instead, use the guided path:

```bash
make build
# optional: install the built binaries into ~/.local/bin
make install-local

./bin/loopgate setup
```

If you ran `make install-local`, replace `./bin/...` below with the bare command
names such as `loopgate` and `loopgate-policy-admin`.

`loopgate setup` is the guided first-run path. It:
- initializes or reuses your local policy-signing key
- lets you choose a starter policy profile: `strict` or `balanced`
- shows the setup plan before it mutates local state
- signs the selected policy
- checks for `python3` before Claude hook install
- installs Claude Code hooks
- can install and load a macOS LaunchAgent so Loopgate keeps running in the background

Starter profiles:
- `balanced` is the recommended daily-driver: Claude `Read`, `Glob`, `Grep`, `Edit`, and `MultiEdit` stay open inside the repo root, while `Write` and allowed Bash commands require approval.
- `strict` is the higher-sensitivity option: repo reads stay open, but all Claude file edits require approval and Bash stays disabled.

If you need the broader `developer` template, render and review it manually
with `./bin/loopgate-policy-admin render-template -preset developer`. That
template is kept as an experimental escape hatch, not as part of the supported
v1 setup path.

Important:
- Claude hook install is global to your local Claude Code config under `~/.claude/`
- until you remove those hooks, Claude Code will keep routing governed hook events through Loopgate

Fast smoke test after setup:
1. run `/hooks` inside Claude Code and confirm the 7 Loopgate hook entries are present
2. ask Claude Code to read `README.md`
3. run `./bin/loopgate-ledger tail -verbose`

Expected result:
- you should see a recent `hook.pre_validate` audit event for the Claude action you just triggered
- if the request needed approval or was denied, the tail output should make that obvious too

If you prefer the manual operator path, see [Setup](./docs/setup/SETUP.md).

On first start, Loopgate may ask macOS Keychain to create the default audit
HMAC checkpoint key. If Keychain access is denied or canceled, startup fails
closed and you should rerun from an interactive macOS login session.
For keychain-backed commands, prefer the stable `./bin/...` binaries over
`go run`. A fresh `go run` build changes the executable identity and can
trigger repeated macOS approval prompts.

Running `./bin/loopgate` in a terminal keeps it attached to that terminal.
For a more durable background path on macOS, install the LaunchAgent:

```bash
./bin/loopgate install-launch-agent -load
```

That LaunchAgent pins the current Loopgate executable path, so use the built
`./bin/loopgate` or an installed `loopgate` binary rather than `go run`.

If you prefer a simple shell-managed background run from the repo root:

```bash
mkdir -p runtime/logs runtime/state
nohup ./bin/loopgate > runtime/logs/loopgate.stdout.log 2> runtime/logs/loopgate.stderr.log < /dev/null &
echo $! > runtime/state/loopgate.pid
```

Stop that background process with:

```bash
kill "$(cat runtime/state/loopgate.pid)"
```

Default local socket:

```text
runtime/state/loopgate.sock
```

Loopgate uses a signed policy:

```bash
./bin/loopgate-policy-sign -verify-setup
./bin/loopgate-policy-admin validate
```

`-verify-setup` infers the current signed policy `key_id` by default. Pass
`-key-id` only when you intentionally want to verify or apply against a
different signer than the repo’s current `core/policy/policy.yaml.sig`.

If Loopgate is already running:

```bash
./bin/loopgate-policy-admin apply -verify-setup
```

## Operator flow

The current practical operator flow is:

1. start Loopgate locally
2. connect Claude Code hooks to the local socket
3. tune signed policy for your real low-risk vs approval-required actions
4. inspect local audit when something is denied, approved, or surprising
5. hot-apply policy changes without restarting Loopgate

Start here:
- [Getting started](./docs/setup/GETTING_STARTED.md)
- [Operator guide](./docs/setup/OPERATOR_GUIDE.md)
- [Setup](./docs/setup/SETUP.md)
- [Policy reference](./docs/setup/POLICY_REFERENCE.md)
- [Glossary](./docs/setup/GLOSSARY.md)
- [Doctor and ledger tools](./docs/setup/DOCTOR_AND_LEDGER.md)
- [HTTP API for local clients](./docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md)
- [Policy signing](./docs/setup/POLICY_SIGNING.md)
- [Ledger and audit integrity](./docs/setup/LEDGER_AND_AUDIT_INTEGRITY.md)
- [Threat model](./docs/loopgate-threat-model.md)
- [Release candidate checklist](./docs/roadmap/release_candidate_checklist.md)
- [Changelog](./CHANGELOG.md)
- [Support](./SUPPORT.md)
- [Security reporting](./SECURITY.md)

## Known limitations

Loopgate is publishable, but it is still an experimental local-first alpha.

Current realities to keep in mind:
- macOS-first, single-node operator flow is the active shipped scope
- Claude Code hooks and the governed MCP broker path are the practical attachment surface today
- the supported starter policy profiles for the guided path are intentionally narrow: `strict` and `balanced`
- provider-backed OAuth/PKCE connection flows still exist in-tree as experimental groundwork, but they are not part of the main v1 onboarding story
- local audit integrity is strong local-machine evidence, not remote notarization; see [Ledger and audit integrity](./docs/setup/LEDGER_AND_AUDIT_INTEGRITY.md) for the exact hash-chain and checkpoint limits
- internal package cleanup is in progress, so contributor ergonomics are improving but not yet boring

Current gap tracking lives here:
- [Active product gaps](./docs/roadmap/loopgate_v1_product_gaps.md)
- [Release candidate checklist](./docs/roadmap/release_candidate_checklist.md)

## Repository layout

```text
cmd/loopgate/              primary Loopgate server
claude/hooks/scripts/      tracked Claude hook bundle source copied by install-hooks
cmd/loopgate-policy-sign/  policy signing CLI
cmd/loopgate-policy-admin/ policy validate/diff/explain/apply CLI
cmd/loopgate-doctor/       operator diagnostics CLI
internal/loopgate/         Loopgate control plane and governed runtime
core/policy/               signed policy files
config/                    runtime configuration
docs/                      setup, operator docs, architecture, reports
runtime/                   local state and logs (fully gitignored)
```

## Related repositories

Loopgate’s memory and continuity work now lives in the separate sibling
repository named `continuity`, so this repo can stay focused on:

- policy
- approvals
- audit
- Claude hook governance
- sandbox mediation
- governed MCP broker flows

Historical design notes and older product planning that no longer describe the
current Loopgate product have been moved to the separate `ARCHIVED`
repository.

## Status

Experimental and under active hardening.

For current behavior, prefer the operator-facing docs in [docs/](./docs), the
running code, and the signed policy files under [core/policy/](./core/policy).
Historical material lives in the `ARCHIVED` and `continuity` sibling repos.

## License

Loopgate is licensed under the Apache License, Version 2.0. See
[LICENSE](./LICENSE) and [NOTICE](./NOTICE).

## Support

For setup questions, non-sensitive bug reports, and operator workflow issues,
see [SUPPORT.md](./SUPPORT.md). For vulnerability reports or trust-boundary
issues, use the private path described in [SECURITY.md](./SECURITY.md).
