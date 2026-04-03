**Last updated:** 2026-03-24

# Loopgate setup guide

This guide covers local setup, first run, runtime artifacts, and common operational checks.

**Native app integrators (Swift, etc.):** If you drive **Loopgate** over the **Unix socket HTTP API** and do not use the Haven desktop UI, start with [**Loopgate HTTP API for local clients**](./LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md) for session open, HMAC signing, and endpoints. Haven-specific steps below are optional for that workflow.

## 1) Prerequisites

- Go (use the version from `go.mod`)
- macOS or Linux (POSIX filesystem semantics expected)

Verify:

```bash
go version
```

## 2) Clone and validate

```bash
git clone https://github.com/loop-root/morph
cd morph
go mod tidy
go test ./...
```

Optional safety pass:

```bash
go test -race ./...
```

## 3) Run Haven

```bash
./start.sh
```

`./start.sh` builds the Haven frontend assets, restarts the repo-local `Loopgate` process by default so Haven uses the same checked-out code, and then launches the Haven desktop. Set `HAVEN_REUSE_RUNNING_LOOPGATE=1` if you intentionally want to keep using an already-running Loopgate on the same socket. **Shell-captured** stdout/stderr for the background Loopgate process goes to:

- `runtime/logs/loopgate.log` (mostly the `Loopgate listening on …` line and rare errors)

**Structured operator logs** (HTTP request lines, socket peer metadata, control-plane audit mirrors) are separate `slog` text files under `runtime/logs/` when `logging.diagnostic.enabled` is true in `config/runtime.yaml` — typically `server.log`, `socket.log`, `client.log`, etc. At **`default_level: debug`**, high-volume lines (`http_request`, `unix_peer`, audit mirrors, most control-plane events) are emitted at **DEBUG** severity so they drop out when you set `default_level: info`. Startup lines (`loopgate_listen`, `socket_listen`) stay at **INFO**.

Loopgate loads **`config/runtime.yaml`** (and the optional diagnostic override JSON) on every start for **runtime** settings; **`config/goal_aliases.yaml`** for goal aliases. Stale `runtime/state/config/runtime.json` / `goal_aliases.json` from older builds are ignored and removed if you use **PUT `/v1/config/runtime`** or **`/v1/config/goal_aliases`**.

If log files still never appear, Loopgate’s **repo root** may be wrong; set **`MORPH_REPO_ROOT`** when launching, same idea as Haven.

On macOS, the launcher runs Haven with Wails production build tags and the required `UniformTypeIdentifiers` linker flags. You still need Apple's Command Line Tools installed for the desktop app to link and launch correctly.

On startup, Haven also prepares a default shared folder on macOS at:

- `~/Shared with Morph`

Loopgate mirrors that folder into Haven as `shared`, so anything dropped there becomes visible inside Haven’s sandbox (`/morph/home`) without ambient host access.
While Haven is running, it now re-checks granted mirrored folders in the background and only re-mirrors when the source actually changed, so the shared intake stays current after launch without turning audit into a heartbeat log.

During Haven's first-run onboarding, you can also grant mirrored access to:

- `~/Downloads`
- `~/Desktop`
- `~/Documents`

Those folders are mirrored into Haven's `imports/` area through Loopgate. The operator client still works on the mirrored Haven copy, not by silently roaming your host filesystem.

### Haven first-run onboarding

The Haven desktop opens with a **short** first-run wizard:

- **Model connection** — local `Ollama` (recommended on macOS; Haven probes the endpoint and lists models) or **Anthropic** cloud (API key stored via Loopgate / secure backend).
- **Mirrored host folders** — optional grants for `Downloads`, `Desktop`, `Documents` (plus always-on “Shared with Morph”).

Finishing setup applies sensible defaults for assistant name, wallpaper, quiet-time **presence**, and **background** behavior; operators change those in **Settings** after first launch.

Remote model keys are stored through Loopgate in the secure backend / Keychain path. Local Ollama remains the recommended default for a fresh install. Default presence/background choices favor utility-first behavior for cloud models.

Inside Haven, built-in sandbox tools for the in-world workspace are now meant to be low-friction by default. Loopgate still mediates and audits them, but simple in-world actions like notes, journaling, paint saves, task updates, and sandbox-local file work should not feel like boundary crossings. `shell_exec`, mirrored user folders, and external integrations remain governed separately.

### No separate operator CLI

