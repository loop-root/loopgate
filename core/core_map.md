# Core Map

This file maps **`core/`** — the checked-in Loopgate policy defaults and signing material.

Use it when changing:

- default capability policy YAML
- detached signature handling for policy distribution and verification

## Core Role

- **`core/policy/`** is the checked-in governance source of truth:
  - `policy.yaml` — active Loopgate policy defaults and local development constraints
  - `policy.yaml.sig` — detached signature for the current policy
  - `policy.example.yaml` — example operator policy showing the intended shape without local secrets

Historical `core/memory/` residue has been preserved in the sibling `continuity`
repo and removed from the active Loopgate tree.

## Relationship Notes

- Loaders and types: `internal/config/config_map.md`
- Enforcement: `internal/policy/policy_map.md`, `internal/loopgate/`

## Important Watchouts

- Policy edits have direct security impact and must remain signed.
- The checked-in policy file is still an operator-facing default, not a toy example.
- Do not reintroduce runtime-generated memory, ledger, or key artifacts under `core/`.
