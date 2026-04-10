**Last updated:** 2026-04-10

# Hybrid Disambiguation And Prompt Policy Report

## 1. Summary

This slice closes the biggest remaining read-side gap in the current memory MVP:

- hybrid evidence selection now uses a shared relation-hint scorer in both the
  runtime and the benchmark harness
- hybrid evidence lookup now asks for a bounded wider candidate pool so the
  reranker can actually see sibling rationale notes inside the same thread
- the targeted `hybrid_recall_matrix` is now `7` fixtures instead of `5`
- the current targeted read is:
  - continuity control `0/7`
  - baseline RAG `0/7`
  - stronger RAG `0/7`
  - hybrid `7/7`

That is strong evidence for the current MVP claim:

> continuity-backed current state plus bounded evidence lookup is materially
> stronger than either pure control path on the current state-plus-evidence
> benchmark slice.

It is still a targeted proof slice, not a universal claim about every memory
problem.

## 2. What changed

### 2.1 Shared relation-hint scorer

Files:

- `/Users/adalaide/Dev/loopgate/internal/relationhints/relationhints.go`
- `/Users/adalaide/Dev/loopgate/internal/relationhints/relationhints_test.go`
- `/Users/adalaide/Dev/loopgate/internal/memorybench/runner.go`
- `/Users/adalaide/Dev/loopgate/internal/loopgate/memory_backend_hybrid.go`

The benchmark harness and the live hybrid backend now share the same bounded
relation-hint scoring model.

Why:

- the prior hybrid logic had drifted into two copies
- both copies were too blunt and mostly rewarded token overlap
- that made them pick the wrong sibling note inside the right design thread

The new scorer:

- builds one bounded relation target from the evidence probe plus recalled
  continuity state hints
- scores phrase coverage before raw token overlap
- still stays deterministic and bounded
- does not widen the final evidence payload

### 2.2 Wider but bounded hybrid candidate pool

Files:

- `/Users/adalaide/Dev/loopgate/internal/relationhints/relationhints.go`
- `/Users/adalaide/Dev/loopgate/internal/memorybench/runner.go`
- `/Users/adalaide/Dev/loopgate/internal/loopgate/memory_backend_hybrid.go`

Hybrid evidence lookup now uses a bounded wider search pool.

Why:

- reranking only helps if the right sibling notes are in the candidate set
- the earlier benchmark path was still searching too narrowly
- the new pool is still bounded and explicit; it does not turn hybrid discover
  into broad uncontrolled search

### 2.3 New hybrid ambiguity fixtures

Files:

- `/Users/adalaide/Dev/loopgate/internal/memorybench/fixtures.go`
- `/Users/adalaide/Dev/loopgate/internal/memorybench/scenario_selection.go`

Added:

- `hybrid.memory_artifact_lookup_current_contract_and_prompt_policy.v1`
- `hybrid.continuity_review_restart_follow_up_and_lineage_rationale.v1`

Why:

- the old hybrid matrix was still too small
- we needed direct coverage for “right thread, wrong second note” on the
  product-facing wake-state/artifact contract and on review/restart lineage

## 3. Prompt policy

The product contract is now explicit.

### 3.1 Wake state

Wake state is injected by default and should remain small:

- active goals
- unresolved tasks
- current project context
- deadlines
- stable profile facts
- a very small number of recent derived continuity hints when clearly relevant

Wake state is not:

- a broad evidence surface
- a graph expansion surface
- a place to dump supporting design notes

### 3.2 Artifact lookup

Stored continuity artifacts sit behind explicit lookup/get calls:

- `/v1/memory/artifacts/lookup`
- `/v1/memory/artifacts/get`

Use them when the model needs more stored state context than wake state should
carry.

### 3.3 Hybrid evidence

Hybrid evidence is advisory context on discover only.

Use it when:

- the query is asking for supporting rationale or related background
- continuity has already surfaced a current state anchor
- bounded evidence can help answer the question without pretending to be
  authoritative memory

Do not use it as:

- automatic wake-state expansion
- recursive graph traversal
- a durable artifact class

## 4. Current targeted scores

Run IDs:

- `continuity_hybrid_matrix_20260410_v7`
- `rag_baseline_hybrid_matrix_20260410_v7`
- `rag_stronger_hybrid_matrix_20260410_v7`
- `hybrid_hybrid_matrix_20260410_v7`

Counts:

| Backend | Overall |
| --- | --- |
| `continuity_tcl` (`production_write_parity`) | `0/7` |
| `rag_baseline` (`candidate_governance=continuity_tcl`) | `0/7` |
| `rag_stronger` (`candidate_governance=continuity_tcl`) | `0/7` |
| `hybrid` | `7/7` |

Read:

- continuity-only still fails for the expected reason: it has the current state
  but not the broader evidence corpus
- RAG-only still fails for the opposite reason: it can search evidence but does
  not own the canonical current state
- the bounded hybrid path now clears the current targeted state-plus-evidence
  matrix without widening wake state or turning evidence into authority

## 5. What this proves

This proves:

- the runtime hybrid backend is no longer just “promising in theory”
- the read-side contract can support an honest MVP UI story
- the current targeted hybrid matrix now behaves like the architecture says it
  should

This does not prove:

- that hybrid is universally best on all evidence retrieval
- that TCL semantic coverage is complete
- that long-horizon evidence retrieval is solved
- that the current targeted matrix is broad enough to become a universal
  marketing claim

## 6. Red/green

Focused verification:

```bash
go test ./internal/relationhints ./internal/memorybench ./cmd/memorybench
go test ./internal/loopgate -run 'Test(HybridMemoryDiscover_ReturnsEvidenceSidecar|HybridMemoryDiscover_ReranksSiblingEvidenceByRelationHints|HybridMemoryDiscover_FailsClosedWhenEvidenceRetrievalFails|MemoryArtifactLookupAndGet_ExposeRememberedFact|LookupMemoryArtifacts_UsesConfiguredBackend|GetMemoryArtifacts_UsesConfiguredBackend|GetMemoryArtifacts_RejectsUnsupportedArtifactRef)'
```

Targeted reruns:

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build go run ./cmd/memorybench -output-root /tmp/memorybench-live-continuity -run-id continuity_hybrid_matrix_20260410_v7 -profile extended_fixtures -backend continuity_tcl -repo-root /Users/adalaide/Dev/loopgate -continuity-seeding-mode production_write_parity -scenario-set hybrid_recall_matrix
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build go run ./cmd/memorybench -output-root /tmp/memorybench-live-rag -run-id rag_baseline_hybrid_matrix_20260410_v7 -profile extended_fixtures -backend rag_baseline -candidate-governance continuity_tcl -repo-root /Users/adalaide/Dev/loopgate -rag-qdrant-url http://127.0.0.1:6333 -rag-collection memorybench_default -rag-seed-fixtures -scenario-set hybrid_recall_matrix
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build go run ./cmd/memorybench -output-root /tmp/memorybench-live-rag -run-id rag_stronger_hybrid_matrix_20260410_v7 -profile extended_fixtures -backend rag_stronger -candidate-governance continuity_tcl -repo-root /Users/adalaide/Dev/loopgate -rag-qdrant-url http://127.0.0.1:6333 -rag-collection memorybench_rerank -rag-reranker Xenova/ms-marco-MiniLM-L-6-v2 -rag-seed-fixtures -scenario-set hybrid_recall_matrix
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build go run ./cmd/memorybench -output-root /tmp/memorybench-live-hybrid -run-id hybrid_hybrid_matrix_20260410_v7 -profile extended_fixtures -backend hybrid -repo-root /Users/adalaide/Dev/loopgate -continuity-seeding-mode production_write_parity -rag-qdrant-url http://127.0.0.1:6333 -rag-collection memorybench_rerank -rag-reranker Xenova/ms-marco-MiniLM-L-6-v2 -rag-seed-fixtures -scenario-set hybrid_recall_matrix
```