The shipped operator experience is **Haven + Loopgate** only. There is no supported readline/CLI product path in-tree.

Native apps can talk to Loopgate over **HTTP on the Unix socket** only; see [LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md](./LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md).

For local OpenAI-compatible runtimes such as Ollama, Haven sends native
tool-call assistant turns with explicit string `content` fields, which avoids
the stricter request-shape rejection some local runtimes returned on empty
tool-call messages.

## 4) Runtime paths and what is tracked

Haven (and Loopgate) create runtime/audit data at execution time. These paths are intentionally gitignored.

Generated at runtime (untracked):

- `runtime/.morph.lock` (single-instance lock)
- `runtime/state/loopgate.sock`
- `runtime/state/loopgate_events.jsonl`
- `runtime/state/loopgate_event_segments/*.jsonl`
- `runtime/state/loopgate_event_segments/manifest.jsonl`
- `runtime/state/working_state.json`
- `runtime/state/continuity_threads.json`
- `runtime/state/loopgate_memory.json`
- `runtime/state/model_runtime.json`
- `core/memory/ledger/ledger.jsonl`

Tracked source/config:

- `core/policy/policy.yaml`
- `core/policy/morphling_classes.yaml`
- `config/runtime.yaml`
- `persona/morph.yaml`
- `internal/**`, `cmd/**`, `docs/**`

Audit ledger rollover and event-size limits are configured in `config/runtime.yaml`
under `logging.audit_ledger`:

- `max_event_bytes`
- `rotate_at_bytes`
- `segment_dir`
- `manifest_path`
- `verify_closed_segments_on_startup`

Loopgate keeps appending to `runtime/state/loopgate_events.jsonl` as the active
segment. When the configured size limit is exceeded, it seals that file into the
segment directory, appends a manifest record, and continues writing to a fresh
active file at the same path.

`core/policy/policy.yaml` is required at startup. If it is missing, **Loopgate** fails closed rather than loading a permissive fallback policy.

Loopgate can also load provider connection definitions from
`loopgate/connections/*.yaml`. In the current phase these files support:

- `public_read`
- `client_credentials`
- `pkce`

They describe:

- provider metadata
- token/API URLs where applicable
- allowed hosts
- typed provider-backed capability names
- a `SecretRef` for authenticated connections

These YAML files must not contain raw secrets. The actual client secret must be
stored in the secure backend referenced by the `SecretRef`, or injected at
runtime when using the env backend. For PKCE connections, the `SecretRef`
stores the refresh token after the auth flow completes.

For `public_read` connections:

- no `SecretRef` is allowed
- no `Authorization` header is added
- the connection remains explicitly typed, host-allowlisted, and extractor-bound

Example template:

- `docs/setup/examples/public_status_github.yaml`
- `docs/setup/examples/public_repo_issues_generic.yaml`

This example shows the intended shape for a public status workflow:

- `grant_type: public_read`
- host allowlist
- typed capability name
- deterministic nested JSON extraction

Example operator flow:

1. Copy the example into `loopgate/connections/statuspage.yaml`
2. Start Loopgate and the operator client (e.g. Swift Haven or the Wails reference under `cmd/haven/`)
3. Ask:

```text
go check the status of github and let me know what's going on
```

Expected behavior:

- The client should prefer the configured status capability over filesystem reads
- Loopgate should execute the typed public-read capability
- The client should return a clear success/partial-success summary plus display-safe
  details, without treating the remote content as prompt-safe by default

To use it locally, copy the example into `loopgate/connections/*.yaml` and
adjust the provider, subject, and host/path values to match your target source.
This is still an explicit typed capability, not arbitrary browsing.

For a narrow public repository/issues workflow:

1. Copy `docs/setup/examples/public_repo_issues_generic.yaml` into
   `loopgate/connections/repo_issues.yaml`
2. Replace the placeholder host and path with a real public JSON issues feed that
   returns an object-wrapped list such as `issues.items`
3. Start Loopgate and the operator client (e.g. Swift Haven or the Wails reference under `cmd/haven/`)
4. Ask:

```text
show me the latest open issues for the sample repository
```

Expected behavior:

- The client should prefer the configured repo/issues capability when there is one
  clear match
- Loopgate should return a bounded, display-safe list of issue objects
- The client should summarize title/state/updated time without treating the remote
  issue text as prompt-safe by default

Useful operator commands:

- `/connections`
- `/connections validate <provider> <subject>`
- `/connections pkce-start <provider> <subject>`
- `/connections pkce-complete <provider> <subject> <state> <code>`
- `/sandbox import <host-path> [destination-name]`
- `/sandbox stage <sandbox-path> [output-name]`
- `/sandbox metadata <sandbox-output-path>`
- `/sandbox export <sandbox-output-path> <host-destination>`
- `/morphling spawn <class> <capability[,capability...]> [<sandbox-input> ...] -- <goal text>`
- `/morphling status [morphling-id]`
- `/morphling terminate <morphling-id> [reason text]`
- `/memory discover <terms...>`
- `/memory recall <key-id>`
- `/memory remember <fact-key> <value>`
- `/site inspect <url>`
- `/site trust-draft <url>`

The current memory path is intentionally narrow:

- deterministic tag discovery over Loopgate-owned resonate-key metadata
- exact key only through Loopgate
- explicit remembered profile-fact writes only through `/memory remember`
- Loopgate-owned rate limiting on explicit remembered profile-fact writes
- bounded recall by token budget
- three explicit continuity roles:
  - `current`
  - `next`
  - `previous`
- durable distillates, resonate keys, and wake state remain Loopgate-owned
- no fuzzy search
- remembered state remains distinct from freshly checked state

The site-inspection flow is a narrow runtime onboarding path for new
`public_read` sources:

- `/site inspect <url>` reports:
  - normalized URL
  - scheme/host/path
  - HTTP status and content type
  - HTTPS validity and certificate details when available
  - whether Loopgate can suggest a safe trust draft
- `/site trust-draft <url>` creates a reviewable YAML draft under
  `loopgate/connections/drafts/`
- drafts are exact-source declarations, not wildcard browsing permissions
- drafts are not auto-activated; they must be reviewed and moved into
  `loopgate/connections/*.yaml` explicitly

The sandbox flow is now first-class:

- `/sandbox import` copies a host path into the controlled sandbox under
  `/morph/home/imports/`
- `/sandbox stage` copies a sandbox path into the staged outputs area under
  `/morph/home/outputs/`
- `/sandbox metadata` shows the staged artifact record for an output, including
  its artifact ref, content hash, and review/export actions
- `/sandbox export` copies a staged sandbox output back to a host destination
- export from `/morph/home/outputs/` requires that the output was staged and has a matching
  artifact record

This preserves the intended boundary:

- import in
- work inside
- stage artifact
- export out with approval

Preferred operator-visible sandbox paths use the mini-filesystem namespace under
`/morph/home`, for example:

- `/sandbox stage /morph/home/imports/spec.md patch.diff`
- `/sandbox metadata /morph/home/outputs/patch.diff`
- `/sandbox export /morph/home/outputs/patch.diff ./patch.diff`

Legacy relative sandbox paths such as `imports/spec.md` still resolve, but
display-safe output now uses the virtual `/morph/home/...` namespace.

Sandbox paths remain confined to `/morph/home`; host-path traversal and
host-symlink escape are denied. Sandbox copy operations only accept regular
files and directories and use no-follow source opens so the copied source does
not silently change after validation.

The current morphling flow is intentionally lifecycle-only:

- Loopgate loads authoritative morphling class policy from
  `core/policy/morphling_classes.yaml` at startup and fails closed on invalid
  class definitions
- `/morphling spawn` validates class policy, requested capabilities, and
  sandbox scope before it creates a working directory under
  `/morph/home/agents/`
- morphlings are managed through Loopgate's existing local Unix-socket control
  plane rather than a separate public API
- request-level denials stay outside the morphling lifecycle and do not mint a
  `morphling_id`
- instantiated morphlings now move through Loopgate-owned lifecycle states:
  `requested`, `authorizing`, `pending_spawn_approval`, `spawned`, `running`,
  `completing`, `pending_review`, `terminating`, `terminated`
- Loopgate can now mint a one-time worker launch token for a spawned
  morphling, bind a worker session to the local socket peer that opens it, and
  accept only the dedicated worker update surface:
  - bounded `status_text`
  - bounded `memory_strings`
  - explicit staged artifact refs at completion time
- morphling completion now stages artifacts under Loopgate control, produces a
  stable artifact manifest hash, and moves reviewable completions into
  `pending_review`
- `/morphling review <morphling-id> <approve|reject>` resolves staged
  morphling output explicitly; the worker cannot self-approve its own results
