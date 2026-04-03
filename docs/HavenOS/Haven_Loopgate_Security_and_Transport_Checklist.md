**Last updated:** 2026-03-26

# Haven ↔ Loopgate: Security hardening & transport checklist

This document is the **working roadmap** for (1) **security and integrity fixes** on the Loopgate side and (2) **optional post-launch** transport hardening on macOS. It extends the review summarized from `~/Downloads/morph-review.md` and the agreed engineering plan.

**Rule of thumb:** *Code wins over this doc.* Update this checklist when items ship or scope changes.

**v1 product decision (2026-03-25):** Haven ↔ Loopgate **v1 ships with HTTP** on the **local control-plane binding** (Unix domain socket in typical layouts). That is the **standard v1 architecture**—chosen to reduce engineering cost and time to a signed `.dmg`. **Apple XPC is not required for v1**; section 0 below describes a **future optional** path if the team revisits Mach-bound IPC after ship.

---

## 0. Transport pivot: Apple XPC (macOS) — **post-launch / optional (TBD)**

**Schedule:** **TBD** — no committed milestone; v1 remains **HTTP on Unix domain socket** until a separate product/engineering decision schedules this work.

**Goal (if pursued later):** Use **XPC** as an **optional, hardened** IPC between the **Haven desktop process** and the **Loopgate / control-plane** side on macOS—**instead of** relying solely on **HTTP** on the local UDS listener for UI and shell features.

**Why (when revisited):** XPC can add **explicit Mach service boundaries**, **code signing and sandbox integration**, and **reduced attack surface** versus an HTTP stack on the same boundary. **v1 accepts HTTP over UDS** as the cost-effective local profile; this section remains the design backlog **if** we later justify the native bridge and migration work.

**What stays:** Loopgate remains the **authority** for policy, capabilities, audit, continuity, and secrets. **Domain logic** (memory mutations, task workflow, approvals) **does not move**—only **how Haven invokes it** changes at the adapter.

**Undo / migration scope:**

- [ ] Inventory **HTTP routes** used only for **Haven UI** (e.g. wake-state–class reads, **task list / status** if still exposed over HTTP) and mark them **deprecated** once XPC parity exists.
- [ ] Implement an **XPC service** (or embed Mach listener) with a **versioned message protocol** (typed requests/responses, size limits, explicit error domains)—avoid ad-hoc JSON blobs without bounds.
- [ ] **Swift / Objective-C bridge** (or small native helper) from Haven to XPC; keep **Go** on the Loopgate side with a **thin native shim** if Loopgate remains a Go binary (XPC listener in helper process talking to Loopgate via existing UDS **only behind localhost**, or in-process refactor—**decide one architecture** and document it here).
- [ ] **Code signing & entitlements:** define which binaries participate, hardened runtime, and sandbox profiles for Haven vs Loopgate helper.
- [ ] **Session integrity:** preserve today’s **control session + signed request** semantics **at the application layer** inside XPC payloads (or document a stronger replacement that is **not** weaker than current HMAC + nonce rules).
- [ ] **Migrate callers:** switch Haven from `internal/loopgate` HTTP client paths for UI operations to XPC; **feature-flag** or **build-tag** during transition.
- [ ] **Remove redundant HTTP** handlers for migrated operations; leave **non-UI** or **emergency/debug** paths only if explicitly justified and locked down.
- [ ] **Tests:** integration tests for XPC round-trips; regression tests ensuring **no silent downgrade** of auth or audit behavior.

**Open decision (fill in when chosen):**

- [ ] **Architecture A:** Loopgate Go binary exposes UDS only to a **trusted local XPC helper**; Haven talks XPC → helper → UDS.
- [ ] **Architecture B:** Loopgate gains a **cgo / native** XPC endpoint (larger change).
- [ ] **Architecture C:** Split processes differently (document).

*Record the chosen option and rationale in a short ADR or a subsection below.*

---

## 1. Integrity & audit (Tier 1)

