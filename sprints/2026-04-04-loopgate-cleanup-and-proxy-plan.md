# Loopgate cleanup and proxy plan

**Created:** 2026-04-04  
**Status:** active sprint plan  
**Scope:** delete legacy product surfaces that are safe to remove, remove the web admin/policy UI surface, and define + implement a minimal proxy v0 for the real memory UX.

This plan is intentionally narrower than the enterprise phased plan. It is the execution tracker for turning Loopgate into a cleaner, product-agnostic control plane with a usable automatic memory path.

---

## Why this plan exists

The current memory engine and benchmark story are strong enough to justify product work, but the delivery UX is still wrong.

- A **dedicated IDE bridge** (historically in-tree MCP; **now deprecated/removed** — ADR 0010) can be an explicit operator surface, but it exposes extra protocol/tool plumbing and does not create the "told it once, new session still knows" experience by default; **normative control plane** is **HTTP on the Unix socket** (and future **proxy** for chat).
- The repo still carries legacy Haven/Morph naming, docs, and shells that blur what Loopgate is.
- The web admin/policy UI is unnecessary attack surface and maintenance debt for the current product direction.

The sprint goal is to remove that ambiguity first, then build the smallest proxy slice that can create the default memory UX without widening scope.

---

## Non-goals

- no new custom UI
- no new IDE shell or OSS IDE fork
- no memory extraction / standalone product work
- no proxy platform boil-the-ocean rewrite
- no replacement web admin console
- no "quarantine forever" bucket for dead surfaces that are safe to delete now

If a legacy surface is unused and safely removable, delete it.

---

## Guiding rules

1. Delete dead surfaces instead of preserving them "just in case".
2. Keep Loopgate product-agnostic: control plane, **HTTP-on-UDS** typed APIs, proxy (when shipped); **in-tree MCP removed** (reserved for future ADR only).
3. Do not weaken policy, audit, or memory invariants to make proxy feel magical.
4. Keep proxy fast by making memory work conditional and bounded.
5. Prefer small PRs with tests first.

---

## Track 1 — Remove legacy Haven/Morph surfaces

### Goal

Make the active repo clearly about Loopgate as a control plane, not a mixed Loopgate/Haven/Morph product shell.

### Removal target classes

1. **Docs and maps**
   - remove active product language that treats Haven or Morph as current primary surfaces
   - keep only historical references when required to explain migration or legacy compatibility

2. **Prompt/runtime language**
   - remove Haven/Morph-specific instructions from the active generic prompt surface
   - preserve tool and memory safety guidance, but make it Loopgate-agnostic

3. **Legacy UI/runtime code**
   - delete `cmd/haven/` if no active Loopgate runtime path still depends on it
   - delete `internal/haven/` if no active Loopgate runtime path still depends on it
   - if a small shared compatibility piece is still referenced by active Loopgate code, replace that dependency first, then delete the legacy package

### Exit criteria

- active docs no longer present Haven/Morph as current product surfaces
- active prompt path no longer describes a Haven/Morph persona in the generic Loopgate path
- `cmd/haven/` and `internal/haven/` are either deleted or reduced to the minimal compatibility code still required by tested active paths

### Safety rule

Do not do a giant rename storm first. Delete dead surfaces first. Rename residual active identifiers later only where they still misrepresent active behavior.

---

## Track 2 — Remove the web admin/policy UI

### Goal

Delete the loopback admin console and its startup/config/documentation surface.

### Why

- extra web-auth surface
- extra docs drift
- extra hardening cost
- not required for the near-term product

### Removal targets

- `internal/loopgate/admin_console.go`
- `internal/loopgate/admin_console_ui.go`
- `internal/loopgate/admin_console_test.go`
- `--admin` startup path in `cmd/loopgate/main.go`
- admin-console startup/config branches in `internal/loopgate/server.go`
- `docs/setup/ADMIN_CONSOLE.md`
- architecture/setup references that still present the admin console as an active product surface

### Exit criteria

- no `--admin` CLI flag
- no admin-console listener startup path
- no admin token requirement
- no `/admin/*` surface
- docs no longer present a web policy/admin console as part of the product

