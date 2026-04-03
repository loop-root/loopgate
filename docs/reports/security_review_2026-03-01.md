**Last updated:** 2026-03-24

# Security Review

Status: historical review snapshot  
Authority: non-authoritative  
Purpose: document architectural feedback at the time of the review

This document reflects a point-in-time security review and may not
fully match the current implementation state.

The authoritative architecture specification is located at:

docs/design_overview/architecture.md

# Loopgate security review – March 2026 (historical)

**Terminology:** Body text below sometimes says *Morph* for the unprivileged client. Current docs use **operator client** / **Haven** for that role and treat **Loopgate** as the primary product name.

This document summarizes the current security posture of the operator-client + Loopgate architecture and highlights areas that have improved as well as areas that still require hardening. It is intended to guide further work by human contributors and automated coding agents (Codex).

---

# Overall Assessment

The system has undergone meaningful security hardening compared to earlier revisions.

Key improvements include:

- A clearer control‑plane split between Morph and Loopgate
- Unix domain socket transport with strict permissions
- Peer credential verification for client identity
- Signed privileged requests with nonce and timestamp
- Replay protection mechanisms
- Approval state machine improvements
- Explicit secret‑export denial
- Result classification metadata

The architecture is now credible as a **local-first control‑plane security model**, though several areas still require additional tightening.

---

# Architecture Review

The core architectural model remains sound:

Model → Morph → Loopgate → Integrations

Morph acts as an **unprivileged client runtime**, while Loopgate operates as the **privileged control plane**.

This boundary is now implemented in code rather than existing only in documentation, which is a major improvement.

Loopgate is responsible for:

- Policy evaluation
- Secret resolution
- OAuth flows
- Approval management
- Capability token issuance
- Integration execution

Morph is responsible for:

- CLI and UI rendering
- Model interaction
- Local session and memory
- Forwarding capability requests to Loopgate

This separation should remain strictly enforced.

---

# Transport and Authentication Hardening

Recent changes significantly strengthened the transport layer.

Current protections include:

- Unix domain socket transport
- Socket directory permissions set to 0700
- Socket file permissions set to 0600
- Peer credential verification on Linux and macOS
- Server‑issued session authentication key

Privileged requests are now signed with:

- Session identifier
- Timestamp
- Nonce
- HMAC signature

Replay protection exists through nonce tracking and request identifier checks.

This transforms the control plane from a simple local RPC interface into a **peer‑bound authenticated session protocol**.

However, the current trust boundary still relies on **same‑user OS identity**, which is acceptable for v1 but should be documented clearly.

---

# Approval System

The approval system has improved substantially.

Enhancements include:

- Explicit approval state machine
- Pending approval registry
- Decision nonce usage
- Approval token issuance
- Replay protection
- Actor/session binding

Approval flows now correctly prevent Morph from self‑authorizing privileged actions.

Loopgate remains the single source of truth for approval state.

Further hardening could include:

- Binding approvals to normalized request parameters
- Shorter approval TTLs
- Single‑use approval tokens for high‑risk actions

---

# Capability Token Model

Capability tokens are currently implemented as opaque random strings with in‑memory claim tracking.

This model is functional but not yet fully mature.

Strengths:

- Capability scoping
- Session binding
- Expiry enforcement

Areas for improvement:

- Explicit token identifiers (jti)
- Single‑use capability tokens for sensitive actions
- Replay detection at the token level
- Binding tokens to normalized request arguments

At present the **request envelope security is stronger than the capability token model itself**.

---

# Replay Protection

The system now includes several replay‑defense mechanisms:

- Request ID uniqueness enforcement
- Signed request nonce tracking
- Duplicate request rejection

These protections significantly reduce the risk of replay attacks within a session.

Further improvement could include periodic pruning of nonce tracking structures to prevent memory growth.

---

# Secret Handling

Secret‑handling rules are now explicitly enforced.

Loopgate denies attempts to retrieve:

- API keys
- Access tokens
- Refresh tokens
- Client secrets

Secret material remains stored behind secure store interfaces and is not exposed to Morph.

Tests exist verifying that secret export requests are rejected.

This invariant should remain non‑negotiable.

---

# Result Isolation

Result classification metadata now exists to control how outputs propagate through the system.

Current classifications include flags such as:

- prompt_eligible
- memory_eligible
- display_only
- audit_only
- quarantined

This provides the basis for preventing unsafe transport payloads from entering prompt context.

However, the current implementation still allows raw file content to flow through structured results for certain capabilities such as fs_read.

This behavior should be treated as an explicit policy decision rather than an implicit default.

Future improvements should include:

- Capability‑specific classification policies
- Mandatory quarantine for remote integration payloads
- Explicit safe output schemas for integrations

---

# Audit Logging

Audit logging has improved but still lacks a fully consistent failure policy.

Some logging paths propagate errors, while others treat audit writes as best‑effort.

Recommended improvement:

Define two categories of events:

Critical events (must fail closed if audit logging fails):

- capability execution
- approval decisions
- policy denials
- secret access attempts

Informational events (may warn but continue):

- UI rendering
- diagnostics

A unified audit policy would prevent inconsistent behavior.

---

# Remaining Architectural Risks

The following areas should be prioritized for future hardening.

## Duplicate Logging Implementations

Legacy logging paths in some Morph adapters still swallow errors and duplicate functionality already implemented in the orchestrator logger.

These paths should be removed or redirected to the canonical logger.

## Capability Result Policy

Result classification should not rely solely on the absence of quarantine markers.

Each capability should define its own output classification policy.

## Capability Token Semantics

Token semantics should be strengthened with:

- explicit token identifiers
- single‑use options
- tighter request binding

## Classification Consistency

Result classification flags should be validated server‑side to prevent contradictory states.

---

# Dependency Review

The dependency footprint remains very small.

Direct dependencies include:

- github.com/chzyer/readline
- gopkg.in/yaml.v3

Indirect dependency:

- golang.org/x/sys

The latter is justified due to peer credential support.

Overall dependency posture is excellent and aligns with the goal of minimizing supply‑chain risk.

---

# UI Security Boundary

The UI subsystem remains clean and intentionally simple.

Key positive traits:

- String‑based rendering
- ANSI aware layout
- No heavy TUI frameworks
- Safe display helpers

The next improvement should focus on making the UI **classification aware**, meaning it renders results based solely on control‑plane metadata rather than making local decisions about sensitivity.

---

# Recommended Next Hardening Steps

Priority order:

1. Remove legacy logging paths that swallow errors
2. Implement capability‑specific result classification
3. Strengthen capability token semantics
4. Define a unified audit failure policy
5. Add server‑side validation for classification metadata
6. Implement quarantine storage for remote integration payloads

---

# Conclusion

The Morph project has transitioned from an experimental architecture into a credible security‑aware control plane.

The most important progress is that **security boundaries are now enforced in code rather than merely described in documentation**.

Further improvements should focus on consistency, capability output policy, and token semantics.

With continued incremental hardening, the system can evolve into a robust local‑first AI orchestration platform.
