**Last updated:** 2026-04-10

# Memorybench Honest Rerun Report

## 1. Summary

This report records the first full honest rerun on the expanded `70`-fixture
matrix and preserves the earlier `61`-fixture honest rerun as the pre-expansion
baseline.

Why this report exists:

- `/tmp` benchmark artifacts are operational evidence, not durable history
- the benchmark matrix changed materially
- the repo needed one place that explains what changed, why it changed, and
  what the new results actually prove

## 2. What changed since the preserved `61`-fixture snapshot

### Change 1: the scored fixture matrix expanded from `61` to `70`

What changed:

- poisoning grew from `8` fixtures to `14`
- safety precision grew from `6` fixtures to `9`
- contradiction stayed at `34`
- task resumption stayed at `13`

Why:

- the old poisoning bucket was too small to support broad claims
- the old safety controls were too thin to prove we were not just rewarding
  overblocking
- expanding the matrix tests whether the current governance story still holds
  when the family surface gets wider

### Change 2: poisoning and safety now carry explicit subfamily taxonomy

What changed:

- poisoning now breaks into families such as `authority_spoof`,
  `secret_exfil`, `delayed_trigger`, `format_laundering`, and
  `long_history_laundering`
- safety precision now breaks into families such as `secret_language`,
  `approval_language`, `format_control`, and `safety_language`

Why:

- one giant poisoning bucket hides failure modes
- subfamilies make regressions explainable
- subfamilies are required if TCL is going to earn claims about family-level
  semantic compression later

### Change 3: governance-only RAG targeted runs no longer fail on zero corpus docs

What changed:

- the benchmark harness now seeds an out-of-scope placeholder collection
  document for governance-only RAG targeted runs

Why:

- poisoning-heavy or governance-only scoped runs can legitimately have zero
  retrievable in-scope evidence
- failing those runs made the benchmark dishonest about the policy surface
  under test

### Change 4: the full honest rerun was repeated on the expanded matrix

What changed:

- continuity product path, continuity synthetic control, governed
  `rag_baseline`, and governed `rag_stronger` were rerun on the same checked-in
  `70`-fixture corpus

Why:

- the repo needed a current scoreboard that matches the checked-in executable
  matrix
- the older `61`-fixture scores were no longer enough once the matrix changed

## 3. Preserved `61`-fixture honest rerun

Preserved run IDs:

- `continuity_fixture_parity_20260409_honest_v7`
- `continuity_fixture_synth_20260409_honest_v7`
- `rag_baseline_fixture_20260409_honest_v2`
- `rag_stronger_fixture_20260409_honest_v2`

Preserved scoreboard:

| Backend | Overall | Poisoning | Contradiction | Task | Safety |
| --- | --- | --- | --- | --- | --- |
| `continuity_tcl` parity | `61/61` | `8/8` | `34/34` | `13/13` | `6/6` |
| `continuity_tcl` synthetic control | `54/61` | `8/8` | `27/34` | `13/13` | `6/6` |
| governed `rag_baseline` | `33/61` | `8/8` | `19/34` | `0/13` | `6/6` |
| governed `rag_stronger` | `29/61` | `8/8` | `15/34` | `0/13` | `6/6` |

Why preserve it:

- it is the clean pre-expansion baseline
- it separates product-path improvements from later matrix broadening
- it makes the delta to `70` fixtures legible instead of burying older scores

## 4. Current `70`-fixture honest rerun

Run IDs:

- `continuity_fixture_parity_20260410_honest_v8`
- `continuity_fixture_synth_20260410_honest_v8`
- `rag_baseline_fixture_20260410_honest_v3`
- `rag_stronger_fixture_20260410_honest_v3`

Current scoreboard:

| Backend | Overall | Poisoning | Contradiction | Task | Safety |
| --- | --- | --- | --- | --- | --- |
| `continuity_tcl` parity | `70/70` | `14/14` | `34/34` | `13/13` | `9/9` |
| `continuity_tcl` synthetic control | `63/70` | `14/14` | `27/34` | `13/13` | `9/9` |
| governed `rag_baseline` | `42/70` | `14/14` | `19/34` | `0/13` | `9/9` |
| governed `rag_stronger` | `38/70` | `14/14` | `15/34` | `0/13` | `9/9` |

Important subfamily read:

- continuity parity is `7/7` on `memory_contradiction.answer_in_query`
- continuity synthetic control is also `7/7` on `memory_contradiction.answer_in_query`
- continuity parity is `27/27` on `memory_contradiction.slot_only`
- continuity synthetic control falls to `20/27` on `memory_contradiction.slot_only`
- governed `rag_baseline` is `19/27` on `memory_contradiction.slot_only`
- governed `rag_stronger` is `15/27` on `memory_contradiction.slot_only`

## 5. What the new results mean

### What they support

- the real control-plane continuity path remains strongest on contradiction and
  task continuity
- the product path still beats the synthetic continuity control only on
  contradiction, which is the current best evidence that the governed memory
  path is buying real truth-maintenance behavior rather than benchmark-local
  retrieval tuning
- once governance is policy-matched, poisoning becomes a tie in this harness
- that tie still matters operationally because most ordinary RAG deployments do
  not have a shared TCL governance lane

### What they do not support

- they do not prove TCL already solves open-ended semantic poisoning
- they do not prove continuity is a better general retrieval system than RAG
- they do not remove the need for RAG-favored evidence-retrieval fixtures
- they do not prove hybrid state-plus-evidence composition yet because that
  matrix still does not exist

## 6. Current engineering read

The current benchmark story is now narrower and more honest:

- continuity wins because it is better governed state memory
- RAG still needs its own fair evidence-retrieval bucket
- poisoning is currently a governance-surface result, not a retrieval
  differentiator
- the biggest remaining missing proof is negative space:
  - where RAG should win
  - where hybrid should win

## 7. Next work

- add explicit `RAG should win` evidence-retrieval fixtures
- add explicit `hybrid should win` state-plus-evidence fixtures
- keep expanding poisoning families with matched benign controls
- keep preserving promoted benchmark history in repo docs instead of relying on
  temporary artifact directories alone
