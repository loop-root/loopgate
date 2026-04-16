# Support

Loopgate is an experimental, security-sensitive project. Support is best-effort
and centered on the current `main` branch.

## What support is for

Use normal support channels for:

- setup and installation questions
- operator workflow questions
- policy, audit, or approval behavior that seems confusing
- bug reports that are not security-sensitive
- documentation gaps or examples that would make Loopgate easier to evaluate

Unless the repository adopts a separate discussion forum later, open a GitHub
issue for non-sensitive support requests and bug reports.

## What does not belong in a public support request

Do not post full exploit details, secrets, tokens, or sensitive local runtime
artifacts in a public issue.

If you believe you found a vulnerability or trust-boundary issue, use the
private reporting path described in [SECURITY.md](./SECURITY.md) instead of a
public support thread.

## Current support expectations

Loopgate is still being hardened. Please assume:

- support is best-effort, not SLA-backed
- only the current `main` branch is considered supported
- compatibility may change while the governance surface is still evolving
- examples and defaults are optimized for the current local-first macOS-focused
  product shape

## Good support reports

A useful report usually includes:

- what command you ran
- what you expected to happen
- what happened instead
- whether the behavior was a denial, runtime error, or audit surprise
- the relevant Loopgate version, branch, or commit if known
- redacted diagnostic context if the problem depends on local configuration
- whether `go run ./cmd/loopgate-ledger verify` succeeded
- whether `go run ./cmd/loopgate-doctor report` showed anything unexpected

## Scope boundaries

This repository is for the current Loopgate product only.

- continuity and memory work belongs in the separate `continuity` repo
- historical design notes and stale planning material belong in `ARCHIVED`
- security-sensitive issues belong in the private path described in
  [SECURITY.md](./SECURITY.md)
