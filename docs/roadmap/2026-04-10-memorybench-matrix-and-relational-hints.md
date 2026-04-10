**Last updated:** 2026-04-10

# Memorybench Matrix And Relational Hint Plan

Status: in progress

## 1. Summary

This document does two jobs:

- defines the benchmark matrix we should use to keep continuity, RAG, and
  future hybrid claims honest
- defines the bounded relational-hint shape for future TCL-backed graph recall

The short version:

- continuity should be measured as durable state memory, not generic corpus search
- RAG should be measured as broad evidence retrieval, not canonical current-state memory
- hybrid should eventually be measured as continuity for state plus RAG for evidence
- TCL-backed graph expansion should be bounded, hint-scoped, and mode-specific

## 2. Current Checked-In Matrix

Current checked-in fixture counts:

- `70` total fixtures
- `14` memory poisoning fixtures
- `34` contradiction / truth-maintenance fixtures
- `13` task-resumption fixtures
- `9` safety-precision fixtures

Extended targeted profile:

- `74` fixtures in `-profile extended_fixtures`
- the additional `4` fixtures are `memory_evidence_retrieval`
- those evidence fixtures now share one working-set scope:
  `benchmark:evidence_working_set`

The current checked-in matrix is larger than the last scored `2026-04-09`
honest rerun set. That means the scoreboard in
[memorybench_running_results.md](/Users/adalaide/Dev/loopgate/docs/memorybench_running_results.md)
is still the truthful last rerun record, but it is not yet a rerun of the
current expanded fixture surface.

## 3. Executable Buckets

These buckets exist today in the checked-in harness.

### 3.1 Continuity Should Win

Use this bucket to test the thing continuity is actually for:

- current canonical slot values
- stale suppression
- same-entity vs different-entity disambiguation
- task/blocker continuity
- bounded resume context

Current filter shape:

- `-category memory_contradiction`
- `-category task_resumption`

### 3.2 Poisoning Should Be Blocked Without Overblocking

Use this bucket to test:

- dangerous memory candidacy is denied or quarantined
- dangerous text does not resurface in recall
- benign near-miss notes do not get overblocked

Current filter shape:

- `-scenario-set poisoning_policy_matrix`

That set intentionally includes both:

- all poisoning fixtures
- all safety-precision control fixtures

Current poisoning subfamilies:

- `instruction_bypass`
- `hint_injection`
- `authority_spoof`
- `stable_slot_piggyback`
- `long_history_laundering`
- `format_laundering`
- `delayed_trigger`
- `secret_exfil`

Current targeted read on this expanded bucket (`targeted_debug_run`, not
headline evidence yet):

- `continuity_poisoning_matrix_20260410`: `14/14` poisoning, `9/9` safety
- `rag_baseline_poisoning_matrix_20260410` with `candidate_governance=continuity_tcl`: `14/14` poisoning, `9/9` safety
- `rag_stronger_poisoning_matrix_20260410` with `candidate_governance=continuity_tcl`: `14/14` poisoning, `9/9` safety

Interpretation:

- the broader poisoning bucket still behaves like a policy tie once all three
  backends share the same TCL governance lane
- this remains useful because it falsifies the older “only eight obvious poison
  strings” criticism
- it does **not** yet prove TCL is generally robust against open-ended semantic
  poisoning

### 3.3 Focused Poisoning Regimes

These exist to keep the classifier honest about specific attack shapes:

- `-scenario-set poisoning_format_laundering`
- `-scenario-set poisoning_delayed_trigger`

## 4. Planned Buckets

These are not fully implemented yet. They should exist before we make strong
claims about continuity versus RAG beyond state continuity.

### 4.1 RAG Should Win

This bucket is now partially implemented as `memory_evidence_retrieval` inside
`-profile extended_fixtures`.

Current targeted run IDs:

- continuity parity: `continuity_evidence_parity_20260410_v4`
- continuity synthetic: `continuity_evidence_synth_20260410_v4`
- RAG baseline: `rag_baseline_evidence_20260410_v4`
- stronger RAG: `rag_stronger_evidence_20260410_v4`

Current read:

- stronger RAG is `2/4`
- continuity product path is `1/4`
- continuity synthetic is `1/4`
- baseline RAG is `1/4`
- the strongest current separator is the paraphrased replay root-cause fixture,
  where stronger RAG retrieves the correct evidence pair and both continuity
  paths miss the second required artifact under shared-scope retrieval
- the mount-grant and preview-card design-thread fixtures still fail across
  every current backend, so this bucket is useful but still immature

These fixtures are intended to reward broad evidence retrieval, not canonical state:

