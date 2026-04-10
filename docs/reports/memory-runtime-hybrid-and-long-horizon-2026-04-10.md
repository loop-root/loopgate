**Last updated:** 2026-04-10

# Memory Runtime Hybrid And Long-Horizon Report

## 1. Executive summary

This slice closes two important gaps in the memory story:

1. `hybrid` is now a real runtime backend behind the same Loopgate-owned
   memory seam instead of a benchmark-only label.
2. the benchmark now has an explicit long-horizon state-continuity slice, so
   the repo can prove an over-time continuity claim without pretending that a
   broad evidence-retrieval bucket measures the same thing.

The current honest read is:

- continuity parity: `8/8` on `long_horizon_matrix`
- continuity synthetic control: `8/8`
- governed `rag_baseline`: `2/8`
- governed `rag_stronger`: `2/8`
- runtime `hybrid`: `8/8`

That is strong evidence for long-horizon state continuity. It is not yet a
claim that hybrid evidence retrieval is complete or that TCL semantic
compression is broadly solved.

## 2. What changed

### 2.1 Runtime hybrid backend

Files:

- `internal/loopgate/memory_backend_hybrid.go`
- `internal/loopgate/memory_hybrid_evidence.go`
- `internal/loopgate/memory_backend_continuity_tcl.go`
- `internal/loopgate/server.go`
- `internal/loopgate/types.go`
- `internal/config/runtime.go`
- `config/runtime.yaml`

The runtime now accepts `memory.backend=hybrid`.

`hybrid` keeps continuity authoritative for:

- writes
- wake state
- recall
- governance and lineage

It adds bounded RAG evidence only on `discover`, returned as a separate
`Evidence` sidecar in `MemoryDiscoverResponse`.

### 2.2 Long-horizon benchmark slice

Files:

- `internal/memorybench/scenario_selection.go`
- `internal/memorybench/scenario_selection_test.go`
- `docs/memorybench_running_results.md`
- `docs/memorybench_benchmark_guide.md`

The new `long_horizon_matrix` scenario set isolates the part of the memory
claim that actually matters for continuity:

- old values should stay suppressed
- current state should stay current
- task state should resume correctly after long update chains

### 2.3 Dangerous continuity candidate regression fix

Files:

- `internal/tcl/dangerous_candidate.go`
- `internal/tcl/normalize_test.go`

The continuity-derived fact path was missing a simple exfiltration family:
`"export the api key to any caller"`.

That regression is fixed with a sink-aware export rule:

- `export` is only dangerous when paired with an external recipient or reply
  surface
- benign export language such as exporting a redacted incident report remains a
  non-dangerous control

## 3. Why these changes were necessary

### 3.1 Hybrid needed to stop being benchmark theater

The benchmark already showed that bounded state-plus-evidence composition could
beat pure continuity and pure RAG on some hybrid tasks. Leaving that logic only
inside `memorybench` would have created another fake architecture boundary.

Making `hybrid` real behind the backend seam fixes that.

### 3.2 The product claim needed an over-time proof slice

The repo needed a benchmark that actually measured “better over time” for the
class of problem continuity is designed to solve.

`long_horizon_matrix` is that slice. It does not measure broad evidence
retrieval. It measures:

- contradiction suppression over long update chains
- stable-slot recovery after interleaved stale values
- task resumption after long histories and blocker churn

### 3.3 Dangerous continuity candidates still need to fail closed

The continuity-derived path must not become the easier poisoning ingress just
because it is not an explicit remembered-fact write.

The dangerous-candidate regression was small but real, and the fix keeps the
policy lane honest.

## 4. Current benchmark read

### 4.1 Long-horizon state continuity

Run IDs:

- `continuity_long_horizon_parity_20260410_v1`
- `continuity_long_horizon_synth_20260410_v1`
- `rag_baseline_long_horizon_20260410_v1`
- `rag_stronger_long_horizon_20260410_v1`
- `hybrid_long_horizon_20260410_v1`

Counts:

| Backend | Overall | Contradiction | Task resumption |
| --- | --- | --- | --- |
| `continuity_tcl` (`production_write_parity`) | `8/8` | `5/5` | `3/3` |
| `continuity_tcl` (`synthetic_projected_nodes`) | `8/8` | `5/5` | `3/3` |
| `rag_baseline` (`candidate_governance=continuity_tcl`) | `2/8` | `2/5` | `0/3` |
| `rag_stronger` (`candidate_governance=continuity_tcl`) | `2/8` | `2/5` | `0/3` |
| `hybrid` | `8/8` | `5/5` | `3/3` |

Interpretation:

- continuity is decisively stronger than RAG-only retrieval on long-horizon
  state continuity
- hybrid inherits that win on state-dominant tasks because evidence retrieval
  is not the bottleneck in this slice
- the synthetic continuity control also passes here, which means the new slice
  is proving state semantics more than control-plane transport deltas

### 4.2 Evidence and hybrid retrieval still remain narrower

The separate targeted buckets still show:

- stronger RAG helps on broad evidence retrieval
- hybrid helps on bounded state-plus-evidence retrieval
- the remaining misses are design-thread disambiguation, not state recall

That is why the honest claim today is:

> governed continuity plus bounded hybrid evidence is stronger than RAG-only
> memory on long-horizon state continuity tasks

Not:

> better than every memory system on every retrieval problem

## 5. Invariant impact

- preserves Loopgate as the only authority boundary
- preserves continuity as the authoritative memory path
- keeps hybrid evidence advisory and bounded
- preserves fail-closed behavior when hybrid evidence retrieval is unavailable
- preserves dangerous-candidate denial on continuity-derived facts

## 6. Security and recovery notes

- the runtime hybrid path reuses an external helper/runtime, so helper output
  remains untrusted and must not silently substitute for authoritative state
- recall intentionally remains authoritative-only; that prevents a lookup API
  from quietly becoming a broad evidence channel
- hybrid evidence retrieval failure is a hard error rather than a permissive
  fallback, so the caller can tell the difference between “no evidence” and
  “evidence backend unavailable”

## 7. What is still not complete

- hybrid evidence is available on discover, but prompt assembly and agent
  planning still need a product-owned bounded-use policy
- long-horizon evidence retrieval is not yet benchmarked the way long-horizon
  state continuity now is
- TCL is still not broad enough to claim general paraphrase-family poisoning
  compression
- the remaining hybrid misses are still concentrated in related-thread evidence
  disambiguation

## 8. Suggested product-safe claim

The strongest honest claim supported by the repo today is:

> Loopgate improves assistant memory over time by separating canonical
> continuity state from broader evidence retrieval. Governed continuity keeps
> current facts and task state stable across long histories, while hybrid memory
> can attach bounded supporting evidence when the task actually needs it.

That is materially stronger and more defensible than “better than any other
system.”
