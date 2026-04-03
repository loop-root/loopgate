**Last updated:** 2026-03-24

# TCL Conflict Anchor Design

**Status:** Approved

**Goal:** Define a TCL-owned conflict-anchor shape that can represent contradiction slots for explicit remembered facts and future continuity-derived candidates without forcing all memory artifacts into a key-value fact model.

**Summary:** TCL should emit structured semantic slot metadata as part of normalization. Loopgate should consume that metadata for persistence and contradiction handling, but Loopgate must not become the semantic authority that invents or repairs anchors locally. When TCL cannot derive a stable anchor, the candidate should remain unanchored and coexist rather than being forced into a false contradiction slot.

---

## Problem

The current contradiction handling path in Loopgate can compare memories using locally derived conflict keys. That is too narrow for the TCL bridge direction and too fragile for future continuity-derived memory candidates.

The practical requirement is broader:

- explicit fact slots must fit naturally
- task transitions and todo lifecycle changes must fit naturally
- continuity observations must fit naturally
- the shape must not assume everything is a `fact_key=fact_value` pair
- semantic slot derivation should live in TCL normalization, not in Loopgate heuristics

If continuity-derived examples feel awkward in the shape, the shape is too narrow.

## Design goals

- Represent contradiction slots across explicit facts, transitions, and observations.
- Keep the authoritative internal representation structured.
- Provide a deterministic canonical string key for storage and comparison.
- Keep TCL responsible for semantic anchor derivation.
- Keep Loopgate responsible for governance, supersession, retention, and winner selection.
- Fail closed when a stable anchor cannot be derived.
- Avoid forcing continuity-derived candidates into a fake key-value model.

## Non-goals

- This design does not make TCL an authority source.
- This design does not widen memory ingestion by itself.
- This design does not attempt to classify every possible user utterance.
- This design does not require Loopgate to reconstruct semantic anchor structure from stored keys.
- This design does not replace the current eligibility, authority-lane, recency, and certainty winner policy.

## Proposed anchor shape

The authoritative internal shape should be a TCL-owned structured descriptor:

```go
type ConflictAnchor struct {
	Version  string
	Domain   string
	Entity   string
	SlotKind string
	SlotName string
	Facet    string
}
```

Each populated anchor must serialize to a canonical comparison key:

`<domain>:<entity>:<slot_kind>:<slot_name>[:<facet>]`

Examples:

- `usr_profile:identity:fact:name`
- `usr_preference:favorite:fact:favorite_coffee`
- `task:lifecycle:transition:status`
- `session:session_ctx:state:current_focus`

For v1, canonical serialization is collision-safe because each populated field
must use only:

- lowercase ASCII letters
- digits
- underscore

Delimiter characters such as `:` are forbidden inside anchor fields.

For contradiction handling, equality must use the tuple:

- `Version`
- `CanonicalKey()`

That means the canonical key string is the semantic slot key, while version
still participates in replay-safe equality. Loopgate should persist both values
if version-aware comparison is needed during replay or future migrations.

### Field meanings

- `Domain`: the broad semantic area, such as `usr_profile`, `task`, `session`, or `workspace`
- `Entity`: the thing or subdomain that owns the slot, such as `identity`, `favorite`, `lifecycle`, or `session_ctx`
- `SlotKind`: the category of contradiction slot, such as `fact`, `transition`, `state`, or `event`
- `SlotName`: the concrete slot axis being competed on, such as `name`, `status`, or `current_focus`
- `Facet`: an optional refinement for cases where a slot needs a sub-axis, such as `ui_theme` or `time_of_day`
- `Version`: the anchor schema version, starting at `v1`

## Why this shape

This shape is intentionally wider than a fact-only attribute model.

It works because:

- explicit remembered facts still map naturally into `fact` slots
- task lifecycle changes map naturally into `transition` slots
- continuity observations map naturally into `state` or `observation` slots
- the canonical key remains compact and deterministic
- the structured representation preserves meaning that would be lost in a flat string-only internal model

This avoids the failure mode where continuity-derived memory is awkwardly translated into fake fact attributes just to fit a narrow contradiction system.

## Placement in TCL

The anchor should be attached to the normalized TCL node, not created by policy output and not inferred by Loopgate.

Proposed addition:

```go
type TCLNode struct {
	ID       string
	ACT      Action
	OBJ      Object
	QUAL     []Qualifier
	OUT      Action
	STA      State
	REL      []TCLRelation
	META     TCLMeta
	ANCHOR   *ConflictAnchor
	DECISION *TCLDecision
}
```

