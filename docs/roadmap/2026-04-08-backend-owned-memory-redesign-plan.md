**Last updated:** 2026-04-09

# Backend-Owned Memory Authority Redesign Plan

Status: in progress

## 1. Summary

Phase 1 TCL integration improved the explicit memory-write path, but memory
authority is still spread across HTTP handlers, `Server` methods, continuity
mutation helpers, and a half-wired backend.

That split is the main structural problem.

This plan tightens the design around one rule:

- the model may suggest or propose memory semantics
- Loopgate remains the only authority that decides what persists
- the memory backend becomes the real owner of memory ingestion, governance,
  durable commit, wake-state assembly, and recall

The goal is not to make memory more clever.
The goal is to make it narrower, more deterministic, easier to reason about,
and harder to poison.

## 2. Problem Statement

Today the codebase has the right intent but the wrong ownership shape.

Current issues:

- explicit `memory.remember` flows through a stronger TCL-aware path, but
  continuity-derived persistence still has separate logic
- the live write path still centers on `rememberMemoryFact(...)` and
  `inspectContinuityThread(...)` instead of the backend
- `MemoryBackend` exists, but its write methods are not the authoritative path
- handlers in `internal/loopgate/server_memory_handlers.go` still do more than
  transport, auth, and request shaping
- wake-state assembly and retrieval are too coupled to partition state and too
  reliant on heuristic ranking
- confidence-like scores are present, but their meaning is underspecified
- the current malicious-memory policy is still closer to regex/motif matching
  than real TCL-grounded semantic governance

This creates three risks:

1. design drift between docs and code
2. too many places where durable-memory decisions can subtly diverge
3. a false sense that the backend boundary already exists when it mostly does not

## 2.1 Progress So Far

Completed slices in the current refactor:

- `memory.read`, `memory.write`, `memory.review`, and `memory.lineage`
  control capabilities gate the memory routes
- live `remember`, `inspect`, `discover`, and `recall` requests now cross the
  backend seam before memory logic runs
- live review, tombstone, and purge mutations now cross the backend seam before
  continuity-governance logic runs
- explicit remember request normalization and validated-candidate analysis now
  run through backend-owned helpers in the live path
- the dead per-artifact backend store hooks were removed; `SyncAuthoritativeState`
  remains the single projection-sync hook until a narrower real storage seam is needed
- focused retrieval-ranking tests were moved onto the real continuity backend so
  they no longer depend on a stub-only retrieval path
- continuity `inspect` now rejects event bundles whose `session_id`, `thread_id`,
  `scope`, `ledger_sequence`, or `event_hash` metadata do not line up with the
  authenticated request context, so valid-looking packets cannot smuggle
  another session's continuity into durable memory
- continuity-derived `provider_fact_observed` entries now persist only bounded
  scalar fact values that survive TCL analysis; nested payloads and dangerous
  candidates are dropped before they become durable memory facts
- stable profile-slot discover now performs exact anchored admission first for
  `name`, `preferred_name`, `timezone`, and `locale`, so the current anchored
  value remains discoverable even when tag overlap is weak and heuristic
  ranking would otherwise miss it
- continuity-derived fact analysis now runs through a backend-owned typed
  continuity candidate helper before persistence, so `inspect` no longer
  hand-assembles persisted facts from inline TCL analysis results
- continuity inspection writes now persist a Loopgate-owned canonical
  `observed_packet` record instead of storing the raw inspect request body in
  continuity JSONL, and distillation now derives from that typed packet rather
  than the caller-supplied payload bundle
- raw `/v1/continuity/inspect` has been removed; the supported operator path is
  now server-loaded `/v1/continuity/inspect-thread`
- server-loaded `/v1/continuity/inspect-thread` now sends a backend-owned
  observed packet directly instead of first fabricating a raw client-style
  continuity request
- when the supported observed packet path carries server-owned source refs,
  derived distillates and fact provenance now preserve those refs instead of
  collapsing back to synthetic `ledger_sequence:*` placeholders
- the supported Haven `inspect-thread` path now binds observed event
  `session_id` to the authoritative control session id and carries the stable
  threadstore event hash on each server-owned source ref
- the explicit remember TCL candidate-builder seam is now backend-owned instead
  of hanging off `Server`, so targeted test injection no longer leaves server
  ownership in the live candidate-analysis path
- wake-state and recall facts now carry an explicit `state_class`
  (`authoritative_state` vs `derived_context`) so clients and future prompt
  assembly do not have to infer hard-vs-soft memory from source-ref heuristics
