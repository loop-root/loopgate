**Last updated:** 2026-04-16 (updated with additional product gaps and Pi adapter)

# Loopgate V1 Product Gaps

This document captures product-level improvements identified during the 2026-04-15
senior code review. These are distinct from the correctness and security fixes tracked
in `loopgate_v1_hardening_plan.md`. Most items here are about closing the gap between
"this works" and "an operator can confidently run this."

**Operator surface note:** The active operator surfaces are the CLI tools,
`loopgate-doctor`, and the Pi agent harness adapter described in item 17.
Earlier drafts with retired product naming have been cleaned up.

Items are ordered by impact, not difficulty. Each section includes enough
implementation detail to hand off to a fresh agent session.

---

## Status snapshot — 2026-04-16 evening pass

Canonical closure report:
- [`docs/reports/reviews/2026-04-16/review_status.md`](../reports/reviews/2026-04-16/review_status.md)

Checklist status:

- [x] `1` `loopgate init` command
- [x] `2` Auth failures must be in the audit ledger
- [x] `3` Approval CLI — list and act from the terminal
- [x] `4` `loopgate-doctor` surfaces the audit-integrity posture and `bootstrap_pending`
- [x] `5` Nonce replay retention now matches the 1-hour session TTL
- [ ] `6` Rate limit the hook endpoint
  Deferred for after the first public polish pass.
- [ ] `7` End-to-end integration test
  Deferred for after the first public polish pass.
- [x] `8` Version tagging and `CHANGELOG`
- [x] `9` Structured startup output
- [x] `10` `policy explain` command
- [x] `11` Flip HMAC checkpoint default to on
- [x] `12` Replace `policy.yaml` with a well-commented starter policy
- [x] `13` `AGENTS.md` in the repo
- [x] `14` `Makefile`
- [x] `15` CI policy signing check
- [ ] `16` Coverage gate on `loopgate-policy-sign`
  Deferred.
- [ ] `17` Pi agent harness adapter
  Deferred; not part of the current public-release minimum.

---

## 1. `loopgate init` command

**Impact:** Highest. Unlocks every other improvement.

### Problem

A new user must understand Ed25519, key IDs, and policy signing before Loopgate
will start. The current flow requires running four or more `go run ./cmd/...` commands
in the right order, understanding the relationship between the signing key and the
trust anchor, and knowing where the runtime socket lands. The trust anchor is also
currently hardcoded in the binary, so users who generate their own key cannot use
it without modifying source.

### What to build

A single `loopgate init` command that:

1. Generates an Ed25519 keypair and writes it to `runtime/state/policy-signing.key`
   and `runtime/state/policy-signing.pub` (private permissions, `O_EXCL` so it
   never overwrites an existing key)
2. Writes the public key as the user trust anchor to `config/trust-anchor.pub`
   (or an equivalent config-layer path that `trustedPolicySigningKeys()` reads
   before falling back to the compiled default)
3. Signs the default policy with the new key
4. Creates the `runtime/state/` directory structure
5. Prints a short confirmation: key ID, socket path, and the exact command to
   start the server

### Files to touch

- `cmd/loopgate-init/` — new command (or add `init` subcommand to existing CLI)
- `internal/config/policy_signing.go` — add user trust anchor file lookup before
  compiled default in `trustedPolicySigningKeys()`
- `docs/setup/GETTING_STARTED.md` — replace the current multi-step preamble with
  `loopgate init && go run ./cmd/loopgate`

### Acceptance criteria

A user who has never run Loopgate can go from `git clone` to a running, policy-signed
server with one command and one follow-up.

---

## 2. Auth failures must be in the audit ledger

**Impact:** High. Product integrity issue, not just a correctness gap.

### Problem

The ledger is the product. Its value is "you can audit what happened." Right now,
invalid tokens, expired tokens, and peer binding mismatches write only to ephemeral
slog output. An operator reviewing the ledger after an incident sees no record of
authentication failures — the ledger has a lie of omission.

This is tracked as P1 in `loopgate_v1_hardening_plan.md` but deserves emphasis here
because it is architectural: the audit contract should cover the full request lifecycle,
not just capability decisions.

