**Last updated:** 2026-03-24

# TCL Conflict Anchor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Loopgate-local semantic conflict-key heuristics with TCL-owned conflict anchors and persist `Version + CanonicalKey()` so contradiction handling stays deterministic, fail-closed, and future-compatible with continuity-derived candidates.

**Architecture:** Add a structured `ConflictAnchor` to TCL normalization, validate it strictly, and have Loopgate persist only the anchor version and canonical key. Explicit remembered facts become the first consumer, while wake-state and contradiction resolution switch from local semantic derivation to the persisted `Version + CanonicalKey()` tuple. Legacy records remain anchorless during replay and must not trigger heuristic synthesis.

**Tech Stack:** Go 1.24, `internal/tcl`, Loopgate continuity memory, append-only replay/state snapshots, Go tests with `go test`.

---

## Scope and constraints

- TCL owns semantic anchor derivation.
- Loopgate owns persistence, supersession, retention, ambiguity handling, and replay.
- If TCL emits no anchor, Loopgate must not invent one.
- If TCL emits an invalid anchor, the explicit-memory path fails closed and does not persist.
- Contradiction equality is the tuple `Version + CanonicalKey()`.
- Do not commit unless the user explicitly requests it.

## File structure

### Files to create

- `internal/tcl/conflict_anchor_test.go`
  - unit tests for anchor validation and canonical serialization
- `internal/loopgate/memory_conflict_anchor_test.go`
  - focused Loopgate tests for anchor-driven supersession and coexistence

### Files to modify

- `internal/tcl/types.go`
  - add `ConflictAnchor`, attach it to `TCLNode`, and expose canonical serialization helpers
- `internal/tcl/validator.go`
  - validate anchor presence, charset, required fields, and version rules
- `internal/tcl/normalize.go`
  - emit anchors for explicit facts and return `nil` for unstable slots
- `internal/tcl/normalize_test.go`
  - extend normalization coverage for anchored and unanchored explicit facts
- `internal/loopgate/memory_tcl.go`
  - surface TCL anchor metadata to Loopgate consumption paths
- `internal/loopgate/continuity_memory.go`
  - persist anchor version/key, remove local semantic anchor synthesis, and use persisted tuple in supersession and wake-state winner selection
- `internal/loopgate/continuity_memory_test.go`
  - update wake/replay tests to assert tuple-based contradiction handling and no fallback synthesis
- `internal/loopgate/types.go`
  - add persisted anchor fields to durable/returned fact shapes if needed for state/replay compatibility
- `docs/rfcs/0009-memory-continuity-and-recall.md`
  - note that contradiction slots are now TCL-owned anchors rather than Loopgate-local semantic keys
- `docs/roadmap/roadmap.md`
  - record implementation status once the slice lands

---

## Task 1: Add TCL conflict-anchor type and validation

**Files:**
- Create: `internal/tcl/conflict_anchor_test.go`
- Modify: `internal/tcl/types.go`
- Modify: `internal/tcl/validator.go`

- [ ] **Step 1: Write the failing anchor validation and canonicalization tests**

Create `internal/tcl/conflict_anchor_test.go` with tests for:

```go
func TestConflictAnchorCanonicalKey_V1(t *testing.T) {
	anchor := ConflictAnchor{
		Version:  "v1",
		Domain:   "usr_profile",
		Entity:   "identity",
		SlotKind: "fact",
		SlotName: "name",
	}
	if got := anchor.CanonicalKey(); got != "usr_profile:identity:fact:name" {
		t.Fatalf("expected canonical key usr_profile:identity:fact:name, got %q", got)
	}
}

func TestConflictAnchorCanonicalKey_IncludesFacet(t *testing.T) {
	anchor := ConflictAnchor{
		Version:  "v1",
		Domain:   "usr_preference",
		Entity:   "stated",
		SlotKind: "fact",
		SlotName: "preference",
		Facet:    "ui_theme",
	}
	if got := anchor.CanonicalKey(); got != "usr_preference:stated:fact:preference:ui_theme" {
		t.Fatalf("unexpected canonical key %q", got)
	}
}

func TestValidateNode_RejectsInvalidConflictAnchor(t *testing.T) {
	node := TCLNode{
		ACT: ActionStore,
		OBJ: ObjectMemory,
		STA: StateActive,
		META: TCLMeta{
			ACTOR:  ObjectUser,
			TRUST:  TrustUserOriginated,
			CONF:   8,
			TS:     1,
			SOURCE: "user_input",
		},
		ANCHOR: &ConflictAnchor{
			Version:  "v1",
			Domain:   "usr:profile",
			Entity:   "identity",
			SlotKind: "fact",
			SlotName: "name",
		},
	}
	if err := ValidateNode(node); err == nil {
		t.Fatal("expected invalid delimiter-bearing anchor to fail validation")
	}
}
```

