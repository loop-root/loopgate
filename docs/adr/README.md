# Architecture Decision Records (ADRs)

Lightweight, timestamped decisions so future contributors (and agents) implement **consistent** code instead of re-litigating settled tradeoffs.

## When to add an ADR

- Choosing between two real approaches (library, storage shape, default behavior).
- Something that will look “wrong” without context in six months.
- Security or tenancy semantics that could be “simplified” into a vulnerability.

Skip an ADR for trivial renames or obvious bugfixes.

## Format

- One file per decision: `NNNN-short-title.md` (four-digit sequence, hyphenated title).
- Start with **Date** (ISO) and **Status** (`proposed` | `accepted` | `superseded by 00NN`).

Body (roughly three sentences, can be a short bullet list if clearer):

1. **Context + decision:** What we chose and why.
2. **Tradeoff:** What we gave up or what pain we accept.
3. **Escape hatch:** If the tradeoff bites, what migration path we would take.

Link from code comments when helpful: `See docs/adr/NNNN-....md`.

## Index

| ID | Title | Status |
|----|-------|--------|
| 0001 | Engineering communication discipline | accepted |
| 0002 | Single-node tenant model before multi-node | accepted |
| 0003 | Diagnostic slog as operator product surface | accepted |
| 0004 | Deployment tenant from runtime config (not client JSON) | accepted |
| 0005 | MCP server: stdio + mcp-go, delegated session over UDS client | accepted |

Use `template.md` when creating a new ADR.