### Safety rule

Delete only the UI/web surface. Do not accidentally delete underlying policy/config/audit data structures that are still used by the control plane or future proxy / **out-of-tree IDE bridge** work.

---

## Track 3 — Proxy v0

### Goal

Build the smallest local proxy that creates the default memory UX:

- user says something naturally
- Loopgate can remember durable facts automatically when appropriate
- a new session can get bounded current memory automatically

### Product boundary

Proxy v0 is not a second agent runtime and not a UI. It is a request/response mediation layer between the IDE and the model provider.

### Proxy v0 responsibilities

1. **Observe the latest turn**
2. **Decide cheaply whether memory work is needed**
3. **Inject bounded current memory when useful**
4. **Capture durable facts when explicit or high-confidence**
5. **Audit what was injected or stored**

### Proxy v0 non-responsibilities

- no autonomous planning
- no broad prompt rewriting
- no hidden background worker fleet
- no full enterprise transport matrix in v0
- no replacement for **in-tree** MCP typed tools (removed — use HTTP capabilities or out-of-tree forwarders)

### Performance model

Proxy stays responsive by using a cheap gate before any memory work:

1. inspect latest user turn
2. run a cheap trigger
3. if no trigger, forward immediately
4. if triggered, do bounded wake/discover work
5. inject a compact memory block and forward

### Hot-path constraints

- no full memory scan on every turn
- strict prompt-token budget for injected memory
- no repeated discovery passes by default
- short-lived cache for wake/discover results where safe
- fail closed on invalid memory capture, but do not silently inject stale or guessed memory

### Minimal supported cases in v0

1. **Task resumption**
   - inject bounded current blocker/next-step context

2. **Stable profile/settings facts**
   - timezone
   - locale
   - communication style
   - similarly stable explicit user facts already supported by the validated memory contract

3. **Explicit remember statements**
   - "remember that ..."
   - "keep in mind ..."
   - other clearly explicit durable-memory requests

### Exit criteria

- one provider path works through the proxy
- proxy injects bounded memory only when the cheap trigger fires
- explicit remember statements can persist through the validated memory path without manual **IDE bridge** schema entry
- proxy actions are auditable and test-covered
- no broad regression in ordinary non-memory requests

---

## Planned PR order

### PR 1 — Docs/maps cleanup

- remove active Haven/Morph product framing from docs and maps
- make Loopgate daemon + **HTTP control plane** + proxy + typed APIs the official surfaces (**in-tree MCP deprecated**)

### PR 2 — Prompt cleanup

- remove Haven/Morph-specific wording from the active generic prompt path
- preserve memory/tool safety rules

### PR 3 — Admin console removal

- delete admin console code, tests, flag, startup path, and docs

### PR 4 — Legacy Haven/Morph code deletion

- delete `cmd/haven/` and `internal/haven/` once active dependencies are gone or replaced

### PR 5 — Proxy v0 design doc

- write the concrete request/response flow, trigger logic, audit behavior, and latency budget

### PR 6 — Proxy v0 skeleton

- implement one provider path
- implement cheap trigger + bounded injection
- implement explicit durable fact capture
- add audit/debug trace

---

## Test strategy

Every track needs tests before deletion or behavior changes.

### Cleanup / deletion tests

- prompt tests prove generic Loopgate path no longer uses Haven/Morph persona language
- startup tests prove admin-console path is gone
- focused import/build tests prove deleting legacy surfaces did not break active Loopgate binaries

### Proxy tests

- no-trigger requests forward without memory lookup
- task-resumption trigger injects bounded memory
- explicit remember statement stores through validated memory path
- invalid memory capture fails closed
- memory lookup failure does not silently inject junk
- every injection/capture path emits audit/debug evidence

---

## Progress log

Append short notes here as the plan moves.

| Date | Update |
|------|--------|
| 2026-04-04 | Plan created. Current repo inventory confirms active legacy surfaces in `cmd/haven/`, `internal/haven/`, active admin-console code in `internal/loopgate/admin_console*.go`, and prompt/docs drift still referencing Haven/Morph. |

