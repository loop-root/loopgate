**Last updated:** 2026-04-04

# Proxy v0

## Purpose

Proxy v0 is a **governance proxy**, not a memory-injection engine.

Its job is to let existing developer tools talk to model providers through
Loopgate so Loopgate can enforce policy, approvals, audit, and tool governance
without requiring a custom IDE shell.

Proxy v0 exists to solve these problems:

- existing IDEs and agent hosts need a default governed path
- Loopgate must be able to mediate risky actions consistently
- approval and audit behavior should not depend on whether a host happens to
  call an MCP tool
- local clients should be able to use Loopgate as the control plane while
  keeping their normal UI

Proxy v0 does **not** try to solve memory UX by silently rewriting prompts.

---

## Non-goals

Proxy v0 is not:

- a memory injection layer
- a per-token content rewriter
- a second agent runtime
- a generic prompt optimizer
- a second memory store
- a hidden recall mechanism
- a remote internet-facing service

Memory remains available through explicit Loopgate surfaces such as MCP and
typed HTTP APIs.

---

## Core product position

Loopgate proxy should do what proxies are good at:

- route requests
- mediate policy
- gate tools and risky actions
- bind sessions
- log and audit decisions
- enforce approval flow

Loopgate proxy should **not** silently add memory content the user did not ask
for or the host did not explicitly request.

That is the wrong fit for the current memory architecture.

---

## Why memory is out of scope for v0

The current memory system is valuable, but it is not yet a safe default prompt
mutation layer.

The repo’s architecture already establishes:

- memory is not authority
- wake state is bounded and derived
- recall is governed and should be explicit
- TCL is a normalization and validation layer, not a truth engine

So proxy v0 should not:

- inject wake state automatically into prompts
- perform hidden recall
- auto-capture conversational material into durable memory by default

For now, memory stays on explicit surfaces:

- `memory.remember`
- `memory.discover`
- `memory.recall`
- wake-state diagnostics
- benchmark-backed evaluation and demos

---

## Transport model

Proxy v0 is request-level and local-only by default.

The client is configured to send provider traffic to Loopgate first.

```text
IDE / agent host
  -> Loopgate proxy
  -> provider API
```

Loopgate receives the request because it is the configured upstream endpoint.
It then creates its own outbound provider request.

This means:

- no TLS interception tricks
- no decryption of someone else’s traffic
- no per-token inspection requirement

Loopgate only sees the request because the client sends it there directly.

---

## Request model

Proxy v0 works at the **request level**, not the token level.

For each request:

1. client sends a model/provider request to Loopgate
2. Loopgate authenticates and binds the request to a control session
3. Loopgate evaluates whether the request shape, tools, and target are allowed
4. Loopgate applies approval or denial logic where required
5. Loopgate forwards the request upstream if allowed
6. Loopgate streams the upstream response back to the client
7. Loopgate records audit and diagnostic metadata

This is the whole v0 model.

---

## What proxy v0 should govern

### 1. Session binding

Proxy requests should bind to:

- tenant
- user
- local session id
- provider/model target

Loopgate should remain the authority for which session is allowed to do what.

### 2. Provider and model policy

Loopgate should be able to govern:

- which providers are allowed
- which models are allowed
- which connections or credentials are used
- whether certain model families or endpoints are blocked

### 3. Tool and capability mediation

The proxy should enforce:

- whether tool use is allowed at all
- whether specific tool classes are allowed
- when approval is required
- whether the tool/capability request is denied

This is where the governance value is strongest.

### 4. Audit and observability

The proxy should record:

- request metadata
- policy decisions
- denials
- approval creation/resolution
- upstream provider failures

It should not log secret-bearing payloads or raw sensitive request bodies.

---

## What proxy v0 should not govern yet

Proxy v0 should not try to:

- infer memory candidates from every conversation
- add hidden memory context to prompts
- optimize prompts with derived summaries
- synthesize continuity from weak heuristics

Those are separate product problems and should not be mixed into the first
proxy milestone.

---

## Performance model

Proxy v0 should be fast because it does very little on the hot path besides
governance checks and forwarding.

### Fast path

1. parse request
2. bind session
3. validate provider/model/tool policy
4. forward upstream

### Streaming rule

Do not buffer the entire model response before returning it unless the chosen
provider protocol forces that behavior.

Prefer pass-through streaming with request/decision metadata logged separately.

### Avoid

- per-token interception
- per-turn memory lookups
- extra provider round trips for policy checks
- hidden background agent behavior

---

## Failure behavior

Proxy v0 must fail conservatively.

### If policy evaluation fails

- deny
- return explicit reason when safe
- audit the denial

### If approval is required

- create approval through Loopgate
- do not forward the risky action until approved
- make the pending state explicit

### If upstream provider call fails

- return the failure clearly
- do not pretend the request succeeded

### If audit append fails on a security-relevant action

- fail closed

This must remain aligned with the existing Loopgate invariants.

---

## Minimal v0 scope

Proxy v0 should support:

- one local transport profile
- one upstream API family first
- one request/response streaming path
- session binding
- provider/model policy checks
- tool governance and approval mediation
- audit + diagnostics

That is enough to prove the product direction.

---

## Relationship to MCP

MCP remains the explicit control surface.

Use MCP for:

- `memory.remember`
- `memory.discover`
- `memory.recall`
- status and diagnostics
- explicit governance-aware operations

Use proxy for:

- default governed model path
- tool and policy mediation
- approval and audit enforcement

MCP and proxy are complementary:

- MCP = explicit control plane surface
- proxy = default governed request path

---

## Relationship to memory

Memory is still important, but not as a hidden proxy feature in v0.

Current position:

- keep memory in Loopgate
- keep it explicit and governed
- keep improving it through benchmarks and narrow APIs
- do not silently blend it into proxy prompts

Future integration, if any, should only happen after a separate design proves it
can be done without violating the system’s memory and authority model.

---

## Recommended build order

1. define the exact upstream API shape to support first
2. add proxy session binding and request forwarding
3. enforce provider/model/tool policy on the forwarded path
4. wire approvals and denials into the forwarded path
5. add audit and diagnostics
6. test the governed request path from a real IDE/client

Memory integration is explicitly out of scope for this milestone.

---

## Open questions

1. Which provider/protocol should v0 support first?
2. How will the client identify a proxy session?
3. Which request fields must be normalized before policy evaluation?
4. What is the exact approval UX when the proxied request wants a risky tool?
5. Which audit metadata is necessary and sufficient on the proxy path?
