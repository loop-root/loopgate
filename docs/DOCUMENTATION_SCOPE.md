**Last updated:** 2026-04-01

# Documentation scope (public vs local-only)

This file is **tracked** so clones know what ships in the repository versus what maintainers keep **locally** (gitignored).

## Intentional local copies (not on GitHub yet)

Some trees are **gitignored** so they do not appear when this repository is published (e.g. on GitHub). Maintainers may still **copy** them into a checkout for day-to-day work:

| Path | Role |
|------|------|
| Root `AGENTS.md` | Cursor/Codex agent rules and hard invariants (optional local copy; tracked engineering norms also live in `docs/loopgate-threat-model.md`, `SECURITY.md`, `CONTRIBUTING.md`, `docs/design_overview/loopgate.md`) |
| `docs/superpowers/` | Dated agent plans, model feedback, phase reports, demo scripts |
| `docs/reports/` | Historical security/code review snapshots |
| `context_map.md`, `*_map.md` | Repo navigation maps (often gitignored to reduce review noise) |

**If/when the repo is opened on GitHub:** keep these entries gitignored unless you deliberately choose to publish them; duplicates can remain in a private primary checkout.

## Maintainer checkout outside the repo (optional)

Some long-form or point-in-time docs may live under **`~/Dev/projectDocs/morph/`** (see that tree’s `README.md`). They are **not** part of every clone. Public engineering direction for **Loopgate** stays in tracked `docs/` (especially `docs/roadmap/roadmap.md`, RFCs, and `docs/design_overview/loopgate.md`).

## Public-facing (tracked)

| Area | Path | Role |
|------|------|------|
| Index | `docs/README.md` | Entry points and RFC lists |
| Setup & ops | `docs/setup/` | Install, secrets, tools, Loopgate HTTP API for integrators |
| Numbered RFCs | `docs/rfcs/` | Transport, memory, promotion, extraction policy |
| AMP (neutral protocol) | `docs/AMP/` | Vendored RFCs, implementation mapping, `local-uds-v1` conformance checklist |
| Product RFCs | `docs/product-rfcs/` | **Stable IDs `RFC-MORPH-*`:** historical prefix; content describes **Loopgate**, Haven, sandbox, **morphlings** — see folder `README.md` |
| TCL RFCs | `docs/TCL-RFCs/` | Thought Compression Language specs |
| Design overview | `docs/design_overview/` except internal-only files below | Architecture, contracts, how-it-works |
| Haven product | `docs/HavenOS/` except `plans/` | **[`HavenOS/README.md`](./HavenOS/README.md)** — index; *Morph* = in-app persona in this folder. Consumer demo / Swift Haven; **`cmd/haven/`** Wails reference-only |
| Roadmap | `docs/roadmap/roadmap.md` | Baseline and direction |
| Memorybench (public) | `docs/memorybench_*.md` | Plain-English, glossary, guide, running results |
| Security | `docs/loopgate-threat-model.md` | Primary threat model |

## Local-only / internal (gitignored in normal workflows)

Not published with the repository when those ignore rules are active. Optional in a given worktree.

| Area | Path | Why internal |
|------|------|--------------|
| Agent & session plans | `docs/superpowers/` | Dated execution plans, model logs, phase reports |
| Point-in-time reviews | `docs/reports/` | Historical snapshots, not normative |
| Scratch & samples | `docs/dev/` | Local examples |
| Haven UI sprint plans | `docs/HavenOS/plans/` | Dated workspace/UI notes |
| Agent handoff | `docs/design_overview/next_agent_handoff.md` | Session-to-session automation context |
| Product review notes | `docs/design_overview/workflow_milestone_1_review.md` | Internal milestone review |
| Dated integration plan | `docs/roadmap/2026-03-22-tcl-memory-integration-plan.md` | Superseded by `roadmap.md` + RFCs |
| Working backlog | `docs/design_overview/execution_backlog.md` | Maintainer task stack |

## Repo root (often gitignored)

| Path | Role |
|------|------|
| `AGENTS.md` | Agent session rules (local copy workflow) |

Tracked substitutes for invariant text: `docs/loopgate-threat-model.md`, `SECURITY.md`, `CONTRIBUTING.md`, and `docs/design_overview/loopgate.md`.

## Editor / backup cruft (gitignored)

- `docs/.obsidian/` — Obsidian vault settings (local)
- `**/*.md.old` — Ad-hoc backup copies next to real docs

## Engineering status without `docs/superpowers/`

For **public** status of what is implemented, use:

- `docs/design_overview/loopgate.md` — control plane and transport snapshot
- `docs/roadmap/roadmap.md` — feature baseline
- `docs/setup/SETUP.md` — what operators run today

Do not rely on gitignored trees in CI or contributor docs.