- [ ] **Step 2: Run the TCL tests to verify they fail**

Run: `go test ./internal/tcl/... -run 'TestConflictAnchor|TestValidateNode_RejectsInvalidConflictAnchor' -count=1`

Expected: FAIL because `ConflictAnchor`, `CanonicalKey`, and anchor validation do not exist yet.

- [ ] **Step 3: Implement the minimal anchor type and validation**

Add:

- `ConflictAnchor` to `internal/tcl/types.go`
- `ANCHOR *ConflictAnchor` to `TCLNode`
- `CanonicalKey()` helper that serializes deterministically
- validator rules in `internal/tcl/validator.go`:
  - `Version == "v1"`
  - required `Domain`, `Entity`, `SlotKind`, `SlotName`
  - charset limited to lowercase ASCII, digits, underscore
  - `Facet` optional but same charset rules

- [ ] **Step 4: Run the TCL tests to verify they pass**

Run: `go test ./internal/tcl/... -run 'TestConflictAnchor|TestValidateNode_RejectsInvalidConflictAnchor' -count=1`

Expected: PASS.

---

## Task 2: Emit anchors from explicit fact normalization

**Files:**
- Modify: `internal/tcl/normalize.go`
- Modify: `internal/tcl/normalize_test.go`

- [ ] **Step 1: Write the failing explicit-normalization tests**

Add tests to `internal/tcl/normalize_test.go` for:

```go
func TestNormalizeMemoryCandidate_ExplicitNameGetsConflictAnchor(t *testing.T) {
	node, err := NormalizeMemoryCandidate(MemoryCandidate{
		Source:              CandidateSourceExplicitFact,
		SourceChannel:       "user_input",
		NormalizedFactKey:   "name",
		NormalizedFactValue: "Ada",
		Trust:               TrustUserOriginated,
		Actor:               ObjectUser,
	})
	if err != nil {
		t.Fatalf("normalize explicit name: %v", err)
	}
	if node.ANCHOR == nil || node.ANCHOR.CanonicalKey() != "usr_profile:identity:fact:name" {
		t.Fatalf("expected name anchor, got %#v", node.ANCHOR)
	}
}

func TestNormalizeMemoryCandidate_ExplicitPreferenceThemeGetsConflictAnchor(t *testing.T) {
	node, err := NormalizeMemoryCandidate(MemoryCandidate{
		Source:              CandidateSourceExplicitFact,
		SourceChannel:       "user_input",
		NormalizedFactKey:   "preference.stated_preference",
		NormalizedFactValue: "dark mode",
		Trust:               TrustUserOriginated,
		Actor:               ObjectUser,
	})
	if err != nil {
		t.Fatalf("normalize explicit preference: %v", err)
	}
	if node.ANCHOR == nil || node.ANCHOR.CanonicalKey() != "usr_preference:stated:fact:preference:ui_theme" {
		t.Fatalf("expected ui theme anchor, got %#v", node.ANCHOR)
	}
}

func TestNormalizeMemoryCandidate_UnstablePreferenceHasNoConflictAnchor(t *testing.T) {
	node, err := NormalizeMemoryCandidate(MemoryCandidate{
		Source:              CandidateSourceExplicitFact,
		SourceChannel:       "user_input",
		NormalizedFactKey:   "preference.stated_preference",
		NormalizedFactValue: "I like things better this way",
		Trust:               TrustUserOriginated,
		Actor:               ObjectUser,
	})
	if err != nil {
		t.Fatalf("normalize unstable preference: %v", err)
	}
	if node.ANCHOR != nil {
		t.Fatalf("expected no anchor for unstable preference, got %#v", node.ANCHOR)
	}
}
```

