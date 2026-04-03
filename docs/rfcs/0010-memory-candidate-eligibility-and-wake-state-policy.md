**Last updated:** 2026-03-24

# RFC 0010: Memory Candidate Eligibility and Wake State Policy

Status: draft

## 1. Summary

This RFC defines the first policy layer for Morph persistent memory after
[RFC 0009](./0009-memory-continuity-and-recall.md).

It answers five questions:

1. what may become a memory candidate
2. what is denied from durable memory by default
3. what a wake state must contain and how large it may become
4. how startup continuity should be assembled
5. how bounded recall requests should behave

This RFC is intended to finish the memory control-plane policy before deeper
implementation work begins.

## 2. Non-goals

This RFC does not define:

- semantic search
- vector indexing
- unrestricted retrieval
- model-owned memory promotion
- mutable fact editing
- freeform prose wake-state dumps

## 3. Memory Candidate Eligibility

Memory candidacy is explicit and deny-by-default.

If an event or artifact is not explicitly eligible, it must not become durable
memory.

No memory artifact may gain more trust than its inputs and derivation policy
justify.

### 3.1 Allowed sources

The following may become memory candidates when they are provenance-bearing,
policy-allowed, and structurally bounded:

- provider-backed structured facts derived through deterministic extractors
- bounded scalar values with explicit field metadata
- operator-approved derived artifacts
- explicit task and goal state transitions
- unresolved work items with structured identifiers
- session lifecycle milestones relevant to continuity
- prior distillates or wake-state artifacts used as source lineage

### 3.2 Denied sources

The following are denied as memory candidates by default:

- raw quarantined content
- blob-ref content
- tainted display-only prose
- unreviewed model-authored summaries
- freeform remote text
- generic filesystem content without a deterministic extraction contract
- internal runtime noise and telemetry
- capability denials that do not materially affect continuity

### 3.3 Immutability rule

`memory_candidate` classification should be treated as immutable after the event
or artifact is created.

In other words:

- an event either enters the continuity stream as memory-candidate eligible, or
  it does not
- later code must not silently upgrade a historical non-candidate into durable
  memory

If future policy needs to make historical content durable, it must do so by
creating a new derived artifact with its own provenance and audit trail.

### 3.4 Epistemic flavor

Memory artifacts should preserve enough epistemic shape to avoid collapsing
historical observation, interpretation, and fresh verification into one
undifferentiated truth state.

Useful first flavors are:

- `remembered`
- `derived`
- `freshly_checked`

Future implementations may refine these into narrower categories such as:

- observed fact
- task state
- derived summary
- unresolved hypothesis
- fresh verification required

### 3.5 Default deny behavior

Absence of explicit eligibility is a denial.

Memory candidacy must not be inferred from:

- event presence
- field names
- shortness of text
- repeated access
- operator viewing

When both are available, memory should prefer observed facts and structured task
state over inferred narrative text.

## 4. Wake State Schema

A wake state is a compact structured startup checkpoint.

It must not become a large mutable memory blob.

Wake state should preserve continuity, not imply fresh truth.

### 4.1 Required fields

The first wake-state schema should contain:

- `id`
- `scope`
- `created_at_utc`
- `persona_ref`
- `source_refs`
- `active_goals`
- `unresolved_items`
- `recent_facts`
- `resonate_keys`

### 4.2 Suggested shape

```json
{
  "id": "wake_global_20260308T180700Z",
  "scope": "global",
  "created_at_utc": "2026-03-08T18:07:00Z",
  "persona_ref": "persona:morph@v1",
  "source_refs": [
    {"kind": "distillate", "ref": "dist_abc"}
  ],
  "active_goals": [
    "monitor public service health"
  ],
  "unresolved_items": [
    {
      "id": "todo_status_followup",
      "text": "check repo issue backlog after incident review"
    }
  ],
  "recent_facts": [
    {
      "name": "github_status_indicator",
      "value": "none",
      "source_ref": "dist_abc"
    }
  ],
  "resonate_keys": ["rk_recent_status"]
}
```

### 4.3 Bounded counts

