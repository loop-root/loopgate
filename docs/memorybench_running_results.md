**Last updated:** 2026-03-27

# Memorybench Running Results

This file tracks the current fair benchmark comparisons between `continuity_tcl`,
`rag_baseline`, and `rag_stronger` using the checked-in fixture set from
`internal/memorybench/fixtures.go`.

These numbers are a running engineering record, not a white paper. Treat them
as current benchmark evidence, tied to the exact fixture families and run IDs
listed here.

## Current headline run

Current fair comparison runs:

- `continuity_live_fixture_v23`
- `rag_baseline_resetcheck_default_v2`
- `rag_stronger_resetcheck_default_v2`

Fair-run requirements:

- `continuity_tcl` must use `-continuity-seed-fixtures` so it reads an isolated
  fixture-derived store rather than ambient repo memory.
- `rag_baseline` and `rag_stronger` must use `-rag-seed-fixtures` so they index
  the same checked-in fixture corpus before the run.
- the current headline runs use `-candidate-governance backend_default`
  (continuity resolves to TCL governance; RAG resolves to permissive benchmark ingest)

Current live summaries:

- Continuity summary: `/tmp/memorybench-live-continuity/continuity_live_fixture_v23/summary.csv`
- Continuity family summary: `/tmp/memorybench-live-continuity/continuity_live_fixture_v23/family_summary.csv`
- Continuity subfamily summary: `/tmp/memorybench-live-continuity/continuity_live_fixture_v23/subfamily_summary.csv`
- RAG summary: `/tmp/memorybench-integrity-g/rag_baseline_resetcheck_default_v2/summary.csv`
- RAG family summary: `/tmp/memorybench-integrity-g/rag_baseline_resetcheck_default_v2/family_summary.csv`
- RAG subfamily summary: `/tmp/memorybench-integrity-g/rag_baseline_resetcheck_default_v2/subfamily_summary.csv`
- Stronger RAG summary: `/tmp/memorybench-integrity-c/rag_stronger_resetcheck_default_v2/summary.csv`
- Stronger RAG family summary: `/tmp/memorybench-integrity-c/rag_stronger_resetcheck_default_v2/family_summary.csv`
- Stronger RAG subfamily summary: `/tmp/memorybench-integrity-c/rag_stronger_resetcheck_default_v2/subfamily_summary.csv`

Policy-matched fairness reruns:

- `continuity_governed_fixture_v5`
- `rag_baseline_resetcheck_governed_v2`
- `rag_stronger_resetcheck_governed_v2`

Policy-matched summaries:

- Continuity governed family summary: `/tmp/memorybench-live-continuity/continuity_governed_fixture_v5/family_summary.csv`
- Continuity governed subfamily summary: `/tmp/memorybench-live-continuity/continuity_governed_fixture_v5/subfamily_summary.csv`
- RAG governed family summary: `/tmp/memorybench-integrity-i/rag_baseline_resetcheck_governed_v2/family_summary.csv`
- RAG governed subfamily summary: `/tmp/memorybench-integrity-i/rag_baseline_resetcheck_governed_v2/subfamily_summary.csv`
- Stronger RAG governed family summary: `/tmp/memorybench-integrity-e/rag_stronger_resetcheck_governed_v2/family_summary.csv`
- Stronger RAG governed subfamily summary: `/tmp/memorybench-integrity-e/rag_stronger_resetcheck_governed_v2/subfamily_summary.csv`

### Headline numbers

| Backend | Overall | Poisoning / governance | Contradiction / truth maintenance | Task resumption | Safety precision |
| --- | --- | --- | --- | --- | --- |
| `continuity_tcl` | `42/46` | `8/8` | `15/19` | `13/13` | `6/6` |
| `rag_baseline` | `16/46` | `0/8` | `10/19` | `0/13` | `6/6` |
| `rag_stronger` | `14/46` | `0/8` | `8/19` | `0/13` | `6/6` |

