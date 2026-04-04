**Last updated:** 2026-03-24

# RFC 0009: Memory Continuity, Wake State, and Recall

Status: draft

## 1. Summary

Operator-visible persistent memory should behave like a continuity stream with controlled
compaction, not a mutable database of model-owned facts.

The system should:

- preserve continuity across sessions
- avoid replaying full raw history into every prompt
- keep durable memory as derived, provenance-bearing artifacts
- keep Loopgate as the authority over what may become long-term memory

This RFC defines the intended conceptual model and the first schema layer for:

- continuity events
- continuity threads
- wake state
- distillates
- resonate key index entries
- recall requests and responses

This RFC does not introduce semantic search, mutable fact stores, or unrestricted
history replay.

## 2. Goals

- preserve cross-session continuity without replaying full raw history
- keep durable memory append-only and auditable
- make durable memory artifacts derived, explicit, and reviewable
- distinguish remembered information from freshly checked information
- let the operator client start from a compact wake state instead of a blank session
- allow deeper recall only through explicit Loopgate-governed requests

## 2.1 Design bias

Memory artifacts should prefer structured facts over prose.

Prose summaries may exist, but they are secondary helpers.
They must not become the primary durable memory representation when a more
structured form is available.

## 3. Non-goals

This RFC does not define:

- a mutable database of trusted facts
- unrestricted semantic search over all prior content
- model-owned memory truth
- automatic promotion of summaries into trusted memory
- raw ledger replay into every prompt
- direct model access to deep memory stores

## 4. Core model

The intended model has three layers.

### 4.1 Active context window

This is the small working set currently visible to the model.

It is the "conscious" slice of state, not the full memory corpus.

It may contain:

- recent conversation turns
- current task state
- current safe structured tool results
- compact wake-state content
- a few selected resonate keys

It must remain small and bounded.

It must not silently expand just because more continuity exists on disk.

### 4.2 Continuity stream

The continuity stream is the append-only sequence of memory-relevant events
across time.

It is:

- durable
- replayable
- auditable
- not limited to a single process session

Older material may roll out of the active context window without being lost.

Recall from the continuity stream is explicit.
It must not imply prompt inclusion by default.

### 4.3 Distillation / long-term recall

Older continuity material may be compacted into durable derived artifacts:

- wake states
- distillates
- resonate keys

These artifacts are governed by Loopgate memory policy and provenance rules.

They are not trusted just because they are concise or summarized.

Derived memory should prefer structured fields and explicit metadata over large
freeform narrative text.

## 5. Trust rules

Memory is a trust target.

The same architectural rules that govern capabilities and promotion also apply
to durable memory.

Required invariants:

- the model does not directly decide what becomes long-term memory
- durable memory artifacts must be derived and provenance-bearing
- raw quarantined content must not silently flow into durable memory
- recall must go through a governed contract, not direct model access
- remembered information and freshly checked information must remain
  distinguishable in final answers
- no memory artifact may gain more trust than its inputs and derivation policy
  justify

Consequences of the trust ceiling rule:

- summaries do not become truth just because they are concise
- recalled items do not become prompt-safe just because they were stored before
- distillates are not automatically authoritative
- memory remains classified, provenance-bearing, and bounded

Loopgate governs:

- which continuity inputs are memory candidates
- which derived artifacts are eligible for distillation
- which memory artifacts may be recalled for a given use

The operator client governs:

- active context assembly
- local memory ownership and session continuity
- rendering remembered vs fresh information clearly

## 5.1 TCL classification bridge

Thought Compression Language (TCL) may serve as a derived semantic
classification layer ahead of durable-memory decisions.

Its intended role is:

- normalize memory candidates into a compact semantic form
- classify value, risk, and poisoning-like patterns
- derive semantic signatures for matching against known dangerous families
- recommend keep / drop / flag / quarantine / review dispositions

Important boundary:

- TCL is not canonical durable memory
- TCL is not an authority source
- TCL does not by itself make a candidate eligible for persistence
- Loopgate remains the authority over whether a candidate becomes a distillate,
  explicit remembered fact, review item, quarantine item, or denial

Initial implementation should wire TCL to explicit memory writes first.
Once that path is stable and tested, Loopgate may also classify conservative
continuity-derived fact candidates to persist stable anchor tuples and semantic
signatures on the resulting durable artifacts.

Current implementation status:

- explicit `memory.remember` writes flow through TCL before persistence
- continuity-derived provider-fact candidates may persist TCL-derived anchor
  tuples and semantic signatures when classification succeeds
- unsupported continuity-derived facts may still persist as unanchored facts,
  but they should not receive synthetic contradiction slots
- model-proposed TCL, relation graphs, and richer semantic links remain future
  work

## 5.2 Conflict anchors and contradiction slots

Contradiction handling requires a stable notion of "same semantic slot."

Two memories compete only when they share the same non-empty TCL-derived
conflict anchor tuple.

The anchor should be derived conservatively as:

- version
- domain
- entity
- slot kind
- slot name
- optional facet

Loopgate may persist and compare the tuple as:

`version + canonical_key`

Examples:

- `v1 + usr_profile:identity:fact:name`
- `v1 + usr_preference:favorite:fact:favorite_coffee`
- `v1 + usr_preference:stated:fact:preference:time_of_day`
- `v1 + usr_preference:stated:fact:preference:ui_theme`

Important rules:

- facts with different anchor tuples do not supersede each other
- facts with an empty or missing anchor tuple do not auto-compete and should
  coexist until a later classifier can derive a stable anchor
- generic slots such as `preference.stated_preference` must derive the
  semantic facet from the remembered value; if the anchor is not stable enough
  to determine, the system should prefer coexistence over false conflict
- winner selection should only run after anchor derivation succeeds
- Loopgate must not synthesize contradiction anchors locally during wake
  assembly, replay, or legacy compatibility reads

## 6. Continuity scopes

Memory should not be one giant undifferentiated stream.

The system should support multiple continuity scopes:

- global continuity
- thread continuity
- project or task continuity
- session-local working continuity

Wake-state assembly may combine:

- one global wake state
- one current-thread wake state
- a small number of relevant resonate keys

This keeps continuity lanes separate without implying parallel execution.

## 7. Artifact types

### 7.1 Continuity event

A continuity event is the smallest durable memory-relevant unit.

It is append-only and references source lineage rather than mutating history.

Proposed shape:

```json
{
  "id": "ce_20260308_000001",
  "ts_utc": "2026-03-08T18:00:00Z",
  "scope": "thread:status-check",
  "session_id": "s-123",
  "type": "tool.result.summary",
  "source_refs": [
    {
      "kind": "ledger_event",
      "ref": "ledger:line:812",
      "sha256": "..."
    }
  ],
  "classification": {
    "memory_candidate": true,
    "prompt_eligible": false,
    "quarantined": false
  },
  "payload": {
    "capability": "statuspage.summary_get",
    "summary": "GitHub reports all systems operational."
  }
}
```

### 7.2 Continuity thread

A continuity thread is a logical lane for related events.

It is not a mutable summary blob.
It is an indexable scope descriptor.

Proposed shape:

```json
{
  "thread_id": "thread:status-check",
  "scope_type": "thread",
  "created_at_utc": "2026-03-08T18:00:00Z",
  "last_event_at_utc": "2026-03-08T18:07:00Z",
  "tags": ["status", "github"],
  "active": true
}
```

### 7.3 Wake state

A wake state is the compact startup checkpoint used to restore continuity
without replaying full history.

It must remain structured and small.

Wake state is not a replacement for deep recall.
It is a bounded startup checkpoint only.

Proposed shape:

```json
{
  "id": "wake_global_20260308T180700Z",
  "scope": "global",
  "created_at_utc": "2026-03-08T18:07:00Z",
  "persona_ref": "persona:morph@v1",
  "source_refs": [
    {"kind": "distillate", "ref": "dist_abc"},
    {"kind": "resonate_key", "ref": "rk_xyz"}
  ],
  "active_goals": [
    "monitor public service health"
  ],
  "unresolved_items": [
    "follow up on repo issue triage"
  ],
  "recent_facts": [
    {
      "fact": "GitHub status was operational at last check",
      "source_ref": "dist_abc"
    }
  ],
  "resonate_keys": ["rk_xyz", "rk_123"]
}
```

Wake states are derived artifacts.
They are not freeform memory dumps.

Required wake-state constraints:

- bounded number of active goals
- bounded number of unresolved items
- bounded number of recent facts
- bounded number of resonate keys
- no large prose sections
- no raw quarantined or blob-backed content

If a candidate wake-state assembly exceeds those limits, it must be compacted or
trimmed deterministically rather than expanded.

### 7.4 Distillate

A distillate is a durable derived memory artifact created from eligible
continuity inputs.

It compresses older continuity into a recallable unit while preserving
lineage and use constraints.

Structured facts are the primary content of a distillate.
Compact summary text is optional and secondary.

Proposed shape:

```json
{
  "id": "dist_abc",
  "scope": "thread:status-check",
  "created_at_utc": "2026-03-08T18:05:00Z",
  "source_refs": [
    {"kind": "continuity_event", "ref": "ce_20260308_000001"}
  ],
  "tags": ["status", "github"],
  "facts": [
    {
      "name": "status_indicator",
      "value": "none",
      "value_class": "enum"
    }
  ],
  "summary_text": "GitHub reported all systems operational during the checked interval.",
  "classification": {
    "display": true,
    "prompt": false,
    "memory": true
  }
}
```

Important:

- distillates are not trusted because they are summarized
- distillates are derived artifacts with provenance and classification
- summary text must not replace structured facts when structured facts are
  available

### 7.5 Resonate key index entry

A resonate key is a lightweight recall handle.

It points to a deeper durable artifact but does not grant unrestricted access.

Proposed shape:

```json
{
  "id": "rk_xyz",
  "scope": "thread:status-check",
  "created_at_utc": "2026-03-08T18:06:00Z",
  "target_ref": "dist_abc",
  "kind": "distillate",
  "tags": ["status", "github"],
  "hint": "Most recent GitHub status check"
}
```

Important:

- a resonate key is a recall handle, not an authority token
- Loopgate still decides what can be reopened from a key
- returning a resonate key or recalled item does not automatically make it
  prompt-eligible
- a resonate key may later carry bounded semantic compression derived from TCL,
  but it must reconstruct only the general contour of memory rather than full
  raw prose

## 7.6 Supersession retention and compaction

Supersession is not immediate deletion.

When a newer memory wins a contradiction slot:

- the losing memory should remain in the append-only event log
- the losing inspection lineage should be marked tombstoned
- lineage should retain a pointer to the replacement inspection, distillate, and
  resonate key where available
- the winning inspection may record which prior inspection it superseded

Compaction is a separate later step.

For the current implementation, the built-in default supersession retention
window is 30 days before a superseded lineage may be purged.

This window is intentionally fixed for now so contradiction handling does not
depend on ad hoc magic numbers or per-call caller choice. It can become
runtime-configurable later once operators have enough usage evidence.

## 8. Recall contract

Recall should be explicit and mediated.

The model should not receive deep memory by default.

Recall is not equivalent to prompt inclusion.
Recalled artifacts must still be checked against their own classification and
use policy before entering active prompt context.

### 8.1 Recall request

Proposed shape:

```json
{
  "scope": "thread:status-check",
  "reason": "compare previous outage state to freshly checked provider state",
  "requested_keys": ["rk_xyz"],
  "max_items": 3
}
```

### 8.2 Recall response

Proposed shape:

```json
{
  "scope": "thread:status-check",
  "items": [
    {
      "kind": "distillate",
      "ref": "dist_abc",
      "summary_text": "GitHub reported all systems operational during the checked interval.",
      "classification": {
        "display": true,
        "prompt": false,
        "memory": true
      },
      "source_refs": [
        {"kind": "continuity_event", "ref": "ce_20260308_000001"}
      ]
    }
  ]
}
```

Recall responses should remain:

- bounded
- provenance-bearing
- target-use classified
- non-prompt by default unless a separate policy step says otherwise

## 9. What enters the continuity stream

Only meaningful, policy-eligible, memory-candidate inputs should enter the
continuity stream.

Likely inputs:

- session lifecycle events
- explicit task or goal transitions
- provider-backed structured results that pass memory policy
- operator-approved derived memory artifacts
- important denials or approvals when relevant to continuity

Likely exclusions:

- raw quarantined content
- arbitrary freeform remote text
- generic model chatter without durable continuity value
- internal runtime noise

Default rule:

- absence of explicit memory-candidate eligibility is a denial

Memory-candidate status should be attached intentionally, not inferred from
event presence alone.

## 10. Distillation eligibility

Not every continuity event becomes a distillate.

Eligibility should be governed by explicit policy and provenance rules.

Distillation candidates should be:

- memory-candidate events
- derived from non-quarantined or explicitly memory-safe artifacts
- small enough to compact deterministically
- explainable in lineage terms
- shaped enough to become structured facts or bounded memory records

Distillation must not silently bless:

- quarantined raw content
- tainted text not approved for memory use
- model-authored freeform speculation

Memory-candidate events should prefer:

- bounded scalar values
- explicit task/goal state
- operator-approved derived memory artifacts
- stable provider-backed facts with provenance

Memory-candidate events should avoid:

- large free text
- tainted display-only content
- raw remote prose
- unreviewed model-generated summaries

## 11. Wake-state requirements

A wake state must contain enough structure to provide continuity without
becoming a new giant summary blob.

Required properties:

- small and bounded
- structured
- provenance-bearing
- easy to distinguish from fresh provider data

A wake state should not:

- contain raw quarantined content
- contain unrestricted deep history
- act as a mutable memory database

Default startup assembly rule:

- load one global wake state
- optionally load one active-thread wake state when the current task or thread
  is known
- include a small bounded set of resonate keys
- do not load multiple thread wake states by default

This keeps startup continuity comprehensible and bounded.

## 12. Remembered vs fresh truth boundary

The operator client must distinguish:

- remembered historical continuity
- newly checked provider or system state

The operator client should also avoid collapsing these different kinds of memory claims into
one undifferentiated truth bucket:

- what was observed
- what was inferred
- what is currently true

These are different epistemic states.

Example:

- observed: "On 2026-03-08, GitHub status reported all systems operational."
- inferred: "This likely means the outage resolved."
- currently true: "I just checked again and GitHub currently reports all systems operational."

Example operator-facing answer shape:

- "Previously, we discussed a GitHub outage."
- "I just checked GitHub status again and it now reports all systems operational."

This distinction is part of the trust model, not just a UX preference.

Remembered information may inform what to check next.
It must not be presented as freshly verified state.

Memory artifacts should preserve enough epistemic flavor to keep those states
distinct.
Initial useful categories are:

- remembered
- derived
- freshly checked

## 13. Storage model

The intended storage split remains simple:

- append-only JSONL for continuity-like streams
- structured JSON for wake states, distillates, and resonate-key indices
- YAML for persona and configuration only

This is conceptually stream-like, but it does not require Kafka or a message
broker.

## 14. Immediate implementation direction

The next design and implementation steps should be:

1. define a continuity-event schema aligned with the existing ledger/distillate
   model
2. define a small wake-state schema and startup load path
3. define a bounded recall request/response contract
4. tighten distillate eligibility rules before deep recall grows

This RFC is intentionally ahead of the current implementation.
It should guide memory evolution without implying that the full recall system
already exists.
