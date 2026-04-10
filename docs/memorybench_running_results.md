**Last updated:** 2026-04-10

# Memorybench Running Results

This file tracks the current fair benchmark comparisons between `continuity_tcl`,
`rag_baseline`, and `rag_stronger` using the checked-in fixture set from
`internal/memorybench/fixtures.go`.

The current truthful baseline is the `2026-04-10` honest rerun set on the
expanded `70`-fixture matrix. The prior `2026-04-09` honest rerun set is
preserved below as the last `61`-fixture snapshot.

These numbers are a running engineering record, not a white paper. Treat them
as current benchmark evidence, tied to the exact fixture families and run IDs
listed here.

For the change-by-change rerun narrative, see
[Memorybench Honest Rerun Report (2026-04-10)](/Users/adalaide/Dev/loopgate/docs/reports/memorybench-honest-rerun-2026-04-10.md).

This file is conservative on promotion. A benchmark result is headline-eligible
only if it is a `scored_fixture_run` and its top-level `run_metadata.json`
confirms the expected backend, retrieval path, and seeding mode.
`targeted_debug_run` and `unscored_debug_run` results are useful investigation
artifacts, but they are not headline evidence.

## Historical promoted headline

Current fair comparison runs:

- `continuity_full_parity_slotpref_20260403`
- `continuity_full_synth_slotpref_20260403` (retrieval-only control)
- `rag_baseline_full_default_slotpref_20260403`
- `rag_stronger_full_default_slotpref_20260403`

Fair-run requirements:

- `continuity_tcl` must use an explicit scored seeding mode:
  - `synthetic_projected_nodes` for the synthetic retrieval microbench
  - `production_write_parity` for authenticated write-path continuity seeding
- continuity headline runs must declare whether they are:
  - `retrieval_path_mode=control_plane_memory_routes`
  - `retrieval_path_mode=mixed_control_plane_and_projected_node_sqlite`
  - `retrieval_path_mode=projected_node_sqlite_backend`
- only the first two count as current product-facing continuity evidence for
  `production_write_parity`
- `debug_ambient_repo` is never eligible for headline comparison.
- `rag_baseline` and `rag_stronger` must use `-rag-seed-fixtures` so they index
  the same checked-in fixture corpus before the run.
- seeded RAG headline runs currently require
  `retrieval_path_mode=rag_search_helper`
- the current headline runs use `-candidate-governance backend_default`
  (continuity resolves to TCL governance; RAG resolves to permissive benchmark ingest)

Promotion requirements for this headline were:

- repeated targeted reruns, not just one targeted pass
- stable passes on the old timezone/locale `4/4` regression guard
- stable passes on the harder preview-bias targeted `5/5` set

This promoted headline uses the hardened `61`-fixture scored matrix, but the
published numbers predate both the fully control-plane continuity seeding and
discover/recall migration and the later fixture-matrix expansion. Treat them as
historical only. The current truthful rerun baseline is the `2026-04-10`
honest rerun set below. The `2026-04-09` honest rerun set is preserved as the
last `61`-fixture snapshot.

## Current honest rerun set (70-fixture matrix)

Fresh post-hardening reruns:

- continuity: `continuity_fixture_parity_20260410_honest_v8`
  - `retrieval_path_mode=control_plane_memory_routes`
  - `seed_path_mode=control_plane_memory_and_todo_workflow_routes`
- continuity synthetic control: `continuity_fixture_synth_20260410_honest_v8`
  - `retrieval_path_mode=projected_node_sqlite_backend`
  - `seed_path_mode=synthetic_projected_nodes`
- RAG baseline: `rag_baseline_fixture_20260410_honest_v3`
  - `retrieval_path_mode=rag_search_helper`
  - `seed_path_mode=python_rag_fixture_seed`
- stronger RAG: `rag_stronger_fixture_20260410_honest_v3`
  - `retrieval_path_mode=rag_search_helper`
  - `seed_path_mode=python_rag_fixture_seed`

Current counts from that rerun set:

| Backend | Overall | Poisoning / governance | Contradiction / truth maintenance | Task resumption | Safety precision |
| --- | --- | --- | --- | --- | --- |
| `continuity_tcl` (`production_write_parity`) | `70/70` | `14/14` | `34/34` | `13/13` | `9/9` |
| `rag_baseline` (`candidate_governance=continuity_tcl`) | `42/70` | `14/14` | `19/34` | `0/13` | `9/9` |
| `rag_stronger` (`candidate_governance=continuity_tcl`) | `38/70` | `14/14` | `15/34` | `0/13` | `9/9` |

Useful read:

- the honest continuity path still holds `13/13` on task resumption after
  moving those scenarios onto real `todo` workflow state and real discover/recall
- the current honest control-plane continuity run is now `34/34` on
  contradiction
- the current honest control-plane continuity run is now also `14/14` on
  poisoning and `9/9` on safety precision
- continuity answer-in-query is now `7/7`, versus `0/7` for both governed RAG
  comparators
- continuity slot-only is now `27/27`, versus `19/27` for governed
  `rag_baseline` and `15/27` for governed `rag_stronger`
- the fixture expansion increased the measured policy surface without changing
  the current contradiction or task-resumption story
- poisoning remains a tie once governance is policy-matched across all three
  backends; the differentiator is still contradiction and task continuity
- the synthetic control remains below the product path only on contradiction,
  which continues to support the claim that the control-plane memory path is
  buying real truth-maintenance behavior rather than benchmark-local lookup tricks

Current rerun artifacts:

- Summary: `/tmp/memorybench-live-continuity/continuity_fixture_parity_20260410_honest_v8/summary.csv`
- Family summary: `/tmp/memorybench-live-continuity/continuity_fixture_parity_20260410_honest_v8/family_summary.csv`
- Subfamily summary: `/tmp/memorybench-live-continuity/continuity_fixture_parity_20260410_honest_v8/subfamily_summary.csv`
- Synthetic summary: `/tmp/memorybench-live-continuity/continuity_fixture_synth_20260410_honest_v8/summary.csv`
- Synthetic family summary: `/tmp/memorybench-live-continuity/continuity_fixture_synth_20260410_honest_v8/family_summary.csv`
- Synthetic subfamily summary: `/tmp/memorybench-live-continuity/continuity_fixture_synth_20260410_honest_v8/subfamily_summary.csv`
- RAG baseline summary: `/tmp/memorybench-live-rag/rag_baseline_fixture_20260410_honest_v3/summary.csv`
- RAG baseline family summary: `/tmp/memorybench-live-rag/rag_baseline_fixture_20260410_honest_v3/family_summary.csv`
- RAG baseline subfamily summary: `/tmp/memorybench-live-rag/rag_baseline_fixture_20260410_honest_v3/subfamily_summary.csv`
- Stronger RAG summary: `/tmp/memorybench-live-rag/rag_stronger_fixture_20260410_honest_v3/summary.csv`
- Stronger RAG family summary: `/tmp/memorybench-live-rag/rag_stronger_fixture_20260410_honest_v3/family_summary.csv`
- Stronger RAG subfamily summary: `/tmp/memorybench-live-rag/rag_stronger_fixture_20260410_honest_v3/subfamily_summary.csv`

Previous honest `61`-fixture artifacts remain preserved:

- `/tmp/memorybench-live-continuity/continuity_fixture_parity_20260409_honest_v7/*`
- `/tmp/memorybench-live-continuity/continuity_fixture_synth_20260409_honest_v7/*`
- `/tmp/memorybench-live-rag/rag_baseline_fixture_20260409_honest_v2/*`
- `/tmp/memorybench-live-rag/rag_stronger_fixture_20260409_honest_v2/*`

## Preserved prior honest rerun set (61-fixture matrix)

The earlier honest rerun remains worth keeping because it was the last scored
snapshot before the poisoning and safety matrix expansion. The raw `/tmp`
artifacts may expire, so the scoreboard is preserved here in repo docs.

Preserved `2026-04-09` run IDs:

- continuity parity: `continuity_fixture_parity_20260409_honest_v7`
- continuity synthetic control: `continuity_fixture_synth_20260409_honest_v7`
- RAG baseline: `rag_baseline_fixture_20260409_honest_v2`
- stronger RAG: `rag_stronger_fixture_20260409_honest_v2`

Preserved `61`-fixture counts:

| Backend | Overall | Poisoning / governance | Contradiction / truth maintenance | Task resumption | Safety precision |
| --- | --- | --- | --- | --- | --- |
| `continuity_tcl` (`production_write_parity`) | `61/61` | `8/8` | `34/34` | `13/13` | `6/6` |
| `continuity_tcl` (`synthetic_projected_nodes`) | `54/61` | `8/8` | `27/34` | `13/13` | `6/6` |
| `rag_baseline` (`candidate_governance=continuity_tcl`) | `33/61` | `8/8` | `19/34` | `0/13` | `6/6` |
| `rag_stronger` (`candidate_governance=continuity_tcl`) | `29/61` | `8/8` | `15/34` | `0/13` | `6/6` |

Read:

- the `70`-fixture rerun preserved the same contradiction and task-resumption
  shape while broadening poisoning from `8` to `14` fixtures and safety
  precision from `6` to `9`
- the preserved `61`-fixture snapshot remains the right before/after reference
  when tracking what the matrix expansion changed versus what the product path
  already proved

Historical `2026-04-03` live summaries:

- Continuity parity summary: `/tmp/memorybench-full-matrix-slot-preference/continuity_full_parity_slotpref_20260403/summary.csv`
- Continuity parity family summary: `/tmp/memorybench-full-matrix-slot-preference/continuity_full_parity_slotpref_20260403/family_summary.csv`
- Continuity parity subfamily summary: `/tmp/memorybench-full-matrix-slot-preference/continuity_full_parity_slotpref_20260403/subfamily_summary.csv`
- Continuity synthetic control summary: `/tmp/memorybench-full-matrix-slot-preference/continuity_full_synth_slotpref_20260403/summary.csv`
- Continuity synthetic control family summary: `/tmp/memorybench-full-matrix-slot-preference/continuity_full_synth_slotpref_20260403/family_summary.csv`
- Continuity synthetic control subfamily summary: `/tmp/memorybench-full-matrix-slot-preference/continuity_full_synth_slotpref_20260403/subfamily_summary.csv`
- RAG baseline summary: `/tmp/memorybench-full-matrix-slot-preference/rag_baseline_full_default_slotpref_20260403/summary.csv`
- RAG baseline family summary: `/tmp/memorybench-full-matrix-slot-preference/rag_baseline_full_default_slotpref_20260403/family_summary.csv`
- RAG baseline subfamily summary: `/tmp/memorybench-full-matrix-slot-preference/rag_baseline_full_default_slotpref_20260403/subfamily_summary.csv`
- Stronger RAG summary: `/tmp/memorybench-full-matrix-slot-preference/rag_stronger_full_default_slotpref_20260403/summary.csv`
- Stronger RAG family summary: `/tmp/memorybench-full-matrix-slot-preference/rag_stronger_full_default_slotpref_20260403/family_summary.csv`
- Stronger RAG subfamily summary: `/tmp/memorybench-full-matrix-slot-preference/rag_stronger_full_default_slotpref_20260403/subfamily_summary.csv`

Policy-matched fairness reruns:

- `continuity_fixture_parity_20260410_honest_v8`
- `rag_baseline_fixture_20260410_honest_v3`
- `rag_stronger_fixture_20260410_honest_v3`

Policy-matched summaries:

- Continuity governed family summary: `/tmp/memorybench-live-continuity/continuity_fixture_parity_20260410_honest_v8/family_summary.csv`
- Continuity governed subfamily summary: `/tmp/memorybench-live-continuity/continuity_fixture_parity_20260410_honest_v8/subfamily_summary.csv`
- RAG governed family summary: `/tmp/memorybench-live-rag/rag_baseline_fixture_20260410_honest_v3/family_summary.csv`
- RAG governed subfamily summary: `/tmp/memorybench-live-rag/rag_baseline_fixture_20260410_honest_v3/subfamily_summary.csv`
- Stronger RAG governed family summary: `/tmp/memorybench-live-rag/rag_stronger_fixture_20260410_honest_v3/family_summary.csv`
- Stronger RAG governed subfamily summary: `/tmp/memorybench-live-rag/rag_stronger_fixture_20260410_honest_v3/subfamily_summary.csv`

### Current policy-matched headline numbers

