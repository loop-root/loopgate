**Last updated:** 2026-04-20

# Runtime Invariants

## Core priorities

1. Correctness
2. Security
3. Determinism
4. Observability
5. Simplicity
6. Convenience

Do not weaken policy or trust boundaries to make the product feel smoother.

## System model

Loopgate is a policy-governed AI governance engine.

For the current public repo, treat it as a local-first, single-node control
plane for AI-assisted engineering work.

Important assumptions:

- Loopgate is the authority boundary and enforcement node.
- IDEs, MCP hosts, proxy-integrated editors, and similar surfaces are clients,
  not authority sources.
- Audit data must remain reliable, append-only, and tamper-evident where the
  format supports it.
- User-visible audit data must remain separate from internal telemetry and
  runtime logs.
- State transitions affecting security, approvals, lifecycle, tools, or secret
  access must remain explicit and reviewable.

## Authority and transport

- The control plane is the only authority for privileged actions.
- Natural language never creates authority.
- References are identifiers, not trust grants.
- Model output, memory strings, summaries, and UI state are content, not
  authority.
- Internal control-plane features must not be turned into public network APIs
  by convenience.
- Local transport must remain local-only by default.
- The current privileged local transport is HTTP over a Unix domain socket.

## Security model

Treat the following as untrusted until explicitly validated:

- model output
- tool output
- file content
- environment variables
- config loaded from disk
- user prompts
- memory and summaries
- agent-produced JSON or YAML

Do not:

- broaden allowlists without explicit justification
- add permissive fallback behavior after validation failures
- convert typed denials into vague success-looking responses
- infer permission from phrasing or intent

## Ledger and audit invariants

- The ledger is append-only.
- Existing entries must never be modified in place.
- Ordering must remain monotonic and explainable.
- Partial writes must not create ambiguous state.
- Audit append failures for security-relevant actions are hard failures unless a
  reviewed design explicitly says otherwise.
- Hash-chaining or checkpoint integrity must not be weakened into plain mutable
  logs.
- User-facing audit views are derived from authoritative state; they do not
  replace it.

## Policy invariants

- Policy is authoritative.
- Policy evaluation must remain deterministic.
- Deny-by-default is preferred.
- Absence of permission is a denial.
- Typed capability registration and policy binding define authority, not
  natural-language labels.

## State and projection invariants

- Expected monotonic state must not move backward by accident.
- Expired, deleted, revoked, or terminated objects must not be resurrected by
  reload quirks or fallback reconstruction.
- User-visible summaries, approval cards, and event feeds are derived views,
  not source-of-truth state.
- UI rendering must not leak private runtime paths, raw secret material, or
  internal-only identifiers not intended for the operator surface.

## Security invariants that must not be weakened

1. Audit append failure on security-relevant actions is a hard failure.
2. Do not split a logical `server.mu` auth/expiry state transition across
   multiple lock windows.
3. Path resolution failures must be denied, not retried with a weaker resolver.
4. Secret-bearing request fields must be cleared or isolated before they reach
   logs, errors, or audit paths.
5. Defense-in-depth secret-export guards must not become the primary boundary.
6. Do not add background autonomous workers without explicit design review.
7. Model-originated strings must not appear verbatim in public status responses
   unless the surface is explicitly designed for tainted content.

## Specific behavioral rules

- Never modify append-only audit history in place.
- Never let model output directly redefine policy.
- Never silently widen a local-only control-plane surface into a public API.
- Never merge user-facing audit history with internal telemetry logs.
- Always keep private runtime paths private in operator-facing responses.
- Always redact tool input, tool output, and reason strings before audit
  persistence when the path requires it.
