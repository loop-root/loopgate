# Loopgate

**Last updated:** 2026-04-20

**Loopgate** is a local-first authority boundary for AI-assisted engineering work.

It puts **signed policy**, **explicit approvals**, and an **append-only local
audit ledger** between an AI harness and the tools it can invoke.

Current harness focus: **Claude Code + project hooks + Loopgate**

## Status

Loopgate is:
- experimental
- security-sensitive
- local-first
- governance-focused

Loopgate is **not** yet:
- a stable compatibility target
- a packaged desktop product
- a browser-based admin UI
- a multi-harness platform

## Who it is for

Loopgate is for engineers and security-minded operators who want:
- deterministic allow / approve / deny behavior for AI tool use
- less prompt-based babysitting and less approval rubber-stamping
- a durable local record of what the agent actually did
- a real authority boundary instead of chat text pretending to be policy

## What you can do today

The current product scope is intentionally narrow:
- govern Claude Code hooks before tool execution
- require approval for higher-risk actions
- allow low-risk actions with audit
- keep policy signed and local
- inspect a durable local audit ledger
- use a repo-local operator CLI for setup, status, smoke testing, and uninstall

The guided first-run path leads operators toward three starter profiles:
- `balanced`
- `strict`
- `read-only`

The current product contract is here:
- [Loopgate V1 product contract](./docs/loopgate_v1_product_contract.md)

## Quick start

There are now two practical ways to try Loopgate:
- published macOS release install without Go
- source checkout plus `make quickstart`

The published install path currently targets macOS release archives.
Linux remains source-first and experimental for now.

Fastest path without a Go toolchain:

```bash
curl -fsSL https://raw.githubusercontent.com/loop-root/loopgate/main/scripts/install.sh | sh

loopgate setup
loopgate status
loopgate test
```

The installer downloads the latest published release archive for your macOS
architecture, installs a self-contained Loopgate root under
`~/.local/share/loopgate/<version>`, and installs wrapper commands under
`~/.local/bin`.

If you want to pin a specific release candidate:

```bash
curl -fsSL https://raw.githubusercontent.com/loop-root/loopgate/main/scripts/install.sh | sh -s -- --version v0.2.0-rc2
```

Release packaging and install logic lives in:
- `scripts/package_release.sh`
- `scripts/install.sh`

Requirements:
- Go 1.25 or newer to build from source
- Python 3 on `PATH` for Claude hook scripts
- Claude Code for the active hook-based harness

Fastest path from a source checkout:

```bash
make quickstart
```

`make quickstart` builds the local binaries and runs `./bin/loopgate quickstart`,
which applies the recommended defaults:
- starter policy profile: `balanced`
- Claude Code hook install into `~/.claude/`
- macOS LaunchAgent install and load so Loopgate keeps running in the background

Then verify the local operator flow:

```bash
./bin/loopgate status
./bin/loopgate test
```

If you want to choose options interactively instead, use the guided setup path:

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
- lets you choose a starter policy profile: `balanced`, `strict`, or `read-only`
- shows the setup plan before it mutates local state
- signs the selected policy
- checks for `python3` before Claude hook install
- installs Claude Code hooks
- can install and load a macOS LaunchAgent so Loopgate keeps running in the background
- ends with a deterministic operator summary including the selected profile, signer `key_id`, policy paths, socket path, audit ledger path, and next commands

Starter profiles:
- `balanced` is the recommended daily-driver: Claude `Read`, `Glob`, `Grep`, `Edit`, and `MultiEdit` stay open inside the repo root, while `Write` and allowed Bash commands require approval.
- `strict` is the higher-sensitivity option: repo reads stay open, but all Claude file edits require approval and Bash stays disabled.
- `read-only` is the lowest-friction evaluation profile: Claude `Read`, `Glob`, and `Grep` stay open inside the repo root, while Claude writes and edits, Bash, and web access stay disabled.

If you need the broader `developer` template, render and review it manually
with `./bin/loopgate-policy-admin render-template -preset developer`. That
template is kept as an experimental escape hatch, not as part of the supported
v1 setup path.

Important:
- Claude hook install is global to your local Claude Code config under `~/.claude/`
- until you remove those hooks, Claude Code will keep routing governed hook events through Loopgate

Fast smoke test after setup:
1. run `./bin/loopgate status`
2. run `./bin/loopgate test`
3. if you are using Claude Code, run `/hooks` inside Claude Code and confirm the 7 Loopgate hook entries are present
4. ask Claude Code to read `README.md`
5. run `./bin/loopgate-ledger tail -verbose`

Expected result:
- `loopgate status` should show the signed policy, signer, `operator_mode`, `daemon_mode`, `launch_agent_state`, Claude hook state, socket path, and daemon health
- `loopgate test` should print a governed `fs_list` proof plus the matching `request_id` and audit ledger path
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

If you later want to remove Loopgate's machine-level wiring again:

```bash
./bin/loopgate uninstall
./bin/loopgate uninstall --purge
```

`loopgate uninstall` removes Loopgate-managed Claude hook entries, removes the
copied Loopgate hook scripts from `~/.claude/hooks/`, and unloads/removes the
per-repo macOS LaunchAgent when present. It deliberately leaves the local
binaries, signed policy files, and runtime/audit state in place so removal of
evidence or operator data is always explicit. The command now points you at the
right next offboarding step for your mode, including `loopgate uninstall
--purge` or `./bin/loopgate uninstall --purge`.

`loopgate uninstall --purge` is the stronger local offboarding path. It also
removes repo-scoped `runtime/` state, default installed Loopgate binaries under
`~/.local/bin` when present, and the local signer material tied to the current
policy `key_id`. It still does not delete tracked repo files such as
`core/policy/policy.yaml` or `core/policy/policy.yaml.sig`. For a source
checkout, deleting the repo itself remains an explicit manual step. For a
published install, the managed install root is removed, but external signer
trust material may still remain if it was not owned by that install.

Useful lower-level removal commands:

```bash
./bin/loopgate remove-hooks
./bin/loopgate remove-launch-agent
make uninstall-local
```

Use `make uninstall-local` only if you previously copied the binaries into your
local install directory such as `~/.local/bin`.

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
- the supported starter policy profiles for the guided path are intentionally narrow: `balanced`, `strict`, and `read-only`
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

## License

Loopgate is licensed under the Apache License, Version 2.0. See
[LICENSE](./LICENSE) and [NOTICE](./NOTICE).

## Support

For setup questions, non-sensitive bug reports, and operator workflow issues,
see [SUPPORT.md](./SUPPORT.md). For vulnerability reports or trust-boundary
issues, use the private path described in [SECURITY.md](./SECURITY.md).