- [ ] **Step 2: Run the normalization tests to verify they fail**

Run: `go test ./internal/tcl/... -run 'TestNormalizeMemoryCandidate_(ExplicitNameGetsConflictAnchor|ExplicitPreferenceThemeGetsConflictAnchor|UnstablePreferenceHasNoConflictAnchor)' -count=1`

Expected: FAIL because normalization does not emit anchors yet.

- [ ] **Step 3: Implement minimal explicit-anchor derivation**

In `internal/tcl/normalize.go`:

- populate `node.ANCHOR` for stable explicit slots:
  - `name`
  - `preferred_name`
  - `preference.favorite_*`
  - `preference.stated_preference` when a stable facet like `ui_theme` or `time_of_day` is derivable
- leave `ANCHOR=nil` when the slot is unstable
- keep the existing dangerous-pattern logic intact

- [ ] **Step 4: Run the normalization tests to verify they pass**

Run: `go test ./internal/tcl/... -run 'TestNormalizeMemoryCandidate_(ExplicitNameGetsConflictAnchor|ExplicitPreferenceThemeGetsConflictAnchor|UnstablePreferenceHasNoConflictAnchor)' -count=1`

Expected: PASS.

---

## Task 3: Persist `Version + CanonicalKey()` in Loopgate explicit-memory flow

**Files:**
- Create: `internal/loopgate/memory_conflict_anchor_test.go`
- Modify: `internal/loopgate/memory_tcl.go`
- Modify: `internal/loopgate/continuity_memory.go`
- Modify: `internal/loopgate/types.go`

- [ ] **Step 1: Write the failing Loopgate explicit-memory tests**

Create `internal/loopgate/memory_conflict_anchor_test.go` with tests for:

```go
func TestRememberMemoryFact_SupersedesOnlyWhenAnchorTupleMatches(t *testing.T)
func TestRememberMemoryFact_CoexistsWhenTCLReturnsNoAnchor(t *testing.T)
func TestRememberMemoryFact_FailsClosedWhenTCLValidationFails(t *testing.T)
```

Assertions:

- `name=Ada` then `name=Grace` supersedes because both normalize to `v1 + usr_profile:identity:fact:name`
- `preference.stated_preference=mornings` then `preference.stated_preference=dark mode` coexist because they normalize to different anchor tuples
- unstable generic preference writes coexist because TCL emits no anchor
- if TCL emits an invalid anchor or validation failure reaches the explicit-remember path, the request is denied, no durable fact is written, and no supersession side effects occur

- [ ] **Step 2: Run the explicit-memory tests to verify they fail**

Run: `go test ./internal/loopgate/... -run 'TestRememberMemoryFact_(SupersedesOnlyWhenAnchorTupleMatches|CoexistsWhenTCLReturnsNoAnchor|FailsClosedWhenTCLValidationFails)' -count=1`

Expected: FAIL because Loopgate still relies on local semantic conflict-key logic and does not yet enforce the new TCL-owned anchor contract end-to-end.

- [ ] **Step 3: Implement minimal Loopgate anchor persistence**

In `internal/loopgate/memory_tcl.go` and `internal/loopgate/continuity_memory.go`:

- expose anchor version/key from TCL analysis
- persist the tuple on explicit remembered facts
- switch explicit supersession lookup from local derived conflict heuristics to persisted tuple equality
- if TCL emits no anchor, do not auto-supersede
- if TCL validation fails for the explicit-memory path, fail closed before persistence and without rewriting or stripping the anchor

If persistent fact structs need fields, add them in the smallest place that survives replay cleanly.

- [ ] **Step 4: Run the explicit-memory tests to verify they pass**

Run: `go test ./internal/loopgate/... -run 'TestRememberMemoryFact_(SupersedesOnlyWhenAnchorTupleMatches|CoexistsWhenTCLReturnsNoAnchor|FailsClosedWhenTCLValidationFails)' -count=1`

Expected: PASS.

---

## Task 4: Replace Loopgate-local wake conflict synthesis with persisted anchor tuples

**Files:**
- Modify: `internal/loopgate/continuity_memory.go`
- Modify: `internal/loopgate/continuity_memory_test.go`

