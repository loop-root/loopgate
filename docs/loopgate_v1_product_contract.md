**Last updated:** 2026-04-24

# Loopgate V1 Product Contract

Loopgate v1 is the **local authority boundary between Claude Code and the
tools, files, and sites it can touch**.

That is the product.

The practical user value is two-fold:

1. reduce repeated permission prompts for safe-ish work by converting them into
   signed allow-with-audit policy
2. create the local foundation for business-grade AI tool administration:
   policy control, approvals, access boundaries, and audit review outside the
   chat client

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
- three supported starter policy profiles: `balanced`, `strict`, and
  `read-only`
- CLI-first local admin/operator commands for setup, status, smoke testing,
  policy review, approvals, and audit inspection

## Explicit non-v1 surface

These may exist in the repo, but they are not the supported v1 product story:

- provider-backed OAuth or PKCE onboarding flows
- general secret brokerage as a user-facing feature
- remote admin node or policy push
- multi-harness parity across Cursor, Codex, and others
- a new desktop UI as the core product
- a centralized enterprise policy server
- continuity or memory inside Loopgate

The planned admin-console TUI is in scope as a local operator/admin surface
over existing Loopgate authority paths. It is not a separate authority source
and not a remote management plane.

## Product standard

Loopgate is not done if it causes constant rubber-stamping.

The product should:

- reduce approval fatigue
- reduce rubber-stamping
- reduce babysitting
- increase policy clarity
- increase operator confidence in what the agent actually did
- make the admin boundary visible: policy and approvals live in Loopgate, not
  in prompt text or UI convenience state

## Practical success criteria

- fresh install to governed Claude workflow is simple and obvious
- the default profile is useful without becoming noisy
- denials are explicit and explainable
- approvals are reserved for meaningfully higher-risk actions
- the audit ledger can reconstruct who requested what, what was allowed or
  denied, what changed, and why
