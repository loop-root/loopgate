# Memory System — Gap Review
**Date:** 2026-03-31
**Scope:** `internal/tcl/`, `internal/loopgate/continuity_memory.go`, `cmd/haven/memory_intent.go`, `cmd/haven/capabilities.go`, memorybench results

---

## How the System Works (Mental Model)

There are two write paths and one read path. Understanding which path handles what is the key to understanding the whole system.

### Write Path 1 — Explicit memory (`memory.remember`)

1. **Deterministic pre-run detection** (`memory_intent.go`): the in-repo reference client (`cmd/haven/`) scans the user's raw message for a small set of regex patterns before the model runs. Matches like "call me X", "remember my name is X", "remember I prefer X" bypass the model entirely and call `memory.remember` directly.

2. **Model-proposed memory**: The model can also call `memory.remember` mid-conversation when the capability hint suggests it. The model proposes a `fact_key` and `fact_value`.

3. **TCL normalization** (`normalize.go`, `memory_registry.go`): The proposed key is run through `CanonicalizeExplicitMemoryFactKey()`. Keys that don't match the registry return an empty string → normalization fails → write denied. No error is surfaced to the model; the call just fails silently from the user's perspective.

4. **Node construction**: The canonical key is converted to a structured `TCLNode` — the "compression" step. The node captures: what kind of operation (`ACT`), what it's about (`OBJ`), qualifiers (`QUAL`), state (`STA`), and a conflict anchor (`ANCHOR`). The anchor is a domain:entity:slot tuple that identifies the logical slot this fact occupies.

5. **Risk motif detection**: The node is checked for dangerous structural patterns. Currently one motif is defined: `private_external_memory_write` (fires when `STR + MEM + [PRI, EXT] + WRT`), which is set during normalization if the source text contains secret/bypass/exfiltration keyword combinations.

6. **Policy evaluation** (`policy.go`): If the risk motif fired → hard deny, quarantine. Otherwise → `DispositionKeep`, approved. The current policy is this simple — one motif, two outcomes.

7. **Persistence**: On approval, Loopgate creates an inspection record, a distillate with the fact and semantic projection (anchor version + key + SHA-256 signatures), and a resonate key. Writes to SQLite + state JSON + audit log.

### Write Path 2 — Continuity inspection

After a conversation, the in-repo reference client (`cmd/haven/`) calls `DistillThread()` which sends conversation events to `InspectContinuityThread`. If the conversation meets a threshold, Loopgate derives distillates from the thread content — inferred facts, not explicitly requested ones. This path uses trust level `TIF` (inferred, confidence 6) vs explicit's `TUS` (user-originated, confidence 8).

### Read Path — Wake state assembly

At session start, the wake state is assembled from all eligible (non-tombstoned) distillates and injected into the model's system context:

```
Remembered continuity follows...
remembered_fact: name = Ada
remembered_fact: preference.favorite_coffee = oat latte
active_goal: finish Loopgate MVP
unresolved_item: investigate attachments crash
```

Budget: 2000 tokens default. Soft maxes: 5 goals, 12 facts, 8 resonate keys, 10 unresolved items. Trim priority: resonate keys → facts → goals → items.

---

## What TCL Actually Is

TCL (Thought Compression Language) is not a general-purpose language. It's a fixed-vocabulary semantic type system for classifying memory intent into a canonical typed shape. The "compression" is converting a rich free-text proposal ("User prefers dark mode") into a normalized node:

```
STR | MEM | [PRI] | ACT | anchor(v1:usr_preference:stated:fact:preference:ui_theme)
```

This shape drives two things:
- **Governance**: The policy checks structural patterns (node shape + qualifiers + motifs), not the text. Can't be bypassed by paraphrasing.
- **Supersession**: Same anchor tuple = same logical slot = overwrite. The anchor enables exact deterministic deduplication without semantic similarity.

---

## Gap 1 — Key Registry is Silently Too Narrow

**The problem:** `CanonicalizeExplicitMemoryFactKey()` only accepts these key patterns:

| Pattern | Canonical result |
|---|---|
| `name`, `user.name`, `my_name`, `full_name` | `name` |
| `preferred_name`, `user_preferred_name` | `preferred_name` |
| `preference.*` | `preference.*` |
| `routine.*` | `routine.*` |
| `project.*` | `project.*` |
| `profile.*` / `user.*` | `profile.*` |

