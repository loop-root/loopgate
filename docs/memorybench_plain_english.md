**Last updated:** 2026-03-27

# Memorybench In Plain English

This document explains `memorybench` without assuming ML or retrieval-systems
background.

It is written for a technical reader who wants to understand what the benchmark
is doing, what the results mean, and what claims are actually justified.

## 5-Minute Explanation

`memorybench` is a controlled test harness for one question:

Can our continuity-memory system keep the *right current state* better than a
RAG-style retrieval system?

It compares three backends:

- `continuity_tcl`
- `rag_baseline`
- `rag_stronger`

It runs the same checked-in fixture scenarios against each backend and measures
four broad things:

- can the system reject or contain bad memory candidates
- can it retrieve the right current fact instead of stale or wrong-looking facts
- can it resume the right task after interruptions
- can it do that without overblocking benign content or stuffing too much junk
  into the prompt

The benchmark is strongest today on **truth maintenance**:

- keeping the newest correct value
- suppressing stale values
- not confusing same-entity preview labels with canonical slot values

It is weaker as a universal safety proof.

Why:

- in default runs, poisoning compares real TCL governance against a permissive
  RAG ingest model
- in policy-matched runs, poisoning becomes much less differentiating

So the current cleanest claim is:

`continuity_tcl` is outperforming the current RAG comparators on controlled
truth-maintenance and task-state workloads.

The strongest things still *not* proven are:

- universal superiority over all strong RAG systems
- production safety superiority in all settings
- external validity beyond controlled seeded workloads

## What Memorybench Is

`memorybench` is a repeatable benchmark harness.

It does not ask a model to “judge vibes.” It does four concrete things:

1. Loads a checked-in set of scenario fixtures.
2. Seeds each backend from those fixtures in a fair, isolated way.
3. Runs retrieval and governance checks against those fixtures.
4. Writes machine-readable results and CSV summaries.

The key idea is isolation:

- continuity runs should use a temporary fixture-seeded continuity store, not
  your real repo memory
- RAG runs should use a deterministic fixture-seeded Qdrant collection, not
  ad hoc documents

That is what makes the runs comparable.

## What Each Backend Is Doing

### `continuity_tcl`

This is the continuity-memory path.

In benchmark mode it:

- seeds an isolated continuity/TCL-backed store from the fixtures
- retrieves projected nodes from that store
- can use continuity/TCL candidate governance for poisoning-style tests

What makes it special:

- it is not just raw text retrieval
- it has structured identity/profile/task state
- it can keep canonical/current state separate from stale or distractor state

### `rag_baseline`

This is the simplest honest RAG comparator in the benchmark.

In benchmark mode it:

- seeds a Qdrant collection from the fixture corpus
- retrieves text chunks through the benchmark Python helper
- does not add extra reranking unless explicitly configured

What it represents:

- “take the benchmark text, embed it, search it, and use the retrieved text”

This is useful because it gives a normal flat retrieval baseline.

### `rag_stronger`

This is a somewhat stronger RAG comparator, not a fundamentally different
architecture.

In benchmark mode it:

- uses the same fixture corpus and retrieval helper
- widens the candidate pool
- adds reranking

What it represents:

- “a better-tuned RAG recipe”

Important limitation:

- it is still just one stronger RAG configuration, not an upper bound on what
  all RAG systems could do

## What Each Benchmark Family Is Testing

### `memory_poisoning`

Question:

Can bad memory be accepted, or later resurfaced, when it should be denied,
quarantined, or ignored?

Examples:

- “remember this dangerous instruction”
- “old hostile continuity text should become durable memory”
- authority/provenance spoof attempts

What a pass means:

- dangerous memory was blocked, quarantined, or kept from surfacing

What a fail means:

- the benchmark saw dangerous content accepted or resurfaced

Important fairness note:

- default poisoning runs are partly a **governance comparison**
- they are not a pure raw-retrieval bake-off

### `memory_contradiction`

Question:

Can the system keep the current truth and suppress stale, superseded, or wrong
entity values?

Examples:

- latest preference wins over earlier preferences
- current preferred name wins over stale alias
- current timezone wins over same-entity preview labels
- current locale wins over different-entity distractors

What a pass means:

- the correct current value was retrieved
- stale or wrong-looking distractors did not win

What a fail means:

- the current value was missed
- stale memory intruded
- the wrong current-looking item was retrieved

This is the family where continuity memory is currently strongest.

### `task_resumption`

Question:

After interruption, can the system resume the *right work* with the *right
state* and without dragging in lots of irrelevant baggage?

Examples:

- blocker changed after a restart
- multiple updates changed the next step
- multi-hop dependency context matters

What a pass means:

- the required current context was retrieved
- stale blockers or old next steps did not intrude
- the retrieved context stayed within bounded budget

What a fail means:

- critical current context was missing
- stale or wrong context was injected
- too much baggage came back with the answer

### `memory_safety_precision`

Question:

Is the system avoiding false positives?

This family exists to stop the benchmark from rewarding dumb overblocking.

Examples:

- benign notes that contain words like “approval,” “override,” or “secret”
  but are not malicious

What a pass means:

- benign content still persists or retrieves as expected

What a fail means:

- the system became a false-positive machine and blocked valid content

## What Each Metric Means

### Run-level outcomes

- `passed`
  - the scenario passed its explicit rubric
- `score`
  - overall scenario score
- `truth_maintenance_score`
  - how well the scenario preserved current truth and avoided stale/wrong state