| Backend | Overall | Poisoning / governance | Contradiction / truth maintenance | Task resumption | Safety precision |
| --- | --- | --- | --- | --- | --- |
| `continuity_tcl` (`production_write_parity`, honest control-plane baseline) | `70/70` | `14/14` | `34/34` | `13/13` | `9/9` |
| `continuity_tcl` (`synthetic_projected_nodes`, honest retrieval-only control) | `63/70` | `14/14` | `27/34` | `13/13` | `9/9` |
| `rag_baseline` (`candidate_governance=continuity_tcl`) | `42/70` | `14/14` | `19/34` | `0/13` | `9/9` |
| `rag_stronger` (`candidate_governance=continuity_tcl`) | `38/70` | `14/14` | `15/34` | `0/13` | `9/9` |

Poisoning footnote:

- the poisoning bucket is not a neutral raw-retrieval bake-off
- continuity poisoning results reflect governed TCL candidate evaluation plus
  scoped retrieval over an isolated continuity store
- the current fairness tables in this file use
  `candidate_governance=continuity_tcl` for the RAG comparators too, so the
  `14/14` poisoning tie is the meaningful current comparison
- historical default-RAG runs that used permissive benchmark governance remain
  useful only as harness history, not as the current fair comparison
- read this bucket as a governance-plus-retrieval differential under the
  harness, not as a universal claim that any production RAG stack would leak
  the same payloads

Targeted `2026-04-10` poisoning expansion read (`targeted_debug_run`, not
headline evidence):

- `continuity_poisoning_matrix_20260410`: `14/14` poisoning and `9/9` safety
- `rag_baseline_poisoning_matrix_20260410` with `candidate_governance=continuity_tcl`:
  `14/14` poisoning and `9/9` safety
- `rag_stronger_poisoning_matrix_20260410` with `candidate_governance=continuity_tcl`:
  `14/14` poisoning and `9/9` safety

Read:

- the broadened poisoning bucket still ties across all three backends once they
  share the same TCL governance lane
- that is a useful policy result, but it is not a contradiction or task-memory
  differentiator
- the bucket is broader than the earlier `8` poisoning fixtures, but it still
  does not justify a general claim that TCL already solves open-ended semantic
  poisoning

### Policy-matched fairness rerun

Same retrieval backends, same fixtures, but with `candidate_governance=continuity_tcl`
for all compared runs:

| Backend | Overall | Poisoning / governance | Contradiction / truth maintenance | Task resumption | Safety precision |
| --- | --- | --- | --- | --- | --- |
| `continuity_tcl` (`production_write_parity`) | `70/70` | `14/14` | `34/34` | `13/13` | `9/9` |
| `rag_baseline` | `42/70` | `14/14` | `19/34` | `0/13` | `9/9` |
| `rag_stronger` | `38/70` | `14/14` | `15/34` | `0/13` | `9/9` |

Read:

- once governance is policy-matched, poisoning becomes a tie in this harness
- the surviving gap is now contradiction plus task resumption
- the honest continuity run is now `+15` over governed `rag_baseline` on
  contradiction
- the real product path is now also `+7` over the synthetic retrieval-only
  continuity control on contradiction, which is the clearest current measure of
  what the control-plane memory path buys beyond pure projected-node lookup
- the current control-plane run recovered that gap through product changes,
  not through benchmark routing shortcuts
- the move from `57/61` to `61/61` came from fixing benchmark candidate
  governance semantics, not from a new retrieval shortcut or a doc-only
  relabel
- synthetic continuity is now rerun on the same governance semantics, but it
  remains retrieval-only control evidence rather than product-truth evidence
- continuity now clears every current fixture family in the honest scored set

Policy-matched contradiction subfamilies:

| Backend | `answer_in_query` | `slot_only` |
| --- | --- | --- |
| `continuity_tcl` (`production_write_parity`) | `7/7` | `27/27` |
| `rag_baseline` | `0/7` | `19/27` |
| `rag_stronger` | `0/7` | `15/27` |

### Per-family deltas

| Family | Continuity | RAG baseline | Delta vs baseline | Stronger RAG | Delta vs stronger |
| --- | --- | --- | --- | --- | --- |
| `memory_poisoning` | `14/14` | `14/14` | `0` | `14/14` | `0` |
| `memory_contradiction` | `34/34` | `19/34` | `+15` | `15/34` | `+19` |
| `task_resumption` | `13/13` | `0/13` | `+13` | `0/13` | `+13` |
| `memory_safety_precision` | `9/9` | `9/9` | `0` | `9/9` | `0` |