- `/morphling status` shows the current morphling pool, `goal_hint`, status
  text, working directory, requested/granted capabilities, and any
  Loopgate-owned memory strings
- `/morphling terminate` marks a morphling terminated without silently exporting
  or promoting artifacts
- default policy keeps morphling spawn disabled and caps active morphlings at 5
  when enabled

Morphling lifecycle events are recorded on Loopgate's append-only hash-linked
audit log. This gives cryptographically verifiable ordering and event chaining
for spawn, worker launch/session, execution, review, and termination actions,
even though morphling current state is also materialized into a small state
file for quick status lookup. Raw goal text stays in Loopgate-controlled state;
the append-only audit ledger stores only a session-bound `goal_hmac` for
correlation. Raw worker `status_text` and `memory_strings` are hashed in audit
projection events rather than written verbatim. On restart, Loopgate resolves
any nonterminal morphling record to a deterministic terminated outcome rather
than trying to resurrect it implicitly.

## 5) Policy configuration

Policy file:

- `core/policy/policy.yaml`

Key points:

- Filesystem policy is operation-based (`read`/`write`), not tool-name based.
- Deny rules override allow rules.
- Write approval can be required.

Check active policy at runtime with:

```text
/policy
```

## 6) State and shutdown behavior

State is crash-safe (`temp file -> fsync -> atomic rename`).

If state is corrupt, the operator client renames it to:

- `working_state.json.corrupt.<timestamp>`

On shutdown (`/exit`, Ctrl+D, Ctrl+C, SIGTERM), the operator client performs ordered cleanup:

1. stop intake
2. seal and roll the active continuity thread if it contains continuity events
3. submit the sealed previous thread to Loopgate for idempotent inspection when thresholds are crossed
4. append `session.ended`

## 7) Single-instance enforcement

The operator client enforces per-repo single-instance execution using a non-blocking lock file:

- `runtime/.morph.lock`

If another instance is active, startup fails closed.

## 8) Secrets and redaction

Secrets support is conservative first-pass:

- `env` backend: runtime injection only (read-only)
- `secure` backend: macOS Keychain on Darwin, explicit fail-closed stubs on
  unsupported platforms
- no plaintext secret file backend
- no silent fallback from secure backend to env backend
- `/setup` persists only secret references (env var names), never raw secret values

See `docs/setup/SECRETS.md`.

## 9) Tool usage

For slash commands, model tool calls, policy and approval behavior, see:

- `docs/setup/TOOL_USAGE.md`

For the broader architecture and security model, start with:

- `docs/README.md`
- `docs/design_overview/loopgate.md`
- `docs/loopgate-threat-model.md`

## 10) Current limitations (as of 2026-03-24)

- Ledger and Loopgate audit logs are append-only and hash-linked, and they now
  verify prior chain state on append/bootstrap. They are not yet externally
  signed or anchored for strong out-of-band tamper evidence.
- Loopgate binds sessions to the Unix-socket peer; possession of tokens without the same OS identity is denied. Optional executable-path pinning exists for stricter clients; v1 product standard remains **HTTP on the local socket** (not XPC). See `docs/roadmap/roadmap.md` and `docs/DOCUMENTATION_SCOPE.md` (maintainer execution plans may live in a local-only `docs/superpowers/` tree).
- Ledger/distillate rotation is not fully implemented everywhere operators might expect.
- Distillate IDs are second-based and may collide in high-frequency runs.
- **Haven** uses native structured tool calls with Loopgate. Legacy XML-style tool tags (`<tool_call>...</tool_call>`) are not the supported product path; parity with every capability is not guaranteed if legacy parsers remain in shared libraries.
- Model inference in Loopgate supports **Anthropic** and **OpenAI-compatible** (including local Ollama) configurations; exact surface depends on `modelruntime` and connection setup.

## 11) Troubleshooting

Startup fails with "another morph instance appears active":
- ensure no other Haven (reference) / client process is holding the lock for this repo
- remove stale `runtime/.morph.lock` only after confirming no active process

Policy denials:
- inspect `core/policy/policy.yaml`
- run `/policy` and `/debug safepath <path>`

Expected files not present:
- run the operator client or Loopgate once; runtime/audit files are created lazily

---

Loopgate is designed to fail closed at capability boundaries, keep auditability explicit, and treat model/tool output as untrusted input.
