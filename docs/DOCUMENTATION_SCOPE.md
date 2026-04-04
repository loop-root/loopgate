**Last updated:** 2026-04-03

# Documentation scope (public vs local-only)

This file is **tracked** so clones know what ships in the repository versus what maintainers keep **locally** (gitignored).

## Intentional local copies (not on GitHub yet)

| Path | Role |
|------|------|
| Root `AGENTS.md` | Cursor/Codex agent rules (optional local copy; tracked norms also in `docs/loopgate-threat-model.md`, `SECURITY.md`, `CONTRIBUTING.md`, `docs/design_overview/loopgate.md`) |
| `docs/superpowers/` | Dated agent plans, phase reports |
| `docs/reports/` | Historical security/code review snapshots |
| `context_map.md`, `*_map.md` | Repo navigation maps (often gitignored) |

## Maintainer checkout outside the repo (optional)

Long-form archives may live outside this clone. Public engineering direction for **Loopgate** stays in tracked `docs/` (especially `docs/roadmap/roadmap.md`, RFCs, and `docs/design_overview/loopgate.md`).

## Public-facing (tracked)

| Area | Path | Role |
|------|------|------|
| Index | `docs/README.md` | Entry points and RFC lists |
| Setup & ops | `docs/setup/` | Install, secrets, tools, HTTP API, MCP |
| Numbered RFCs | `docs/rfcs/` | Transport, memory, promotion, extraction policy |
| AMP (neutral protocol) | `docs/AMP/` | Vendored RFCs, implementation mapping, conformance |
| Product RFCs | `docs/product-rfcs/` | Stable IDs `RFC-MORPH-*` (legacy prefix); see folder `README.md` |
| TCL RFCs | `docs/TCL-RFCs/` | Thought Compression Language specs |
| Design overview | `docs/design_overview/` except internal-only files below | Architecture, contracts |
| Roadmap | `docs/roadmap/roadmap.md` | Baseline and direction |
| Memorybench (public) | `docs/memorybench_*.md` | Plain-English, glossary, guide, results |
| Security | `docs/loopgate-threat-model.md` | Primary threat model |

## Local-only / internal (gitignored in normal workflows)

| Area | Path | Why internal |
|------|------|--------------|
| Agent & session plans | `docs/superpowers/` | Dated execution plans |
| Point-in-time reviews | `docs/reports/` | Historical snapshots |
| Scratch & samples | `docs/dev/` | Local examples |
| Agent handoff | `docs/design_overview/next_agent_handoff.md` | Session automation context |
| Product review notes | `docs/design_overview/workflow_milestone_1_review.md` | Internal milestone review |
| Dated integration plan | `docs/roadmap/2026-03-22-tcl-memory-integration-plan.md` | Superseded |
| Working backlog | `docs/design_overview/execution_backlog.md` | Maintainer task stack |

## Repo root (often gitignored)

| Path | Role |
|------|------|
| `AGENTS.md` | Agent session rules (local copy workflow) |

Tracked substitutes: `docs/loopgate-threat-model.md`, `SECURITY.md`, `CONTRIBUTING.md`, `docs/design_overview/loopgate.md`.

## Editor / backup cruft (gitignored)

- `docs/.obsidian/` — Obsidian vault settings (local)
- `**/*.md.old` — Ad-hoc backup copies

## Engineering status without `docs/superpowers/`

For **public** status of what is implemented, use:

- `docs/design_overview/loopgate.md`
- `docs/roadmap/roadmap.md`
- `docs/setup/SETUP.md`

Do not rely on gitignored trees in CI or contributor docs.