- the removed raw compatibility inspect route used to drop caller-supplied
  `source_refs` before packets entered backend-owned observed continuity state;
  first-class provenance refs now arrive only through the supported
  server-loaded path
- backend-owned observed packets now allowlist source-ref kinds, so any new
  provenance source has to be added intentionally instead of arriving as an
  arbitrary string in observed continuity state
- continuity-derived observed facts now preserve bounded event `text` and
  `output` tags alongside the derived value so retrieval has some context for
  same-key preview-label and different-entity disambiguation instead of
  reducing everything to value-only overlap
- discover ranking now treats generic profile-name slot queries as
  `preferred_name`, keeps preview-targeted queries separate from canonical slot
  queries, and suppresses other-entity distractors when a user-scoped
  remembered candidate is present
- explicit preference supersession now covers the current benchmarked theme and
  indentation wording families, so stale older values stop coexisting with the
  latest explicit preference write

Still not finished:

- candidate normalization and TCL analysis are still split between backend
  methods and `Server`-owned injectable seams for test fault injection
- diagnostic wake and some partition-state helpers still exist as compatibility
  seams for tests and should not become new production authority paths

## 3. Design Goals

- keep Loopgate as the only authority boundary
- make the backend the single memory authority path inside Loopgate
- keep TCL derived and assistive, never authoritative
- treat explicit user input as untrusted
- treat model-proposed memory envelopes as untrusted
- keep wake state compact, structured, and clearly split between hard state and
  soft continuity
- preserve append-only lineage and durable audit requirements
- reduce the number of files and functions that can mutate memory state
- make benchmark claims honest by forcing benchmark parity through the real path

## 4. Hard Invariants

- natural language is never authority
- model output is untrusted input
- explicit user input is untrusted input
- results are content, not commands
- memory is context, not authority
- Loopgate policy decides persistence, review, quarantine, or denial
- append-only audit remains required for security-relevant memory mutation
- continuity packets are not authority; authenticated session binding and
  backend validation decide whether the packet is even admissible for inspection
- fail closed on unsupported source lanes, invalid TCL, missing provenance, or
  ambiguous supersession
- confidence must not become a hidden permission system

## 5. Target Ownership Split

The ownership split should be explicit and boring.

### 5.1 HTTP handlers

Files:

- `internal/loopgate/server_memory_handlers.go`

Handlers should own only:

- transport method checks
- session authentication
- signed-request verification
- required `memory.*` control capability checks
- strict JSON decode and response encoding

Handlers should not own:

- memory candidacy decisions
- TCL normalization decisions
- wake-state construction logic
- durable memory mutation orchestration

### 5.2 Memory backend

Files:

- `internal/loopgate/memory_backend.go`
- `internal/loopgate/memory_backend_continuity_tcl.go`

The backend should become the real owner of:

- candidate ingestion
- authoritative-source resolution
- TCL analysis and proposal validation
- persistence decisions
- append ordering for memory artifacts
- wake-state assembly
- discover
- recall
- lineage/governance transitions such as review, tombstone, and purge

### 5.3 TCL service

Files:

- `internal/tcl/*`
- `internal/loopgate/memory_tcl.go`

TCL should own:

- normalization
- validation
- semantic signatures
- risk motifs
- policy disposition recommendations

TCL should not own:

- persistence
- session authority
- approvals
- durable mutation

### 5.4 Storage layer

The storage layer should own:

- durable event and node persistence
- exact anchor lookup
- bounded query support
- projection/wake materialization support

It should not own:

- policy
- approval
- candidate eligibility

## 6. Unified Memory Object Model

The system should stop treating explicit memory writes and continuity-derived
memory as fundamentally different kinds of truth.

They are different source lanes, but they need the same lifecycle shape.

### 6.1 `MemoryCandidateInput`

Untrusted input to the backend.

Required properties:

- source lane
- tenant and control-session binding
- authoritative source refs or bounded explicit input fields
- evidence snippets or structured fields
- optional model-proposed TCL envelope
- optional model-proposed confidence

Important rule:

- a proposal is a hint, not a fact

Allowed initial source lanes:

- explicit user memory request
- Loopgate-owned workflow transition
- Loopgate-owned continuity event ref
- operator-reviewed derived artifact

Denied by default:

- arbitrary client-submitted continuity bundles as durable derivation input
- raw tool output without a deterministic extraction contract
- raw filesystem content
- unreviewed model-authored prose summaries

### 6.2 `ValidatedMemoryCandidate`

Generic validated candidate returned by backend-side analysis.

This should generalize the current explicit-memory-only validated candidate
shape so all durable memory writes go through one contract.

Required properties:

