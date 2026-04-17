**Last updated:** 2026-04-14

# Loopgate architecture overview

This repository is centered on **Loopgate**: a local-first governance kernel for
AI-assisted engineering work.

The active product is deliberately narrow:

- local HTTP control plane on a Unix domain socket
- signed policy
- approval and denial workflows
- append-only local audit
- Claude Code hook governance
- sandbox mediation
- request-driven governed MCP broker flows

The in-tree continuity and memory subsystem is not part of Loopgate's active
architecture. That work lives in the separate sibling repository named
`continuity`.

## 1) Current system classification

As of **2026-04-14**, the implemented and supported shape is:

- local-first
- single-machine
- HTTP over Unix domain socket
- deny-by-default
- append-only audit
- request-driven authority

This document describes the current shipped direction, not retired product
experiments and not speculative deployment profiles.

## 2) High-level execution model

Typical local flow:

`developer tool -> Loopgate (HTTP on UDS) -> validation / policy / approval / execution -> structured result + durable audit`

Current client directions:

- Claude Code hooks
- direct local HTTP clients
- optional out-of-tree MCP forwarders that call the same Loopgate HTTP API

In-tree stdio MCP has been removed. See
`docs/ADR/0010-macos-supported-target-and-mcp-removal.md`.

## 3) Component ownership

### Unprivileged clients

Clients can:

- gather user input
- render approvals and denials
- submit signed requests to Loopgate
- display Loopgate status and audit-derived views

Clients are not authority. Natural language is not authority. Model output is
not authority.

### Loopgate

Loopgate owns:

- policy evaluation
- control sessions and signed requests
- approval state
- capability execution mediation
- sandbox boundaries
- provider credential handling
- governed MCP server lifecycle and execution
- authoritative local audit persistence

### Continuity

Continuity is outside the active Loopgate kernel. Any continuity-specific design
work belongs in the separate `continuity` repo rather than this one.

## 4) Trust boundaries

Trusted:

- Loopgate itself
- validated signed policy
- Loopgate-controlled audit persistence

Untrusted:

- model output
- user prompts
- tool output
- local config before validation
- external provider payloads
- anything a client claims about its own intent

## 5) Invariants currently enforced

- Loopgate is the authority boundary.
- Signed policy remains authoritative.
- Privileged requests require a bound control session and signed envelope.
- Security-relevant actions remain auditable.
- Sandbox and host filesystem boundaries stay explicit.
- Secrets stay inside Loopgate-managed storage and resolution paths.
- MCP execution remains request-driven and server-owned.

## 6) Current implementation state

Loopgate currently ships:

- session open and signed-request validation
- capability execution and approval workflows
- sandbox import / stage / export mediation
- provider connection validation and auth flows
- governed MCP launch / execute / stop / status
- hot policy apply for already signed policy
- local operator and troubleshooting flows

See `docs/design_overview/loopgate.md`,
`docs/design_overview/loopgate_locking.md`,
`docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`, and
`docs/setup/OPERATOR_GUIDE.md` for the operator-facing view.

## 7) Near-term direction

Near-term work stays inside the governance kernel:

- operator docs and troubleshooting
- cleanup of stale historical docs and local-path residue
- audit and ledger hardening
- tighter capability and policy ergonomics

Anything continuity-specific should move to the separate `continuity` repo
rather than back into Loopgate.
