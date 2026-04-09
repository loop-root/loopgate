# Loopgate security, memory, and architecture review

**Date:** 2026-04-08  
**Scope:** Current implementation under `internal/loopgate`, `internal/tcl`, `internal/audit`, `internal/config`, related tests, and the current docs/RFC/ADR set describing authority, continuity, audit, and local control-plane behavior.  
**Method:** Code-grounded static review with adversarial assumptions: malformed input exists, crashes happen, concurrency is unsafe until proven otherwise, and model output is untrusted input.  
**Primary focus:** memory implementation, design drift, trust-boundary integrity, maintainability, and architectural simplification.

---

## 1. Executive summary

This codebase is not governance theater, but it is not internally consistent enough yet to deserve its strongest claims.

The strongest parts are:

- explicit capability execution through Loopgate
- signed local control-plane requests with replay protection
- append-before-mutate audit discipline in several sensitive paths
- the explicit memory write path
- the morphling projection discipline that keeps worker/model text out of operator-facing authority views

The weakest parts are:

- continuity-memory ingestion and derivation
- inconsistent authorization of state-mutating control-plane routes
- a half-real memory backend abstraction
- benchmark framing that is closer to benchmark scaffolding than product-faithful execution

The narrow architectural core worth preserving is:

- Loopgate as the only privileged execution path
- server-side approvals and policy
- append-only audit
- signed local control-plane transport
- explicit bounded worker lifecycle
- explicit-memory writes via validated derived contracts

The memory subsystem is salvageable, but today it mixes one disciplined lane with one much looser lane. That gap is the main source of overclaim.

---

## 2. What is genuinely strong or novel

### 2.1 Explicit remembered-fact path is materially better than typical agent memory

The explicit write path forces a raw-text-free validated contract before persistence. It revalidates that contract at the seam and persists the derived form rather than trusting the original request prose.

Relevant code:

- `internal/loopgate/continuity_memory.go:1618`
- `internal/tcl/validated_candidate.go`
- `internal/loopgate/memory_tcl.go`

This is the right pattern for security-sensitive memory. It keeps downstream persistence and overwrite logic from having to reinterpret untrusted prose later.

### 2.2 Memory mutation ordering is correct in one important place

`mutateContinuityMemory` records audit first, then appends continuity JSONL, then saves current state.

Relevant code:

- `internal/loopgate/continuity_memory.go:702`

That ordering is materially stronger than the common failure mode where durable state mutates first and audit is “best effort.”

### 2.3 Morphling operator projection is disciplined

The operator-facing morphling summary intentionally projects lifecycle state from authoritative fields only and strips worker/model-originated status text, termination prose, and memory strings.

Relevant code:

- `internal/loopgate/morphlings.go:416`
- `internal/loopgate/morphlings.go:443`
- `internal/loopgate/morphlings.go:485`

That is exactly the kind of separation this architecture needs.

### 2.4 Test coverage around continuity edge cases is unusually strong

`internal/loopgate/continuity_memory_test.go` covers replay corruption, anchor migration, tombstone/purge eligibility, wake-state rebuild, explicit-fact supersession, discover ranking, and redaction. That is a real strength.

---

## 3. Critical risks

### 3.1 Hard security risk: continuity inspection accepts caller-supplied event streams as durable-memory input

`handleContinuityInspect` accepts a signed authenticated request body and passes it into `inspectContinuityThread`. `ContinuityInspectRequest.Validate()` only checks shape and bounds. `deriveContinuityDistillate` then persists `provider_fact_observed` payloads into durable memory facts.

Relevant code:

- `internal/loopgate/server_memory_handlers.go:11`
- `internal/loopgate/types.go:1247`
- `internal/loopgate/continuity_memory.go:744`

This is the largest mismatch with the stated memory model. The docs say the control-plane inspector is the derivation boundary for authoritative memory artifacts. In practice, a client can hand Loopgate an attributed event bundle and Loopgate will derive facts from it without an explicit validated-candidate contract equivalent to the explicit memory path.

The problem is not just malformed input. The problem is provenance. Presence of `ledger_sequence` and `event_hash` fields is not proof that the event source set is authoritative.

### 3.2 Hard security risk: memory routes do not enforce capability scope

Memory routes authenticate and verify signatures, but they do not enforce explicit capability scope. Config routes do.

Relevant code:

- `internal/loopgate/server_memory_handlers.go:11`
- `internal/loopgate/server_config_handlers.go:22`

That means the codebase currently has two privilege models:

- capability-scoped access for many privileged operations
- broad authenticated-session access for memory operations

That is not least privilege. It is a hidden trust expansion.

### 3.3 Architectural integrity risk: benchmark “production parity” bypasses real session semantics

The benchmark bridge seeds continuity memory by calling `rememberMemoryFact` directly with a fabricated `capabilityToken{TenantID: ...}` rather than using the live authenticated request path.