Keys outside this set return an empty string → `memory.remember` denied with no meaningful signal back to the model.

**What silently fails:**
- `goal.current_sprint` — no `goal.*` prefix rule
- `work.focus_area` — no `work.*` prefix rule
- `context.recent_topic` — no `context.*` prefix rule
- `task.current_blocker` — no `task.*` prefix (explicit-fact path; task metadata uses a separate source type)
- `habit.morning_routine` — no `habit.*` prefix
- Any key the model invents outside the known prefixes

**The insidious part:** The capability hint currently says: "Prefer keys like `preference.coffee_order`, `routine.friday_gym`, or `project.current_focus`." Those examples all work. But the hint also says "goals" and "work context" as valid categories — and `goal.*` and `work.*` don't have registry entries. So the model learns to attempt those, fails silently, and the user's fact is lost.

**Fix:** Add `goal.*` and `work.*` as recognized prefix rules in `memory_registry.go`:
```go
{rawPrefix: "goal.", canonicalPrefix: "goal."},
{rawPrefix: "work.", canonicalPrefix: "work."},
```

Also update `capabilities.go` so the hint only references key patterns that actually pass canonicalization.

---

## Gap 2 — Stated Preference Facets Are Too Narrow

**The problem:** `preference.stated_preference` is the canonical key for explicit preference statements ("I prefer X"). For supersession to work, the system needs a conflict anchor. The anchor for `preference.stated_preference` is derived from `deriveExplicitPreferenceFacet(value)` — which currently only recognizes two facets:

```go
case "dark mode" / "light mode" → facet = "ui_theme"
case "morning" / "evening" / "afternoon" / "night" → facet = "time_of_day"
```

**What happens for everything else:** `facet = ""` → anchor is nil → no supersession. Entries accumulate. If the user says "I prefer concise answers" four times across four sessions, there are four separate distillate records, all eligible, all appearing in the wake state until the budget trims them.

**Examples that get no anchor:**
- "I prefer bullet points" — no facet
- "I prefer not to be interrupted mid-task" — no facet
- "I prefer you ask one question at a time" — no facet
- "I prefer formal language" — no facet

**Fix:** Expand `deriveExplicitPreferenceFacet()` to cover more common preference categories:

```go
case "bullet" / "list" / "numbered" → "response_format"
case "concise" / "brief" / "short" / "terse" → "verbosity"
case "verbose" / "detailed" / "thorough" → "verbosity"
case "formal" / "professional" → "tone"
case "casual" / "informal" → "tone"
case "one question" / "single question" → "question_style"
```

Or, at minimum, derive a facet from the first significant noun phrase of the value so that repeated preferences on the same topic can supersede each other.

---

## Gap 3 — Same-Entity Preview-Label Confusion (4 Known Failures)

**The problem:** The benchmark consistently fails four fixtures:
- `contradiction.profile_timezone_same_entity_wrong_current_probe.v1`
- `contradiction.profile_locale_same_entity_wrong_current_probe.v1`
- `contradiction.profile_timezone_interleaved_preview_chain_slot_probe.v1`
- `contradiction.profile_locale_interleaved_preview_chain_slot_probe.v1`

What these have in common: there's a canonical stored value (`timezone = PST`), and a conversation has contained "current"-sounding language (`currently in EST`, `timezone preview: EST for this meeting`) about the same entity. The system retrieves the current-looking distractor instead of the canonical slot.

**Why it happens:** The retrieval path uses hint text. Hints for "current"-sounding facts score well against "current" queries. The anchor correctly marks the canonical value, but when retrieval is ranking, recency and current-sounding language can outrank the anchored slot.

**The ablation data confirms this:** `anchors_off` collapses slot-only contradiction to 0/10. Anchors are necessary but not sufficient for the same-entity preview case. The `reduced_context_breadth` ablation shows contradiction survives but resume collapses — so the same-entity failure isn't a context-breadth problem.

