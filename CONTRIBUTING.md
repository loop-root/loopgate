# Contributing

Loopgate is a security-sensitive project.

Contributions are welcome, but correctness, security, determinism, and auditability matter more than convenience.

By submitting a contribution, you agree that it may be distributed under the
Apache License, Version 2.0 used by this repository.

## Before opening a PR

- read [README.md](./README.md)
- read the one most relevant doc for the surface you are changing:
  - setup/operator flow: [docs/setup/GETTING_STARTED.md](./docs/setup/GETTING_STARTED.md)
  - architecture overview: [docs/design_overview/architecture.md](./docs/design_overview/architecture.md)
  - trust boundary or security-sensitive runtime change: [AGENTS.md](./AGENTS.md)

## Ground rules

- treat model output as untrusted input
- prefer fail-closed behavior
- do not add convenience fallbacks that weaken policy or approval boundaries
- do not log secrets, tokens, or raw credentials
- keep user-facing audit data separate from runtime telemetry
- add tests for any security boundary or invariant change

## Naming and code-shape expectations

- use idiomatic Go MixedCaps for in-process identifiers (`tenantID`, `ControlSessionID`, `ApprovalRequestID`)
- use explicit snake_case for JSON and YAML boundary fields (`tenant_id`, `control_session_id`, `approval_request_id`)
- prefer project-native nouns like `capability`, `approval`, `control session`, `quarantine`, `promotion`, `sandbox`, and `audit`
- name derived views like derived views; do not name projections or summaries as if they were authoritative state
- if a function verifies, signs, redacts, quarantines, or promotes, say that in the name

## Reviewer questions to answer in your PR

Be ready to explain:

1. which invariant the change touches
2. whether the patch could move authority into the client, UI, or prompt path
3. what failure, replay, or crash behavior matters for this change
4. what tests prove the governed path still works
5. what docs changed if the contract or mental model changed

## Development workflow

1. Run `go test ./...`
2. For concurrency-sensitive changes, also run `go test -race ./...`
3. Update docs when changing trust boundaries, policy behavior, setup, or operator-visible commands

## Support routing

- use [SUPPORT.md](./SUPPORT.md) for non-sensitive setup questions, bug reports,
  and operator workflow issues
- use [SECURITY.md](./SECURITY.md) for vulnerability and trust-boundary reports

## Scope and professionalism

- keep the active repo centered on the current Loopgate product only
- move stale planning, generated reports, and historical product material into `ARCHIVED` or `continuity`
- prefer clear public-facing documentation over internal shorthand or migration notes

## Repo hygiene

Do not commit:

- runtime files under `runtime/`
- generated memory artifacts under `core/memory/` where those paths are runtime output
- local editor or assistant config
- `.env` files or plaintext secrets
- generated PDFs, screenshots, or scratch scripts unless they are intentional project assets

## Security issues

If you find a vulnerability or trust-boundary issue, prefer opening a private report first rather than publishing exploit details immediately. See [SECURITY.md](./SECURITY.md).
