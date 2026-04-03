# ADR 0016: Admin Console v0 Authentication Mechanism

## Status

Accepted

## Date

2026-04-01

## Context

Phase 5 of the Loopgate enterprise roadmap introduces the **Admin Console** (a minimal, web-based surface for visualizing policies, audit logs, and control sessions). Organizational IdP integration for operators is **out of scope** for v0, but the surface must not be anonymously reachable.

Implementation lives in **`internal/loopgate/admin_console.go`** (no reuse of any legacy `web/admin` tree in this repo).

## Decision

1. **Dual opt-in:** `admin_console.enabled` in `config/runtime.yaml` **and** a **`--admin` CLI flag** must both be set before a TCP listener is opened. This avoids accidentally exposing the UI when config is toggled without an explicit operator choice.
2. **Bootstrap secret:** Operators set **`LOOPGATE_ADMIN_TOKEN`** (minimum **24** runes). The process hashes it once at startup with **bcrypt**; login compares the submitted password to that hash. Missing/short token when `--admin` is used **fails closed** (process exits).
3. **Transport binding:** `admin_console.listen_addr` is validated to **loopback only** (e.g. `127.0.0.1:9847`). Default listen address when enabled and unset: **`127.0.0.1:9847`**.
4. **Session cookie:** On success, the server issues an **`HttpOnly`**, **`SameSite=Lax`** cookie named **`lg_admin_session`** containing an opaque random session id stored in an **in-memory** map with expiry. `Secure` is not set so plain HTTP on loopback works; reverse proxies terminating TLS should enforce HTTPS at the edge.
5. **Audit / CSV:** Exported and HTML-rendered audit `data` uses **`secrets.RedactStructuredFields`** so sensitive keys and token-shaped strings do not leak through the admin UI.

## Consequences

- **Positive:** Clear fail-closed startup, no long-lived plaintext admin password on disk, loopback-only bind, same-process authority as the UDS server.
- **Negative:** Manual token rotation; token must be handled carefully in shell history and process listings.
- **Escape hatch:** Replace cookie sessions with OIDC/OAuth or mTLS for admin-node connectivity; keep config + CLI gates for local-only profiles.
