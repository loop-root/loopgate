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

## Setup (`docs/setup/`)

- `SETUP.md`, `SECRETS.md`, `TOOL_USAGE.md`, `ADMIN_CONSOLE.md`, `TENANCY.md`
- `LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md` — HTTP over Unix socket for integrators
- `LOOPGATE_MCP.md` — `loopgate mcp-serve`

## Roadmap and threat model

- `docs/roadmap/roadmap.md`
- `docs/loopgate-threat-model.md`

## RFCs

- `docs/rfcs/` — numbered design RFCs
- `docs/product-rfcs/` — stable IDs `RFC-MORPH-*` (legacy prefix); Loopgate / morphling / sandbox specs
- `docs/TCL-RFCs/` — Thought Compression Language

## Benchmarks

- `docs/memorybench_*.md`

## Superpowers / agent planning (`docs/superpowers/`)

Specs and plans for structured agent work (often gitignored in published clones).

## Other

- `docs/README.md` — docs index entry
- `docs/assets/` — shared assets

## Relationship notes

- **Code** wins over docs when they disagree; update docs when behavior changes deliberately.

## Important watchouts

- Do not document secrets or machine-specific paths that should stay local.
