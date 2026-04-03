**Last updated:** 2026-03-24

# Phase 1 implementation report — TCL memory quality

**Date:** 2026-03-25  
**Plan:** `docs/superpowers/plans/2026-03-25-master-implementation-plan.md` (Phase 1 only)  
**Design reference:** `docs/superpowers/specs/2026-03-23-tcl-conflict-anchor-design.md`

## Summary

Phase 1 (Tasks 1–4) is **complete**: TCL conflict anchors, explicit key normalization before anchor derivation, stable-only anchors for explicit facts, and Loopgate persistence plus wake/replay behavior using TCL-owned anchor tuples. This report lists **all** code and doc changes tied to Phase 1, including work delivered across agent sessions.

## Invariants and security (unchanged intent)

- Loopgate remains the authority boundary; TCL does not grant authority.
- Explicit remember still fails closed when TCL analysis errors (`DenialCodeMemoryCandidateInvalid`).
- Anchor fields remain charset-restricted; delimiter characters in anchor components are rejected at validation.
- No broadening of fuzzy key matching: alias table is explicit and small.

## Code changes by area

### `internal/tcl/`

| File | Change |
|------|--------|
| `types.go` | `ConflictAnchor`, `TCLNode.ANCHOR`, `CanonicalKey()` (existing baseline). |
| `validator.go` | `validateConflictAnchor` / field charset; version `v1` only (existing baseline). |
| `normalize.go` | `deriveExplicitFactConflictAnchor`, unstable preference → `ANCHOR=nil` (existing baseline). **Added** `canonicalizeExplicitFactKey` and application **before** `deriveExplicitFactConflictAnchor` inside `normalizeExplicitFactCandidate`. |
| `conflict_anchor_test.go` | Unit tests for canonical key and invalid anchor rejection (existing baseline). |
| `normalize_test.go` | Tests for anchored name, theme preference, unstable preference without anchor (existing baseline). |
| `key_normalization_test.go` | **New:** `TestNormalizeExplicitFactKey_NameAliasesCollapseToName`, `TestNormalizeExplicitFactKey_PreferredNameAliasesCollapse`, `TestNormalizeExplicitFactKey_PreferenceAliasesCollapseToStableFacet` (via `NormalizeMemoryCandidate` + expected `ANCHOR.CanonicalKey()`). |

**Alias table** (case-insensitive match, canonical output as listed):

- `user_name`, `my_name`, `full_name` → `name`
- `user_preferred_name` → `preferred_name`
- `preference.theme`, `preference.ui_theme` → `preference.stated_preference`
- All other keys: trimmed, passed through unchanged.

### `internal/loopgate/`

| File | Change |
|------|--------|
| `memory_tcl.go` | `memoryConflictAnchorTuple`, explicit remember analysis pipeline (existing baseline). |
| `continuity_memory.go` | Persist `ConflictKeyVersion` / `ConflictKey` on explicit facts; tuple-based supersession; wake `appendRecentFactCandidate` uses persisted tuple; anchorless path uses per-fact slot keys without tuple collision (existing baseline). |
| `types.go` / distillate fact structs | Anchor fields on persisted facts (existing baseline). |
| `memory_conflict_anchor_test.go` | **Renamed/enhanced:** `TestRememberMemoryFact_PersistsTCLConflictAnchorTuple` → **`TestRememberMemoryFact_SupersedesOnlyWhenAnchorTupleMatches`**, now asserts persistence **and** supersession (Ada → Grace) with tombstone on first inspection. Unchanged names: `TestRememberMemoryFact_CoexistsWhenTCLReturnsNoAnchor`, `TestRememberMemoryFact_FailsClosedWhenTCLValidationFails`. |
| `continuity_memory_test.go` | **Renamed:** `TestWakeState_ConflictingDerivedFactsBecomeAmbiguousWhenRecencyAndCertaintyTie` → **`TestWakeState_EqualStrengthAnchoredContradictionBecomesAmbiguous`** (behavior unchanged; aligns with master plan Task 4). Existing tests already match plan names: `TestWakeState_UsesPersistedAnchorTupleForConflictResolution`, `TestWakeState_LegacyAnchorlessFactsDoNotTriggerFallbackSynthesis`, `TestReplay_AnchorlessLegacyRecordsRemainAnchorless`. |

### Derived / continuity paths still using `deriveFactConflictKey`

Provider-derived facts in `continuity_memory.go` still populate `ConflictKey` via **`deriveFactConflictKey`** (legacy string shape for non–explicit-remember distillates). That is **orthogonal** to explicit remember’s TCL tuple persistence; Phase 1 does not remove that helper.

## Documentation updates

| File | Change |
|------|--------|
| `internal/tcl/tcl_map.md` | Documented `canonicalizeExplicitFactKey` and `key_normalization_test.go`. |
| `internal/loopgate/loopgate_map.md` | Documented `memory_tcl.go`, `memoryConflictAnchorTuple`, and `memory_conflict_anchor_test.go` with master-plan test names. |
| `docs/docs_map.md` | Listed `docs/superpowers/reports/` and clarified `plans/` vs `specs/`. |
| `docs/superpowers/plans/2026-03-25-master-implementation-plan.md` | Phase 1 marked **complete**; all Task 1–4 step checkboxes set to `[x]`; pointer to this report. |

## Verification commands

```bash
go test ./internal/tcl/... -count=1
go test ./internal/loopgate/... -run 'TestRememberMemoryFact_|TestWakeState_|TestReplay_|TestConflictAnchor|TestValidateNode_RejectsInvalidConflictAnchor|TestNormalizeExplicitFactKey_|TestNormalizeMemoryCandidate_' -count=1
```

Full package verification (recommended before merge):

```bash
go test ./internal/tcl/... ./internal/loopgate/... -count=1
```

## Follow-ups (not Phase 1)

- **Phase 2+** per master plan: continuity mutation ordering, inspect bounds, etc.
- Optional: extend alias table only with explicit product need + tests (avoid fuzzy expansion).
- Optional: long-term alignment between `deriveFactConflictKey` output and TCL `CanonicalKey()` for derived facts if product requires a single conflict vocabulary.