### What to build

In `internal/loopgate/request_auth.go`, `authenticate()`: after each failure branch,
append a ledger entry with event type `auth.failed`, including:

- timestamp
- peer UID (if available from `peercred`)
- failure reason (token invalid / token expired / peer binding mismatch)
- request path
- a stable request ID

Do not log the token value itself.

### Files to touch

- `internal/loopgate/request_auth.go` — add ledger writes in each failure branch
  of `authenticate()`
- `internal/ledger/ledger.go` — confirm `AppendEvent` is safe to call before the
  full request context is established (it should be, since it only needs the
  artifact root path)

---

## 3. Approval CLI — list and act from the terminal

**Impact:** High. The governance model only works if operators can use it.

### Problem

Approvals are the core of the governance story. But right now, acting on a pending
approval requires knowing the raw HTTP API. There is no `loopgate-policy-admin`
subcommand for listing pending approvals or approving them. An operator running
Loopgate headless has no practical way to action the approval flow.

### What to build

Add subcommands to `cmd/loopgate-policy-admin` (or a new `cmd/loopgate-admin`):

```
loopgate-policy-admin approvals list
    Prints pending approvals: ID, capability, actor, requested at, expiry

loopgate-policy-admin approvals allow <approval-id>
    Approves the request. Requires operator confirmation prompt before
    sending the approval.

loopgate-policy-admin approvals deny <approval-id>
    Denies the request.
```

These should call the existing `/v1/approval/...` HTTP endpoints over the Unix socket,
so no new server-side logic is needed — just CLI surface.

### Files to touch

- `cmd/loopgate-policy-admin/main.go` — add `approvals` subcommand routing
- New handler functions calling the existing approval API over the socket
- `docs/setup/OPERATOR_GUIDE.md` — document the headless approval flow

---

## 4. `loopgate-doctor` should surface the HMAC checkpoint default

**Impact:** Medium. Changes the operator's mental model of what they're running.

### Problem

HMAC checkpoints exist and meaningfully counter the audit chain's biggest weakness
(a root-level writer replacing the file with a new consistent chain). They are
disabled by default in `config/runtime.yaml`. An operator running `loopgate-doctor`
gets no signal about this. They may believe their audit trail is stronger than it is.

### What to build

Add a check to `loopgate-doctor` output:

```
audit chain:     ok    (append-only, SHA-256 hash chain)
hmac checkpoint: warn  (disabled — chain detects tampering but cannot prevent replacement)
                       set audit.hmacCheckpoints.enabled=true in config/runtime.yaml
                       to enable tamper-prevention checkpoints
```

### Files to touch

- `cmd/loopgate-doctor/` — add audit integrity check that reads
  `config/runtime.yaml` and reports HMAC checkpoint status
- `docs/setup/LEDGER_AND_AUDIT_INTEGRITY.md` — add a section explaining the
  default-off posture and when to enable it

---

## 5. Nonce map silent failure alerting

**Impact:** Medium. Will cause unexplained request denials in real use.

### Problem

The `seenRequests` map caps at 65,536 entries and fails closed when full. The
24-hour replay window against a 1-hour session TTL means the map fills 24x faster
than sessions turn over. When it fills, requests are denied silently with no log
entry distinguishing "nonce map full" from any other denial. An operator will not
know why requests are failing.

Two viable fixes. Pick one:

**Option A (preferred):** Reduce the replay window to match session TTL (1 hour).
Sessions expire in 1 hour, so replaying a nonce from a 6-hour-old session is
already impossible. The 24-hour window is wider than the threat model requires.

**Option B:** Keep the 24-hour window but add a warning log when the map exceeds
80% capacity (52,428 entries), and surface it in `loopgate-doctor` as a utilization
check.

### Files to touch

- `internal/loopgate/server.go` — nonce map capacity constants and fill check
- `cmd/loopgate-doctor/` — add replay map utilization check (if Option B)

---

## 6. Rate limit the hook endpoint

**Impact:** Medium. Prevents a runaway agent from saturating the control plane.

### Problem

