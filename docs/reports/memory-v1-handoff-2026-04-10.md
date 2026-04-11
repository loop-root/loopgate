**Last updated:** 2026-04-10

# Memory v1 handoff

## Direct answer

Loopgate memory is ready for a truthful v1 product integration if the UI stays inside the current contract:

- inject a small wake state for current authoritative state
- use bounded artifact lookup/get for deeper recall
- use the runtime `hybrid` backend when state plus supporting evidence both matter
- do not treat memory strings, summaries, or evidence snippets as authority

This is strong enough to build a prototype UX around today. It is not a claim that TCL semantic compression or fully generalized graph memory is complete.

## What exists now

### 1. Backend-owned authority path

The live authority path is backend-owned for:

- `remember`
- `inspect`
- `discover`
- `recall`
- review / tombstone / purge

Key files:

- `internal/loopgate/memory_backend.go`
- `internal/loopgate/memory_backend_continuity_tcl.go`
- `internal/loopgate/memory_backend_continuity_tcl_candidate.go`
- `internal/loopgate/memory_backend_continuity_tcl_inspect.go`
- `internal/loopgate/memory_backend_continuity_tcl_retrieval.go`

Important contract:

- raw continuity inspect input is gone from the public control plane
- explicit remember and continuity-derived facts both flow through backend-owned candidate analysis
- Loopgate remains the authority; model output and client payloads are untrusted content

### 2. Wake state split

Wake state now distinguishes:

- `authoritative_state`
- `derived_context`

This matters for prompt honesty. Current profile/task/project state can be surfaced as durable memory without implying that every recalled continuity snippet is equally authoritative.

Key files:

- `internal/loopgate/continuity_memory.go`
- `internal/memory/wake_state.go`

### 3. Exact-first stable slot retrieval

Stable profile slots are not left to fuzzy overlap alone. Retrieval is exact-first for current anchored values like:

- `name`
- `preferred_name`
- `timezone`
- `locale`

Key file:

- `internal/loopgate/memory_backend_continuity_tcl_retrieval.go`

### 4. Runtime hybrid backend

`hybrid` is now a real runtime backend, not just a benchmark label.

Current behavior:

- continuity stays authoritative for writes, wake state, and recall
- bounded RAG evidence is added on discover when evidence is actually needed
- evidence remains advisory context, not durable authority

Key files:

- `internal/loopgate/memory_backend_hybrid.go`
- `internal/loopgate/memory_hybrid_evidence.go`

### 5. Stored artifact lookup/get

The model no longer has to choose between:

- giant wake-state injection
- no second-read path

There is now a bounded stored-artifact API:

- `POST /v1/memory/artifacts/lookup`
- `POST /v1/memory/artifacts/get`

Key files:

- `internal/loopgate/memory_artifact_refs.go`
- `internal/loopgate/server_memory_handlers.go`
- `internal/loopgate/types.go`
- `internal/loopgate/client.go`

## Honest product claim

This repository can now honestly support the following claim:

> Loopgate improves assistant memory over time by combining governed continuity memory for durable state with retrieval-based evidence memory for broader working context.

This repository cannot yet honestly support stronger claims such as:

- "better than any other system"
- "general semantic memory compression is solved"
- "the graph / relational-hint model is fully productized"

## Benchmark evidence

Current benchmark record is in:

- `docs/memorybench_running_results.md`

Important current signals:

- truthful continuity baseline: `70/70`
- long-horizon state continuity:
  - continuity `8/8`
  - hybrid `8/8`
  - governed RAG baseline `2/8`
  - governed stronger RAG `2/8`
- targeted evidence bucket:
  - continuity `3/6`
  - stronger RAG `4/6`
- targeted hybrid recall bucket:
  - continuity `0/7`
  - RAG baseline `0/7`
  - stronger RAG `0/7`
  - hybrid `7/7`

Interpretation:

- continuity is materially stronger than RAG-only memory on long-horizon current-state continuity tasks
- stronger RAG is still stronger on broad evidence retrieval alone
- bounded hybrid composition is the right MVP path for state-plus-evidence recall

## UI contract for the prototype repo

### Inject by default

Inject only the smallest high-value current state:

- active goals
- unresolved tasks / blockers
- current project context
- deadlines
- stable user profile facts
- a very small amount of clearly relevant derived context

### Fetch on demand

Use lookup/get for:

- bulky evidence notes
- prior design discussion
- supporting rationale
- related continuity artifacts
- non-authoritative context that should not permanently live in wake state

### Do not imply

The UI should not imply that:

- recalled text is authority
- evidence snippets are canonical truth
- hybrid evidence is durable memory
- model inference is persisted memory

## What remains phase 2

Not complete yet:

- broader TCL semantic family coverage
- broader poisoning-family coverage
- fully productized relational graph traversal
- live prompt-assembly policy refinement beyond the current bounded wake-state plus lookup split
- broader negative-space benchmarking where RAG should win

These are follow-on improvements, not blockers to a truthful prototype.

## Recommended prototype posture

Build the prototype around:

1. small wake state
2. explicit artifact lookup/get
3. hybrid discover only when the task needs supporting evidence
4. clear UI distinction between authoritative state and supporting context

That gives a real governed memory workflow without pretending the system is further along than it is.