- [ ] **Step 1: Write the failing wake/replay tests**

Add or update focused tests in `internal/loopgate/continuity_memory_test.go` for:

```go
func TestWakeState_UsesPersistedAnchorTupleForConflictResolution(t *testing.T)
func TestWakeState_LegacyAnchorlessFactsDoNotTriggerFallbackSynthesis(t *testing.T)
func TestWakeState_EqualStrengthAnchoredContradictionBecomesAmbiguous(t *testing.T)
```

Assertions:

- wake selection uses persisted `Version + CanonicalKey()`
- legacy or anchorless facts do not fall back to `fact_key`-based local synthesis
- equal-strength same-anchor contradictions are omitted from the resolved winner set

- [ ] **Step 2: Run the wake/replay tests to verify they fail**

Run: `go test ./internal/loopgate/... -run 'TestWakeState_(UsesPersistedAnchorTupleForConflictResolution|LegacyAnchorlessFactsDoNotTriggerFallbackSynthesis|EqualStrengthAnchoredContradictionBecomesAmbiguous)' -count=1`

Expected: FAIL because wake-state code still synthesizes or fills conflict information locally.

- [ ] **Step 3: Implement the minimal winner-selection change**

In `internal/loopgate/continuity_memory.go`:

- remove local semantic derivation from winner selection paths
- use only persisted `Version + CanonicalKey()` for contradiction slots
- leave anchorless facts outside automatic contradiction slots
- preserve the existing winner order inside an anchor slot:
  - eligibility
  - authority lane
  - recency
  - certainty
- preserve deterministic ambiguity handling

- [ ] **Step 4: Run the wake/replay tests to verify they pass**

Run: `go test ./internal/loopgate/... -run 'TestWakeState_(UsesPersistedAnchorTupleForConflictResolution|LegacyAnchorlessFactsDoNotTriggerFallbackSynthesis|EqualStrengthAnchoredContradictionBecomesAmbiguous)' -count=1`

Expected: PASS.

---

## Task 5: Lock replay, docs, and verification

**Files:**
- Modify: `internal/loopgate/continuity_memory_test.go`
- Modify: `docs/rfcs/0009-memory-continuity-and-recall.md`
- Modify: `docs/roadmap/roadmap.md`

- [ ] **Step 1: Write the failing replay/regression test**

Add a replay-oriented regression test:

```go
func TestReplay_AnchorlessLegacyRecordsRemainAnchorless(t *testing.T)
```

Assertion:

- replay preserves stored anchor state exactly
- replay does not synthesize anchor tuples from `fact_key`, `fact_value`, or previous Loopgate heuristics

- [ ] **Step 2: Run the replay/regression test to verify it fails**

Run: `go test ./internal/loopgate/... -run 'TestReplay_AnchorlessLegacyRecordsRemainAnchorless' -count=1`

Expected: FAIL until replay and compatibility paths stop deriving anchors locally.

- [ ] **Step 3: Implement the minimal replay/compat fix**

Adjust replay/compatibility paths so:

- stored anchor tuples are used as-is
- missing tuples remain missing
- no read-time repair or migration synthesis occurs

- [ ] **Step 4: Update docs to match the landed behavior**

Update:

- `docs/rfcs/0009-memory-continuity-and-recall.md`
  - note that contradiction slots are TCL-owned anchors and Loopgate no longer derives semantic anchors locally
- `docs/roadmap/roadmap.md`
  - record that explicit-memory contradiction handling now uses persisted TCL anchor tuples and that anchorless legacy records coexist rather than triggering local fallback logic

- [ ] **Step 5: Run the final verification suite**

Run:

```bash
go test ./internal/tcl/... ./internal/loopgate/... ./cmd/haven/... ./internal/shell/...
```

Expected: PASS with no new failures in TCL, Loopgate, Haven, or shell packages.

---

## Execution notes

- Keep the first code slice explicit-fact only even though the anchor shape is generic.
- Do not widen continuity candidate ingestion in the same implementation pass.
- Do not leave any fallback path in Loopgate that re-derives semantic anchors from fact fields after TCL anchors land.
- If a compatibility shim is unavoidable for test bootstrapping, make it syntactic-only, fail-closed, and explicitly covered by regression tests.
