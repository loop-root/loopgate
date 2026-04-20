# Changelog

All notable changes to Loopgate's public release state will be tracked in this
file.

This project is still experimental and local-first. Early `0.x` versions mark
honest source snapshots and operator-facing behavior, not a stable compatibility
guarantee.

## [v0.2.0-rc1] - 2026-04-20

First release candidate for the narrowed Claude-first Loopgate product.

### Added

- a tracked v1 product contract for the current public Loopgate surface
- guided setup plan previews, verification hints, and Python preflight checks
- richer starter policy profile descriptions in the setup wizard

### Changed

- narrowed the supported product story to Claude Code governance, signed policy,
  approvals, and local audit
- updated the starter profiles so `strict` stays read-first while `balanced`
  allows patch-style Claude edits inside the repo root
- added top-level `loopgate help` / `loopgate -h` command discovery
- serialized Claude settings mutations with a lockfile during hook install and
  removal

### Fixed

- `remove-hooks` no longer creates fresh empty Claude settings files when no
  settings file existed on disk
- removed the unused `github.com/chzyer/readline` dependency from the module

### Security and hardening

- made `/v1/config/*` runtime and connection updates stricter, audited, and
  rollback-aware
- fixed `providerRuntime` snapshot races in configured capability reads
- enforced `allowed_hosts` across redirects for provider-backed outbound
  requests
- serialized per-connection token issuance and bounded repeated auth-denial
  audit bursts

### Current scope

This release candidate represents the current Loopgate product only:

- macOS-first local Loopgate server
- signed policy
- Claude Code hook governance
- local approvals and audit
- local operator CLI and diagnostic flows
- experimental provider connection groundwork kept outside the main v1
  onboarding story

Not included in this release contract:

- Linux or Windows as supported operator targets
- future browser or bridge surfaces
- multi-node enterprise deployment
- continuity or memory features that live outside this repo
- provider-backed OAuth/PKCE onboarding as part of the supported first-run path

## [v0.1.0] - 2026-04-16

First public Loopgate release tag for the current macOS-first local governance
product.

### Added

- operator-local policy-signing trust anchors outside the repo checkout
- tracked release candidate checklist and repository sealing review docs
- tracked CI coverage for `go test`, `go vet`, and `govulncheck`

### Changed

- enabled audit HMAC checkpoints by default for the shipped macOS runtime config
- tightened policy-signing test trust so production binaries no longer infer
  test trust from executable naming
- aligned replay retention with the one-hour control-session TTL
- added a dedicated `fs_read` rate-limit denial code

### Security and hardening

- auth-token denials now enter the authoritative audit path
- session MAC responses no longer expose epoch derivation material
- authenticated nonce replay persistence now uses an append-only log instead of
  rewriting a full snapshot on every request

### Current scope

This version represents the current Loopgate product only:

- macOS-first local Loopgate server
- signed policy
- Claude Code hook governance
- local approvals and audit
- local operator CLI and diagnostic flows

Not included in this release contract:

- Linux or Windows as supported operator targets
- future browser or bridge surfaces
- multi-node enterprise deployment
- continuity or memory features that live outside this repo
