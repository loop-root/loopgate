I'll do a thorough senior-engineer review of loopgate. Let me start by mapping the codebase, then dig deep on architecture, security, ops, and docs.

282 Go files, ~70k lines. I'll dispatch parallel exploration agents to cover the breadth, then synthesize.

I'll launch parallel specialized agents to cover this systematically. 70k LOC is too much to read linearly — I'll split by concern.

Four review agents running in parallel (security/transport, architecture/ops, docs/onboarding, dead-code/deps). I'll synthesize their findings when they complete.

Docs review done. Three more agents still running (security, architecture, dead-code). Waiting on them before synthesizing.

Dead-code sweep done. Two agents still running (security/transport, architecture/ops). Waiting on them.

Architecture review in. Waiting on the final agent (security/transport) before synthesis.

# Loopgate: Senior Review — Production Readiness

You asked for the rookie-mentor version. Here it is, cards-on-the-table.

## TL;DR verdict

**Don't ship publicly yet. But you're closer than you think.** The core is genuinely well-built — locking discipline, crypto choices, peer-cred enforcement, hash-chain + HMAC checkpoints, policy signing with Ed25519 and a pinned trust anchor, constant-time comparisons everywhere, zero TODO/FIXME/HACK markers, and a thoughtful comment block documenting 12 named mutexes and lock ordering. That's above-average Go work. But there's one real exploitable bug in the tool sandbox, a scope contradiction between docs and code, and about 20% of `internal/` that shouldn't be in a "governance layer" product.

## 1. Ship blockers

### 🔴 C1 — Symlink escape in host folder tools (exploitable)