Wake state should remain deliberately small.

Initial bounded targets:

- `active_goals`: max 5
- `unresolved_items`: max 10
- `recent_facts`: max 12
- `resonate_keys`: max 8
- `source_refs`: max 16

These are policy targets, not just UI suggestions.

### 4.4 Trimming rules

If a candidate wake state exceeds the bounds:

1. trim oldest or lowest-priority items first
2. prefer keeping structured facts over prose
3. prefer keeping unresolved items over narrative summaries
4. prefer keeping recent, high-signal resonate keys over broad historical sets

Trimming must be deterministic.
It must not silently widen the wake state to avoid making a decision.

Wake-state contents should prefer:

- structured facts
- unresolved items
- active goals
- compact resonate-key handles

Wake-state contents should avoid:

- large prose blocks
- blended historical and fresh claims
- derived narrative standing in for structured facts

## 5. Startup Assembly

Startup assembly must stay bounded and predictable.

### 5.1 Default load rule

On startup, Morph should assemble continuity from:

- one global wake state
- optionally one active-thread wake state when a thread/task scope is known
- a bounded set of resonate keys from those wake states

It should not load multiple thread wake states by default.

### 5.2 Thread activation rule

A thread wake state should load only when at least one of the following is true:

- the current session explicitly resumes a known thread
- the operator request references a known continuity scope
- a local planning step identifies one dominant active scope with high
  confidence

If scope selection is ambiguous, Morph should prefer the global wake state only
rather than loading multiple thread lanes implicitly.

Thread activation must not be inferred from weak textual similarity alone.

### 5.3 Key limits

Initial startup key limits:

- global wake-state keys: max 5
- thread wake-state keys: max 3
- total keys introduced into active context: max 8

Keys are handles, not deep content.
They should remain compact recall hints unless an explicit recall request is
made.

## 6. Recall Scope Rules

Recall must remain bounded and explicit.

### 6.1 Request shape

Initial recall request shape:

```json
{
  "scope": "thread:status-check",
  "reason": "compare previous status check with fresh provider result",
  "requested_keys": ["rk_recent_status"],
  "max_items": 3
}
```

Required fields:

- `scope`
- `reason`
- `max_items`

Optional:

- `requested_keys`

### 6.2 Max items

Initial bounds:

- default `max_items`: 3
- absolute maximum `max_items`: 5

Anything above the maximum must be denied or clamped explicitly by policy.

### 6.3 Scope precedence

Recall should resolve scopes in this order:

1. explicitly requested thread scope
2. explicitly requested project/task scope
3. global scope

Fallback must be narrow.
Failure to resolve a thread scope must not silently expand into broad
cross-scope recall without an explicit policy rule.

### 6.4 Recall and prompt inclusion

Recall does not imply prompt inclusion.

Any recalled item must still pass:

- its own classification
- target-use policy
- prompt/memory eligibility checks

Recalled artifacts remain bounded memory inputs, not automatic prompt context.

Recalled items must also preserve epistemic flavor.
Remembered or derived content must not be presented as freshly checked state.

## 7. Compaction Triggers

Compaction should be explicit and deterministic.

Initial trigger classes:

- event-count threshold
- continuity-stream size threshold
- session boundary

### 7.1 Event-count trigger

When a continuity scope accumulates more than a small bounded number of eligible
events, compaction should be considered.

### 7.2 Stream-size trigger

When the continuity stream for a scope exceeds a configured size threshold,
compaction should be considered even if event count is modest.

### 7.3 Session-boundary trigger

Session end is an acceptable compaction checkpoint for:

- wake-state refresh
- distillate candidate evaluation
- resonate-key creation or refresh

Compaction should not require session end exclusively, but session boundaries
are a reasonable first trigger.

## 8. Implementation direction

The first implementation work after this RFC should be:

1. attach explicit `memory_candidate` policy to continuity-eligible events
2. define the first concrete wake-state JSON schema in code
3. implement bounded global wake-state assembly
4. add a minimal recall contract that returns bounded distillate/key-backed
   results only

The initial implementation should remain small and conservative.