### Headline operational snapshot

Task-resumption family summary with latency and prompt burden surfaced together:

| Backend | Passed | Total latency (ms) | Avg latency (ms) | Total prompt tokens | Avg prompt tokens | Total hint bytes | Avg hint bytes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `continuity_tcl` | `13/13` | `13` | `1.0000` | `213` | `16.3846` | `2524` | `194.1538` |
| `rag_baseline` | `0/13` | `13` | `1.0000` | `450` | `34.6154` | `5203` | `400.2308` |
| `rag_stronger` | `0/13` | `13` | `1.0000` | `452` | `34.7692` | `5223` | `401.7692` |

### Current early read

- `continuity_tcl` is now winning decisively on contradiction under the honest
  control-plane run
- the benchmark is still materially more honest than the older mixed-path
  story, which means the contradiction recovery should be treated as product
  signal rather than harness inflation
- the strongest continuity differentiators are now contradiction plus task
  resumption together
- the slot-only contradiction split is the most informative part of the current
  scoreboard:
  - continuity parity `27/27`
  - continuity synthetic control `20/27`
  - rag baseline `19/27`
  - rag stronger `15/27`
- the answer-in-query split pulls the other way:
  - continuity parity `7/7`
  - both RAG comparators `0/7`
- the RAG integrity fixes made the task-resumption story harsher but more
  believable: both RAG comparators now fail the entire task-resumption family
  while still incurring much higher prompt burden
- the interleaved-chain slice is promoted only after rerun-confirmed stability
  on continuity parity and both RAG comparators
- the largest remaining differentiator is now:
  - continuity keeps the contradiction edge
  - continuity keeps the task-resumption edge
  - RAG remains much more expensive on resume-like retrieval and still does not
    recover the answer-in-query contradiction family

### Cost deltas

Task-resumption family aggregates from `family_summary.csv`:

| Backend | Passed | Avg operational score | Total latency (ms) | Avg latency (ms) | Avg items | Max items | Total hint bytes | Avg hint bytes | Total prompt tokens | Avg prompt tokens | Max prompt tokens |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `continuity_tcl` | `13/13` | `0.8500` | `13` | `1.0000` | `1.4615` | `2` | `2524` | `194.1538` | `213` | `16.3846` | `23` |
| `rag_baseline` | `0/13` | `0.2000` | `13` | `1.0000` | `5.0000` | `5` | `5203` | `400.2308` | `450` | `34.6154` | `45` |
| `rag_stronger` | `0/13` | `0.2000` | `13` | `1.0000` | `5.0000` | `5` | `5223` | `401.7692` | `452` | `34.7692` | `45` |

### Continuity ablations

These are still the last validated ablation runs from the earlier 44-fixture
anchor-sensitive snapshot. They remain useful mechanism evidence, but they have
not yet been rerun on the promoted `61`-fixture production-parity matrix.

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
- before the bounded slot-seeking preference landed, the same-entity timezone
  and locale preview misses both persisted even in full continuity
- keep that historical ablation result in mind when reading the anchor-only
  mechanism story: anchor-like signatures helped a lot, but they were not
  sufficient by themselves for that weakness family

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
- The poisoning bucket is especially governance-heavy. The current policy-matched
  fairness reruns now put both continuity and RAG behind the same TCL governance
  adapter, which is why all three backends tie at `14/14` on the current
  fixture set. Default RAG runs still use a permissive benchmark ingest model.
- That makes poisoning a useful policy-differential measure under this harness,
  not a universal proof that all production RAG stacks would surface or block
  the same attacks after indexing.
- The interleaved-chain slice originally exposed RAG reproducibility problems;
  the promoted historical `61`-fixture snapshot uses rerun-confirmed stable results only
  after collection isolation, scoped retrieval, and scoped overfetch fixes.
- The honest `2026-04-10` control-plane continuity run now clears the current
  `34`-fixture contradiction family, but that does not remove the need for
  future adversarial expansion; this is still a controlled fixture set, not a
  proof against natural-history drift.
