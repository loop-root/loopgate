**Last updated:** 2026-04-08

# How it works (operator client and Loopgate)

**Primary operator experience:** developer tools via **HTTP on the local Unix socket** and, when shipped, **proxy** mode. **In-tree MCP is deprecated and removed** (see `docs/setup/LOOPGATE_MCP.md`, ADR 0010); use native HTTP clients or **out-of-tree** forwarders.

There is **no separate product CLI** in the supported path for new integrations. Privileged work goes through **Loopgate** on a local Unix socket.

## 1) Startup — IDE client or local integration

On launch, the IDE host or local HTTP client:

1. resolves the active **project root** (or workspace binding)
2. loads persona and effective model runtime configuration
3. loads or initializes **client-side** runtime state (continuity threads, local ledger paths)
4. ensures runtime and audit parent directories exist where the client owns them
5. acquires the client lock (e.g. `runtime/.morph.lock`) where applicable
6. connects to **Loopgate** at the local Unix socket (typically `runtime/state/loopgate.sock`)
7. loads authoritative **policy and capability** status from Loopgate
8. opens a Loopgate **control session** for the current session identity
9. loads or recovers **continuity thread-role** state (`current` / `next` / `previous`)
10. loads Loopgate-owned **durable wake state** for governed recall
11. starts the interactive client surface
12. appends `session.started` to the client ledger when that path is active

If Loopgate is unavailable, the client fails explicitly rather than running “offline privileged” mode.

Other HTTP-native clients follow the same control-plane pattern; see [`LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`](../setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md).

## 2) Input routing

### Slash commands and shell-backed actions

Handled through `internal/shell` or the IDE host integration layer. Read-only commands summarize local and Loopgate-visible state. Capability commands are **forwarded to Loopgate** as typed requests.

### Natural language input

- user message is recorded in the **client** ledger (session history)
- the client compiles a persona-aware prompt
- the active model produces assistant text (always **untrusted content**)
- tool calls are parsed from model output (**native structured tools** with Loopgate on supported paths)
- each capability request is sent to **Loopgate** for validation and execution

## 3) Loopgate capability execution path

For each capability request:

1. Client sends a typed request over **HTTP on the Unix socket**.
2. Loopgate validates the capability token and session binding.
3. Loopgate validates capability name and argument schema against policy.
4. Loopgate applies authoritative policy (deny-by-default).
5. Loopgate creates a **pending approval** when required.
6. The **operator client** renders the approval; the operator sends the decision back through Loopgate.
7. Loopgate executes the capability if allowed.
8. Loopgate returns a structured result or typed denial.
9. The client records the outcome in its ledger and session history.

No model call executes a host-affecting tool **directly**.

## 4) Current capability surface

Loopgate-mediated capabilities include:

- `fs_list`, `fs_read`, `fs_write`
- configured provider-backed HTTP read capabilities from `loopgate/connections/*.yaml`

Filesystem capabilities use hardened path logic; enforcement is **Loopgate**. Provider capabilities use Loopgate-owned auth, quarantine raw bodies, and return allowlisted structured fields.

## 5) Logging and audit

**Client** user-visible audit stream (typical layout):

- `core/memory/ledger/ledger.jsonl`

**Loopgate** control-plane event stream:

- `runtime/state/loopgate_events.jsonl`

Separation is intentional: the client ledger is operator-facing session history; Loopgate events are **authoritative** for control-plane actions, morphlings, and promotions.

## 6) Shutdown

Ordered cleanup seals continuity threads, submits inspection when thresholds are met, and appends `session.ended` when that path is active.

## 7) Where to read more

- [`LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md`](../setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md)
- [`loopgate.md`](./loopgate.md)
- [`architecture.md`](./architecture.md)
- [Threat model](../loopgate-threat-model.md)
