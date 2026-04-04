**Last updated:** 2026-03-24

# RFC-MORPH-0005: Continuity & memory (client stream vs Loopgate durability)

- **Status:** Draft — target architecture (partially implemented; see `docs/rfcs/0009`–`0010` for policy detail)
- **Primary authority:** **split** — client owns thread stream and ledger; Loopgate owns inspection, distillates, wake state, governed recall
- **Normative revision:** 2026-03-09

---

# Summary

This RFC defines the **continuity and memory model** for **operator clients** (client-side threads and ledger) in concert with **Loopgate**. Continuity allows the product to feel like the same assistant across sessions while preserving strict boundaries around memory growth, provenance, and trust.

Persistence follows a **sleep / wake model**, not a raw archive of every conversation. Only curated continuity artifacts survive across sessions.

The goal is to balance:

- persistence
- transparency
- safety
- user control

Continuity must always be **bounded, auditable, and purgeable**.

This RFC describes the target continuity model. Current code already has
append-only continuity inputs, bounded wake state, and exact-key recall, but
some wake-state build/load and recall behavior still lives in unprivileged client-local code
rather than a fully Loopgate-mediated memory control plane.

---

# Design Goals

The continuity system must provide:

- cross-session persistence
- bounded memory growth
- provenance tracking
- explicit promotion of durable knowledge
- user-visible purge controls

Continuity must never behave like a hidden mutable knowledge base.

---

# Continuity Layers

Client-side memory is divided into three layers.

## 1. Wake State

Wake state is the small set of information loaded when the client starts.

Purpose:

- restore conversational context
- provide awareness of active projects
- recall recent artifacts

Wake state must remain **bounded and compact**.

Structured facts should remain primary. Prose summaries should be secondary and
bounded.

Typical wake state contents:

- recent task summaries
- active workspace references
- known user preferences
- recently promoted artifacts

Wake state must not contain raw conversation history.

---

## 2. Continuity Artifacts

Continuity artifacts are structured records of past work.

Examples:

- project summaries
- task outcomes
- important design decisions
- artifact promotion records

Artifacts must include provenance metadata.

Example artifact:

```json
{
  "artifact_id": "uuid",
  "type": "task_summary",
  "origin_task": "task-uuid",
  "timestamp": "2026-03-09T22:15:00Z",
  "summary": "Refactored parser module",
  "source_files": [
    "/morph/home/workspace/parser.go"
  ]
}
```

Artifacts are append-only.

They may be referenced but not silently modified.

---

## 3. Thread Continuity (Optional)

Thread continuity tracks short-lived work contexts.

Examples:

- a multi-step code refactor
- a debugging investigation
- a research session

Thread continuity is temporary and may expire automatically.

Thread state should not accumulate indefinitely.

---

# Memory Promotion

Not all information should become durable continuity.

Promotion rules:

- morphlings produce artifacts
- The client summarizes results
- user approves promotion
- Loopgate commits artifact to continuity

This ensures durable memory is intentional.

---

# Purge Controls

Users must always be able to clear memory.

Supported commands:

```
purge thread
purge project
purge continuity
```

Purge operations must remove the corresponding continuity artifacts.

Loopgate must record purge events in the audit ledger.

---

# Memory Classification

The client must distinguish between types of information when answering questions.

Categories:

Remembered

Information stored in continuity artifacts.

Derived

Information inferred from remembered artifacts.

Fresh

Information newly obtained during the current task.

Client responses should reflect these distinctions when relevant.

Remembered information is not fresh truth.
Derived information is not the same as observed fact.
Stored information does not become prompt-safe or authoritative merely because
it persisted across runs.

---

# Storage Model

Continuity artifacts should be stored under Loopgate-controlled state directories.

Example:

```
/morph/state/continuity
```

The client should only access these artifacts through Loopgate APIs in the target
architecture.

The client must not modify continuity files directly.

---

# Size Limits

Continuity storage must remain bounded.

Recommended limits:

- maximum artifact count per project
- maximum artifact size
- optional artifact expiration

Loopgate may prune artifacts according to policy.

---

# Audit Requirements

Loopgate must record the following events:

- artifact promotion
- artifact recall
- artifact purge
- continuity load during wake

These events must be appended to the audit ledger.

---

# Security Invariants

The continuity model must guarantee:

1. The client cannot silently rewrite memory.
2. Durable knowledge must be promoted intentionally.
3. All artifacts include provenance metadata.
4. Users can purge memory at any time.
5. Continuity storage is controlled by Loopgate.

---

# Future Work

Possible improvements:

- semantic artifact indexing
- vector-based recall
- automatic artifact summarization
- timeline visualization of continuity

These features are **out of scope for v1**.

---

# Conclusion

The continuity model allows clients to maintain useful memory across sessions without turning persistence into an uncontrolled knowledge base. By structuring memory as curated artifacts with explicit promotion and purge controls, the design preserves both usefulness and user trust.