- [ ] **S1 – `mutateContinuityMemory` ordering:** Ensure **no durable continuity JSONL append** without a corresponding **successful audit** for the same logical mutation, or add **startup reconciliation** for orphaned continuity lines (fail-closed or operator-visible). Include **todo status** and all `mutateContinuityMemory` call sites in tests.
- [ ] **S2 – Nonce replay persistence:** **Atomic write** (temp + rename) for `nonce_replay.json` where possible; handle **shutdown** directory removal without weakening security; document actual failure modes (current code **fails startup** on corrupt JSON—keep or soften deliberately).
- [ ] **Regression tests:** Injected failures after continuity append, after audit, after `saveMemoryState`; replay idempotency.

---

## 2. Input limits & policy clarity (Tier 2)

- [ ] **S5 – Continuity inspect:** Add **`maxContinuityEventsPerInspection`** (and optional payload caps) in **`normalizeContinuityInspectRequest`** *before* heavy derivation—complement existing **`maxCapabilityBodyBytes`** HTTP cap.
- [ ] **S4 – `isSecretExportCapability`:** Document heuristics; plan **finite registry allowlist** per documented Loopgate invariant (see `docs/loopgate-threat-model.md`); add tests for borderline capability names.
- [ ] **S3 – Morphling state HMAC (optional cleanup):** Decode hex → compare **32-byte** digests with `hmac.Equal`, or document why hex-string compare is intentional.
- [ ] **S6 – Tool name canonicalization:** Audit events record **raw model tool name** + **canonical registry name** where policy allows.

---

## 3. Memory & wake behavior (Tier 3)

- [ ] **§2.4 – Token budget trim:** Under prompt budget pressure, **drop lowest-priority items first** (trim from end of ordered lists or reverse trim order)—verify against product intent; add golden tests.
- [ ] **§2.2 – Derived vs authoritative state:** Reduce or eliminate persisting **derived** wake/diagnostic snapshots alongside authoritative continuity; rely on **rebuild-on-load** where already supported; migration plan + tests.

---

## 4. Concurrency & morphlings (Tier 3)

- [ ] **Worker launch / spawn TOCTOU:** Trace **`spawnMorphling`** and worker paths under **`morphlingsMu` / `server.mu`**; align with **single-lock** rules for one logical operation; concurrent tests.
- [ ] **Morphling spawn as native tool (product):** Register spawn in the tool registry so structured tool-use can reach it—**separate security review** for new surface.

---

## 5. Product / reliability (parallel)

- [ ] **`invoke_capability` vs explicit tools:** Reduce nested JSON failure modes for native tool-use where practical.
- [ ] **Discover / ranking:** Document limitations; plan future retrieval improvements (out of scope for Tier 1–2).

---

## 6. Documentation & meta

- [ ] Update **`docs/design_overview/loopgate.md`** (or equivalent) when XPC lands: transport diagram, trust boundaries.
- [ ] Update **`context_map.md`** if file names or adapter layout change.

---

## 7. Local HTTP reconnaissance (v1 hardening)

**Context:** `docs/HavenOS/Haven_Loopgate_Local_Control_Plane_Posture.md`.

- [x] **`GET /v1/status`** and **`GET /v1/connections/status`:** require **Bearer + signed GET**; **`GET /v1/health`** exposes only `version` + `ok` for probes.
- [x] Update **`docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`**, **`internal/loopgate/client.go`**, Haven bootstrap, integration/shell harnesses, and **`start.sh`** / Swift dev script.
- [x] **Regression tests:** unauthenticated inventory denied; health succeeds (`internal/loopgate/server_test.go`).

**v2 backlog (documented in posture doc, not required for checklist closure here):** executable **codesign** pinning at `session/open`, optional **XPC** transport.

---

## References

- Security review: external `morph-review.md` (2026-03-24).
- **Local control plane posture (v1 / v2):** `docs/HavenOS/Haven_Loopgate_Local_Control_Plane_Posture.md`
- Invariants: `docs/loopgate-threat-model.md`, `docs/design_overview/loopgate.md`; maintainer `AGENTS.md` is gitignored when used for agent sessions.
- Master index: `context_map.md` (local; gitignored in some workflows).
