# Memorybench Hybrid Matrix Report

**Date:** 2026-04-10  
**Status:** targeted benchmark slice landed  
**Scope:** `memory_hybrid_recall` in `-profile extended_fixtures`

## 1. Summary

This change adds the first checked-in benchmark bucket that intentionally
requires both:

- continuity for current governed state
- RAG for broader supporting evidence

The goal was to stop talking about a future hybrid architecture in the abstract
and make it executable enough to measure honestly.

Current targeted result:

- continuity control: `0/3`
- baseline RAG: `0/3`
- stronger RAG: `0/3`
- stronger hybrid: `2/3`

This is the first checked-in bucket where the hybrid path beats both pure
controls instead of merely tying them.

It is still a `targeted_debug_run`, not a promoted headline benchmark.

## 2. Why This Slice Was Needed

The benchmark already had:

- continuity-should-win state fixtures
- poisoning-policy fixtures
- a first RAG-should-win evidence bucket

What was still missing was the realistic middle case:

- the current state is not enough by itself
- broad evidence is not enough by itself
- the answer requires both

Without that bucket, the repo could claim:

- continuity is good at durable state memory
- stronger RAG helps on broad evidence retrieval

but it still could not prove whether a hybrid design actually buys anything
over either control path.

## 3. What Changed

### 3.1 New fixture category and expectations

Files:

- `/Users/adalaide/Dev/loopgate/internal/memorybench/fixtures.go`

Added:

- `CategoryMemoryHybridRecall`
- `HybridRecallExpectation`

The new rubric tracks both halves explicitly:

- required state hints
- required evidence hints
- forbidden state/evidence hints
- whether both halves must be found
- returned-item and hint-byte budgets

Why:

- hybrid success cannot be scored honestly with a single flat retrieval rubric
- we need to know whether a failure came from missing current state, missing
  evidence, or over-expansion

### 3.2 New hybrid fixtures

Files:

- `/Users/adalaide/Dev/loopgate/internal/memorybench/fixtures.go`
- `/Users/adalaide/Dev/loopgate/internal/memorybench/scenario_selection.go`

Added `memory_hybrid_recall` fixtures:

- `hybrid.mount_grant_current_blocker_and_design_rationale.v1`
- `hybrid.replay_recovery_current_step_and_root_cause.v1`
- `hybrid.preview_card_follow_up_and_boundary_rationale.v1`

Added scenario set:

- `hybrid_recall_matrix`

Why:

- these are real “state plus evidence” tasks rather than disguised continuity
  or disguised RAG-only lookups

### 3.3 Shared evidence scope for hybrid retrieval

Files:

- `/Users/adalaide/Dev/loopgate/internal/memorybench/corpus.go`

Added:

- `BenchmarkHybridEvidenceScope`
- `BenchmarkCorpusScope(...)`

Hybrid fixtures now:

- keep continuity state in scenario-specific continuity scope
- put supporting evidence into one shared RAG working-set scope

Why:

- isolated per-scenario corpora make broad retrieval look easier than it is
- hybrid should compete over a real evidence working set, not a toy corpus

### 3.4 Benchmark backend wiring for hybrid

Files:

- `/Users/adalaide/Dev/loopgate/internal/memorybench/backend_config.go`
- `/Users/adalaide/Dev/loopgate/internal/memorybench/types.go`
- `/Users/adalaide/Dev/loopgate/cmd/memorybench/main.go`

Added:

- benchmark-only `hybrid` backend wiring
- explicit retrieval/seed path metadata for hybrid runs
- separate primary continuity discoverer and evidence discoverer

Why:

- the benchmark needed to express “continuity owns current state, RAG owns
  evidence” as an executable path instead of an informal interpretation

### 3.5 Hybrid seeding split

Files:

- `/Users/adalaide/Dev/loopgate/cmd/memorybench/continuity_seeding.go`

Hybrid fixtures now seed:

- state into continuity via production-parity todo workflow paths
- evidence into the RAG corpus only

Why:

- seeding evidence into continuity would fake the comparison
- this keeps the architecture honest: continuity is state memory, not a broad
  evidence dump

### 3.6 Hybrid evaluation path

Files:

- `/Users/adalaide/Dev/loopgate/internal/memorybench/runner.go`
- `/Users/adalaide/Dev/loopgate/internal/memorybench/types.go`

Added:

- `evaluateHybridRecallScenarioFixture(...)`
- explicit missing-state and missing-evidence metrics
- hybrid-specific query builders

Why:

- hybrid needs two retrieval passes and separate failure accounting

### 3.7 Bounded relation-hint combiner

Files:

- `/Users/adalaide/Dev/loopgate/internal/memorybench/runner.go`

The hybrid path now:

1. retrieves current state from continuity
2. keeps a bounded subset of retrieved state hints
3. injects those hints into the evidence lookup query
4. reranks evidence candidates deterministically
5. truncates the evidence side to a small bounded set

Why:

- this is the benchmark-local version of the relational-hint idea
- it tests whether “current state can guide related evidence lookup” helps
  without turning one token into uncontrolled graph expansion

### 3.8 Empty-discovery routing fix for continuity control

Files:

- `/Users/adalaide/Dev/loopgate/cmd/memorybench/main.go`

Bug fixed:

- continuity-only control runs on `hybrid_recall_matrix` were erroring on the
  shared hybrid evidence scope instead of returning zero evidence honestly

Why:

- a control run that crashes on an unseeded scope is not a fair control
- the continuity control now explicitly routes both the per-scenario continuity
  scope and the shared hybrid evidence scope to empty discovery when unseeded

## 4. Tests Added Or Updated

Files:

- `/Users/adalaide/Dev/loopgate/internal/memorybench/fixtures_test.go`
- `/Users/adalaide/Dev/loopgate/internal/memorybench/corpus_test.go`
- `/Users/adalaide/Dev/loopgate/internal/memorybench/scenario_selection_test.go`
- `/Users/adalaide/Dev/loopgate/internal/memorybench/runner_test.go`
- `/Users/adalaide/Dev/loopgate/cmd/memorybench/main_test.go`

Notable coverage:

- extended fixture count and hybrid expectations
- shared hybrid evidence scope without probe leakage
- hybrid scenario-set selection
- hybrid state query and evidence query behavior
- evidence query includes retrieved state hints
- hybrid failure when the evidence half is missing
- hybrid backend wiring
- hybrid path metadata
- continuity-control empty-discovery routing for the shared hybrid evidence scope

Red/green:

```bash
go test ./internal/memorybench ./cmd/memorybench
```

## 5. Targeted Run IDs

Controls:

- `continuity_hybrid_matrix_20260410_v2`
- `rag_baseline_hybrid_matrix_20260410_v2`
- `rag_stronger_hybrid_matrix_20260410_v2`

Hybrid:

- `hybrid_stronger_hybrid_matrix_20260410_v1`
- `hybrid_stronger_hybrid_matrix_20260410_v2`
- `hybrid_stronger_hybrid_matrix_20260410_v3`
- `hybrid_stronger_hybrid_matrix_20260410_v4`

Final reported run:

- `hybrid_stronger_hybrid_matrix_20260410_v4`

## 6. Results

### 6.1 Family-level read

| Backend | Overall | Family average score |
| --- | --- | --- |
| continuity control | `0/3` | `0.6667` |
| baseline RAG | `0/3` | `0.3889` |
| stronger RAG | `0/3` | `0.5000` |
| stronger hybrid | `2/3` | `0.7778` |

### 6.2 Per-scenario read

| Scenario | Continuity | Baseline RAG | Stronger RAG | Stronger hybrid |
| --- | --- | --- | --- | --- |
| Mount grant blocker + design rationale | fail | fail | fail | fail |
| Replay recovery current step + root cause | fail | fail | fail | pass |
| Preview-card follow-up + boundary rationale | fail | fail | fail | pass |

### 6.3 Final hybrid failure note

The remaining failing hybrid scenario is:

- `hybrid.mount_grant_current_blocker_and_design_rationale.v1`

Final reported failure note from `v4`:

> required supporting evidence was missing; hybrid recall missed required state or evidence; stale or irrelevant hybrid context intruded; hybrid recall exceeded the hint-byte budget

Interpretation:

- the continuity side found the right blocker state
- the evidence side still selected the stale demo-status note instead of the
  real renew-path design rationale
- the bounded combiner is helpful, but the design-thread evidence ranking is
  still weak on this one scenario

## 7. What This Proves

This slice proves:

- the harness can now express a real hybrid state-plus-evidence bucket
- continuity-only and RAG-only are both incomplete on this class of task
- bounded state hints can materially improve evidence retrieval quality
- the hybrid path can beat both pure controls on real checked-in fixtures

This slice does **not** prove:

- that the hybrid architecture is solved
- that the current relation-hint combiner is good enough for product use
- that `memory_hybrid_recall` is broad enough yet to be a promoted benchmark

## 8. Pressure Points

The next six-month failure risks are already visible:

- evidence ranking can still pull the wrong thread even when the state anchor
  is correct
- hint-byte budgets will matter if relation-hint expansion is not kept bounded
- the benchmark-local hybrid combiner could drift away from the eventual
  product retrieval shape if it becomes too clever
- if the hybrid bucket grows without stronger negative controls, it will be too
  easy to overfit retrieval heuristics to a small fixed set

## 9. Recommended Next Step

Expand `memory_hybrid_recall` from `3` fixtures to a broader targeted bucket
with at least:

- more design-thread retrieval cases
- more multi-document incident evidence cases
- at least one benign near-miss per hybrid subfamily
- at least one case where the state anchor should suppress an unrelated but
  lexically similar evidence thread

That is the shortest path from “first real hybrid advantage” to a benchmark
bucket strong enough to support product claims.
