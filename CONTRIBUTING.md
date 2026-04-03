# Contributing

Loopgate is a security-sensitive project.

Contributions are welcome, but correctness, security, determinism, and auditability matter more than convenience.

## Before opening a PR

- read [README.md](./README.md)
- read [docs/design_overview/systems_contract.md](./docs/design_overview/systems_contract.md)
- read [docs/design_overview/loopgate.md](./docs/design_overview/loopgate.md)

## Ground rules

- treat model output as untrusted input
- prefer fail-closed behavior
- do not add convenience fallbacks that weaken policy or approval boundaries
- do not log secrets, tokens, or raw credentials
- keep user-facing audit data separate from runtime telemetry
- add tests for any security boundary or invariant change

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
