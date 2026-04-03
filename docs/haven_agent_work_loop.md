**Last updated:** 2026-03-29

# Haven bounded agent work loop (design)

This document describes the **minimum** control-plane support for a **bounded**, **non-autonomous** agent loop: user messages can drive visible task-board work without widening Loopgate authority or adding background workers.

## Authority split (unchanged)

| Layer | Owns |
|-------|------|
| **Haven** | Messages UI, task board presentation, journaling surfaces, continuity UX, when to call model/chat and when to stop |
| **Loopgate** | Policy, approvals, capability execution, audit, structured `todo.*` semantics |
| **Morph (model path)** | Intent interpretation textually; tool/capability calls only through Loopgate |

Natural language does **not** create authority. New HTTP routes are **thin wrappers** around existing `todo.add` / `todo.complete` execution.

## Loopgate: work-item ensure / complete

Implemented in `internal/loopgate/server_haven_agent_work.go`, registered in `server.go`:

- **`POST /v1/haven/agent/work-item/ensure`** — requires actor **`haven`**, capability **`todo.add`**, signed JSON body `{ "text", "next_step?" }`. Runs the same path as `capabilities/execute` for `todo.add` with `source_kind: haven_agent` and carry-over task kind. Returns `{ item_id, text, already_present }`.
- **`POST /v1/haven/agent/work-item/complete`** — requires actor **`haven`**, capability **`todo.complete`**, body `{ item_id, reason? }`. Completes the board item.

Audit events: `haven.agent_work_ensure`, `haven.agent_work_complete` (reason redacted).

Wire details: [LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md](setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md) §7.2.

## Intended client state machine (Haven-owned)

Haven should track a **single** explicit phase per active “work thread” (not a daemon). Suggested enum:

`idle` → `planning` → `acting` → (`waiting_for_approval` | `waiting_for_user`) → (`completed` | `failed`) → `idle`

Rules:

- **No infinite loops** — each user send or explicit resume advances at most one bounded step (e.g. one model round, one capability batch, or a deliberate pause).
- **Approval** — when a tool path returns pending approval, set `waiting_for_approval`, stop streaming further autonomous steps, surface UI approvals (existing Loopgate UI routes). Do not mark the task completed.
- **User input** — set `waiting_for_user` and stop until the user sends the next message.
- **Completion** — call **`work-item/complete`** with the stored `item_id`, emit a short completion line, optional deep link / “take me there” if the product has a route.

Classification (Haven-side heuristic or small model prompt) should bucket messages as:

1. **Answer-only** — chat/model only; no task ensure.
2. **Task / work item** — call **ensure** once per stable title (dedupe via `already_present`), short acknowledgment, then planning/acting.
3. **Tool action** — drive through existing capability execution; respect approval gates.
4. **Approval-gated action** — transition to `waiting_for_approval` immediately when the control plane indicates it.

## End-to-end: “Please organize my files” (Swift product path)

**Small / single-turn organize asks** (e.g. “organize my Downloads”) should **not** require `todo.add`. The model drives **inspect → plan → apply** through the normal **`/v1/haven/chat`** tool loop; Loopgate surfaces **approvals** before anything mutates the host. No task-board row is created unless the product explicitly chooses a **planning / multi-step** UX that calls ensure (see below).

1. User sends message in Haven Messages.
2. Optional: Haven may set lightweight UI state from a narrow heuristic (e.g. folder-organize phrasing) — **not** policy, **not** authority.
3. Haven calls **`/v1/haven/chat`** with the user text. Loopgate runs tools; **approval-gated** capabilities stop the turn with `approval_required` until the operator acts in Loopgate.
4. After successful apply, chat responses may include **`ux_signals`** such as `host_organize_applied` for client polish (e.g. avatar bubble). **No** `work-item/complete` is required for this path.

## Optional: task-board ensure / complete (planning-heavy flows)

When the product wants a **visible Task Board** row before or alongside work (deduped titles, explicit milestones), Haven may call **`work-item/ensure`** then chat, and **`work-item/complete`** when the structured result says the work finished — same as the state machine in §3. That path **requires** `todo.add` / `todo.complete` on the session token; do **not** use it as a gate for simple organize requests.

**Client implementations**

- **Canonical (Swift):** `MessengerViewModel` uses **chat-first** for host-folder organize heuristics — **no** ensure-before-chat. `MorphHTTPClient.havenAgentWorkItemEnsure` / `havenAgentWorkItemComplete` remain available for future task-board–driven flows or other surfaces (e.g. Tasks window).
- **Reference:** the in-repo **Wails** shell (`cmd/haven/`) may still implement ensure + complete around organize for tests and contract validation: `loopgate.Client` methods, `haven:agent_work_phase` events, and `cmd/haven/agent_work_loop.go`. Treat Wails as **frozen for product ship**, not the Swift contract.

## Remaining gaps (before “generally agentic”)

- **Haven**: optional **Task Board** flows may still wire ensure/complete + phase UI + `item_id` persistence where product requires visible milestones; chat-first organize already uses approval pending from **`/v1/haven/chat`**.
- **Classification**: replace or augment heuristics with a constrained classifier; keep fail-closed defaults (unknown → answer-only or explicit ask).
- **Journal / continuity**: same loop shape — anchors only via policy-approved capabilities; no duplicate trackers.
- **E2E tests**: full stack tests live in the Haven repo for client routes and tool/approval behavior.

## Invariants (AGENTS.md)

- No new privileged capabilities; wrappers require existing token scope.
- No background goroutines in Loopgate for this feature.
- Model output remains untrusted; only structured capability results define task ids.
- Denials stay explicit; HTTP + `CapabilityResponse` error shapes preserved.