`/v1/hook/pre-validate` requires only peer UID binding — no session token, no MAC.
Under a heavy AI-assisted session, Claude Code can call this endpoint many times
per minute. There is no rate limiting on the hook path. A runaway agent or
misbehaving harness can hammer the control plane with no back-pressure.

### What to build

A simple per-UID sliding window rate limiter on the hook endpoint. Suggested limit:
120 requests per minute per UID (2/second — generous for normal use, protective
against runaway). Return HTTP 429 when exceeded, which Claude Code will surface as
a tool error rather than silently spinning.

### Files to touch

- `internal/loopgate/server_hook_handlers.go` — add rate limiter middleware
  before the hook policy check
- `internal/loopgate/control_plane_state.go` — store per-UID hook call counters
  (or use a simple in-memory token bucket, no persistence needed)

---

## 7. End-to-end integration test

**Impact:** Medium. The test that catches failures unit tests cannot.

### Problem

Unit tests cover individual functions well. There is no test that starts the actual
server, connects a simulated Claude Code hook subprocess, sends a request through
the full stack, and verifies the audit ledger entry at the end. The integration
seams — socket setup, peer credential binding, hook auth, policy check, ledger
write — are only exercised in production.

### What to build

A single integration test in `internal/loopgate/integration_test.go` (build-tagged
so it does not run in unit mode unless explicitly requested):

1. Start a real `Server` instance with a temp socket path
2. Connect a Unix socket client simulating the hook subprocess
3. Send a `POST /v1/hook/pre-validate` request
4. Assert the response decision matches the test policy
5. Assert the expected event appears in the run ledger

This test would have caught the auth failure audit gap, the hook path resolution
asymmetry, and the nonce persistence issue under load — all of which only appear
when the pieces run together.

### Files to touch

- `internal/loopgate/integration_test.go` — new file, build tag `//go:build integration`
- `Makefile` (or equivalent) — add `make test-integration` target that runs with the tag

---

## 8. Version tagging and CHANGELOG

**Impact:** Low effort, high signal for external users.

### Problem

There is no version tag and no CHANGELOG. For a security tool, version tagging
matters operationally — operators need to know what version they're running when
reporting bugs or auditing their setup. The absence of a changelog signals to
potential users that this may be an unmaintained experiment.

### What to build

- Tag the current state as `v0.1.0` once the ship blockers from
  `loopgate_v1_hardening_plan.md` are closed
- Add `CHANGELOG.md` at the repo root with a single entry for v0.1.0
  summarizing the hardening work done in this cycle
- Add a `version` subcommand or flag to `cmd/loopgate` that prints the
  build version (injected via `-ldflags` at build time)

---

## 9. Structured startup output

**Impact:** High for operator confidence. Small implementation lift.

### Problem

When Loopgate starts, it prints minimal output. An operator has no immediate signal
about what posture the server is running in — which policy is loaded, what the trust
anchor is, how many capabilities are allowed vs approval-required vs denied, or
whether HMAC checkpoints are active. This information exists internally but is
invisible at startup.

### What to build

On server start, print a structured summary to stdout:

```
Loopgate started
  socket:          runtime/state/loopgate.sock
  policy:          core/policy/policy.yaml (key: loopgate-policy-root-2026-04)
  allowed:         12 capabilities
  approval-required: 3 capabilities
  denied:          all others (fail-closed)
  hmac checkpoints: OFF — audit chain detects tampering, does not prevent replacement
  nonce map:       0 / 65536 entries
```

This is the operator's confidence signal before anything runs. The HMAC checkpoint
warning and nonce map utilization should appear here even if they also appear in
`loopgate-doctor`.

### Files to touch

- `internal/loopgate/server.go` — add `printStartupSummary()` called after server
  initialization, before `http.Serve`
- `internal/config/runtime.go` — expose HMAC checkpoint setting for summary
- Keep the format stable so operators can grep it in logs

---

## 10. `policy explain` command

**Impact:** Medium. Makes policy readable without reading YAML or knowing the
capability model.

### Problem

Understanding what the current signed policy actually does requires reading YAML
and knowing the internal capability naming conventions. A non-developer operator
or a new contributor cannot quickly answer "what is Loopgate currently allowing
and denying?" without reading source or docs.

