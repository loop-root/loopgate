# Core Map

This file maps **`core/`** — **checked-in policy** and **memory-related tree** that may mix tracked defaults with local/runtime artifacts.

Use it when changing:

- default capability policy or morphling class YAML
- understanding what under `core/memory/` is durable vs machine-local

## Core Role

- **`core/policy/`** is **source-of-truth YAML** for governance:
  - `policy.yaml` — filesystem, shell, HTTP, morphling defaults, memory thresholds, denials
  - `morphling_classes.yaml` — class definitions and resource envelopes

- **`core/memory/`** may contain ledger fragments, distillates, keys, or historical data. Per `context_map.md`, **not everything here is stable source**; much is runtime-like or historical. Treat as sensitive and do not commit new secrets.

## Relationship Notes

- Loaders and types: `internal/config/config_map.md`
- Enforcement: `internal/policy/policy_map.md`, `internal/loopgate/`

## Important Watchouts

- Policy edits have direct security impact — require tests and review.
- Prefer keeping large generated memory blobs out of git; align with `.gitignore` and project hygiene.