### Why `TCLNode`

- The anchor is part of normalization semantics.
- Policy may inspect the anchor, but it should not author it.
- Loopgate should consume the normalized result, not reinterpret semantics from raw inputs.

## Flow and ownership

1. Loopgate builds a `tcl.MemoryCandidate`.
2. TCL normalization emits a `TCLNode` and optionally an `ANCHOR`.
3. TCL validation checks the anchor shape if present.
4. TCL signatures and policy run on the node.
5. Loopgate persists the canonical anchor key derived from `ANCHOR.CanonicalKey()`.
6. Loopgate winner selection uses the persisted canonical anchor key.
7. If no anchor is present, Loopgate does not invent one.

### Authority split

- TCL owns semantic anchor derivation.
- Loopgate owns persistence, denial, supersession, retention, recall eligibility, and winner policy.
- Absence of an anchor means "no stable contradiction slot was derived," not "Loopgate should guess."

### Validation boundary

The trust boundary must stay explicit:

- untrusted callers do not submit `ConflictAnchor` directly to Loopgate
- TCL normalization constructs the anchor
- TCL validation validates the structured anchor before it can be consumed
- Loopgate may verify that a persisted canonical key is syntactically safe for storage, but it must not repair or reinterpret malformed anchors
- malformed or inconsistent anchors are rejected or treated as absent, never patched into a new semantic slot by Loopgate

## Normalization behavior

Normalization should be conservative and type-aware.

### Explicit facts

Emit anchors when the fact names a stable contradiction slot.

Examples:

- `name=ada` -> `usr_profile:identity:fact:name`
- `preferred_name=adi` -> `usr_profile:identity:fact:preferred_name`
- `preference.favorite_coffee=oat cappuccino` -> `usr_preference:favorite:fact:favorite_coffee`
- `preference.stated_preference=dark mode` -> `usr_preference:stated:fact:preference:ui_theme`

Generic remembered preferences may use `Facet` only when TCL can derive a stable semantic axis with sufficient confidence.

### Task transitions

Transitions should remain transitions.

Examples:

- task opened -> `task:lifecycle:transition:status`
- task closed -> `task:lifecycle:transition:status`
- todo reopened -> `task:lifecycle:transition:status`
- task priority changed -> `task:lifecycle:transition:priority`

This lets successive lifecycle changes compete on a shared slot without pretending they are static facts.

### Continuity observations

Observations should remain observation-derived state slots.

Examples:

- current session focus observed -> `session:session_ctx:state:current_focus`
- active workspace repo observed -> `workspace:session_ctx:state:active_repo`
- current blocker status observed -> `session:session_ctx:state:blocker_status`

### No-anchor cases

If TCL cannot derive a stable contradiction slot without inventing one, it must emit `ANCHOR=nil`.

Examples:

- vague affective notes such as "user seemed frustrated"
- mixed conversational content that plausibly maps to multiple slots
- generic preference prose without a stable semantic axis

For this design, no anchor means:

- no automatic contradiction slot
- no Loopgate fallback anchor synthesis
- coexistence until a later candidate or future classifier can derive a stable slot

## Validation contract

Validation should enforce:

- `Version` is required and must be `v1` for the first implementation slice
- `Domain`, `Entity`, `SlotKind`, and `SlotName` are required when `ANCHOR` is present
- all populated fields are normalized lowercase snake_case
- `Facet` is optional but must follow the same normalization if present
- canonical serialization is deterministic
- only `a-z`, `0-9`, and `_` are allowed inside serialized fields
- malformed anchors fail validation rather than silently degrading into Loopgate heuristics

This preserves the rule that TCL owns semantic interpretation.

## Persistence contract

For the first implementation slice, Loopgate should persist:

- the anchor version
- the canonical key string

Recommended persistence behavior:

- TCL emits the structured anchor
- TCL exposes `CanonicalKey()`
- Loopgate stores the anchor version and canonical key on explicit remembered facts and continuity-derived facts that already carry structured fact records
- contradiction equality uses `Version + CanonicalKey()`, not the canonical key string alone
- Loopgate uses the stored canonical key for contradiction handling
- Loopgate does not reconstruct the structured anchor from the string during normal operation

This keeps the operational contract small and deterministic while preserving room for richer diagnostics later.

## Loopgate consumption rules

Loopgate should follow these rules once TCL anchors exist:

- explicit remembers supersede only when both `Version` and `CanonicalKey()` match
- explicit remembers with empty anchor keys do not auto-supersede
- wake-state and recall conflict resolution use the persisted `Version + CanonicalKey()` tuple
- equal-lane contradictions still use the existing winner order:
  - eligibility
  - authority lane
  - recency
  - certainty
- if candidates share an anchor key but remain equal-strength and contradictory, the slot is treated as ambiguous rather than resolved arbitrarily

### Ambiguous outcome contract

For the first slice, ambiguity must have a deterministic operational meaning:

- both underlying records remain durably stored
- no automatic winner is selected for wake-state or auto-surface paths
- the shared contradiction slot is omitted from the resolved winner set
- if ambiguity is surfaced later, its ordering must derive from stable persisted order rather than insertion timing quirks

This prevents a hidden tie-breaker from reappearing through replay, map iteration, or UUID order.

## Error handling

- invalid anchor shape must fail in TCL validation
- explicit-memory requests whose TCL anchor fails validation fail closed and are not persisted
- broader continuity candidates whose TCL anchor fails validation must not be promoted into contradiction-aware durable memory until they are re-derived successfully
- Loopgate must not repair malformed anchors or silently continue with a rewritten anchor
- audit/log paths should record only safe reason codes and bounded canonical key strings where needed
- raw source text must not be included in anchor-related denial output

## Testing strategy

Tests should be split into three layers.

### `internal/tcl`

- anchor validation accepts valid anchors and rejects malformed anchors
- canonical serialization is deterministic
- normalization emits expected anchors for explicit facts
- normalization emits expected anchors for task transitions and continuity observations once those candidate types are wired
- normalization emits `nil` when the slot is unstable

### `internal/loopgate`

- explicit remembers supersede only when TCL anchor keys match
- explicit remembers coexist when TCL emits no anchor
- wake winner selection uses persisted anchor keys rather than local semantic heuristics
- contradictory equal-strength derived candidates with the same anchor become ambiguous

### Regression boundary

- if TCL emits no anchor, Loopgate must not invent one
- if persisted anchor key is empty, contradiction handling must not silently fall back to `fact_key`
- previous heuristic local anchor derivation should be removed or reduced to compatibility-only migration paths that are clearly bounded and tested

## Replay and migration rules

Replay and migration are part of the contract, not an implementation detail.

Rules:

- replay uses only persisted canonical anchor keys and persisted lineage state
- replay must not synthesize anchors from `fact_key`, `fact_value`, or other legacy fields
- legacy records without anchors remain anchorless during replay
- anchorless legacy records may coexist even if a human could infer they compete
- any migration path that backfills anchors must run as an explicit rewrite or explicit mutation step, never as implicit read-time repair
- mixed populations of anchored and unanchored records must behave deterministically, with anchored records participating in contradiction slots and unanchored records remaining outside those slots unless explicitly rewritten

## Security and invariant impact

### Correctness

The main correctness risk is false contradiction. This design avoids that by preferring no anchor over a guessed anchor.

### Security

The anchor is semantic metadata, not authority. It must not:

- bypass policy
- create eligibility
- directly cause persistence
- override governance outcomes

### Determinism

Canonical serialization and strict validation are required so anchor comparison remains deterministic across restarts and replay.

For v1, anchor vocabulary must also remain stable:

- `Version` is explicit
- supported anchor field strings should come from a narrow closed vocabulary per candidate type
- changing the semantic meaning of an existing field string is a schema change, not a casual normalization tweak

This prevents silent semantic drift where the same real-world slot stops matching across TCL revisions.

### Auditability

Supersession and ambiguity decisions remain Loopgate-owned and auditable. Anchors only help define which memories compete.

### Concurrency

This design does not add new background workers or asynchronous resolution paths. It preserves existing synchronous mutation and replay patterns.

## Open implementation notes

- The first implementation slice should wire this through explicit fact normalization first, even though the shape is generic enough for future continuity-derived candidates.
- Future work can add richer TCL-derived anchors for broader continuity candidate ingestion without changing the anchor structure itself.
- If future diagnostics require it, Loopgate can later persist the structured parts alongside the canonical string, but that is not required for the first slice.

## Recommendation

Adopt the structured `ConflictAnchor` design with a canonical serialized key and attach it to `TCLNode`.

This gives TCL a native way to describe contradiction slots across:

- explicit facts
- task transitions
- continuity observations

without forcing continuity-derived memory into a fact-only model, while preserving Loopgate's authority over governance and lifecycle.
