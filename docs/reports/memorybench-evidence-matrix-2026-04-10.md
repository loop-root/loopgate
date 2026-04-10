**Last updated:** 2026-04-10

# Memorybench Evidence Matrix Report (2026-04-10)

## Summary

This report records the first checked-in `RAG should win` benchmark bucket for
memorybench.

The result is useful but deliberately narrow:

- stronger RAG now beats both continuity paths on one broad evidence fixture
- continuity still wins the main scored state-memory matrix
- the new evidence bucket is informative, but not yet mature enough to promote
  into the headline scoreboard

## What changed

### 1. Added a separate evidence-retrieval profile

The benchmark now has:

- the existing `fixtures` profile for the scored `70`-fixture state-memory matrix
- a new `extended_fixtures` profile for targeted evidence-retrieval work

Why:

- the shipped continuity scoreboard should stay stable
- evidence retrieval is a different benchmark problem from canonical state continuity
- mixing both into one headline denominator would make the repo less honest

### 2. Added a new benchmark category

New category:

- `memory_evidence_retrieval`

Current checked-in evidence fixtures:

- `evidence.semantic_paraphrase_replay_batch_root_cause.v1`
- `evidence.multi_document_mount_grant_design_thread.v1`
- `evidence.incident_qdrant_backfill_socket_stall.v1`
- `evidence.preview_card_authority_boundary_thread.v1`

Each fixture now carries:

- required evidence hints
- forbidden distractor hints
- max returned-item budget
- max retrieved-byte budget

Why:

- broad evidence retrieval needs different scoring than contradiction or task continuity
- the harness needs to punish wrong-context evidence, not just missing state

### 3. Added a dedicated targeted scenario set

New built-in scenario set:

- `rag_evidence_matrix`

Why:

- evidence runs should be easy to rerun without hand-editing fixture lists
- targeted evidence work should stay separate from promoted headline runs

### 4. Added evidence retrieval scoring

The runner now evaluates evidence fixtures by:

- whether required hints were found
- whether forbidden hints intruded
- whether the backend stayed within item and byte budgets

Why:

- evidence retrieval is about finding the right supporting material with bounded context
- it is not the same thing as recovering one canonical current value

### 5. Changed evidence fixtures to a shared working-set scope

All evidence fixtures now share:

- `benchmark:evidence_working_set`

Why:

- isolated per-scenario scopes were too easy and too artificial
- RAG should be tested on one broad working set, not four tiny three-document corpora
- continuity should have to compete on the same broader search surface if we want an honest negative-space bucket

### 6. Kept continuity evidence seeding honest

Evidence fixtures are seeded into continuity as:

- benchmark-local fixture-ingest projected nodes only

They are **not** promoted to:

- remembered facts
- observed-thread authoritative facts
- todo workflow state

Why:

- continuity does not yet have a first-class broad evidence memory lane
- promoting evidence fixtures into authoritative memory state would overstate the product

## Final targeted runs

Run IDs:

- continuity parity: `continuity_evidence_parity_20260410_v4`
- continuity synthetic: `continuity_evidence_synth_20260410_v4`
- RAG baseline: `rag_baseline_evidence_20260410_v4`
- stronger RAG: `rag_stronger_evidence_20260410_v4`

Counts:

| Backend | Overall | Replay root cause | Mount-grant thread | Qdrant incident | Preview-card thread |
| --- | --- | --- | --- | --- | --- |
| `continuity_tcl` (`production_write_parity`) | `1/4` | `0/1` | `0/1` | `1/1` | `0/1` |
| `continuity_tcl` (`synthetic_projected_nodes`) | `1/4` | `0/1` | `0/1` | `1/1` | `0/1` |
| `rag_baseline` (`candidate_governance=continuity_tcl`) | `1/4` | `0/1` | `0/1` | `1/1` | `0/1` |
| `rag_stronger` (`candidate_governance=continuity_tcl`) | `2/4` | `1/1` | `0/1` | `1/1` | `0/1` |

## What the result means

What is now clearly true:

- stronger RAG has at least one honest broad-evidence win that continuity does not
- the shared-scope evidence bucket is doing real work now; it is no longer four isolated mini-corpora
- continuity remains stronger on the scored state-memory matrix, which is still the main product claim

What is **not** yet true:

- we do not yet have a broad “RAG should win” bucket with stable majority wins
- we do not yet have a real `hybrid should win` benchmark path
- we do not yet have evidence that plain baseline RAG is consistently better than continuity on broad retrieval

## Why stronger RAG won the replay case

In `evidence.semantic_paraphrase_replay_batch_root_cause.v1`, stronger RAG
retrieved the correct evidence pair:

- the replay-batch root-cause note
- the warm-writer plus capped-batch mitigation note

Both continuity paths missed the second required artifact once the evidence
fixtures shared one working set instead of isolated scopes.

That is the first clean negative-space result we wanted: a broad semantic
evidence case where stronger RAG helps and continuity state memory does not.

## Remaining gaps

The design-thread fixtures still fail across all current backends:

- `evidence.multi_document_mount_grant_design_thread.v1`
- `evidence.preview_card_authority_boundary_thread.v1`

That means one of two things is still weak:

- the fixture/query design
- the current retrieval backends themselves

Either way, the harness is now honest enough to show the weakness instead of
hiding it.

## Next step

The next useful step is to add a real `hybrid should win` bucket:

- continuity provides current canonical state
- RAG provides supporting evidence
- the benchmark verifies that state stays canonical while evidence stays broad

That is the missing comparison class before we can make a serious product claim
about state memory plus working evidence together.
