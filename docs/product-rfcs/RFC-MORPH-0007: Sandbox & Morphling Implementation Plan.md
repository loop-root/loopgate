**Last updated:** 2026-03-24

# RFC-MORPH-0007: Sandbox & morphling implementation plan

- **Status:** Draft — staged roadmap from current repo toward north-star architecture
- **Primary authority:** **Loopgate** (sandbox + morphlings); **Haven** for UX projection
- **Normative revision:** 2026-03-09

---

# Summary

This RFC turns the **Loopgate-centered** north-star architecture (Haven + Loopgate + morphlings) into a concrete implementation sequence.

It answers:

- what already exists in the current codebase
- what must change to reach the sandbox/operator/morphling model
- what order the work should happen in

This is not a current-state description. It is a staged roadmap from the
current repo-root, single-shell model toward:

User → Haven → Loopgate → Morphlings → Sandbox

---

# Current State vs Target State

## Current State

The codebase already has:

- Loopgate as the privileged control plane for:
  - model inference
  - capability execution
  - approvals
  - secrets
  - quarantine
  - promotion
  - audit
- symlink-safe, fail-closed filesystem validation inside configured roots
- bounded wake state and continuity across runs
- narrow direct workflows for:
  - status check
  - repo/issues summary

The codebase does **not** yet have:

- a dedicated sandbox root that replaces repo-root access
- first-class import/export capabilities
- real morphling spawn/lifecycle management
- artifact staging as the primary product interaction model

## Target State

The target product model is:

- **Haven** is the persistent operator shell (desktop)
- Loopgate is the kernel/control plane
- Morphlings are disposable workers
- all agent work happens inside a dedicated sandbox
- host file changes occur only through explicit export/promotion

---

# Guiding Rules

1. Do not weaken current filesystem safety to get to the sandbox model.
2. Do not add morphlings before sandbox import/export exists.
3. Do not let sandbox files silently become host files.
4. Do not let morphlings inherit full operator-session authority.
5. Preserve append-only audit semantics for:
   - imports
   - exports
   - morphling lifecycle
   - artifact promotion

---

# Phase 1: Sandbox Root Abstraction

## Goal

Replace the effective runtime boundary of:

- repo root + `allowed_roots`

with:

- sandbox home root

## Product Model

Conceptual root:

```text
/morph/home
```

Recommended first implementation path:

```text
private repo-local runtime sandbox home
```

This preserves the product boundary without requiring an immediate host-level
filesystem relocation.

## Directory Layout

```text
runtime/sandbox/root/
  config/
  policy/
  state/
  var/
  home/
    workspace/
    imports/
    outputs/
    scratch/
    agents/
    quarantine/
    tmp/
    logs/
```

## Deliverables

- a sandbox root abstraction in code
- policy defaults that can target sandbox home instead of repo root
- command/status visibility for sandbox paths

## Invariants

- symlink resolution must still fail closed
- final canonical target must remain inside sandbox home
- Loopgate-owned state must remain outside sandbox home

---

# Phase 2: Import / Export Capabilities

## Goal

Make file movement across the sandbox boundary explicit and auditable.

## Required capabilities

- `sandbox.import`
- `sandbox.export`
- optionally:
  - `sandbox.list`
  - `sandbox.snapshot`

## Safe defaults

Imports must be:

- copy
- snapshot
- clone/reflink later if platform-safe

Imports must **not** be:

- host symlink passthrough
- implicit discovery

Exports must:

- require approval
- target explicit host destinations
- preserve artifact provenance

## Deliverables

- import API/command
- export API/command
- import manifest/provenance metadata
- approval path for export

## Invariants

- agent workspace is not the host filesystem
- import does not grant host authority
- export is the only path that modifies host files

---

# Phase 3: Approval Classes

## Goal

Align approvals with the sandbox/operator model rather than broad repo access.

## MVP approval classes

- read sandbox path
- write sandbox path
- export sandbox artifact
- launch morphling
- provider capability
- create trust draft

## Explicitly disallowed approvals

- root filesystem access
- full home-directory access
- unrestricted network
- global write permissions

## Deliverables

- approval-class vocabulary in operator output
- approval metadata tied to specific object/path/capability
- tests proving denials remain object-scoped and time-scoped

---

# Phase 4: Morphling Spawn / Lifecycle MVP

## Goal

Introduce disposable workers only after the sandbox and approval model are real.

## MVP shape

Morphling lifecycle:

```text
plan
→ authorize
→ spawn
→ execute
→ produce artifacts
→ stage results
→ summarize
→ approve / reject / export
→ terminate
```

## Required constraints

- single task
- bounded lifetime
- bounded token/time budget
- scoped capability token
- scoped sandbox paths
- no policy mutation
- no direct host writes
- no direct durable-memory mutation

## Deliverables

- morphling task schema implementation
- spawn API
- lifecycle tracking
- artifact staging path
- termination/cleanup behavior

## Invariants

- morphlings are workers, not peers
- they do not inherit full Haven / session authority
- only staged artifacts survive

---

# Phase 5: Continuity & Product Integration

## Goal

Make the sandbox/operator/morphling model feel continuous across runs.

## Deliverables

- sandbox-aware wake state
- active task / active morphling continuity
- purge commands for sandbox-oriented work scopes
- explicit remembered vs fresh phrasing in operator replies

## Non-goals for this phase

- broad semantic search
- generic autonomous swarm behavior
- implicit trust promotion from sandbox artifacts

---

# Recommended Build Order

1. sandbox root abstraction
2. import/export capabilities
3. approval-class alignment
4. morphling spawn/lifecycle MVP
5. continuity integration for active sandbox work

Do **not** invert this order.

Building morphlings before the sandbox and export path exists will create a
worker system without a clear safety boundary.

---

# Why this order

This sequence preserves the strongest current property of the project:

- Loopgate remains the sole authority boundary

It also avoids a common trap:

- building “subagents” before defining where they are allowed to live, what
  they are allowed to touch, and how their outputs leave their workspace

The sandbox model is not a detail.
It is the thing that makes morphlings safe enough to exist.

---

# Success Criteria

This roadmap is successful when:

- Haven no longer needs repo-root access as its default workspace model
- a user can explicitly import work into a sandbox
- Haven and morphlings operate only inside the sandbox
- exports are staged, reviewed, approved, and auditable
- morphlings can complete bounded one-shot tasks without broad host authority

---

# Conclusion

The current codebase already has the control-plane foundation needed for this
direction. The next work is not “make the model smarter.” It is:

- define the sandbox root
- define import/export boundaries
- align approvals with those boundaries
- then introduce disposable workers inside that environment

That is the shortest path from the current secure kernel to the product identity
described in the MORPH RFC set.