- Current operational metrics focus on retrieval-side burden and benchmark
  latency, not full end-to-end product runtime cost.

For the benchmark-history narrative and fairness-fix chronology, also see:

- Internal methodology narrative (maintainer copy outside this repository; filename `memorybench_internal_report.md`)

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
- `poisoning.delayed_trigger_review_waiver.v1`
- `poisoning.markdown_checklist_secret_exfil.v1`
- `poisoning.yaml_frontmatter_authority_spoof.v1`
- `poisoning.session_cookie_exfil_family.v1`
- `poisoning.deferred_activation_signing_key_handoff.v1`
- `poisoning.approval_waiver_client_secret_note.v1`

Guard families:

- `safety_precision.override_review_note_guard.v1`
- `safety_precision.secret_rotation_guard.v1`
- `safety_precision.interleaved_release_history_note.v1`
- `safety_precision.markdown_release_checklist.v1`
- `safety_precision.yaml_rotation_runbook.v1`
- `safety_precision.review_waiver_denied_postmortem.v1`

### Contradiction / truth maintenance

The promoted `34`-fixture contradiction family now explicitly includes the old
timezone/locale `4/4` regression guard, the harder preview-bias `5/5` set, and
their preview-only controls. The inventory below matches the current checked-in
fixture set.

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
- `contradiction.profile_timezone_interleaved_preview_chain_slot_probe.v1`
- `contradiction.profile_locale_interleaved_preview_chain_slot_probe.v1`
- `contradiction.profile_timezone_close_preview_label_slot_probe.v1`
- `contradiction.profile_timezone_preview_bias_far_match_slot_probe.v1`
- `contradiction.profile_timezone_preview_bias_margin_two_slot_probe.v1`
- `contradiction.profile_timezone_preview_bias_distractor_chain_slot_probe.v1`
- `contradiction.profile_timezone_multiple_preview_labels_slot_probe.v1`
- `contradiction.profile_timezone_conflicting_recent_preview_labels_slot_probe.v1`
- `contradiction.profile_timezone_preview_only_control.v1`
- `contradiction.profile_locale_preview_bias_far_match_slot_probe.v1`
- `contradiction.profile_locale_preview_bias_distractor_chain_slot_probe.v1`
- `contradiction.profile_locale_preview_only_control.v1`
- `contradiction.profile_pronouns_same_entity_wrong_current_probe.v1`
- `contradiction.profile_pronouns_preview_bias_far_match_slot_probe.v1`
- `contradiction.profile_pronouns_preview_only_control.v1`
- `contradiction.identity_profile_name_preview_bias_far_match_slot_probe.v1`
- `contradiction.identity_profile_name_preview_only_control.v1`

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
- `safety_precision.markdown_release_checklist.v1`
- `safety_precision.yaml_rotation_runbook.v1`
- `safety_precision.review_waiver_denied_postmortem.v1`

## Reproduction commands

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-full-matrix-slot-preference \
  -run-id continuity_full_parity_slotpref_20260403 \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -continuity-seeding-mode production_write_parity
```

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-full-matrix-slot-preference \
  -run-id continuity_full_synth_slotpref_20260403 \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -continuity-seeding-mode synthetic_projected_nodes
```

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-full-matrix-slot-preference \
  -run-id rag_baseline_full_default_slotpref_20260403 \
  -profile fixtures \
  -backend rag_baseline \
  -repo-root /Users/adalaide/Dev/loopgate \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_default \
  -rag-seed-fixtures
```

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-full-matrix-slot-preference \
  -run-id rag_stronger_full_default_slotpref_20260403 \
  -profile fixtures \
  -backend rag_stronger \
  -repo-root /Users/adalaide/Dev/loopgate \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_rerank \
  -rag-seed-fixtures
```

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-full-matrix-slot-preference \
  -run-id rag_baseline_full_governed_slotpref_20260403 \
  -profile fixtures \
  -backend rag_baseline \
  -candidate-governance continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_default \
  -rag-seed-fixtures
```

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-full-matrix-slot-preference \
  -run-id rag_stronger_full_governed_slotpref_20260403 \
  -profile fixtures \
  -backend rag_stronger \
  -candidate-governance continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_rerank \
  -rag-seed-fixtures
```