**Potential fix:** When a query matches a known anchor slot (e.g., `usr_profile:timezone`), boost the retrieval score of the fact that owns that anchor tuple over same-entity facts without the anchor. This is the `continuity-preview-slot-preference` benchmark flag — it works, but hasn't shipped as product logic because a large margin is needed to be reliable (suggesting it's fragile as a heuristic alone). A more robust fix would be to explicitly demote "preview" or "upcoming" labeled facts in the retrieval rank for known profile slots.

---

## Gap 4 — Slot-Only Contradiction: RAG Baseline Wins (10/12 vs 8/12)

**The problem:** In the `memory_contradiction.slot_only` subfamily — where the query doesn't contain any hint of the answer, only the slot name — RAG baseline beats continuity (10/12 vs 8/12). This is the only family where continuity loses to a comparator.

**Why:** Slot-only contradiction is a pure lookup regime. The query is essentially "what is the user's preferred name?" with no answer clue. RAG's cosine similarity over the fact text happens to retrieve the right current value in 10 of 12 cases because the correct current fact text is probably more semantically similar to the query. Continuity's retrieval relies on hints, and when hints aren't distinctive enough from the distractor hints, the anchor alone can't save the ranking.

**The ablation confirms it:** `hints_off` collapses slot-only contradiction entirely (0/17). The hints are the load-bearing mechanism for slot-only retrieval, not the anchors. If hint quality for profile slots is weak, the anchor doesn't help with retrieval scoring.

**Potential fix:** For known anchor slots (name, preferred_name, preference facets), generate richer hint text that includes the slot domain and entity explicitly, not just the value. This would make the canonical slot hints more distinctive from non-anchored preview-label hints.

---

## Gap 5 — Continuity Inspection Threshold Is Opaque

**The problem:** The continuity inspection path (write path 2) is what converts a conversation into persistent memory without the user explicitly asking. The threshold that decides whether a conversation "meets the bar" for distillate derivation isn't surfaced in settings, in the benchmark, or in the capability hints.

If the threshold is too high, most conversations produce nothing from inspection. The only memory that accumulates is what the user explicitly requests — which is a significantly weaker experience than the product expectation that the assistant remembers things from conversations.

**What this means practically:** If a user has a rich conversation about their project context, work style, and team dynamics — but never says "remember X" — that conversation may produce zero durable facts. The model's capability hint discourages over-storage, but the threshold may be so conservative that valuable context is discarded.

**Recommendation:** Surface the inspection threshold in developer settings. Add a toggle (or numeric input) next to the idle behaviors toggles in `SettingsView.swift`. Run memorybench with reduced thresholds to measure the quality impact.

---

## Benchmark Honesty Assessment

The benchmark is more honest than most comparable internal AI benchmarks:

**Honest:** The poisoning bucket default-mode unfairness is explicitly documented with a footnote and a policy-matched rerun. The slot_only loss is included in the results rather than excluded. The ablations expose where the system is genuinely weak (hints are load-bearing, not just nice-to-have). The threats-to-validity section is specific and self-critical. The 4 specific failing fixtures are named.

**Limitations:** All fixtures are first-party and internally authored. No external dataset. `rag_stronger` is one stronger RAG configuration, not the upper bound. Task resumption is a category where structured state has an inherent architectural advantage — the 13/13 vs 0/13 gap reflects that, not pure implementation quality. The latency numbers (all 1ms) suggest benchmark-local execution, not realistic model-in-the-loop latency.

---

## Summary of Recommended Changes

| Gap | File | Change |
|---|---|---|
| Key registry too narrow | `internal/tcl/memory_registry.go` | Add `goal.*` and `work.*` prefix rules |
| Key registry too narrow | `cmd/haven/capabilities.go` | Update hint to only reference valid key prefixes |
| Preference facets too narrow | `internal/tcl/memory_registry.go` | Expand `deriveExplicitPreferenceFacet()` |
| Preview-label confusion | `internal/loopgate/continuity_memory.go` | Boost anchor-owning nodes in retrieval rank for known profile slots |
| Hint quality for slot-only | `internal/loopgate/continuity_memory.go` | Enrich hints for known anchor slots with domain+entity context |
| Inspection threshold opaque | `internal/loopgate/server_haven_settings.go` | Surface threshold as a configurable setting |
| Inspection threshold opaque | Native settings UI (if any) that binds the same Loopgate endpoints | Add threshold control in a developer/diagnostics surface |