### What to build

```
loopgate-policy-admin explain
```

Renders the active signed policy in plain English:

```
Current policy (signed by: loopgate-policy-root-2026-04)

ALLOWED without approval:
  - bash commands matching: git, go test, go build, go run
  - filesystem reads under: /path/to/repo
  - MCP tools: (none configured)

REQUIRES APPROVAL:
  - filesystem writes under: /path/to/repo
  - filesystem mkdir

DENIED:
  - filesystem reads outside allowed roots
  - all unlisted capabilities (fail-closed)
```

This does not replace the YAML — it translates it for operators who need a
plain-language summary.

### Files to touch

- `cmd/loopgate-policy-admin/main.go` — add `explain` subcommand
- `internal/policy/` — add `ExplainPolicy(policy) string` renderer

---

## 11. Flip HMAC checkpoint default to on

**Impact:** Medium security improvement. Minimal performance cost for a local tool.

### Problem

HMAC checkpoints are off by default in `config/runtime.yaml`. The likely reason is
performance caution, but for a local single-operator tool the cost is negligible.
The default shapes what every user actually runs. Most users will never read the
config file and will never know the checkpoints exist.

### What to build

Change the default in `config/runtime.yaml` from:

```yaml
audit:
  hmacCheckpoints:
    enabled: false
```

to:

```yaml
audit:
  hmacCheckpoints:
    enabled: true
```

Add a note in the config file explaining what this does and how to disable it if
performance becomes a concern. Update `loopgate-doctor` to show `ok` rather than
`warn` for this check by default.

### Files to touch

- `config/runtime.yaml` — flip default
- `cmd/loopgate-doctor/` — update expected default state in checks
- `docs/setup/LEDGER_AND_AUDIT_INTEGRITY.md` — update to reflect new default

---

## 12. Replace `policy.yaml` with a well-commented starter policy

**Impact:** Medium. Shapes every new user's first experience.

### Problem

`core/policy/policy.yaml` contains a committed comment saying it is not yet a
polished open-source default, and includes a personal denied path
(`.claude/settings.json`). This file is the first policy every new user sees and
signs. It should teach as well as configure.

### What to build

Replace the current policy with a starter policy that:

- Has a header comment explaining what each major section does
- Shows common allow/deny patterns for AI-assisted development
- Includes commented-out examples of approval-required actions
- Removes personal or project-specific paths
- Is genuinely safe to use as a starting point

### Files to touch

- `core/policy/policy.yaml` — rewrite with clean commented defaults
- `docs/setup/GETTING_STARTED.md` — update if any paths change

---

## 13. `AGENTS.md` in the repo

**Impact:** Low effort, high signal. On-brand for a project that governs AI agents.

### Problem

Loopgate governs AI agent activity. It does not have an `AGENTS.md` file of its
own. Any AI assistant working on the Loopgate codebase has no explicit guidance
about what is and isn't appropriate — which authority boundaries to preserve,
which invariants must not be weakened, which parts of the codebase are
security-sensitive.

### What to build

An `AGENTS.md` at the repo root (or `CLAUDE.md` for Claude Code specifically)
covering:

- The non-negotiable invariants: fail-closed behavior, Ed25519 policy authority,
  peer credential binding, append-only ledger
- What to never weaken: HMAC verification, scope checks, audit writes
- How to run the project: build, test, sign policy, start server
- What `loopgate-doctor` is and when to run it
- Where to look when something is confusing: canonical docs, not archived material

### Files to touch

- `AGENTS.md` at repo root — new file

---

## 14. Makefile

**Impact:** Low effort. Reduces friction for contributors and agents.

### Problem

The repo now has `make build`, `make test`, and `make install-local`, but the
operator onboarding path is still too source-oriented and too easy to confuse
with contributor workflow. New operators still have to understand too much
about repo layout, long-running process handling, and detached policy-signing
commands before they can do useful work.

### What to build

A binary-first operator path with:

```makefile
make build          # build all Loopgate binaries into ./bin
make install-local  # install binaries into ~/.local/bin
```

