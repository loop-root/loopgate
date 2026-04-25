# Loopgate Context Map

This file is a fast orientation guide for contributors and agents working in
the active Loopgate repository.

If this file disagrees with code, the code wins. If it disagrees with
`AGENTS.md`, the security and authority rules in `AGENTS.md` win.

## Project in one paragraph

Loopgate is a local-first governance layer for AI-assisted engineering work.
This repository is intentionally narrow: policy, approvals, audit, sandbox
mediation, Claude Code hook governance, and governed MCP broker flows.

Continuity and memory work belongs in the separate `continuity` repo. Older
planning and historical product material belongs in `ARCHIVED`.

## Current product shape

- local HTTP control plane on a Unix domain socket
- signed policy and request integrity
- approval and denial workflows
- append-only local audit ledger
- Claude Code hook governance
- request-driven governed MCP broker execution

This is not the place to add:

- in-tree continuity or memory features
- retired legacy assistant or UI surfaces
- speculative desktop UI product work
- remote or enterprise deployment assumptions in the local control path

## Read this first

If you are new to the repo, read in this order:

1. `README.md`
2. `AGENTS.md`
3. `docs/design_overview/architecture.md`
4. `docs/design_overview/loopgate.md`
5. `docs/setup/OPERATOR_GUIDE.md`
6. `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`

## Top-level layout

```text
/path/to/loopgate
├── cmd/             Executable entrypoints
├── config/          Checked-in runtime configuration
├── core/            Checked-in policy and signing material
├── docs/            Active product, setup, ADR, RFC, and threat-model docs
├── internal/        Control-plane and support packages
├── scripts/         Local helper scripts
├── README.md        Public top-level project summary
├── SECURITY.md      Vulnerability reporting policy
├── SUPPORT.md       Normal support and bug-report routing
└── CONTRIBUTING.md  Contributor expectations
```

Treat `runtime/`, `tmp/`, `output/`, `.claude/`, and other local state paths as
machine-local artifacts, not source of truth.

## Code map

### `cmd/`

- `cmd/loopgate/` — main Loopgate server and hook-management subcommands
- `cmd/loopgate-ledger/` — local ledger inspection, verify, tail, and demo reset
- `cmd/loopgate-policy-sign/` — detached policy signing
- `cmd/loopgate-policy-admin/` — validate, diff, explain, and apply signed policy
- `cmd/loopgate-doctor/` — local diagnostics and report helpers
- `cmd/cmd_map.md` — executable entrypoint map

### `internal/loopgate/`

The authority core:

- session open and signed request validation
- policy/approval enforcement
- capability execution mediation
- sandbox import/export and host-path mediation
- governed MCP launch/execute/stop/status
- authoritative audit persistence hooks

If a change touches trust, approvals, request integrity, or capability
execution, start here.

Important subpackage maps:

- `internal/loopgate/loopgate_map.md` — authority runtime package map
- `internal/loopgate/approval/approval_map.md` — pure approval lifecycle rules
- `internal/loopgate/controlapi/controlapi_map.md` — local wire contracts
- `internal/loopgate/protocol/protocol_map.md` — canonical request envelopes

### `internal/ledger/` and `internal/audit/`

Append-only persistence and inspection helpers.

If a change affects:

- audit integrity
- ordering
- tamper evidence
- readable operator audit output

read these packages first.

### `internal/policy/`, `core/policy/`, and `config/`

Policy/runtime configuration loading and checked-in defaults.

- `core/policy/policy.yaml` is the checked-in signed policy source
- `config/runtime.yaml` is checked-in runtime config
- policy changes must preserve deny-by-default behavior and signed-policy rules

### `internal/sandbox/` and filesystem mediation paths in `internal/loopgate/`

Use these when working on:

- host-folder access
- sandbox import/export
- canonical path checks
- symlink and traversal safety

### `internal/secrets/`

Secret storage, lookup, and redaction. Changes here must preserve the project’s
secret-handling invariants.

### `internal/tools/`

This package defines:

- typed capability schemas and registry behavior
- filesystem, host-folder, and shell tool implementations
- the current governed execution surface used by Loopgate

### Operational support maps

- `claude/claude_map.md` — checked-in Claude Code hook bundle
- `scripts/scripts_map.md` — release, install, and verification scripts
- `.github/workflows/workflows_map.md` — CI workflow map
- `internal/troubleshoot/troubleshoot_map.md` — doctor/report/bundle helpers
- `internal/loopdiag/loopdiag_map.md` — non-authoritative diagnostic logs
- `internal/testutil/testutil_map.md` — test-only signed-policy fixtures

## Docs map

Use active docs in `docs/` as the public-facing source of truth:

- `docs/README.md` — entry points
- `docs/design_overview/architecture.md` — current architecture
- `docs/design_overview/loopgate.md` — product and control-plane overview
- `docs/setup/OPERATOR_GUIDE.md` — operator workflow
- `docs/setup/LEDGER_AND_AUDIT_INTEGRITY.md` — audit and ledger model
- `docs/loopgate-threat-model.md` — current threat model

Historical material should stay out of this repo’s active story.

## Current invariants to preserve

- Loopgate is the authority boundary.
- Natural language is never authority.
- Model output is untrusted input.
- Security-relevant actions stay auditable.
- Policy remains signed and authoritative.
- Sandbox and host filesystem boundaries remain explicit.
- Secrets do not leak into logs, audit, or checked-in files.
- Operator-facing views are derived, not authoritative state.

## If you are changing X, start here

- approvals or execution flow:
  `internal/loopgate/`
- audit or ledger behavior:
  `internal/ledger/`, `internal/audit/`, `cmd/loopgate-ledger/`
- policy semantics:
  `core/policy/`, `internal/policy/`, `cmd/loopgate-policy-admin/`
- hook setup or operator flow:
  `cmd/loopgate/`, `docs/setup/`
- sandbox or filesystem mediation:
  `internal/sandbox/`, `internal/loopgate/`

## Current direction

Keep this repository narrow, publishable, and trustworthy:

- policy
- approvals
- audit
- Claude hook governance
- sandbox mediation
- governed MCP broker flows

If a change pulls Loopgate back toward continuity, a legacy assistant surface,
or a speculative new authority path, it is probably going in the wrong
direction.
