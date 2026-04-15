# Security Policy

Loopgate is an experimental, security-sensitive project. The software is
licensed under Apache License 2.0 and is provided on an "as is" basis. The
architecture is still being hardened.

## Reporting a vulnerability

Please avoid opening a public issue with full exploit details for:

- secret exposure
- approval bypass
- policy bypass
- filesystem escape
- audit/log tampering
- token/session forgery

Instead, use the repository security advisory flow if it is enabled. If that
private reporting path is not available yet, contact the maintainers through
their designated private security channel rather than opening a public issue.

For non-security setup questions, bug reports, and operator workflow support,
use the normal support path described in [SUPPORT.md](./SUPPORT.md).

## Scope priorities

The highest-priority security issues are:

- Loopgate authentication and approval provenance
- secret handling and token isolation
- filesystem boundary escapes
- audit integrity and tamper evidence
- prompt-injection or data-poisoning paths into model context

## Supported versions

There is no formal stable release line yet. Assume only the current `main`
branch is supported.
