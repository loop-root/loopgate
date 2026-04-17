# Future enterprise direction

**Last updated:** 2026-04-16

This document captures future-facing enterprise and multi-node ideas that are
not part of the current shipped Loopgate product.

## Current reality

The active public Loopgate story is:

- local-first
- macOS-first
- single-node
- signed policy
- approvals
- local authoritative audit
- Claude Code hook governance
- governed MCP broker execution

Do not describe the repo as if the remote/admin-node product already ships.

## If multi-node happens later

These are the principles to preserve, not a committed implementation plan:

- a local node remains an enforcement runtime, not a thin client
- a remote admin node would distribute governance state, not replace local enforcement
- tenant isolation remains explicit and fail closed
- admin-node authority would require cryptographic verification
- cached signed policy would remain enforceable offline
- local audit stays authoritative for local enforcement decisions even if later exported

## Why this is separate

The first public Loopgate repo should not overstate features that are still
future design work. Keeping this material here preserves the design intent
without making `README.md`, `AGENTS.md`, or setup docs sound like a broader
product already exists.
