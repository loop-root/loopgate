# Memorybench Targeted Matrix Expansion

**Date:** 2026-04-10  
**Status:** targeted expansion landed  
**Scope:** widen `extended_fixtures` and rerun the evidence and hybrid buckets

## 1. Summary

This slice widened the targeted benchmark profile from `77` fixtures to `81`
fixtures by adding:

- `2` new `memory_evidence_retrieval` fixtures
- `2` new `memory_hybrid_recall` fixtures

The new themes were chosen on purpose:

- offline signed-policy cache versus live admin verification
- resolved-path validation versus virtual-path projection

These are both load-bearing trust-boundary problems, not generic retrieval toys.

Current targeted results after the expansion:

- evidence bucket:
  - continuity parity `3/6`
  - continuity synthetic `3/6`
  - RAG baseline `3/6`
  - stronger RAG `4/6`
- hybrid bucket:
  - continuity control `0/5`
  - RAG baseline `0/5`
  - stronger RAG `0/5`
  - stronger hybrid `3/5`

## 2. Why This Slice Was Needed

The prior targeted buckets were still too small:

- evidence was only `4` fixtures
- hybrid was only `3` fixtures

That left too much risk of accidental overfitting or over-interpreting one
retrieval quirk.

The expansion goal was not to make the numbers prettier. It was to make the
negative space more honest:

- more evidence cases where continuity should not win
- more hybrid cases where neither pure control should be enough

## 3. What Changed

### 3.1 New evidence fixtures

Files:

- `/Users/adalaide/Dev/loopgate/internal/memorybench/fixtures.go`
- `/Users/adalaide/Dev/loopgate/internal/memorybench/scenario_selection.go`

Added:

- `evidence.offline_policy_signature_cache_thread.v1`
- `evidence.resolved_path_virtual_projection_thread.v1`

Why:

- the evidence bucket needed more trust-boundary design-thread retrieval cases,
  not just more incident paraphrase cases

### 3.2 New hybrid fixtures

Files:

- `/Users/adalaide/Dev/loopgate/internal/memorybench/fixtures.go`
- `/Users/adalaide/Dev/loopgate/internal/memorybench/scenario_selection.go`

Added:

- `hybrid.offline_policy_follow_up_and_signature_rationale.v1`
- `hybrid.resolved_path_follow_up_and_projection_rationale.v1`

Why:

- these require a current governed state node plus supporting design rationale
- neither continuity-only nor RAG-only should satisfy the full task

### 3.3 Fixture-count and scenario-set updates

Files:

- `/Users/adalaide/Dev/loopgate/internal/memorybench/fixtures_test.go`
- `/Users/adalaide/Dev/loopgate/internal/memorybench/scenario_selection_test.go`
- `/Users/adalaide/Dev/loopgate/cmd/memorybench/main_test.go`

Updated:

- extended profile count from `77` to `81`
- `rag_evidence_matrix` from `4` to `6`
- `hybrid_recall_matrix` from `3` to `5`

### 3.4 Hybrid reranker improvements

Files:

- `/Users/adalaide/Dev/loopgate/internal/memorybench/runner.go`
- `/Users/adalaide/Dev/loopgate/internal/memorybench/runner_test.go`

Changed:

- hybrid relation-token scoring now drops low-signal stop tokens
- hybrid evidence selection now uses greedy complementary coverage instead of
  only individual overlap
- hybrid byte caps were normalized to a consistent bounded envelope for one
  state node plus two evidence nodes

Why:

- the old hybrid combiner was too easy to fool with a near-duplicate distractor
- it often picked one correct note and one wrong note from the same related
  thread because each note was ranked independently

## 4. Tests

Red/green:

```bash
go test ./internal/memorybench ./cmd/memorybench
```

New or widened coverage includes:

- extended fixture-count expectations
- expanded evidence and hybrid scenario-set coverage
- hybrid reranker preference for relation-specific filesystem evidence
- hybrid reranker preference for complementary preview-card evidence instead of
  a demo-only badge note

## 5. Run IDs

Evidence:

- `continuity_evidence_parity_20260410_v5`
- `continuity_evidence_synth_20260410_v5`
- `rag_baseline_evidence_20260410_v5`
- `rag_stronger_evidence_20260410_v5`

Hybrid:

- `continuity_hybrid_matrix_20260410_v5`
- `rag_baseline_hybrid_matrix_20260410_v5`
- `rag_stronger_hybrid_matrix_20260410_v5`
- `hybrid_stronger_hybrid_matrix_20260410_v7`

## 6. Results

### 6.1 Evidence bucket

| Backend | Overall |
| --- | --- |
| continuity parity | `3/6` |
| continuity synthetic | `3/6` |
| RAG baseline | `3/6` |
| stronger RAG | `4/6` |

Stronger RAG passes:

- replay root-cause paraphrase
- Qdrant backfill incident
- offline policy signature cache
- resolved path virtual projection

Still failing across all current backends:

- mount-grant design thread
- preview-card authority-boundary thread

Read:

- the evidence bucket is now broad enough to show more than one honest RAG
  advantage over continuity
- it still is not broad enough to stand as a promoted headline benchmark

### 6.2 Hybrid bucket

| Backend | Overall |
| --- | --- |
| continuity control | `0/5` |
| RAG baseline | `0/5` |
| stronger RAG | `0/5` |
| stronger hybrid | `3/5` |

Hybrid passes:

- replay recovery current step + root cause
- preview-card follow-up + authority-boundary rationale
- offline policy follow-up + signature rationale

Hybrid still fails:

- mount grant blocker + design rationale
- resolved-path follow-up + projection rationale

Read:

- the hybrid path is now better than both pure controls on three distinct
  state-plus-evidence cases
- the remaining failures are consistent: the state anchor is right, but the
  evidence side still chooses the wrong second note inside a related thread

## 7. What This Proves

This slice proves:

- the targeted matrix is materially broader than it was before
- stronger RAG now has multiple honest wins on the evidence-only bucket
- the stronger hybrid path has a real `3/5` advantage over both pure controls

This slice does **not** prove:

- that the current hybrid combiner is strong enough for product use
- that design-thread retrieval is solved
- that the targeted buckets are ready to become promoted scorecards

## 8. Pressure Point

The remaining retrieval weakness is specific and important:

- when two notes are in the right general thread, the hybrid path can still
  pick the wrong second supporting note

That is better than a total miss, but it is still a real failure mode. It
means the next improvement should target:

- design-thread disambiguation inside a related cluster
- not just more raw overlap or more prompt budget

## 9. Recommended Next Step

Add at least one more targeted family that stresses “right thread, wrong second
note” selection, with explicit near-miss distractors, before trying to promote
either targeted bucket.