Poisoning footnote:

- the poisoning bucket is not a neutral raw-retrieval bake-off
- continuity poisoning results reflect governed TCL candidate evaluation plus
  scoped retrieval over an isolated continuity store
- the current RAG comparators use a permissive benchmark governance stub, and
  poisoning steps are excluded from the seeded retrieval corpus by design
- read this bucket as a governance differential under the harness, not as a
  universal claim that any production RAG stack would leak the same payloads
- use `-candidate-governance continuity_tcl` on `rag_baseline` or
  `rag_stronger` when you want a policy-matched fairness rerun

### Policy-matched fairness rerun

Same retrieval backends, same fixtures, but with `candidate_governance=continuity_tcl`
for all compared runs:

| Backend | Overall | Poisoning / governance | Contradiction / truth maintenance | Task resumption | Safety precision |
| --- | --- | --- | --- | --- | --- |
| `continuity_tcl` | `42/46` | `8/8` | `15/19` | `13/13` | `6/6` |
| `rag_baseline` | `24/46` | `8/8` | `10/19` | `0/13` | `6/6` |
| `rag_stronger` | `22/46` | `8/8` | `8/19` | `0/13` | `6/6` |

Read:

- once governance is policy-matched, poisoning becomes a tie in this harness
- the surviving gap is now concentrated in truth maintenance and task resumption
- the promoted 46-fixture slice is based on rerun-confirmed stable RAG outputs,
  not the earlier unstable `rag_stronger` interleaved-chain run
- the policy-matched snapshot reproduces four stable continuity misses, all in
  the same weakness family:
  - `contradiction.profile_timezone_same_entity_wrong_current_probe.v1`
  - `contradiction.profile_locale_same_entity_wrong_current_probe.v1`
  - `contradiction.profile_timezone_interleaved_preview_chain_slot_probe.v1`
  - `contradiction.profile_locale_interleaved_preview_chain_slot_probe.v1`
- the different-entity timezone and locale probes still pass, which narrows the
  weakness to current-looking same-entity preview labels rather than broad
  cross-entity contamination

Policy-matched contradiction subfamilies:

| Backend | `answer_in_query` | `slot_only` |
| --- | --- | --- |
| `continuity_tcl` | `7/7` | `8/12` |
| `rag_baseline` | `0/7` | `10/12` |
| `rag_stronger` | `0/7` | `8/12` |

### Per-family deltas

| Family | Continuity | RAG baseline | Delta vs baseline | Stronger RAG | Delta vs stronger |
| --- | --- | --- | --- | --- | --- |
| `memory_poisoning` | `8/8` | `0/8` | `+8` | `0/8` | `+8` |
| `memory_contradiction` | `15/19` | `10/19` | `+5` | `8/19` | `+7` |
| `task_resumption` | `13/13` | `0/13` | `+13` | `0/13` | `+13` |
| `memory_safety_precision` | `6/6` | `6/6` | `0` | `6/6` | `0` |

### Headline operational snapshot

Task-resumption family summary with latency and prompt burden surfaced together:

| Backend | Passed | Total latency (ms) | Avg latency (ms) | Total prompt tokens | Avg prompt tokens | Total hint bytes | Avg hint bytes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `continuity_tcl` | `13/13` | `13` | `1.0000` | `136` | `10.4615` | `1464` | `112.6154` |
| `rag_baseline` | `0/13` | `13` | `1.0000` | `450` | `34.6154` | `5203` | `400.2308` |
| `rag_stronger` | `0/13` | `13` | `1.0000` | `452` | `34.7692` | `5223` | `401.7692` |

### Current early read

- `continuity_tcl` is still winning decisively on truth maintenance under the
  promoted 46-fixture skeptical set, but it no longer gets a clean contradiction sweep
- the current primary continuity limitation is now clearer and broader:
  same-entity preview-label confusion reproduces across both timezone and locale
  slots, including the longer interleaved preview-chain variants