- `safety_trust_score`
  - how well the scenario handled poisoning / overblocking / trust boundaries
- `operational_cost_score`
  - how efficiently the scenario was handled under the fixture’s limits

### Contradiction metrics

- `contradiction_hits`
  - correct current fact retrieved
- `contradiction_misses`
  - expected current fact not retrieved
- `false_contradictions`
  - wrong competing value surfaced
- `stale_memory_intrusions`
  - stale or superseded value leaked back in

### Poisoning metrics

- `poisoning_attempts`
  - number of poisoning candidates/scenarios attempted
- `poisoning_blocked`
  - poisoning attempts correctly denied/quarantined/blocked
- `poisoning_leaks`
  - dangerous content resurfaced when it should not have
- `persistence_disposition`
  - whether the candidate was persisted, denied, or quarantined

### Task-resumption metrics

- `task_resumption_success`
  - the right current task state was recovered
- `missing_critical_context`
  - important current state was absent
- `wrong_context_injections`
  - stale or irrelevant context was pulled in

### Cost / retrieval metrics

- `items_returned`
  - how many retrieved items came back
- `retrieval_latency_millis`
  - how long retrieval took inside the harness
- `hint_bytes_retrieved`
  - amount of retrieved hint text
- `retrieved_prompt_tokens`
  - approximate token burden from retrieved content
- `approx_final_prompt_tokens`
  - approximate full prompt size after retrieval

In plain terms:

- `hint bytes` and `prompt tokens` are “how much stuff did we have to drag in
  to solve this”

## What Each Ablation Means

### `anchors_off`

This removes the anchor-like signatures from continuity’s seeded benchmark
state.

What it asks:

- if we remove slot/anchor structure, does continuity get worse in the places
  where anchor structure should matter?

Interpretation:

- if slot-only contradiction collapses, that is evidence anchors matter

### `hints_off`

This removes hint text from continuity’s seeded benchmark state.

What it asks:

- if we remove the lightweight retrieval text, does contradiction or resume
  behavior collapse?

Interpretation:

- if both contradiction and resume get much worse, hints are load-bearing

### `reduced_context_breadth`

This narrows how much related context continuity can return.

What it asks:

- if we reduce related context, does task resumption break?

Important caveat:

- this is a proxy for reduced context breadth
- it is **not** a true graph-traversal ablation

### `continuity-preview-slot-preference`

This is not a product setting. It is a benchmark-local heuristic toggle.

What it asks:

- if the benchmark nudges canonical slot records above same-entity preview
  labels in targeted slot-only contradiction scopes, do the known misses go away?

### `continuity-preview-slot-preference-margin`

This changes how strong that benchmark-local heuristic is.

What it asks:

- how large a match-count deficit can the canonical slot tolerate before the
  preview label still wins?

Interpretation:

- if only a very strong margin fixes the failures, that suggests the benchmark
  weakness is real and the heuristic may be fragile

## How To Interpret Pass/Fail In Each Family

### Poisoning

Pass:

- dangerous memory was governed correctly and did not leak back

Fail:

- dangerous memory was accepted, resurfaced, or not properly contained

### Contradiction

Pass:

- the latest correct value wins

Fail:

- stale, wrong-entity, or current-looking distractor value wins

### Task resumption

Pass:

- the system resumes the right work with the right context and small baggage

Fail:

- it misses critical current state, injects stale context, or needs too much
  retrieval baggage

### Safety precision

Pass:

- benign content still works

Fail:

- the system overblocks harmless content and looks “safe” only because it is
  suppressing too much

## What The Current Strongest Claim Is

The strongest current claim is:

Under controlled, isolated, fixture-seeded workloads, `continuity_tcl`
consistently beats the current benchmarked RAG configurations on
truth-maintenance-style retrieval, and it does so with lower retrieval baggage
on task-state workloads.

That claim is supported by:

- repeated reruns
- fairness fixes
- contradiction subfamily splits
- ablations
- benign controls

## What Is Still Unproven

The benchmark does **not** yet prove:

- that continuity memory is universally better than all strong RAG systems
- that continuity is universally safer in production
- that the current stronger-RAG recipe is the best RAG comparator possible
- that benchmark-local preview-slot heuristic gains should ship as product logic
- that the same wins will automatically hold on messy real-world traces

Also important:

- default poisoning results are partly shaped by governance asymmetry
- policy-matched reruns reduce that issue, but poisoning is still not the
  cleanest headline claim

## Practical Reading Guide

If you only look at a few things, look at these:

1. `memory_contradiction`
   This is the main thesis test.

2. `memory_contradiction.slot_only`
   This is the harder “no answer text in query” contradiction regime.

3. `task_resumption`
   This tells you whether the memory system helps resume real work without
   excess baggage.

4. `memory_safety_precision`
   This tells you whether a seeming safety win is just overblocking.

5. `prompt tokens` and `hint bytes`
   These tell you how expensive a pass was.

## Short Version You Can Say Out Loud

If you need to explain memorybench quickly:

“Memorybench is our controlled comparison harness for continuity memory versus
RAG. It runs the same seeded scenarios against each backend and checks four
things: can the system reject bad memory, keep the latest correct truth, resume
the right task state, and avoid overblocking benign content. Right now the
strongest result is that continuity is much better at truth maintenance and
usually does task-state retrieval with less baggage. The main things still not
proven are broad production safety superiority and generalization beyond
controlled fixture workloads.”
