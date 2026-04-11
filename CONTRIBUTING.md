# Contributing

Loopgate is a security-sensitive project.

Contributions are welcome, but correctness, security, determinism, and auditability matter more than convenience.

## Before opening a PR

- read [README.md](./README.md)
- read [AGENTS.md](./AGENTS.md)
- read [context_map.md](./context_map.md)
- read [docs/design_overview/systems_contract.md](./docs/design_overview/systems_contract.md)
- read [docs/design_overview/loopgate.md](./docs/design_overview/loopgate.md)

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
- prefer project-native nouns like `capability`, `approval`, `control session`, `quarantine`, `promotion`, `morphling`, and `audit`
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

## Repo hygiene

Do not commit:

- runtime files under `runtime/`
- generated memory artifacts under `core/memory/` where those paths are runtime output
- local editor or assistant config
- `.env` files or plaintext secrets
- generated PDFs, screenshots, or scratch scripts unless they are intentional project assets

## Security issues

If you find a vulnerability or trust-boundary issue, prefer opening a private report first rather than publishing exploit details immediately. See [SECURITY.md](./SECURITY.md).