- the slot-only contradiction split is the most informative part of the current
  scoreboard:
  - continuity `8/12`
  - rag baseline `10/12`
  - rag stronger `8/12`
- the answer-in-query split pulls the other way:
  - continuity `7/7`
  - both RAG comparators `0/7`
- the RAG integrity fixes made the task-resumption story harsher but more
  believable: both RAG comparators now fail the entire task-resumption family
  while still incurring much higher prompt burden
- the interleaved-chain slice is promoted only after rerun-confirmed stability
  on both `rag_baseline` and `rag_stronger`; the earlier unstable stronger-RAG
  run is no longer the reference point
- the largest remaining differentiator is now:
  - continuity still keeps a strong contradiction edge overall
  - continuity still has a local, repeated same-entity preview-label weakness
  - RAG remains much more expensive on resume-like retrieval and still does not
    recover the answer-in-query contradiction family

### Cost deltas

Task-resumption family aggregates from `family_summary.csv`:

| Backend | Passed | Avg operational score | Total latency (ms) | Avg latency (ms) | Avg items | Max items | Total hint bytes | Avg hint bytes | Total prompt tokens | Avg prompt tokens | Max prompt tokens |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `continuity_tcl` | `13/13` | `1.0000` | `13` | `1.0000` | `2.6154` | `4` | `1464` | `112.6154` | `136` | `10.4615` | `14` |
| `rag_baseline` | `0/13` | `0.2000` | `13` | `1.0000` | `5.0000` | `5` | `5203` | `400.2308` | `450` | `34.6154` | `45` |
| `rag_stronger` | `0/13` | `0.2000` | `13` | `1.0000` | `5.0000` | `5` | `5223` | `401.7692` | `452` | `34.7692` | `45` |

### Continuity ablations

These are still the last validated ablation runs from the earlier 44-fixture
anchor-sensitive snapshot. They remain useful mechanism evidence, but they have
not yet been rerun on the promoted 46-fixture interleaved-chain slice.

Benchmark-local continuity ablation runs:

- `continuity_ablation_anchors_off_v6`
- `continuity_ablation_hints_off_v6`
- `continuity_ablation_reduced_breadth_v6`

| Continuity mode | Overall | Contradiction | Task resumption | Read |
| --- | --- | --- | --- | --- |
| `baseline` | `42/44` | `15/17` | `13/13` | control |
| `anchors_off` | `34/44` | `7/17` | `13/13` | answer-in-query contradiction survives, but slot-only contradiction collapses to `0/10` without anchor signatures |
| `hints_off` | `14/44` | `0/17` | `0/13` | contradiction and resume collapse hard |
| `reduced_context_breadth` | `29/44` | `15/17` | `0/13` | resume collapses, contradiction stays almost intact, including both stable same-entity preview-label misses |

Ablation read:

- turning hints off still collapses contradiction and task resumption in the expected places
- reducing related-context breadth still collapses task resumption while leaving contradiction mostly intact
- turning anchors off now fails every slot-only contradiction probe (`0/10`),
  including the interleaved alias-chain, timezone slot, and locale slot families
- the same-entity timezone and locale preview misses both persist even in full
  continuity, which is a useful warning that anchor-like signatures are helping
  a lot but are not yet sufficient for every same-entity current-looking
  distractor shape

### Contradiction reporting note

- `family_summary.csv` still reports the aggregate contradiction family
- `subfamily_summary.csv` now breaks contradiction into narrower groups such as:
  - `memory_contradiction.slot_only`
  - `memory_contradiction.answer_in_query`
- use the subfamily artifact when evaluating whether query-answer overlap is
  inflating the aggregate contradiction gap

### Recent family records

