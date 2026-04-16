# Changelog

All notable changes to Loopgate's public release state will be tracked in this
file.

This project is still experimental and local-first. Early `0.x` versions mark
honest source snapshots and operator-facing behavior, not a stable compatibility
guarantee.

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
