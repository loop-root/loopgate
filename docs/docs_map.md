**Last updated:** 2026-05-04

# Docs Map

This file maps **`docs/`** — design intent, setup, RFCs, and benchmarks. Use with `context_map.md` (repo-wide index) when present locally.

## Start here

- `docs/loopgate_v1_product_contract.md` — the current v1 product boundary and success bar
- `docs/README.md` — top-level docs index
- `docs/agent/README.md` — agent-facing surface index and usage contract
- `docs/agent/agent_surfaces.yaml` — machine-readable command/API/docs surface manifest for assisting agents
- `docs/setup/GETTING_STARTED.md` — shortest path to a governed local Claude workflow
- `docs/setup/AGENT_ASSISTED_SETUP.md` — contract for using another agent to help set up governed Claude Code
- `docs/roadmap/admin_console_tui_mvp.md` — bounded local admin-console TUI spec
- `docs/roadmap/harness_usability_execution_plan.md` — focused plan for Claude usability and future harness readiness

## Architecture Decision Records (`docs/adr/`)

Timestamped decisions (context, tradeoff, escape hatch). **Index:** `docs/adr/README.md`.

## Historical archive

Historical reports, legacy product RFCs, and archived planning material have been moved to the separate `ARCHIVED` repository. Extracted continuity design docs now live in the sibling `continuity` repository.

## Archived planning

Historical sprint plans and phased execution notes have been moved to the
separate `ARCHIVED` repository.

## Architecture (`docs/design_overview/`)

- `architecture.md`, `loopgate.md` — current system shape
- `claude_code_hooks_mvp.md` — current operator-harness decision
- older client planning and subsystem design notes have been moved to `ARCHIVED`

## CI (`.github/workflows/`)

- `test.yml` — macOS-first CI for `go test -race`, policy-sign coverage, and the tagged e2e approval flow; lint runs separately on Ubuntu.
- `govulncheck.yml` — module vulnerability scan (`govulncheck`).
- `nightly-verification.yml` — scheduled/manual fuzz smoke plus macOS ship-readiness smoke (`test-race`, `test-e2e`, policy-sign coverage).

## Setup (`docs/setup/`)

- Start with `GETTING_STARTED.md`, `AGENT_ASSISTED_SETUP.md`, `SETUP.md`, and `OPERATOR_GUIDE.md`
- `agent_assisted_prompt.md` — copy-paste setup prompt for an assisting agent
- `LEDGER_AND_AUDIT_INTEGRITY.md`, `SECRETS.md`, `TOOL_USAGE.md`
- `DOCTOR_AND_LEDGER.md` — when to use `loopgate-ledger` versus `loopgate-doctor`
- `LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md` — HTTP over Unix socket for integrators
- `POLICY_SIGNING.md` — detached policy signature workflow and signer CLI
- `POLICY_SIGNING_ROTATION.md` — operator runbook for signer restore, trust-anchor rotation, and emergency replacement

## Agent surfaces (`docs/agent/`)

- `README.md` — agent-facing contract and index
- `agent_surfaces.yaml` — supported agent-usable surfaces, trust posture, docs, and skills
- `skills/loopgate-doctor/SKILL.md` — diagnostic workflow for setup checks, reports, bundles, denial explanations, and live trust checks
- `skills/loopgate-ledger/SKILL.md` — audit ledger verification and inspection workflow
- `skills/loopgate-policy-sign/SKILL.md` — detached policy signature and signer setup workflow
- `skills/loopgate-policy-admin/SKILL.md` — policy inspection, live apply, approvals, and signed grant workflow
- `skills/loopgate-operator/SKILL.md` — setup, server, status, smoke test, hooks, LaunchAgent, and uninstall workflow

## Roadmap and threat model

- `docs/roadmap/roadmap.md`
- `docs/roadmap/admin_console_tui_mvp.md`
- `docs/roadmap/harness_usability_execution_plan.md`
- `docs/roadmap/future_enterprise_direction.md`
- `docs/roadmap/loopgate_v1_hardening_plan.md`
- `docs/loopgate-threat-model.md`

## RFCs

- `docs/rfcs/` — current Loopgate RFCs (`0001`, `0016`)
- older or extracted RFCs have been moved to `ARCHIVED` or `continuity`

## Benchmarks and extracted subsystems

Historical benchmark notes live in the separate `ARCHIVED` repository. Extracted continuity subsystem material now lives in the sibling `continuity` repository.

## Archived planning and reports

Historical agent plans, sprint notes, and extracted subsystem reports now live
in the separate `ARCHIVED` repository under `ARCHIVED/docs/`.

## Other

- `docs/README.md` — docs index entry
- `docs/loopgate_v1_product_contract.md` — current Claude-first v1 product contract
- `docs/contributor/` — contributor engineering guidance that expands the root contract in `AGENTS.md`
- `docs/assets/` — shared assets

## Repository Maps

Repo and package maps live next to the code they describe:

- `context_map.md` — repo-wide orientation
- `cmd/cmd_map.md` — executable entrypoints
- `internal/*/*_map.md` and `internal/loopgate/*/*_map.md` — package-level
  maps for authority, policy, audit, sandbox, tools, secrets, and support
  packages
- `claude/claude_map.md` — Claude Code hook bundle
- `scripts/scripts_map.md` — operational scripts
- `.github/workflows/workflows_map.md` — CI workflows

## Relationship notes

- **Code** wins over docs when they disagree; update docs when behavior changes deliberately.

## Important watchouts

- Do not document secrets or machine-specific paths that should stay local.
- Historical product notes, extracted continuity material, and future deployment docs are not the active repo direction; keep current docs centered on Loopgate, Claude Code governance, local policy, local audit, and MCP governance.
