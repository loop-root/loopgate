# GitHub Workflows Map

This file maps `.github/workflows/`, the hosted CI and scheduled verification
surface.

Use it when changing:

- pull request and main-branch checks
- release-readiness smoke coverage
- vulnerability scanning
- artifact upload behavior

## Core Role

`.github/workflows/` defines GitHub Actions checks for the repository. These
workflows are verification surfaces, not runtime authority paths.

## Key Files

- `test.yml`
  - main PR/push workflow
  - runs policy check, format check, vet, race tests, policy-sign coverage,
    e2e approval flow, install smoke, and lint
  - uploads logs as artifacts for failed or flaky jobs

- `govulncheck.yml`
  - runs `make vuln` with the pinned vulnerability scanner script

- `codeql.yml`
  - runs GitHub CodeQL analysis for Go
  - uses the `security-extended` query suite
  - uploads findings to GitHub code scanning

- `nightly-verification.yml`
  - scheduled/manual fuzz smoke
  - scheduled/manual ship-readiness smoke across race tests, e2e, and
    policy-sign coverage

- `../dependabot.yml`
  - opens grouped weekly update PRs for Go modules and GitHub Actions

## Relationship Notes

- Local command definitions live in `Makefile`.
- Shell helpers live in `scripts/`.
- CI logs are diagnostic artifacts; they are not audit records.
- Code scanning findings are review signals; they are not audit records.

## Important Watchouts

- Keep permissions minimal; current workflows use read-only repository content
  permissions except CodeQL, which needs `security-events: write` to upload code
  scanning findings.
- Do not upload secrets, raw runtime state, or machine-local audit data.
- Prefer invoking `make` targets so local and CI verification stay aligned.