- canonical TCL node
- anchor tuple if one exists
- exact, family, and risk signatures
- epistemic flavor
- safe audit summary
- keep/review/quarantine/deny disposition
- supersession scope

Important rule:

- if the backend cannot validate the candidate, nothing persists

### 6.3 `MemoryArtifact`

Authoritative durable memory output.

Examples:

- explicit remembered fact
- workflow-state fact
- continuity-derived fact
- distillate
- wake-state source record

Each artifact must carry:

- provenance event id
- lifecycle state
- anchor or explicit lack of anchor
- replacement lineage where applicable

## 7. Ingestion Pipeline

All durable memory mutation should go through one backend-owned pipeline.

### 7.1 Step-by-step flow

1. handler authenticates and verifies signed request
2. handler enforces required `memory.*` control capability
3. handler sends one backend command with tenant/session context plus raw request
4. backend resolves authoritative evidence for the source lane
5. backend builds `MemoryCandidateInput`
6. backend runs TCL normalization and policy analysis
7. if a model proposal is present, backend compares proposal against derived TCL
8. mismatch or unsupported interpretation becomes review or denial, not keep
9. backend writes required audit event before durable memory commit
10. backend commits authoritative memory artifact(s)
11. backend rebuilds or updates wake/projection state within the same ownership boundary
12. backend returns a public response plus optional diagnostic detail

### 7.2 Proposal mismatch rule

If a model proposes a TCL envelope and the backend derives a materially
different semantic meaning from the authoritative evidence:

- the proposal is not trusted
- the derived form wins for analysis
- the candidate is either denied or sent to review, depending on the lane

This is the poisoning defense.

### 7.3 Explicit user input rule

`memory.remember` is not a persistence grant.

It means only:

- the caller is allowed to submit a memory candidate for governance

The backend still decides:

- keep
- review
- quarantine
- deny

## 8. Wake-State Model

Wake state should become explicitly two-part inside the backend.

### 8.1 `authoritative_state`

Hard continuity state.

Contents:

- anchored profile slots
- active goals
- unresolved items
- current workflow state
- stable factual state that survived governance

### 8.2 `derived_context`

Soft continuity support.

Contents:

- bounded distillates
- resonate keys
- advisory continuity hints
- recent supporting context

### 8.3 Rules

- authoritative state always outranks derived context
- derived context must never overwrite anchored slots
- derived context should be bounded and aggressively trimmed
- confidence may influence derived-context selection
- confidence must not decide authority or supersession

The current public wake-state response can stay backward-compatible at first.
The internal model should split first, and the diagnostic wake output should
show the split clearly.

## 9. Retrieval Model

Recall should stop depending on one generic heuristic path for everything.

### 9.1 Exact-slot recall first

For stable anchored slots, the backend should do exact lookup before heuristic
search.

Current state:

- discover now does exact anchored admission first for stable profile slots on
  the existing public request path
- recall is still keyed by resonate-key IDs, so exact slot retrieval is not yet
  exposed as a first-class recall request shape

Initial exact-slot targets:

- `identity.name`
- `identity.preferred_name`
- `profile.timezone`
- `profile.locale`
- stable preference facets
- stable workflow identifiers

### 9.2 Heuristic discover second

Heuristic ranking should remain for:

- broad continuity search
- associative lookup
- distillate discovery
- non-anchor fuzzy recall

Confidence and recency can matter here, but they should not be allowed to
override exact anchored state.

## 10. Capability and Trust Boundary Changes

Add explicit control capabilities for memory routes:

- `memory.read`
- `memory.write`
- `memory.review`
- `memory.lineage`

Apply them in the same style as config routes.

Expected route mapping:

- `/v1/memory/wake-state`, `/v1/memory/discover`, `/v1/memory/recall` ->
  `memory.read`
- `/v1/memory/remember`, `/v1/continuity/inspect-thread` -> `memory.write`
- `/v1/memory/inspections/{id}/review` -> `memory.review`
- tombstone/purge/lineage operations -> `memory.lineage`

Important rule:

- authenticated local session is not enough by itself

## 11. Benchmark Boundary

Benchmark claims should become stricter.

Rules:

- benchmark seeding that claims product parity must use the real HTTP over UDS
  client path
- benchmark runs must use real session open, signed requests, and route auth
- direct in-process calls into `rememberMemoryFact(...)` are benchmark-local
  shortcuts, not parity runs
- `production_write_parity` now follows that rule on the supported product path
  by seeding each benchmark scope through isolated Loopgate runtimes over the
  real authenticated `memory.remember`, `/v1/continuity/inspect-thread`, and
  `todo` capability routes