[internal/loopgate/server_host_access_handlers.go:159](internal/loopgate/server_host_access_handlers.go:159) uses `pathUnderResolvedHostRoot` — pure lexical path check (`filepath.Clean` + `Rel` + `Contains "..")`. No `EvalSymlinks`, no `O_NOFOLLOW`, no `openat`.

If the operator grants `~/Downloads` and anything drops a symlink `evil -> /etc/passwd` into it (trivial — any download, any unzip), `host.folder.read`/`list` happily reads through it. `host.plan.apply` can write through a symlinked parent with `os.MkdirAll`+`os.Rename` and clobber files outside the granted root.

**This directly undermines your core value proposition** ("granted folder is the boundary"). The fix pattern already exists in this repo — `internal/sandbox/sandbox.go` and `internal/tools/path_open.go` both use the correct `openat(O_NOFOLLOW|O_DIRECTORY)` chain. Copy that pattern into host_access_handlers.

### 🔴 H1 — Shell allowlist bypass via PATH shadowing

[internal/tools/shell_exec.go:59](internal/tools/shell_exec.go:59) resolves bare command names (`git`, `ls`) through Go's `exec.LookPath`, which inherits the server's `PATH` ([shell_exec.go:257](internal/tools/shell_exec.go:257)). Any writable early-`PATH` directory (`~/.local/bin`, `./node_modules/.bin`, `$PWD` if present) is a bypass: allowlist a name, attacker plants a binary with that name, you execute it. You trust the command name; you should trust the resolved path.

Fix: pre-resolve each allowlist entry to an absolute path at policy-load time and execute _only_ that path. Or override `PATH` to a hermetic `/usr/bin:/bin` for children. Not a one-liner — this is an invariant change.

### 🔴 Scope contradiction: "not a chat harness" vs `/v1/model/reply`

Your README explicitly says Loopgate is a governance layer, not a chat harness. Your project-memory notes say "morphling removal is planned." But [internal/loopgate/server.go:691](internal/loopgate/server.go:691) wires `/v1/model/reply` → `handleModelReply`, which loads `modelruntime`, calls `internal/model/anthropic` and `internal/model/openai` providers, and executes a compiled prompt through 3k+ LOC of [internal/model/](internal/model/), [internal/modelruntime/](internal/modelruntime/), and [internal/prompt/](internal/prompt/).

**Pick one before shipping.** If governance-only: delete all three packages, the route, and `server_model_handlers.go`. That drops ~3k LOC of attack surface _and_ matches the docs. If you're keeping it, rewrite the README — current state is "the docs lie" and a reviewer will notice.

## 2. High-priority cleanup (a week of work, changes what you ship)

### Dead code (delete before public release)

- **[internal/shell/](internal/shell/)** — zero non-test importers. **Delete the package.** This also drops your only use of `github.com/chzyer/readline`, reducing direct deps to 3.
- **[internal/setup/](internal/setup/)** — only consumer is `internal/shell`. Goes with it.
- **`loopgate-admin` (8.3MB)** and **`loopgate/loopgate` (20MB)** — gitignored, but on-disk and confusing. `rm -rf` locally before cutting a release.
- **`output/`** — empty `pdf/` subdir, untracked. Delete.
- **`.external/haven_swift/` (191MB)** — a different product's Xcode project. Explain in AGENTS.md why it's there or drop it.

### Consolidate binaries

Merge `cmd/loopgate-policy-sign` (155 LOC) into [cmd/loopgate-policy-admin/main.go](cmd/loopgate-policy-admin/main.go) as a `sign` subcommand. They share env vars and flags; you end up with fewer binaries to ship, document, and test. Also drops four **600ms sleeps** in `cmd/loopgate-policy-admin/main_test.go:282,412,453,505` (classic flake bait).

Also: **[Makefile](Makefile) only builds `cmd/loopgate`**. Either add `loopgate-doctor`, `loopgate-ledger`, `loopgate-policy-admin` to `make build` or accept that release artifacts are incomplete.

### Linux is broken in subtle ways

- [internal/loopgate/platform_support.go:17](internal/loopgate/platform_support.go:17) — `LOOPGATE_ALLOW_NON_DARWIN=0` enables the bypass because the check is "non-empty." Intuitive opt-out flips to opt-in.
- [internal/secrets/store_selector.go:21](internal/secrets/store_selector.go:21) on Linux returns `ErrSecretBackendUnavailable`, which makes `ensureDefaultAuditLedgerCheckpointSecret` hard-fail startup. Fail-closed is correct, but "HMAC checkpoint default-on" is effectively darwin-only. Document this, or implement a file-based fallback at `$XDG_STATE_HOME/loopgate/secrets/audit_hmac.key` with `0700`.
- **Case-broken ADR links**: ~10 docs link to `docs/adr/` (lowercase) but the dir is `docs/ADR/`. Silently works on macOS default filesystems, breaks on Linux/CI. Carpet-bomb fix with sed.

## 3. Operational debt (2am debuggability)

**The good:**

- Single logging surface via `loopdiag.Manager` with 6 per-channel slog loggers (audit/server/client/socket/ledger/model) — very 2am-friendly.
- Request IDs are threaded through: 85 occurrences across 20 files. You can trace an approval end-to-end by one ID.
- Errors wrap with `%w` and name the failing file consistently.
- `loopgate-doctor bundle` is a real operator artifact, not cosmetic.

**What's missing:**

- **No metrics surface.** Zero `expvar`/`prometheus`/`metric` hits. For single-operator local use this is defensible, but at 2am you'll want "approvals pending," "auth failures/min," "ledger append latency." Add `expvar` on a localhost-only debug endpoint — it's stdlib, ~20 LOC.
- **Unbounded ledger re-scan risk**: [ledger.go:390](internal/ledger/ledger.go:390) chain-state cache is keyed on `(dev, ino, size, ctime)`. Startup is O(events) until the cache warms. If HMAC checkpoints are disabled (Linux default today), this is also your only tamper defense — which is why H4 matters.
- **Unnamed timeout literals**: `5*time.Second` at [server.go:824](internal/loopgate/server.go:824) and `30*time.Second` at [server.go:1312](internal/loopgate/server.go:1312). Name them.
- **[server.go](internal/loopgate/server.go) is 1560 lines**; `internal/loopgate/` has ~110 files in one package. The domain-split handler files (`server_*_handlers.go`) mostly rescue this, but `ui_server.go` (796 LOC) and `site_trust.go` (603 LOC) should probably split out.
- **`currentPolicyRuntime` legacy-compat branch** in [policy_runtime.go:32-54](internal/loopgate/policy_runtime.go:32-54) is a migration-in-flight that every request path hits. Finish it or delete the legacy side. That's a stale-read bug waiting to happen.

## 4. Under load

Honest assessment: it'll protect itself, but not gracefully.

- Ledger `Append` holds `Flock(LOCK_EX)` + `f.Sync()` per event — low thousands/sec ceiling on SSD. A runaway model spamming hooks will backpressure correctly (good), but unrelated approval RPCs serialize behind it briefly.
- No per-UID rate limit on capability requests. You already track `openByUID` in the server — extend it.
- No request-spawned goroutines blocking on fsync, no sync.Map misuse, no unbounded goroutines. Clean.

## 5. Documentation & new-user experience

**Score: 6.5/10.** Honest and non-overclaiming, which is rare. But there's no smoke test after `install-hooks` — "use Claude Code normally" is the cliff. Fix that with **one** paragraph: "run this prompt in Claude, then `loopgate-ledger tail` should show these two events." That single addition is probably worth two points.

**Other friction:**

- Go version and Claude Code prerequisite not in README.
- `loopgate --help` doesn't print a usage banner (falls into the legacy-flag error).
- No `TROUBLESHOOTING.md`, no FAQ. The threat model is thorough but written for security reviewers, not operators.
- `_map.md` files at repo root confuse humans — they're agent orientation scaffolding. Rename `AGENT_*.md` or move under `.internal/`.
- [AGENTS.md](AGENTS.md) (28KB) is Claude-agent instructions, not user docs. Fine, but size-wise it's hard to load in a single agent turn.

## 6. Overall security posture

**Strong core, one real hole.** The good:

- Ed25519 signed policy with pinned DER trust anchor, no `--accept-policy` bypass.
- Peer-cred enforcement on every connection (`LOCAL_PEERCRED`/`SO_PEERCRED`), capability tokens bound to peer identity.
- HMAC comparisons constant-time throughout.
- Sandbox traversal uses `openat(O_NOFOLLOW|O_DIRECTORY)` chains correctly.
- `isWithin` uses segment-boundary matching — no `/etc/passwd_notreally` bypass.
- HTTP has Read/Write/Idle/Header timeouts, `MaxHeaderBytes` 8KB, `MaxBytesReader` on every JSON route.
- No `sh -c`, no 0.0.0.0 listener, no hardcoded keys, no debug bypasses.

The bad:

- C1/H2 host folder handler doesn't use the sandbox pattern the rest of the repo uses. Real bug.
- H1 shell PATH trust is an invariant confusion.
- H3 socket chmod race (non-exploitable in normal paths because peer-cred still gates, but weakens defense-in-depth; fix umask before `net.Listen`).
- H4 Linux HMAC checkpoint story is ambiguous.

## 7. My honest take (as the mentor)

You've built something substantive. The _discipline_ on display — documented lock ordering, fail-closed defaults, no TODO markers, peer-cred on every path, hash-chain + HMAC with clear "checkpoints give you keyed proof, hash chain alone does not" honesty in the code — is better than 80% of pre-release Go servers I've seen. You clearly thought about this as a _security product_, not a "ship it and iterate."

The places you're going to get burned:

1. **The scope contradiction.** `model/modelruntime/prompt` + `/v1/model/reply` being there while the README says otherwise is the single biggest credibility risk at launch. First reviewer who runs `go doc ./internal/loopgate` is going to ask. Pick a direction.
2. **The symlink bug.** The exact kind of bug that looks fine in review but gets exploited day-one when someone writes a blog post. The fix is literally "use the pattern from the other package."
3. **The Linux story.** If you ship saying "macOS-first" clearly and honestly, that's fine. But the `LOOPGATE_ALLOW_NON_DARWIN` escape hatch as currently written is booby-trapped, and the ADR case links will embarrass you in CI.
4. **Readline as a dep of a governance server** is a smell. It goes away when you delete `internal/shell`, which is dead anyway.

The roadmap I'd give a rookie who owned this:

1. **This week:** delete `internal/shell`, `internal/setup`, the two stray binaries, the `output/` dir. Fix the ADR case-link carpet. Fix `LOOPGATE_ALLOW_NON_DARWIN` to compare to `"1"`.
2. **Next week:** fix C1/H1/H2. Merge `policy-sign` into `policy-admin`. Decide the model/prompt question and act on it.
3. **Before any public blog post:** add a 5-line smoke test to `GETTING_STARTED.md`. Write a `TROUBLESHOOTING.md`. Add `expvar` counters on a localhost debug endpoint. Finish the `policy_runtime` legacy migration.
4. **Post-V1:** per-UID rate limit, split `internal/loopgate/` into sub-packages, proper Linux keyring/file secret backend.

Net: this is a good-to-strong foundation with a clear path to shippable in roughly two focused weeks. The bones are right.
