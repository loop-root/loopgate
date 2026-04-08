**Last updated:** 2026-04-01

# System contract: operator client and Loopgate

This document defines what must remain true in the **operator client + Loopgate** split.

**Terminology:** The **operator client** is whatever local process attaches to Loopgate (IDE-hosted agent, CLI, native UI, or proxy-integrated editor). This contract uses **operator client** / **client** for the unprivileged runtime. Prefer **HTTP on the Unix socket** for real workflows (`docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`). **In-tree MCP is deprecated** (`docs/setup/LOOPGATE_MCP.md`, ADR 0010).

## 1) Purpose

The **operator client** is the local client/runtime shell (chat, planning, session UX).

**Loopgate** is the local control plane.

The client is responsible for model interaction, local memory, and user experience.

Loopgate is responsible for policy, approvals, capability execution, and integration auth.

## 2) Non-negotiable invariants

### 2.1 Append-only user ledger

- The client's user-visible ledger is append-only.
- Existing ledger entries must not be rewritten in place.
- Security-relevant client outcomes must remain observable.
- Ledger entries now carry chained metadata so sequence and prior-hash continuity remain visible across restarts.

### 2.2 Separate control-plane telemetry

- Loopgate writes its own audit-safe runtime event stream.
- Loopgate telemetry must not be silently merged into the client's user ledger.
- Critical client-side ledger events must use must-persist audit handling; warn-only behavior is reserved for diagnostics and non-security telemetry.

### 2.3 Loopgate is the control-plane authority

- Capability execution must be authorized by Loopgate.
- Approval state must be created and enforced by Loopgate.
- Policy decisions for Loopgate-mediated capabilities must be made in Loopgate.
- The client may display and submit approval decisions, but it must not self-authorize execution.
- Privileged client-to-Loopgate requests must carry Loopgate-issued session binding proof, not just bearer tokens.

### 2.4 Model output is untrusted

- Model output is content, not authority.
- Parsed capability requests must be validated before being sent to Loopgate.
- Model output must not directly mutate trusted state.

### 2.5 Secret export is forbidden

- Loopgate must never reveal raw provider credentials, refresh tokens, access tokens, client secrets, or stored key material to the operator client.
- No API contract may expose raw third-party secret material.
- Loopgate-issued capability tokens are allowed; provider tokens are not.

### 2.6 HTTP content is not prompt input by default

- Raw HTTP or integration bodies must not be fed into operator-facing prompts by default.
- Loopgate should return structured, extracted fields only.
- Raw bodies must remain quarantined inside Loopgate unless a future explicit reviewed path is designed.
- Response metadata should classify whether results are prompt-eligible, memory-eligible, or quarantined.
- Capability result classification must be explicit per capability; it must not be inferred solely from whether a quarantine marker is present.

## 3) Trust boundaries

### Trusted

- Operator client binary
- Loopgate binary
- Loopgate policy enforcement
- Client ledger append implementation
- Client state persistence implementation

### Untrusted

- model responses
- user prompts
- file contents
- capability arguments
- capability results until validated for display/use
- external integration responses
- Loopgate connection metadata until decoded and validated by the client

## 4) Current execution contract

Current flow:

`Model -> client parser -> Loopgate capability request -> Loopgate policy/approval/tool execution -> structured response -> client ledger/history`

This means:

- The client is no longer the execution authority for current filesystem capabilities
- Loopgate is the current tool dispatcher for those capabilities
- The client remains the session and continuity-thread owner on the unprivileged side

## 5) Secrets contract

Current reality:

- model provider config is stored locally in the client as non-secret metadata only
- Loopgate validates model runtime config and resolves model provider secrets for live inference
- Loopgate now owns persisted connection records and secret-ref validation for integrations
- raw integration/provider secrets are still not stored in client-visible runtime state
- Loopgate now resolves `SecretRef` values through OS-backed secure storage on macOS and fail-closed secure stubs elsewhere
- Loopgate now supports `client_credentials` and PKCE connection flows, with in-memory access tokens and secure-backend refresh-token storage for PKCE

Target contract:

- third-party integration secrets live in Loopgate-owned secure storage
- Loopgate performs OAuth and token exchange itself
- The client never receives provider tokens

## 6) Approval contract

- approval requests originate in Loopgate
- The client only renders approval prompts and submits decisions
- approval denial and approval grant must both be auditable
- capability tokens must not bypass approval requirements
- approval decisions must bind to normalized action parameters
- approval decisions must be single-use and bound to a Loopgate-issued decision proof

## 7) Capability-token contract

- minted only by Loopgate
- short-lived
- scoped by requested capabilities
- revocable by expiry or Loopgate restart in the current MVP
- never convertible into provider credentials
- used only alongside a signed control-session request envelope

The normative token transport, replay, and denial rules are defined in [RFC 0001](../rfcs/0001-loopgate-token-policy.md).
UI-facing rendering and bridge behavior must also follow the [UI Surface Contract](./ui_surface_contract.md).

## 8) Current scope vs planned scope

### Implemented now

- local Loopgate Unix-socket service
- Loopgate-owned policy and approval flow for filesystem capabilities
- client-to-Loopgate capability-token session flow
- Loopgate token use bound to the authenticated Unix-socket peer
- single-use approval decision nonce flow
- explicit denial for secret-export-like capability names
- structured capability responses
- chained Loopgate audit metadata per event
- chained client ledger metadata per event
- persisted Loopgate connection records with secret refs only
- macOS Keychain-backed secure secret resolution for Loopgate
- configured client-credentials and PKCE connection flows inside Loopgate
- typed provider-backed read capability execution with quarantined raw payloads

### Not yet implemented

- authorization-code flow without PKCE
- Windows Credential Manager and Linux Secret Service backends
- broader external integrations
- skill manifests
- broader structured HTTP adapter framework
- public APIs

## 9) Definition of done for future control-plane changes

A control-plane change is done when:

- Loopgate remains the execution authority
- The operator client does not gain a convenience bypass
- secret-export invariants remain enforced
- denial paths are explicit and tested
- user ledger and Loopgate telemetry stay separate
- docs reflect the new trust boundary
