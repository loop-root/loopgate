**Last updated:** 2026-04-07

# Docs Map

This file maps **`docs/`** — design intent, setup, RFCs, and benchmarks. Use with `context_map.md` (repo-wide index) when present locally.

## Architecture Decision Records (`docs/adr/`)

Timestamped decisions (context, tradeoff, escape hatch). **Index:** `docs/adr/README.md`.

## Security reports (`docs/reports/`)

Actionable hardening plans and architecture reviews (e.g. `security-hardening-plan-2026-04.md`, `loopgate-security-architecture-review-2026-04-06.md`).

## Implementation sprints (repo root `sprints/`)

Phased execution plans. **Start:** `sprints/README.md` and the latest `sprints/20*-*.md` file.

## Architecture (`docs/design_overview/`)

- `architecture.md`, `loopgate.md`, `systems_contract.md` — system shape
- `claude_code_hooks_mvp.md` — current operator-harness decision
- `operator_planning_model.md` — neutral client planning model

## CI (`.github/workflows/`)

- `govulncheck.yml` — module vulnerability scan (`govulncheck`).

## Setup (`docs/setup/`)

- Start with `SETUP.md` and `OPERATOR_GUIDE.md`
- `LEDGER_AND_AUDIT_INTEGRITY.md`, `SECRETS.md`, `TOOL_USAGE.md`
- `LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md` — HTTP over Unix socket for integrators
- `POLICY_SIGNING.md` — detached policy signature workflow and signer CLI
- `POLICY_SIGNING_ROTATION.md` — operator runbook for signer restore, trust-anchor rotation, and emergency replacement
- `TENANCY.md`, `ADMIN_CONSOLE.md` — future-facing or archived direction; not part of the current local-first product story

## Roadmap and threat model

- `docs/roadmap/roadmap.md`
- `docs/loopgate-threat-model.md`

## RFCs

- `docs/rfcs/` — numbered design RFCs
- `docs/product-rfcs/` — legacy `RFC-MORPH-*` material retained for historical context; not the active source of truth for the current product
- `docs/TCL-RFCs/` — Thought Compression Language

## Benchmarks and historical PoCs

- `docs/memorybench_*.md` — historical memory/continuity benchmark material, not part of the active Loopgate product surface

## Superpowers / agent planning (`docs/superpowers/`)

Specs and plans for structured agent work (often gitignored in published clones).

## Other

- `docs/README.md` — docs index entry
- `docs/assets/` — shared assets

## Relationship notes

- **Code** wins over docs when they disagree; update docs when behavior changes deliberately.

## Important watchouts

- Do not document secrets or machine-specific paths that should stay local.
- Haven, Morph, morphling-heavy, multi-tenant, and admin-console docs are not the active repo direction; keep current docs centered on Loopgate, Claude Code governance, local policy, and local audit.
