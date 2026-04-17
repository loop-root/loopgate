**Last updated:** 2026-04-14

# Docs Map

This file maps **`docs/`** — design intent, setup, RFCs, and benchmarks. Use with `context_map.md` (repo-wide index) when present locally.

## Architecture Decision Records (`docs/ADR/`)

Timestamped decisions (context, tradeoff, escape hatch). **Index:** `docs/ADR/README.md`.

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

- Start with `GETTING_STARTED.md`, `SETUP.md`, and `OPERATOR_GUIDE.md`
- `LEDGER_AND_AUDIT_INTEGRITY.md`, `SECRETS.md`, `TOOL_USAGE.md`
- `DOCTOR_AND_LEDGER.md` — when to use `loopgate-ledger` versus `loopgate-doctor`
- `LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md` — HTTP over Unix socket for integrators
- `POLICY_SIGNING.md` — detached policy signature workflow and signer CLI
- `POLICY_SIGNING_ROTATION.md` — operator runbook for signer restore, trust-anchor rotation, and emergency replacement

## Roadmap and threat model

- `docs/roadmap/roadmap.md`
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
- `docs/assets/` — shared assets

## Relationship notes

- **Code** wins over docs when they disagree; update docs when behavior changes deliberately.

## Important watchouts

- Do not document secrets or machine-specific paths that should stay local.
- Historical product notes, extracted continuity material, and future deployment docs are not the active repo direction; keep current docs centered on Loopgate, Claude Code governance, local policy, local audit, and MCP governance.
