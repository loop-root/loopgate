**Last updated:** 2026-04-10

# Loopgate current-state security review

## Scope

Review target:

- current repository state under `internal/loopgate`, `internal/secrets`, `cmd/loopgate`, and current docs
- emphasis on control-plane drift, loopholes, stale surfaces, and security-sensitive behavior

Review method:

- static review of high-risk control-plane handlers
- route and capability-scope inspection
- filesystem and secret-handling spot checks
- stale-code and stale-doc drift scan

This is a current-state review, not a historical comparison exercise.

## Executive summary

The core Loopgate trust model is still materially stronger than it was in earlier snapshots:

- local HTTP-on-UDS remains the real authority path
- memory authority moved behind backend-owned seams
- raw continuity ingest is gone
- audit failure handling is still generally fail-closed on sensitive flows

The biggest current weakness is not hidden model magic. It is drift in route governance.

Several signed, authenticated control-plane routes still bypass the newer explicit control-capability model. The worst cases are:

- Haven memory inventory/reset
- connection management
- site inspect / trust-draft

Those routes look governed because they require bearer auth plus signed requests, but they are not currently bound to route-level control scopes. That is the main security gap to close first.

## Findings

### F1 — High: Haven memory inventory/reset bypass memory control capabilities

Affected code:

- `internal/loopgate/server_haven_memory_handlers.go:24`
- `internal/loopgate/server_haven_memory_handlers.go:54`
- `internal/loopgate/server.go:545`
- `internal/loopgate/server.go:546`

Why this matters:

- `GET /v1/ui/memory` exposes display-safe but still sensitive memory inventory state
- `POST /v1/ui/memory/reset` archives and reinitializes the authoritative tenant memory partition

Both handlers authenticate the session and verify request integrity, but neither calls `requireControlCapability(...)`.

Impact:

- any session that can authenticate and sign requests can read or reset tenant memory through the UI routes, even if the token lacks `memory.read`, `memory.write`, or stronger governance scopes
- this weakens the repo’s stated control-capability model and creates a route-specific bypass around the newer memory protection work

Why this is especially serious:

- the public docs describe these routes as auditable operator memory flows
- the runtime already has explicit memory control capabilities elsewhere
- destructive memory reset should not rely on "signed session" alone

Recommendation:

- require `memory.read` for `GET /v1/ui/memory`
- require at least `memory.write` for `POST /v1/ui/memory/reset`, and consider a stricter review/governance capability if reset is treated as a destructive operator action
- add regression tests proving denial without the required route scopes

### F2 — High: Connection-management and site-trust routes bypass explicit control scopes

Affected code:

- `internal/loopgate/server_connection_handlers.go:78`
- `internal/loopgate/server_connection_handlers.go:98`
- `internal/loopgate/server_connection_handlers.go:136`
- `internal/loopgate/server_connection_handlers.go:174`
- `internal/loopgate/server_connection_handlers.go:212`
- `internal/loopgate/server_connection_handlers.go:280`
- `internal/loopgate/server.go:576`
- `internal/loopgate/server.go:581`

Supporting context:

- `internal/loopgate/site_trust.go:135`
- `internal/loopgate/site_trust.go:223`

Why this matters:

The following routes are signed and authenticated but not bound to explicit control capabilities:

- `GET /v1/connections/status`
- `POST /v1/connections/validate`
- `POST /v1/connections/pkce/start`
- `POST /v1/connections/pkce/complete`
- `POST /v1/sites/inspect`
- `POST /v1/sites/trust-draft`

Impact:

- any authenticated session token can inspect connection state, validate provider connections, initiate or complete PKCE flows, inspect arbitrary HTTPS sites, and create trust-draft files without route-scope enforcement
- `site.inspect` is especially sensitive because it performs outbound fetches and `site.trust-draft` writes draft config under repo-managed state

Why this is a real loophole:

- signed request integrity prevents tampering, but it is not a substitute for least-privilege route scoping
- the repo already moved memory/config routes behind explicit control capabilities; these routes lag behind that model

Recommendation:

- add explicit control capabilities for connection read/write and site inspect/trust flows, or bind them to existing config/admin scopes if that is the intended model
- deny these routes by default when the token lacks the relevant scope
- add tests proving that a valid signed request is still denied without the route capability

### F3 — Medium: Operator mount write-grant renewal weakens TTL semantics

Affected code:

- `internal/loopgate/ui_server.go:102`
- `internal/loopgate/ui_server.go:177`
- `internal/loopgate/ui_server.go:211`

Why this matters:

`PUT /v1/ui/operator-mount-write-grants` can renew an existing grant by setting:

- `updatedGrantExpiresAtUTC = nowUTC.Add(operatorMountWriteGrantTTL)`

The route:

- authenticates the session
- verifies the signed body
- checks that the mounted root already exists for that session

But it does not require an explicit control capability or a fresh approval to extend the grant.

Impact:

- an already-authorized session can keep a host-write grant alive indefinitely by repeated renewal
- TTL becomes closer to a soft lease than an enforced expiry barrier

This is not as severe as F1/F2 because it does not create a new grant from nothing, but it does weaken the intended time bound on privileged host writes.

Recommendation:

- require an explicit control capability for grant updates
- decide whether renewal should require fresh approval instead of mere session possession
- add tests proving expired grants cannot be revived without the intended authority path

### F4 — Medium: Startup can delete arbitrary paths from env-controlled socket configuration

Affected code:

- `cmd/loopgate/main.go:42`
- `cmd/loopgate/main.go:43`
- `internal/loopgate/server.go:732`

Why this matters:

The daemon accepts `LOOPGATE_SOCKET` from the environment, applies `filepath.Clean`, and later runs:

- `os.RemoveAll(server.socketPath)`

before binding the Unix socket.

Impact:

- a compromised launcher environment or operator mistake can cause Loopgate startup to delete an arbitrary filesystem path instead of only removing a stale socket file

This requires local control over process launch, so it is not a remote exploit. It is still a real destructive behavior bound to an insufficiently validated path.

Recommendation:

- constrain socket paths to an expected runtime directory under `runtime/state` or another explicit allowlist
- remove only socket-like files, not arbitrary cleaned paths
- fail closed on invalid socket path configuration

### F5 — Low: Haven preferences fail open and are written with weaker permissions

Affected code:

- `internal/loopgate/server_haven_settings.go:206`
- `internal/loopgate/server_haven_settings.go:212`
- `internal/loopgate/server_haven_settings.go:227`

Why this matters:

- preference read failure silently returns empty defaults
- malformed JSON silently resets to defaults
- saved preference file uses mode `0644`

Impact:

- low-severity state drift and reduced operator observability
- weaker file permissions than most runtime state written under `runtime/state`

This is not a major exploit path, but it is a real fail-open configuration smell inside a security-sensitive repo.

Recommendation:

- surface preference parse failure explicitly
- tighten file permissions to `0600` unless a documented reason requires wider read access

## Stale code and documentation drift

### 1. Route/documentation drift on memory reset

Docs still present the memory UI routes as intentional operator flows:

- `docs/setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md:260`
- `docs/design_overview/loopgate.md:169`

The problem is not that the docs are wrong about the feature existing. The drift is that the feature description now implies a governed operator surface, while the code currently lacks explicit route-scope gating.

### 2. Roadmap metadata drift

`docs/roadmap/roadmap.md` is current in many sections, but the header still says:

- `docs/roadmap/roadmap.md:7`

This creates avoidable ambiguity between "last updated" and "snapshot from".

Recommendation:

- either refresh the snapshot metadata or relabel it clearly as historical framing

### 3. Compatibility ballast remains visible

Examples:

- deprecated Haven HTTP aliases remain in `internal/loopgate/server.go:565`
- empty-tenant morphling visibility fallback remains in `internal/loopgate/morphlings.go:363`

These are not immediate exploitable bugs on their own, but they are drift points that deserve explicit retirement plans.

### 4. Historical documentation volume is high but mostly labeled honestly

The repo contains a large number of historical or superseded docs. Most are labeled well. The issue is discoverability noise, not direct security vulnerability.

Representative examples:

- `docs/reports/claude-code-review-2026-02-25.md:3`
- `docs/superpowers/plans/2026-03-25-master-implementation-plan.md:3`
- `docs/ADR/0005-mcp-server-stdio-mcp-go.md:2`

Recommendation:

- keep historical docs, but consider a stronger archive index so operators do not confuse them with live architecture contracts

## Notable strengths

The review did not find evidence that the repo has drifted back into these older failure modes:

- raw continuity inspect is removed from the public surface
- memory authority is backend-owned instead of handler-owned
- in-tree MCP remains removed
- append/audit failure handling is still mostly explicit on sensitive paths
- hybrid memory is additive on read, not a hidden authority bypass

That matters because the current issues are mostly "route governance lagged the architecture," not "the core trust model collapsed."

## Recommended fix order

1. Close F1 by gating Haven memory routes behind explicit memory control capabilities.
2. Close F2 by scoping connections/site routes behind explicit control capabilities.
3. Decide policy for operator mount write-grant renewal and implement it explicitly.
4. Constrain `LOOPGATE_SOCKET` handling so startup cannot `RemoveAll` arbitrary paths.
5. Clean low-severity config/document drift and historical-surface cleanup after the route scope fixes land.

## Suggested follow-up tests

- deny `GET /v1/ui/memory` without `memory.read`
- deny `POST /v1/ui/memory/reset` without the intended destructive-memory scope
- deny connection validate/PKCE/site inspect/trust-draft without their route scopes
- ensure signed request integrity alone is insufficient for those routes
- ensure invalid `LOOPGATE_SOCKET` paths fail before any filesystem deletion
- ensure Haven prefs parse failures are visible and do not silently downgrade state
