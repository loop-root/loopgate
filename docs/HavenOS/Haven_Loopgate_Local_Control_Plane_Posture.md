**Last updated:** 2026-03-26

# Haven ↔ Loopgate: local control plane security posture (v1 and v2 backlog)

This document records **engineering and threat-model findings** for the **v1** Loopgate binding (**HTTP on the Unix domain socket**). It states what we **assume**, what we **enforce today**, what is **intentionally deferred**, and what remains on the **v2 backlog** without pretending the boundary is stronger than it is.

**Rule of thumb:** *Code wins over this doc.* When behavior changes, update this file and the integrator guide (`docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`).

**Related:** `docs/loopgate-threat-model.md`, `docs/HavenOS/Haven_Loopgate_Security_and_Transport_Checklist.md`, `docs/design_overview/loopgate.md`, RFC 0001 (token policy).

---

## 1. Honest scope: what v1 is protecting against

- Loopgate listens on a **machine-local Unix socket**, not a public TCP port. **OS file permissions** on the socket and its directory limit *who* can connect (typically the desktop user who owns the runtime tree).
- The threat model **does not** treat “same desktop user” as fully trusted: **any process running as that user** that can open the socket may speak HTTP to Loopgate.
- If an attacker **already runs arbitrary code as that user**, they can often do **much worse** than call Loopgate (read user files, abuse other local IPC, social-engineer prompts, etc.). Loopgate hardening **reduces blast radius** and **reconnaissance value** of the control plane; it does **not** solve a fully compromised user account by itself.

We document this plainly so **v2** work (client attestation, optional XPC-class transport) can be prioritized against **realistic** residual risk, not implied guarantees v1 does not provide.

---

## 2. What v1 already enforces (strengths)

- **Peer identity from the kernel:** Unix socket peer credentials (e.g. UID / PID / platform-specific fields) are attached to the request context for privileged paths.
- **Session binding:** Capability and approval tokens minted at `POST /v1/session/open` are bound to the **full peer identity** recorded at open time. A **different process** (even same UID) cannot reuse another process’s token: `authenticate` and `authenticateApproval` reject **peer mismatch**.
- **Signed request envelope:** After session open, privileged routes use the **HMAC + nonce** contract (see `internal/loopgate/client.go` and RFC 0001) so arbitrary POST bodies are not accepted without session integrity.
- **Policy and capability gates:** Execution remains **server-side** policy and registry driven; natural language and client labels do not grant authority.

---

## 3. Unauthenticated vs authenticated HTTP routes (reconnaissance)

**Shipped v1 behavior (2026-03-26):**

| Route | Auth | Notes |
| --- | --- | --- |
| `GET /v1/health` | None | **Liveness only:** `version`, `ok`. No policy, capabilities, or connections. |
| `GET /v1/status` | Bearer + signed GET | Full inventory (policy, capabilities, counts, connections) — same envelope as other privileged GETs. |
| `GET /v1/connections/status` | Bearer + signed GET | Connection summaries. |

**Residual note:** Any same-user process that can open the socket can still call **`/v1/health`** and learn the server is up plus a version string. That is intentional for launch scripts and cheap probes; it is **not** a capability map.

---

## 4. Session open (`POST /v1/session/open`) without prior token

- By design, **session open** has **no** bearer token (bootstrap). Any peer that passes validation can request a scoped session if it satisfies **requested_capabilities** ∩ server registry and **policy**.
- **Optional executable path pinning** exists in code (`expectedClientPath` + `resolveExePath` in `handleSessionOpen`) but is **not** wired from production `NewServerWithOptions` today—so v1 does **not** generally restrict *which* binary opened the session.
- **Residual risk:** A same-user process that learns valid capability **names** (from shipped binaries, docs, or another cooperating client) can still attempt **session open** and then use **signed** requests like a legitimate client—subject to policy, audits, and session limits. Unauthenticated **`GET /v1/status`** no longer provides that inventory.

**v2 backlog (not v1 commitment in this doc):**

- **macOS code signing verification** of the peer executable (pin Team ID + signing identifier; **fail closed** on verification failure).
- And/or **XPC-class transport** (see `Haven_Loopgate_Security_and_Transport_Checklist.md` §0) to tighten *who* can be a client, with **application-layer** session semantics preserved or improved.

We are **not** claiming v1 eliminates same-user impersonation of a “legitimate” Loopgate client; we **are** documenting that inventory is no longer exposed without a session, and that **client attestation / transport** remain the right levers for stronger same-user boundaries.

---

## 5. Relationship to “bigger problems”

If an operator is compromised at the **user-account** level, Loopgate hardening is **defense in depth**, not a cure. The product still benefits from:

- **Less** structured recon over HTTP on the socket.
- **Stronger** binding of tokens to process identity for **non-bootstrap** traffic.
- **Clear** documentation so security reviews and future transport work do not overstate v1 guarantees.

---

## 6. When to revisit (v2 triggers)

Re-open this posture explicitly when:

- Unauthenticated surface changes beyond **`GET /v1/health`** (if we ever shrink that too).
- `expectedClientPath` or **codesign-based** client verification ships.
- **XPC** (or another Mach-bound profile) is scheduled and adapter boundaries are defined.
- The integrator guide or threat model **assumptions** change (e.g. multi-user or remote transport).

---

## 7. Summary table

| Topic | v1 posture (honest) | Planned / v2 |
| --- | --- | --- |
| Transport | HTTP on UDS | Optional XPC-class IPC (TBD) |
| Token + signing after open | Yes | Preserve or strengthen with transport |
| Peer binding on token use | Yes (full peer identity) | Keep; may combine with stronger client proof |
| Unauthenticated GET inventory | **`GET /v1/health` only** (no policy/caps); status/connections signed | N/A |
| Client binary attestation | Not generally enforced | Codesign pin +/or XPC |
| “Safe if user is evil” | **No** | Still no; depth vs account compromise |
