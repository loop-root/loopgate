**Last updated:** 2026-04-20

# Loopgate V1 Product Contract

Loopgate v1 is the **local authority boundary between Claude Code and the
tools, files, and sites it can touch**.

That is the product.

## What v1 must do

- enforce deterministic local policy
- allow low-risk work without constant babysitting
- require inline approval for higher-risk actions
- hard-block explicitly forbidden tools, paths, sites, and repos
- record an immutable local audit ledger that can reconstruct governed agent
  actions

## Primary user

The current primary user is a developer or engineer using **Claude Code** on a
local machine.

## Supported v1 surface

- Claude Code hooks
- local Loopgate control plane over HTTP on a Unix domain socket
- signed policy
- allow / approval / block decisions
- append-only local audit with integrity checks
- guided first-run setup
- two supported starter policy profiles: `strict` and `balanced`

## Explicit non-v1 surface

These may exist in the repo, but they are not the supported v1 product story:

- provider-backed OAuth or PKCE onboarding flows
- general secret brokerage as a user-facing feature
- remote admin node or policy push
- multi-harness parity across Cursor, Codex, and others
- a new desktop UI as the core product
- continuity or memory inside Loopgate

## Product standard

Loopgate is not done if it causes constant rubber-stamping.

The product should:

- reduce approval fatigue
- reduce rubber-stamping
- reduce babysitting
- increase policy clarity
- increase operator confidence in what the agent actually did

## Practical success criteria

- fresh install to governed Claude workflow is simple and obvious
- the default profile is useful without becoming noisy
- denials are explicit and explainable
- approvals are reserved for meaningfully higher-risk actions
- the audit ledger can reconstruct who requested what, what was allowed or
  denied, what changed, and why