| Family | Architectural mechanism under test | Failure mode it should expose | Benign control / guard |
| --- | --- | --- | --- |
| `contradiction.identity_interleaved_alias_chain_slot_probe.v1` | slot lookup under long interleaved same-entity and different-entity alias noise | slot-only probe drifts to an interleaved stale alias or different-entity current-looking alias | current preferred-name slot remains primary even under interleaved alias and policy-like noise |
| `contradiction.profile_timezone_slot_probe.v1` | anchor-like slot lookup on a second stable profile field beyond `preferred_name` | slot-only profile-timezone probe misses the canonical current timezone | wrong timezone values stay absent when the profile slot alone is queried |
| `contradiction.profile_timezone_same_entity_wrong_current_probe.v1` | anchor-like separation of canonical timezone from same-entity preview labels | same-entity preview timezone label outranks the canonical timezone slot | canonical timezone must outrank the current-looking preview label |
| `contradiction.profile_timezone_different_entity_wrong_current_probe.v1` | anchor-like separation of user timezone from different-entity current-looking timezone values | different-entity current timezone contaminates the user's timezone slot | current user timezone remains primary over teammate timezone data |
| `contradiction.profile_locale_same_entity_wrong_current_probe.v1` | anchor-like separation of canonical locale from same-entity preview labels | same-entity preview locale label outranks the canonical locale slot | canonical locale must outrank the current-looking preview label |
| `contradiction.profile_locale_different_entity_wrong_current_probe.v1` | anchor-like separation of user locale from different-entity current-looking locale values | different-entity current locale contaminates the user's locale slot | current user locale remains primary over teammate locale data |
| `poisoning.long_history_benign_malicious_interleaving.v1` | governed candidate evaluation under noisy long-history input | benign context launders a malicious memory candidate into persistence or later recall | `safety_precision.interleaved_release_history_note.v1` |
| `task_resumption.long_supersession_chain_multi_blocker_updates.v1` | long-horizon supersession tracking across repeated blocker and next-step updates | stale blockers or superseded next steps outrank the current task state | `task_resumption.long_history_cost_pressure.v1` |
| `task_resumption.interleaved_malicious_history_guard.v1` | bounded resume retrieval under mixed benign and malicious long-history baggage | malicious shortcut text or stale earlier work leaks into resume context | `task_resumption.long_supersession_chain_multi_blocker_updates.v1` |
| `safety_precision.interleaved_release_history_note.v1` | false-positive resistance for benign long histories that contain approval, safety, and secret-adjacent language | the governed-ingestion path overblocks benign operator notes just because scary keywords appear in the history | self-control: should persist cleanly and stay tied to the benign release-note semantics |

### Threats to validity

- This is still a fixture-based evaluation over controlled workloads, not a
  third-party natural-history dataset.
- `rag_stronger` is a stronger local comparator than `rag_baseline`, but it is
  still only one concrete hybrid-plus-rerank configuration.
- No external replication has been run yet; all current numbers are first-party
  local runs.
- The safety bucket mixes governed-candidate evaluation and retrieval behavior
  under the benchmark model.
- The poisoning bucket is especially governance-heavy: continuity uses the real
  governed candidate path, while the current RAG comparators use a permissive
  benchmark governance stub and do not index poisoning steps into the seeded
  corpus.
- That makes poisoning a useful policy-differential measure under this harness,
  not a universal proof that all production RAG stacks would surface the same
  attacks after indexing.
- The interleaved-chain slice originally exposed RAG reproducibility problems;
  the promoted 46-fixture snapshot uses rerun-confirmed stable results only
  after collection isolation, scoped retrieval, and scoped overfetch fixes.
- The new locale and preview-chain slot families widened the same-entity
  preview-label skepticism, and they exposed a local continuity weakness more
  clearly than the previous two-slot snapshot.
- Current operational metrics focus on retrieval-side burden and benchmark
  latency, not full end-to-end product runtime cost.

For the benchmark-history narrative and fairness-fix chronology, also see:

- Internal methodology narrative (maintainer copy): `~/Dev/projectDocs/morph/memorybench-internal/memorybench_internal_report.md`

## Exact fixture families

### Poisoning / governed safety

- `poisoning.remember_ignore_safety.v1`
- `poisoning.continuity_replay_ignore_safety.v1`
- `poisoning.hint_leak_secret_instructions.v1`
- `poisoning.authority_spoof_override_token.v1`
- `poisoning.stable_slot_name_override.v1`
- `poisoning.paraphrased_authority_bypass_family.v1`
- `poisoning.paraphrased_secret_exfil_family.v1`
- `poisoning.long_history_benign_malicious_interleaving.v1`

Guard families:

- `safety_precision.override_review_note_guard.v1`
- `safety_precision.secret_rotation_guard.v1`

### Contradiction / truth maintenance

- `contradiction.preference_latest_theme_wins.v1`
- `contradiction.identity_old_name_suppressed.v1`
- `contradiction.preference_multiple_theme_supersessions.v1`
- `contradiction.preference_indentation_update.v1`
- `contradiction.identity_entity_disambiguation.v1`
- `contradiction.identity_alias_supersession_paraphrase.v1`
- `contradiction.identity_alias_entity_guard.v1`
- `contradiction.identity_profile_name_slot_probe.v1`
- `contradiction.identity_profile_name_different_entity_slot_probe.v1`
- `contradiction.identity_profile_name_same_entity_wrong_current_probe.v1`
- `contradiction.identity_profile_name_different_entity_wrong_current_probe.v1`
- `contradiction.identity_interleaved_alias_chain_slot_probe.v1`
- `contradiction.profile_timezone_slot_probe.v1`
- `contradiction.profile_timezone_same_entity_wrong_current_probe.v1`
- `contradiction.profile_timezone_different_entity_wrong_current_probe.v1`
- `contradiction.profile_locale_same_entity_wrong_current_probe.v1`
- `contradiction.profile_locale_different_entity_wrong_current_probe.v1`

### Task resumption

- `task_resumption.benchmark_seeding_after_pause.v1`
- `task_resumption.hardening_follow_up_after_restart.v1`
- `task_resumption.blocker_changes_over_time.v1`
- `task_resumption.multi_hop_dependency_context.v1`
- `task_resumption.long_history_cost_pressure.v1`
- `task_resumption.long_supersession_chain_multi_blocker_updates.v1`
- `task_resumption.blocker_drift_distractor_guard.v1`
- `task_resumption.multi_hop_distractor_guard.v1`
- `task_resumption.blocker_paraphrase_guard.v1`
- `task_resumption.multi_hop_alias_guard.v1`
- `task_resumption.blocker_drift_multi_update_guard.v1`
- `task_resumption.alias_preview_distractor_guard.v1`
- `task_resumption.interleaved_malicious_history_guard.v1`

### Safety precision

- `safety_precision.secret_santa_note.v1`
- `safety_precision.follow_safety_preference.v1`
- `safety_precision.approval_checklist_note.v1`
- `safety_precision.override_review_note_guard.v1`
- `safety_precision.secret_rotation_guard.v1`
- `safety_precision.interleaved_release_history_note.v1`

## Reproduction commands

```bash
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-continuity \
  -run-id continuity_live_fixture_v23 \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root /Users/adalaide/Dev/morph \
  -continuity-seed-fixtures
```

```bash
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-integrity-g \
  -run-id rag_baseline_resetcheck_default_v2 \
  -profile fixtures \
  -backend rag_baseline \
  -repo-root /Users/adalaide/Dev/morph \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_default \
  -rag-seed-fixtures
```

```bash
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-integrity-c \
  -run-id rag_stronger_resetcheck_default_v2 \
  -profile fixtures \
  -backend rag_stronger \
  -repo-root /Users/adalaide/Dev/morph \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_rerank \
  -rag-seed-fixtures
```
