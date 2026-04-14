# Core Map

This file maps **`core/`** — checked-in policy plus a memory-related tree that still contains cleanup debt.

Use it when changing:

- default capability policy or morphling class YAML
- understanding what under `core/memory/` is durable design input versus machine-local runtime residue

## Core Role

- **`core/policy/`** is **source-of-truth YAML** for governance:
  - `policy.yaml` — Loopgate policy defaults and local development constraints
  - `morphling_classes.yaml` — legacy worker class definitions; not part of the active product story and a cleanup candidate

- **`core/memory/`** is **not** clean source-of-truth application state. It has historically mixed:
  - format/reference material the code still knows how to read
  - tracked historical runtime artifacts
  - local memory/ledger leftovers that should not grow in git

Treat anything under `core/memory/` as sensitive until proven otherwise. Do not commit new runtime-generated memory, keys, or ledger files there.

## Relationship Notes

- Loaders and types: `internal/config/config_map.md`
- Enforcement: `internal/policy/policy_map.md`, `internal/loopgate/`

## Important Watchouts

- Policy edits have direct security impact — require tests and review.
- The checked-in policy file is still a local working policy today, not yet a polished open-source-safe default.
- Prefer keeping generated memory blobs, ledgers, and keys out of git; align with `.gitignore` and the cleanup plan.
