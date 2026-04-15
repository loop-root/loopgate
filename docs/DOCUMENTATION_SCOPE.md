**Last updated:** 2026-04-14

# Documentation scope (public vs local-only)

This file is **tracked** so clones know what ships in the repository versus what maintainers keep **locally** (gitignored).

## Intentional local copies

| Path | Role |
|------|------|
| Root `AGENTS.md` | Agent and contributor guidance for local workflows; tracked public-facing norms also appear in `docs/loopgate-threat-model.md`, `SECURITY.md`, `CONTRIBUTING.md`, and `docs/design_overview/loopgate.md` |
| Historical archive repo | Historical security/code review snapshots and extracted design material |
| `context_map.md`, `*_map.md` | Repo navigation maps (often gitignored) |

## Maintainer checkout outside the repo (optional)

Long-form archives may live outside this clone. Public engineering direction for **Loopgate** stays in tracked `docs/` (especially `docs/roadmap/roadmap.md`, RFCs, and `docs/design_overview/loopgate.md`).

## Public-facing (tracked)

| Area | Path | Role |
|------|------|------|
| Index | `docs/README.md` | Entry points and RFC lists |
| Setup & ops | `docs/setup/` | Install, secrets, tools, HTTP API, **ledger/audit integrity** (`LEDGER_AND_AUDIT_INTEGRITY.md`); MCP doc = **deprecated in-tree** (removed — ADR 0010), out-of-tree / future-ADR reservation |
| Numbered RFCs | `docs/rfcs/` | Transport, memory, promotion, extraction policy |
| Design overview | `docs/design_overview/` except internal-only files below | Architecture, contracts |
| Roadmap | `docs/roadmap/roadmap.md` | Baseline and direction |
| Security | `docs/loopgate-threat-model.md` | Primary threat model |

## Local-only / internal (gitignored in normal workflows)

| Area | Path | Why internal |
|------|------|--------------|
| Archived repo | `ARCHIVED/docs/` | Historical design notes, reports, prototypes, extracted continuity docs |

## Repo root (often gitignored)

| Path | Role |
|------|------|
| `AGENTS.md` | Agent session rules (local copy workflow) |

Tracked substitutes: `docs/loopgate-threat-model.md`, `SECURITY.md`, `CONTRIBUTING.md`, `docs/design_overview/loopgate.md`.

## Editor / backup cruft (gitignored)

- `docs/.obsidian/` — Obsidian vault settings (local)
- `**/*.md.old` — Ad-hoc backup copies

## Engineering status without archived planning docs

For **public** status of what is implemented, use:

- `docs/design_overview/loopgate.md`
- `docs/roadmap/roadmap.md`
- `docs/setup/SETUP.md`

Do not rely on gitignored trees in CI or contributor docs.