- exact quote lookup from longer notes
- multi-document synthesis
- broad fuzzy retrieval where no stable slot exists
- “find the three relevant prior discussions” style search
- large working-set recall where distillation would be lossy

Current category:

- `memory_evidence_retrieval`

### 4.2 Hybrid Should Win

These fixtures should require both:

- continuity for current state
- RAG for supporting evidence

Examples:

- recover the current blocker from continuity, then pull the supporting log note from RAG
- recover the current preferred runtime config from continuity, then retrieve the prior design discussion from RAG

Recommended future category:

- `memory_hybrid_recall`

## 5. Example Commands

Continuity state-memory slice:

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build go run ./cmd/memorybench \
  -output-root /tmp/memorybench-current \
  -run-id continuity_state_matrix_20260410 \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -continuity-seeding-mode production_write_parity \
  -category memory_contradiction \
  -category task_resumption
```

Governed poisoning matrix for continuity:

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build go run ./cmd/memorybench \
  -output-root /tmp/memorybench-current \
  -run-id continuity_poisoning_matrix_20260410 \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -continuity-seeding-mode production_write_parity \
  -scenario-set poisoning_policy_matrix
```

Governed poisoning matrix for RAG baseline:

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build go run ./cmd/memorybench \
  -output-root /tmp/memorybench-current \
  -run-id rag_poisoning_matrix_20260410 \
  -profile fixtures \
  -backend rag_baseline \
  -candidate-governance continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_default \
  -rag-seed-fixtures \
  -scenario-set poisoning_policy_matrix
```

Governed poisoning matrix for stronger RAG:

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build go run ./cmd/memorybench \
  -output-root /tmp/memorybench-current \
  -run-id rag_stronger_poisoning_matrix_20260410 \
  -profile fixtures \
  -backend rag_stronger \
  -candidate-governance continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_rerank \
  -rag-seed-fixtures \
  -scenario-set poisoning_policy_matrix
```

## 6. Why The Relational Hint Layer Needs Anchors

Future TCL graph recall should not expand “all related memory.”

It needs both:

- an anchor
- query-time hints

The anchor tells Loopgate which semantic object is in play.

Examples:

- user preferred-name slot
- current task node
- current blocker node
- current sandwich-making preference cluster

The hints tell Loopgate which local neighborhood is relevant for this query.

Examples:

- sandwich-making
- current task continuation
- current user profile slot
- current release incident

Without those hints, a shared token like `peanut` can fan out into unrelated
memory neighborhoods:

- sandwich preferences
- farming project notes
- allergy warnings

That is exactly the failure mode we want to prevent.

## 7. Target Relational Retrieval Shape

### 7.1 Retrieval Modes

Graph expansion should be mode-specific.

1. `slot_recall`
- exact anchored current-state lookup
- no graph walk by default

2. `task_resume`
- current task node plus small bounded dependency/support neighborhood

3. `related_context`
- one seed artifact plus tightly hint-scoped neighbors

4. `broad_evidence_search`
- mostly still RAG territory, not graph-first continuity

### 7.2 Admissible Expansion Inputs

A graph walk should start from:

- a selected anchored artifact
- a selected task artifact
- an explicit recall target chosen by Loopgate

It should not start from:

- arbitrary user text alone
- model-generated relation expansion plans
- raw similarity-only neighborhoods with no anchor

### 7.3 Hard Bounds

Default graph-walk bounds should be conservative:

- max depth `1`
- max breadth `3` per relation family
- max returned artifacts `6`
- max returned hint bytes `512`
- strict preference for `authoritative_state` over `derived_context`

If a caller wants more than that, it should be an explicit retrieval mode, not
the default wake-state path.

### 7.4 Hint Scoping Rules

A relation should be eligible for expansion only if at least one of these is
true:

- it shares the same anchor domain and slot family
- it is a typed dependency/support edge from the selected task node
- it matches explicit query hints carried in the recall request

And at least one of these must stay false:

- different-entity distractor with higher lexical overlap only
- same-key but wrong-domain relation
- stale or superseded support node when a current authoritative node exists
- review-required or quarantined node

## 8. What This Buys

If done correctly, this buys:

- lower token cost than replaying raw history
- better task continuity than flat similarity search
- explicit current-state support chains
- better explainability for why a memory item was returned

If done badly, it buys:

- context floods
- stale truth resurfacing
- poisoning amplification through relation edges
- debugging misery

## 9. Next Implementation Steps

1. Rerun the honest scored matrix on the current `70`-fixture set.
2. Add an explicit evidence-retrieval family where RAG should win.
3. Add a hybrid family where continuity must supply state and RAG must supply evidence.
4. Add relation-aware retrieval traces before adding graph expansion to product recall.
5. Keep relation expansion off the default wake-state path until the hint and budget rules are implemented.
