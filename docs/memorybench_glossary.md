**Last updated:** 2026-03-27

# Memorybench Glossary

This is the short-reference glossary for benchmark terms used in
`memorybench`.

## Backends

### `continuity_tcl`

The continuity-memory backend under test.

In benchmark runs it uses an isolated fixture-seeded continuity/TCL-backed
store and retrieves structured projected nodes from it.

### `rag_baseline`

The simplest benchmarked RAG comparator.

It seeds a Qdrant collection from the fixture corpus and retrieves flat text
chunks without the stronger reranking setup.

### `rag_stronger`

A somewhat stronger RAG comparator.

It uses the same fixture corpus but adds reranking and a wider candidate pool.
It is stronger than `rag_baseline`, but not an upper bound for all possible
RAG systems.

## Governance

### `governed`

A run where candidate memory is evaluated through a governance path before it
is treated as acceptable memory behavior.

In policy-matched fairness runs, RAG can be made to reuse TCL governance.

### `permissive`

A run where the benchmark candidate evaluator allows memory through instead of
blocking or quarantining it.

Useful as a baseline, but not a fair “safe RAG” comparator by itself.

## Continuity internals

### `hints`

Short retrieval-facing text attached to continuity nodes.

Think of them as compact retrieval clues, not full history dumps.

### `anchors`

Structured signatures that help continuity distinguish canonical slots and
entities from lookalike distractors.

In the benchmark they matter most on slot-only contradiction probes.

### `reduced_context_breadth`

A benchmark-local continuity ablation that narrows how much related context can
be returned.

It is a proxy for reduced breadth, not a true graph-traversal disable switch.

## Contradiction terms

### `slot-only contradiction`

A contradiction scenario where the probe does **not** include the expected
answer text.

This is the harder and cleaner test of whether the backend can recover the
canonical current slot value without getting spoon-fed the answer.

### `answer-in-query contradiction`

A contradiction scenario where the query includes wording that is closer to the
expected answer.

This is easier for flat retrieval systems and should be read separately from
slot-only contradiction.

## Safety / quality terms

### `safety precision`

The benchmark family that checks whether the system avoids false positives.

A good safety system should block malicious content **without** blocking benign
operator notes just because they use scary-looking words.

## Cost terms

### `prompt tokens`

Approximate number of tokens contributed by retrieved content to the prompt.

This is a rough “how much prompt baggage did this success cost?” metric.

### `hint bytes`

Approximate raw byte count of retrieved hint text.

This is a lower-level measure of how much retrieval text was pulled in.

## Reporting terms

### `family summary`

The CSV summary aggregated by benchmark family, for example:

- `memory_poisoning`
- `memory_contradiction`
- `task_resumption`
- `memory_safety_precision`

Use this when you want the big picture by family.

### `subfamily summary`

The CSV summary aggregated by a narrower split inside a family.

Most importantly, contradiction is split into:

- `memory_contradiction.slot_only`
- `memory_contradiction.answer_in_query`

Use this when a family-level total hides important differences between harder
and easier probe regimes.

## One-sentence mental model

If you want the shortest possible translation:

`continuity_tcl` is structured current-state memory, `rag_baseline` is simple
flat retrieval, `rag_stronger` is better-tuned flat retrieval, and the
benchmark asks which one keeps the right current truth with the least baggage
without becoming unsafe or overblocking.
