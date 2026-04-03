**Last updated:** 2026-04-01

# Docs Map

This file maps **`docs/`** — where to find **design intent**, **setup**, **RFCs**, and **Haven product** writing. Use it with `context_map.md` (repo-wide index).

## Architecture Decision Records (`docs/adr/`)

Timestamped decisions (context, tradeoff, escape hatch). **Index:** `docs/adr/README.md`. New decisions: copy `docs/adr/template.md`, assign the next `NNNN` prefix, and add a row to the README table.

## Implementation sprints (repo root `sprints/`)

Phased execution plans with exit criteria and engineering discipline notes. **Start:** `sprints/README.md` and the latest `sprints/20*-*.md` file.

## Haven product (`docs/HavenOS/`)

North star, desktop shell, host access, capabilities, roadmaps, specs, plans. Start here for **what Haven should feel like**. **Index + “Morph” persona note:** [`HavenOS/README.md`](./HavenOS/README.md).

Notable entrypoints (see also `context_map.md`):

- **Security + transport checklist:** `Haven_Loopgate_Security_and_Transport_Checklist.md` — **v1 uses HTTP on the local socket**; optional XPC-class hardening is post-launch backlog
- **Local control plane posture (v1 assumptions vs v2 backlog):** `Haven_Loopgate_Local_Control_Plane_Posture.md` — unauthenticated route findings, peer binding, session open, honest same-user scope
- Experience and roadmap: `MVP Experience Spec.md` (in-repo); dated implementation/roadmap narratives may live under `~/Dev/projectDocs/morph/haven-archives/`
- Architecture of the shell: `Dashboard and Agent OS Model.md`, `Desktop Blueprint.md`, `HavenOS_Northstar.md`
- Security and actions: `Host Access and Action Model.md`, `Loopgate Capability System.md`, `App Surface and Capability Taxonomy.md`
- Plans subfolder: `docs/HavenOS/plans/` for scoped UI/product plans

## Architecture (`docs/design_overview/`)

- `architecture.md`, `loopgate.md`, `systems_contract.md` — high-level system shape

## Setup (`docs/setup/`)

- `SETUP.md`, `SECRETS.md` (includes **enterprise secrets roadmap**: Vault/KMS/HSM/TPM-class backends), `TOOL_USAGE.md`, **`ADMIN_CONSOLE.md`** (v0 loopback admin UI: `--admin`, `LOOPGATE_ADMIN_TOKEN`, tenant-scoped audit/sessions) — operator-facing setup
- `TENANCY.md` — `tenant_id` / `user_id` on control sessions (`config/runtime.yaml`), audit, morphling checks, **on-disk memory partitions** under `runtime/state/memory/partitions/`, migration from legacy layout, per-tenant Haven reset scope
- `LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md` — **HTTP over Unix socket** for Swift/native apps: tokens, HMAC envelope, endpoint inventory (v1 transport)
- `LOOPGATE_MCP.md` — **`loopgate mcp-serve`**: stdio MCP, delegated `LOOPGATE_MCP_*` env, tools forwarding to socket HTTP (AMP-aligned)

## Roadmap and threat model

- `docs/roadmap/roadmap.md` — product/engineering roadmap
- `docs/loopgate-threat-model.md` — threat model (scope and assumptions)

## RFCs

- `docs/rfcs/` — numbered design RFCs
- `docs/rfcs/0011-swappable-memory-backends-and-benchmark-harness.md` — backend comparison harness, fixture-driven benchmark shape
- `docs/rfcs/0014-tcl-conformance-and-anchor-freeze.md` — canonical TCL source, conformance fixture model, conservative anchor rules
- `docs/product-rfcs/` — product specs (stable IDs `RFC-MORPH-*`; content is Loopgate / Haven / morphlings)
- `docs/TCL-RFCs/` — Thought Compression Language RFCs

## Benchmarks

- `docs/memorybench_plain_english.md` — plain-English explanation of what the benchmark is doing, what each backend/family/metric means, and what the current strongest claim really is
- `docs/memorybench_glossary.md` — one-page benchmark term map for backend names, ablations, contradiction regimes, and reporting artifacts
- `docs/memorybench_benchmark_guide.md` — operator/agent guide for setup, fair runs, ablations, extension workflow, and artifact interpretation
- `docs/memorybench_running_results.md` — tracked headline results, exact fixture families, and fair-run reproduction commands for the current benchmark set
- `~/Dev/projectDocs/morph/memorybench-internal/memorybench_internal_report.md` — methodology evolution and internal engineering narrative (maintainer checkout; not in clone)

## Superpowers / agent planning (`docs/superpowers/`)

- Specs and implementation plans used for structured agent work (e.g. TCL conflict anchor). Not runtime code.
- `docs/superpowers/plans/` — active master plan and superseded/consolidated plans
- `docs/superpowers/specs/` — approved design references (e.g. conflict anchor shape)
- `docs/superpowers/reports/` — phase completion and implementation summaries for agent runs (e.g. `2026-03-25-phase-1-implementation-report.md`, `2026-03-25-phase-1-ship-blockers-and-hardening-report.md`)
- `docs/superpowers/demos/` — short demo scripts for ship milestones (e.g. `2026-03-25-haven-90s-demo-script.md`)

## Other

- `docs/README.md` — docs index entry
- `docs/assets/` — shared assets for docs
- `docs/reports/` — reports and one-off writeups (gitignored when present)
- `~/Dev/projectDocs/morph/` — maintainer archives moved out of `docs/` (see `DOCUMENTATION_SCOPE.md`)

## Relationship Notes

- **Code** wins over docs when they disagree; update docs when behavior changes deliberately.

## Important Watchouts

- Do not document secrets or machine-specific paths that should stay local.