Relevant code:

- `internal/loopgate/memorybench_bridge.go:98`

This makes the benchmark useful for engineering, but it is not fully product-faithful. It bypasses actual session open, peer binding, request signing, and route-level enforcement.

### 3.4 Consistency risk: backend sync failure occurs after authoritative mutation

`mutateContinuityMemory` updates audit, event log, and state file, then updates in-memory authoritative state, then finally calls `partition.backend.SyncAuthoritativeState(...)`. If backend sync fails, the authoritative mutation already happened and the caller still gets an error.

Relevant code:

- `internal/loopgate/continuity_memory.go:729`

Today the backend is mostly a projection/index, so the blast radius is limited. The transaction boundary is still wrong.

---

## 4. Memory-system review

### 4.1 The explicit write path is disciplined; the continuity path is not

The explicit path is much closer to the architecture you want:

- narrow key registry
- explicit canonicalization
- validated contract
- anchor derivation
- explicit denial and audit

The continuity path is much looser:

- client submits event bundles
- event bundles are only shape-validated
- `provider_fact_observed` payloads become durable facts
- persistence does not go through the same validated contract

That split is the central memory problem in this repo.

### 4.2 Memory policy is still too weak to justify stronger claims

TCL memory governance currently reduces to:

- one known dangerous motif hard-denies
- everything else keeps

Relevant code:

- `internal/tcl/policy.go:1`
- `internal/tcl/dangerous_candidate.go:7`

This is not a mature memory-governance policy. It is a narrow deny heuristic wrapped in a stronger vocabulary.

### 4.3 Retrieval is heuristic-heavy and fragile

The projected-node discover path is token overlap on:

- hint text
- node kind
- exact/family signature text
- small extra admission text

Then it uses a narrow slot-preference reordering hook for certain explicit profile slots.

Relevant code:

- `internal/loopgate/memory_sqlite_store.go:773`

That is benchmarkable, inspectable, and easy to debug. It is not robust. It is still heavily dependent on hint quality and carefully tuned query shapes.

### 4.4 Supersession is solid only in the narrow anchored slice

Explicit remembered facts with stable anchors supersede correctly. Outside that slice, the system often falls back to coexistence plus ranking behavior.

That is defensible as a conservative design choice, but it means the current truth-maintenance story is much narrower than the overall memory narrative suggests.

### 4.5 Overall judgment on the memory system

The memory system is promising but underspecified and partially overfit.

- Explicit remembered facts: relatively robust
- Continuity-derived facts: too permissive
- Retrieval/ranking: fragile and heuristic-heavy
- Benchmark claims: directionally useful, not yet strong proof

The repo should stop speaking about “continuity memory” as if it were one uniformly governed mechanism. It is at least two mechanisms with very different trust properties.

---

## 5. Architectural drift review

### 5.1 Drift from the AMP continuity authority model

The AMP continuity RFC says the control-plane inspector is the derivation boundary for authoritative memory artifacts and that client-local helpers remain untrusted until rebound through that path.

Relevant docs:

- `docs/AMP/AMP-RFCs/0006-continuity-and-memory-authority.md:97`

The explicit memory write path mostly honors that. The continuity inspection path only partially does. It currently behaves as if a signed client-submitted event bundle is already a trustworthy inspector input.

### 5.2 Drift in the “swappable memory backend” story

The backend abstraction exists, but the live implementation does not really hand memory authority to the backend. `BuildWakeState`, `Discover`, and `Recall` mostly proxy server state, and write-side backend methods are not wired.

Relevant code:

- `internal/loopgate/memory_backend_continuity_tcl.go:49`

That means the backend boundary is conceptually larger than the implementation currently earns.

### 5.3 Drift between review docs and code

There is documentation drift even inside the repo’s own self-critique. Example: `docs/reviews/memory_reviewGaps.md` still flags missing `goal.*` and `work.*` registry support, but those prefix rules already exist in `internal/tcl/memory_registry.go`.

That is not a security bug, but it is evidence that analysis artifacts are starting to lag the code.

---

## 6. Code-quality and maintainability review

### 6.1 `continuity_memory.go` is overloaded

`internal/loopgate/continuity_memory.go` currently owns:

- request normalization
- write governance
- lineage
- wake assembly
- recall gating
- discover behavior
- ranking heuristics
- review/tombstone/purge flows
- audit payload shaping

This file is too load-bearing. It is hard to reason about invariants when persistence, policy, and retrieval heuristics all live together.

### 6.2 The memory backend abstraction is half-real

Half-real abstractions are dangerous because they increase the number of mental models without actually isolating failure or ownership boundaries. That is where the memory backend sits today.

### 6.3 Too much behavior is encoded as distributed heuristics

Examples:

- preference facet inference
- goal normalization fallback
- slot-preference reordering
- hint-text token overlap
- benchmark seeding modes

The system is functionally working, but structurally more brittle than the repo’s architectural tone implies.

### 6.4 Audit semantics are mixed

Many security-sensitive paths hard-fail on audit unavailability. Some Haven/UI paths explicitly ignore audit failure with `_ = server.logEvent(...)`.

That split may be justified, but it depends on maintainers remembering which category each route belongs to. That is operationally fragile.

---

## 7. Security boundary review

### 7.1 Capability execution is scoped; memory/control routes are less consistently scoped

This is one of the clearest trust-boundary inconsistencies in the repo.

If the design wants all meaningful privileged actions to be capability-scoped or explicitly control-scoped, memory routes need the same treatment.

### 7.2 Same-user local process remains a real threat class

The repo understands this. The code binds tokens to peer identity and signs requests, which is good. But for routes that only require “authenticated local client with a signed session,” the practical authority surface is still broader than the docs often read.

### 7.3 Haven trusted-sandbox auto-allow is an intentional bypass path

This is audited and constrained. It is not a hidden bug. It is still a deliberate approval bypass path and should be treated as such in future architecture claims.

Relevant code:

- `internal/loopgate/server.go:2391`

### 7.4 Replay protection is comparatively solid

I did not find an obvious replay hole in the signed-request path. Nonce recording, request replay tracking, and approval nonce handling are materially stronger than what most local agent runtimes ship.

---

## 8. Overcomplexity and simplification opportunities

### 8.1 Make the authoritative memory story brutally simple

The current honest answer is:

- authoritative memory = continuity JSONL + current state JSON
- SQLite = projection/index/debug aid
- backend abstraction = not yet the live authority boundary

The code should say that plainly.

### 8.2 Tighten or remove client-submitted continuity inspection

If continuity inspection cannot prove provenance from authoritative control-plane events, it should not be treated as authoritative memory derivation.

### 8.3 Either make `MemoryBackend` real or collapse it

The current middle state is complexity without enough benefit.

### 8.4 Stop overstating benchmark parity

The benchmark is useful. It is not yet a fully product-faithful exercise of the same trust boundary and route semantics.

---

## 9. Specific file and subsystem findings

### `internal/loopgate/continuity_memory.go`

- Good: explicit fact write contract and audit-before-event ordering
- Bad: continuity inspection persists fact payloads from client-submitted event bundles
- Bad: backend sync happens after authoritative mutation

### `internal/loopgate/server_memory_handlers.go`

- Bad: memory routes lack capability/control-scope checks

### `internal/loopgate/server_config_handlers.go`

- Good: config routes enforce control capabilities, proving the pattern exists and can be reused

### `internal/loopgate/memory_backend_continuity_tcl.go`

- Bad: abstraction is wider than implementation; write-side backend methods are unwired

### `internal/loopgate/memory_sqlite_store.go`

- Good: retrieval/debug behavior is inspectable
- Bad: ranking is still lexical overlap plus bounded heuristics

### `internal/tcl/policy.go` and `internal/tcl/dangerous_candidate.go`

- Bad: governance is still too narrow to justify broad “memory poisoning resistant” confidence

### `internal/loopgate/memorybench_bridge.go`

- Bad: benchmark production-parity seeding bypasses real session and peer-bound request flow

---

## 10. Highest-priority fixes in order

1. Restrict continuity inspection to authoritative control-plane-derived event sources, or remove it from general client access until provenance is enforceable.
2. Add explicit control or capability gating to all state-mutating memory routes.
3. Make continuity-derived fact persistence use a validated, raw-text-free contract comparable to the explicit memory path.
4. Stop calling benchmark seeding “production parity” unless it uses the same authenticated route path and audit/session semantics as product behavior.
5. Decide whether SQLite/index projection is product-critical. If not, demote it explicitly. If yes, make backend boundaries real and fix post-commit sync inconsistency.
6. Replace the current one-motif TCL memory policy with a broader typed review/deny model.

---

## 11. What to preserve at all costs

- Loopgate as the only real privileged execution path
- append-only audit as a hard prerequisite for sensitive mutation
- the explicit-memory validated contract pattern
- signed local control-plane requests with replay protection
- morphling projection discipline that keeps model-originated status out of authoritative UI state
- the existing fail-closed posture in startup, replay, and several audit-sensitive paths

---

## 12. Overall opinion

The project has a real core. The danger is not that it is unserious. The danger is that the memory subsystem and benchmark framing are close enough to serious that they can lull maintainers into overstating what is actually guaranteed.

The strongest version of this system is a narrow one:

- Loopgate owns authority
- approvals are real
- audit is real
- memory is derived, bounded, and conservative
- operator/UI surfaces are projections, not authority

Build that version harder. Cut anything that weakens it or muddies it.