Then build next:

- signed release artifacts for non-Go users
- a setup wizard that walks the operator through init, policy validation, and hook install
- a guided policy-profile chooser for common local modes such as strict, balanced, and permissive-dev

### Files to touch

- `Makefile` at repo root — new file

---

## 15. CI policy signing check

**Impact:** Low. Catches unsigned policy changes before they reach the server.

### Problem

If a contributor changes `core/policy/policy.yaml` without re-signing it, the
error only appears at server startup. CI does not catch this. For a project where
policy signing is a core invariant, the validation should be automated.

### What to build

Add a CI step that runs:

```yaml
- name: Verify policy signature
  run: go run ./cmd/loopgate-policy-sign -verify-setup
```

This fails immediately if the committed policy is not validly signed, before any
human or agent discovers it at runtime.

### Files to touch

- `.github/workflows/test.yml` — add verify step after `go vet`

---

## 17. Pi agent harness adapter

**Impact:** High. Extends Loopgate governance to a second agent harness with
minimal new server-side work.

### What Pi is

Pi (`pi.dev`) is a minimal terminal coding agent with a TypeScript extension model.
Its core philosophy is "primitives, not features" — permission gates, sub-agents,
and plan mode are all built as extensions rather than built-in features. This makes
it a natural fit for Loopgate.

Pi's extension API intercepts tool execution via:

```typescript
pi.on("tool_call", async (event, ctx) => {
    // event.toolName — name of the tool being called (e.g. "bash", "read", "write")
    // event.input    — tool parameters object (e.g. { command: "rm -rf ..." })
    // ctx.hasUI      — whether a terminal UI is available

    return undefined             // allow execution
    return { block: true, reason: "..." }  // block execution
})
```

This maps directly to Loopgate's existing hook endpoint.

### What to build

**A: Pi extension (`loopgate-pi-extension`)**

A TypeScript Pi extension, distributed as a package or single file, that:

1. On `tool_call` event, maps the Pi payload to Loopgate's `HookPreValidateRequest`:

```typescript
const hookRequest = {
    tool_name:  event.toolName,
    tool_input: event.input,
    cwd:        process.cwd(),   // Pi does not expose cwd in the event; use process cwd
}
```

2. POSTs to `http://localhost/v1/hook/pre-validate` over Loopgate's Unix socket
   using Node.js `http.request` with `socketPath` option — no TCP, no network:

```typescript
import * as http from "http"

function callLoopgate(req: object): Promise<{ decision: string; reason?: string }> {
    return new Promise((resolve, reject) => {
        const body = JSON.stringify(req)
        const options = {
            socketPath: process.env.LOOPGATE_SOCKET ?? "runtime/state/loopgate.sock",
            path: "/v1/hook/pre-validate",
            method: "POST",
            headers: { "Content-Type": "application/json", "Content-Length": body.length },
        }
        const clientReq = http.request(options, (res) => {
            let data = ""
            res.on("data", (chunk) => (data += chunk))
            res.on("end", () => resolve(JSON.parse(data)))
        })
        clientReq.on("error", reject)
        clientReq.write(body)
        clientReq.end()
    })
}
```

3. Maps the Loopgate response back to Pi's return value:

```typescript
const result = await callLoopgate(hookRequest)
switch (result.decision) {
    case "allow":
        return undefined
    case "deny":
        return { block: true, reason: result.reason ?? "denied by Loopgate policy" }
    case "approval_required":
        // Block and tell the operator what to do
        return {
            block: true,
            reason: `Loopgate requires operator approval. Run: loopgate-policy-admin approvals list`,
        }
    default:
        // Fail closed if the response is unexpected
        return { block: true, reason: "Loopgate returned an unexpected response" }
}
```

**B: Minor Loopgate server changes**

The `/v1/hook/pre-validate` endpoint currently uses `SO_PEERCRED` for peer UID
binding. This still works for Pi — Pi runs as the same local user. No transport
changes needed.

The only likely adjustment: the Claude Code hook handler may assume specific field
names in `HookPreValidateRequest` that differ from Pi's naming. Verify that
`tool_name` and `tool_input` field names are consistent, or add a normalization
layer in the extension.