- supported production-parity retrieval now uses the real
  `/v1/memory/discover` and `/v1/memory/recall` routes
- the current checked-in `61`-fixture scored set no longer needs projected-node
  fallback, so the honest continuity parity baseline now runs with
  `retrieval_path_mode=control_plane_memory_routes` and
  `seed_path_mode=control_plane_memory_and_todo_workflow_routes`
- the latest honest continuity rerun is now `61/61` overall with `8/8` on
  poisoning, `34/34` on contradiction, `13/13` on task resumption, and `6/6`
  on safety precision
- that recovery came from product changes behind the real control-plane path,
  not from restoring projected-node benchmark shortcuts
- the benchmark-local slot-preference wrapper was inert on that run because the
  checked-in scored fixture set used no projected-node fallback scopes
- the move from `57/61` to `61/61` came from fixing benchmark governance
  evaluation for continuity-style candidates so the scored run now reflects the
  real TCL policy decision instead of an explicit-write validator mismatch

If a shortcut path remains for fixture speed, it should be labeled as such in
code and output.

## 12. Migration Plan

This should be done in conservative phases.

### Phase A: consolidate ownership without changing the external API

- add `memory.*` control capabilities
- make handlers thin
- route remember, inspect, review, tombstone, purge, wake, discover, and recall
  through backend methods
- keep current response shapes

Success condition:

- no live memory route bypasses the backend

### Phase B: unify candidate validation

- generalize the current explicit-memory validated candidate into a shared
  backend contract
- require all durable memory writes to pass through that contract
- deny client-submitted raw continuity event bundles for durable derivation
- allow only Loopgate-owned event refs or other bounded approved lanes

Success condition:

- explicit and continuity-derived durable writes share one governance pipeline

### Phase C: wake-state and recall hardening

- split internal wake state into `authoritative_state` and `derived_context`
- add exact-slot recall
- keep confidence only as advisory ranking input for soft continuity

Success condition:

- stale or low-confidence context can no longer outrank stable anchored facts

### Phase D: storage honesty

- choose one honest story for authority
- either the backend store becomes the authoritative runtime store
- or SQLite remains explicitly a projection until the authoritative move is finished

Do not claim the backend is authoritative while reads and writes still depend on
side state outside it.

## 13. File-Level Change Plan

### First-pass files

- `internal/loopgate/memory_backend.go`
  - replace the current store-ish interface with backend-owned command methods
- `internal/loopgate/memory_backend_continuity_tcl.go`
  - implement the real ingestion, governance, wake, discover, and recall path
- `internal/loopgate/server_memory_handlers.go`
  - enforce `memory.*` capability checks and delegate only
- `internal/loopgate/continuity_memory.go`
  - split large mixed-responsibility write-side logic into backend-owned units
- `internal/loopgate/memory_tcl.go`
  - generalize validated-candidate handling beyond explicit remember
- `internal/tcl/policy.go`
  - move from narrow regex-ish motif logic toward actual TCL semantic family policy
- `internal/loopgate/memorybench_bridge.go`
  - relabel or replace shortcut parity paths

### Likely new files

- `internal/loopgate/memory_backend_commands.go`
- `internal/loopgate/memory_ingest.go`
- `internal/loopgate/memory_wake.go`
- `internal/loopgate/memory_recall.go`

The point is not more files for their own sake.
The point is to stop one giant file from mixing transport, governance,
persistence, ranking, and projection.

## 14. Tests

Required tests:

- memory routes denied without the required `memory.*` control capability
- explicit user memory writes still fail closed on invalid or dangerous TCL
- continuity-derived durable writes denied when source refs are not Loopgate-owned
- proposal-TCL mismatch causes review or denial
- exact-slot recall returns anchored current value even when heuristic ranking
  prefers distractors
- derived context in wake state never overwrites authoritative state
- review, tombstone, and purge continue preserving lineage invariants
- benchmark parity runs fail if they skip the real HTTP/session/auth path
- crash-path tests prove partial projection failure does not create silent
  split-brain authority

## 15. Non-goals

This plan does not:

- widen durable memory candidacy to generic raw prose
- make TCL authoritative
- let the model choose the active backend
- introduce background distillation daemons
- replace Loopgate governance with retrieval logic

## 16. Immediate Next Step

The first implementation step should be small and decisive:

1. add `memory.read`, `memory.write`, `memory.review`, and `memory.lineage`
2. change `MemoryBackend` so the live routes must go through it
3. make `server_memory_handlers.go` transport-only
4. route `memory.remember` and continuity inspection through one backend-owned
   candidate-ingestion path

That gets the ownership boundary right before deeper storage or recall work.
