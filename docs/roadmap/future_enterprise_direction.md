# Future enterprise direction

**Last updated:** 2026-04-24

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

The near-term admin-console TUI belongs to this local-first story. It can make
policy, approvals, hook status, and audit easier to operate on one machine, but
it does not imply that remote enterprise management already exists.

Do not describe the repo as if the remote/admin-node product already ships.

## If multi-node happens later

These are the principles to preserve, not a committed implementation plan:

- a local node remains an enforcement runtime, not a thin client
- a remote admin node would distribute governance state, not replace local enforcement
- tenant isolation remains explicit and fail closed
- admin-node authority would require cryptographic verification
- cached signed policy would remain enforceable offline
- local audit stays authoritative for local enforcement decisions even if later exported

## Enterprise-shaped path

The credible progression is:

1. local Loopgate enforcement with signed policy, approvals, and audit
2. local admin console over the real Loopgate authority APIs
3. policy export/import and review workflows that remain signed
4. managed trust anchors and policy distribution
5. audit export and centralized review
6. remote admin only after local enforcement, identity, and offline policy
   behavior are explicitly designed

The remote layer should distribute and review governance state. It should not
turn local Loopgate into a thin client that can no longer enforce policy when
offline.

## Why this is separate

The first public Loopgate repo should not overstate features that are still
future design work. Keeping this material here preserves the design intent
without making `README.md`, `AGENTS.md`, or setup docs sound like a broader
product already exists.