**C: Environment variable for socket path**

Add `LOOPGATE_SOCKET` as an officially supported environment variable for the
Unix socket path (if not already present). This lets operators point the Pi
extension at a non-default socket location without modifying the extension source.

### Files to touch

- New repo or subdirectory: `adapters/pi/loopgate-extension.ts` — the Pi extension
- `internal/loopgate/server_hook_handlers.go` — verify field name compatibility
  with Pi's `toolName` / `input` payload shape
- `docs/setup/OPERATOR_GUIDE.md` — add Pi setup section: install extension,
  set `LOOPGATE_SOCKET`, start Pi

### What the Pi operator experience looks like

```
$ loopgate init
$ go run ./cmd/loopgate &
$ pi --extension ./adapters/pi/loopgate-extension.ts
```

From that point, every tool Pi attempts to run passes through Loopgate policy
before executing. Denials surface as Pi tool errors. Approval-required actions
block with a message directing the operator to the approvals CLI.

### Key differences from the Claude Code adapter

| | Claude Code | Pi |
|---|---|---|
| Hook mechanism | Shell script invoked by harness | TypeScript extension event |
| Auth | Peer UID binding via `SO_PEERCRED` | Same — runs as local user |
| Tool payload | `tool_name`, `tool_input`, `cwd` from hook env | `toolName`, `input` from event; `cwd` from `process.cwd()` |
| Approval UX | Hook blocks; operator uses approval CLI | Extension blocks; operator uses approval CLI |
| Distribution | Ships as shell scripts in repo | Ships as a `.ts` file or npm package |

---

## 16. Coverage gate on `loopgate-policy-sign`

**Impact:** Medium. Structural fix for the 11% coverage problem.

### Problem

`loopgate-policy-sign` has 11% test coverage and is the most critical tool in
the operator flow. The gap will not close without a structural enforcement. "Someone
should write tests" notes do not work.

### What to build

Add a coverage check to CI specifically for the policy signing package:

```yaml
- name: Check policy-sign coverage
  run: |
    go test -coverprofile=coverage.out ./cmd/loopgate-policy-sign/... ./internal/config/...
    go tool cover -func=coverage.out | awk '/total:/ { if ($3+0 < 60) { print "coverage below 60%: " $3; exit 1 } }'
```

Target: 60% minimum on the first pass, raised to 80% once tests are written.

### Files to touch

- `.github/workflows/test.yml` — add coverage check step
- `internal/config/policy_signing_test.go` — write the missing tests

---

## Implementation order

| # | Item | Effort | Before launch |
|---|------|--------|---------------|
| 1 | `loopgate init` command | Large | Yes — blocks new users |
| 2 | Auth failures in ledger | Small | Yes — audit contract |
| 3 | Approval CLI | Medium | Recommended |
| 4 | Doctor HMAC checkpoint signal | Small | Recommended |
| 5 | Nonce map alerting | Small | Recommended |
| 6 | Hook rate limiting | Small | Optional |
| 7 | Integration test | Medium | Optional |
| 8 | Version tagging + CHANGELOG | Trivial | Yes — launch hygiene |
| 9 | Structured startup output | Small | Yes — operator confidence |
| 10 | `policy explain` command | Medium | Recommended |
| 11 | Flip HMAC checkpoint default to on | Trivial | Recommended |
| 12 | Clean starter `policy.yaml` | Small | Yes — first impressions |
| 13 | `AGENTS.md` | Small | Yes — on-brand and practical |
| 14 | `Makefile` | Small | Recommended |
| 15 | CI policy signing check | Trivial | Recommended |
| 16 | Coverage gate on `loopgate-policy-sign` | Small | Recommended |

**Minimum viable launch set (1, 2, 8, 9, 12, 13):** closes the trust anchor gap,
fixes the audit contract, signals maintainability, gives operators a confidence
signal on startup, makes the default policy honest, and tells agents how to work
in the repo.

**Full recommended set before announcing publicly:** add 3, 4, 5, 10, 11, 14, 15, 16.
These close the gap between "works" and "operators can trust and contribute to it."
