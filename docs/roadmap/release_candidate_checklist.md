**Last updated:** 2026-04-17

# Loopgate Release Candidate Checklist

This checklist is for deciding whether the current Loopgate tree is ready to
cut a release candidate from the public stabilization branch (`release` in the
current local branch model).

It is intentionally conservative. A release candidate should be a judgment that
the current local-first macOS product is coherent, supportable, and honest
about its boundaries, not a promise that every future hardening or product gap
is already closed.

## Scope

This checklist applies to the current active product only:

- macOS-first local Loopgate server
- signed policy
- Claude Code hook governance
- local approvals and audit
- local operator CLIs and diagnostic tools

It does **not** certify:

- Linux or Windows as supported operator targets
- future browser or bridge surfaces
- multi-node enterprise deployment
- continuity or memory features that now live outside this repo

## Release posture

- [ ] `README.md`, `docs/README.md`, and setup docs all describe the same current product boundary
- [ ] the supported target is stated consistently as macOS-first local Loopgate
- [ ] support routing and security reporting docs are present and linked
- [ ] the license, notice, and contributor docs are present and readable

## Security and trust boundaries

- [ ] policy changes still require a valid detached signature
- [ ] operator-local trust anchors for policy signing work without source edits
- [ ] test-only policy-signing trust cannot activate in production binaries
- [ ] auth denials enter the authoritative audit path
- [ ] session MAC responses do not expose epoch derivation material
- [ ] replay persistence uses the append-only nonce log rather than per-request full snapshot rewrite
- [ ] replay retention matches the 1-hour control-session lifetime
- [ ] audit HMAC checkpoints are enabled by default in the shipped macOS config
- [ ] first-start checkpoint bootstrap is documented and behaves predictably

## Verification

- [ ] `go test ./...`
- [ ] `go test -race -count=1 ./...`
- [ ] `go vet ./...`
- [ ] `./scripts/policy_sign_coverage_check.sh`
- [ ] `make test-e2e`
- [ ] tracked CI matches the supported product target closely enough to catch regressions
- [ ] required GitHub checks are configured in repo settings
- [ ] required check name is `test / test`
- [ ] required check name is `test / e2e`
- [ ] required check name is `test / lint`
- [ ] required check name is `govulncheck / govulncheck`

Recommended spot checks before tagging:

- [ ] `./bin/loopgate-policy-sign -verify-setup`
- [ ] `./bin/loopgate-policy-admin validate`
- [ ] `./bin/loopgate-ledger verify`
- [ ] `./bin/loopgate-doctor report`

## Operator flow sanity checks

- [ ] a fresh operator can follow [Getting started](../setup/GETTING_STARTED.md) without hidden steps
- [ ] hook install and remove commands still behave clearly
- [ ] denied-request diagnosis is understandable from `loopgate-ledger tail -verbose`
- [ ] audit integrity posture is understandable from startup output and `loopgate-doctor report`
- [ ] policy-signing docs match the actual trust-anchor and signer path behavior

## Documentation honesty

- [ ] docs do not describe removed or non-shipping surfaces as active
- [ ] active docs do not claim stronger guarantees than the current code provides
- [ ] deferred work is tracked as roadmap or product-gap follow-up, not hidden in prose

## Tagging decision

The tree is ready for a release candidate when:

- the items above are either checked off or deliberately deferred with written rationale
- the remaining open work is product polish or future-scope work, not a blocker in the current trust model
- a new reader sees one product, one operator path, and one honest security story

## After the decision

If you do cut a release candidate:

- add a short release note summarizing:
  - supported target
  - current active harness
  - main governance guarantees
  - the most important non-goals or deferred areas
- update `CHANGELOG.md`
- record the exact commit and tag in the release note or roadmap trail
