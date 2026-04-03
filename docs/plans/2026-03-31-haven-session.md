# Haven OS — Session Plan & Handoff
**Date:** 2026-03-31
**Repos:** `~/Dev/morph` (Go backend) · `~/Dev/Haven` (Swift macOS app)
**Status:** Active development, pre-Kickstarter. No App Store — distributes as DMG. No remote for Haven repo yet.

---

## Completed This Session

- [x] **DMG bundling** — loopgate binary bundled inside `Haven.app/Contents/MacOS/loopgate`; Swift app launches it on startup, seeds config from `Contents/Resources/loopgate-seed/` on first run.
  - `Haven/App/LoopgateLauncher.swift` — production vs dev path, `getpwuid` for real home dir, socket at `/tmp/haven-{uid}.sock`
  - `Haven/App/LoopgateDataSeeder.swift` — first-launch seed from bundle resources
  - `Haven/API/HTTP/MorphHTTPClient.swift` — `hasBundledLoopgate()`, `bundledLoopgateSocketPath()`, `bundledLoopgateDataDirectory()`
  - `Entitlements/Haven.entitlements` — removed `com.apple.security.app-sandbox` (DMG distribution doesn't need it; sandbox breaks subprocess spawning and socket path length)
  - `scripts/build-dmg.sh` — 5-step script: universal Go binary (arm64 + amd64 via lipo with Xcode clang for CGo), xcodebuild archive, inject binary + seed configs, ad-hoc codesign (`-`), hdiutil DMG

- [x] **App icon** — Morph head design: terracotta `#C8573A` background, cream `#FAF4E6` oblong head (rx=18, 172×118), orange dot eyes (cx=58/cx=142, r=16), thin rainbow strip at head bottom (5px per band), antenna. All 7 PNG sizes regenerated (16, 32, 64, 128, 256, 512, 1024px). Source SVG at `/tmp/morph_head3.svg`.

- [x] **Setup wizard — picker text color** — `SetupWizardView.swift`: added `.foregroundStyle(theme.text1)` + `.tint(theme.text1)` + background box to fix white-on-cream model picker text.

- [x] **Setup wizard — deferred folder permissions** — Removed `loadFolderStatuses()` from `loadInitialState()`; now only called when user reaches the folder step in `handlePrimaryAction()` case `.model`. Prevents macOS folder permission prompts on app launch.

- [x] **Messenger scroll fix** — `MessengerViewModel.swift`: added `scrollTick: Int` counter incremented in `updateMessage(id:text:)`. `MessengerView.swift`: stable `"listBottom"` anchor at end of `LazyVStack` + `onChange(of: viewModel.scrollTick)` triggers `scrollToBottom`. Fixes pane jumping up during streaming.

- [x] **Journal ownership framing** — `cmd/haven/capabilities.go`: removed "write freely and proactively" and "you do not need to be asked" from journal runtime fact; kept ownership framing ("it is YOUR journal — not the user's"). `journal.write` RuntimeHint: "write a private journal entry — your own space for honest reflection and thought".

- [x] **Journal once-per-day guard** — `cmd/haven/idle_behaviors.go`: `idleJournal()` stats `scratch/journal/YYYY-MM-DD.md` before running; returns `nil` if file exists and has content. One idle journal entry per calendar day maximum.

- [x] **Idle settings HTTP API** — `internal/loopgate/server_haven_settings.go`: new `GET|POST /v1/haven/settings/idle` endpoint. Reads/writes `idle_enabled` + `ambient_enabled` in `runtime/state/haven_preferences.json` under `havenPreferencesMu`. `havenDefaultAmbientEnabled()` returns false for Anthropic and non-loopback OpenAI-compatible (mirrors `cmd/haven/settings.go` logic). Route registered in `server.go`.

- [x] **Idle manager disk refresh** — `cmd/haven/idle.go`: `watch()` tick re-reads settings from disk at top of each 30s cycle. Settings UI changes take effect within one tick without process restart.

- [x] **Idle settings UI** — `Haven/Windows/Settings/SettingsView.swift`: `DeveloperSettingsTab` now has "Idle Behaviors" section with two toggles: *Background activity* (`idle_enabled`) and *Ambient (journal, paint)* (`ambient_enabled`). Ambient auto-disables when idle is off. `APIModels.swift`: `IdleSettingsResponse` + `IdleSettingsRequest`. `MorphHTTPClient.swift`: `idleSettings()` GET + `setIdleSettings()` POST.

---

## Open Items

### High Priority

#### 1. Chat regression — "I can't reach home base" (Anthropic) + silent hang (local)
**Background:** Commits `a74c3b9` + `8ac5fcd` added `runHavenChatToolLoop` to `handleHavenChat`. Every chat request now runs a multi-round tool loop (up to 12 iterations, 120s timeout context). Two symptoms:
- **Anthropic:** `MorphError.serviceUnavailable` — socket-level read returns 0 (connection closed before response). Server is closing the connection, consistent with a Go panic in the handler that isn't recovered, causing `net/http` to close the connection.
- **Local model:** Request takes the full 120s timeout silently — no typing indicator, user sees nothing.

**Fix 1 — Panic recovery in `handleHavenChat`:**
File: `internal/loopgate/server_haven_chat.go`
Add at the top of the handler:
```go
defer func() {
    if r := recover(); r != nil {
        server.log.Error("haven.chat panic", "panic", r)
        http.Error(writer, "internal error", http.StatusInternalServerError)
    }
}()
```

**Fix 2 — Audit log in error path:**
Currently `server.logEvent("haven.chat", ...)` is called after a success/error branch that returns early on `loopOutcome.err != nil`, so failed sessions are never audited. Move the log call (or add a `defer`) so every chat attempt is recorded regardless of outcome.

**Fix 3 — Typing indicator in Swift:**
File: `Haven/Windows/Messenger/MessengerViewModel.swift`
Add `@Published var isThinking: Bool = false`. Set to `true` before beginning the HTTP request, `false` in the completion/error path.
File: `Haven/Windows/Messenger/MessengerView.swift`
Show a typing indicator bubble (match `MessageRow.swift` style, anchor left/Morph side) when `isThinking == true`.

**Fix 4 — Optional timeout reduction:**
`modelCtx` timeout in `handleHavenChat` is 120s. Consider lowering to 60s so the fallback text appears sooner on slow local models.

---

#### 2. Attachments crash
**Symptom:** Adding an image or file attachment crashes the whole app.
**Likely cause:** The Go backend receives a message with base64-encoded image data but the message handler doesn't handle that field — possibly a nil dereference or JSON decode failure in `handleHavenChat` or the thread store's `AppendEvent`.
**Where to look:**
- `cmd/haven/chat.go` — `SendMessage()` and `runChatLoop()` — check how the `attachments` field is handled
- `internal/loopgate/server_haven_chat.go` — look for attachment handling in the request struct
- `Haven/Windows/Messenger/MessengerView.swift` — the file picker / drag-drop code that builds the attachment payload
**Approach:** Add a request body struct that includes `attachments []struct{ Type, Data string }` and handle gracefully if the model client doesn't support multimodal (return a user-facing error instead of crashing).

---

#### 3. Uninstaller
**What needs to be removed:**
- `~/Library/Application Support/Haven/` — Loopgate data, preferences, journal, notes, paintings, workspace
- `/tmp/haven-{uid}.sock` — socket file (already gone on process exit, but clean up anyway)
- `UserDefaults` keys for Haven (socket override, any cached settings)
- Login items if Haven was set to launch at login
**Implementation options:**
- A shell script bundled inside the DMG (`Uninstall Haven.command`)
- A menu item in Haven's Help or Haven menu → "Uninstall Haven…" with a confirmation dialog
- Recommended: both. Script for power users, menu item for everyone else.
**File to create:** `scripts/uninstall.sh`
**Haven menu integration:** `HavenApp` or `AppMenuView.swift` — add "Uninstall Haven…" that calls `NSWorkspace.open(uninstallScriptURL)` after confirming with an `NSAlert`.

---

#### 4. Haven man page / Morph self-knowledge
**Background:** Morph doesn't know how Haven is laid out — what the sidebar panels do, what apps exist, how the desktop works. This causes it to give confused or wrong instructions to users.
**What to write:** A structured system-prompt injection (not a man page per se, but a "Haven layout fact") that describes:
- Sidebar strip (right edge): quick-open for Messenger
- Sidebar panels: Journal, Tasks, Recall, Paint, Workspace, Notes, Loopgate — which are toggleable in Settings → Appearance
- Desktop: icon grid, sticky notes, drag-to-rearrange
- Apps: Messenger (main chat), Journal (Morph's own), Notes (Morph's working notes), Paint (Morph's canvas), Task Board, Recall
**Where to inject:** `cmd/haven/chat.go` `buildRuntimeFacts()` — add a static Haven layout fact (always injected, no capability gate needed).
**Alternative:** Add it to `capabilities.go` `buildResidentCapabilityFacts()` as an unconditional entry, or add a dedicated `buildHavenLayoutFact()` function.

---

#### 5. Paint tool clarity (MacPaint 2026 aesthetic)
**Agreed vision:** Self-drawing animation where it looks like the model picked up a brush and hand-painted the picture. Old Mac vibes, modern polish. MacPaint-inspired: 512×342 canvas, Haven palette (`#C8573A`, `#FAF4E6`, `#2E2E2E`, `#E8A72A`), chunky strokes, fill patterns.

**SVG `stroke-dashoffset` draw-in animation:**
The model outputs SVG with explicit stroke coordinates, colors, and widths. The Swift view animates each path's `stroke-dasharray` / `stroke-dashoffset` so strokes draw themselves in sequence — gives the "brush being dragged" effect.

**What's needed:**
1. **Capability hint update** — `paint.save` RuntimeHint already says "explicit stroke coordinates, colors, and widths". Needs a concrete SVG format example so the model stops outputting generic SVGs. Add example to `capabilities.go`:
   ```
   Output format: SVG 512×342, paths with stroke (not fill) for brush strokes.
   Example: <path d="M50,100 Q120,80 200,110" stroke="#C8573A" stroke-width="8" stroke-linecap="round" fill="none"/>
   ```
2. **Paint window in Swift** — `Haven/Windows/Paint/` — currently displays static SVG. Needs:
   - Parse path elements in order
   - Animate each with `stroke-dashoffset` from full-length to 0, staggered by ~80ms per path
   - Replay animation button
3. **Gallery link in chat** — after `paint.save` succeeds, tool result should include a link the user can tap to open the Paint window and see the new piece
4. **User → Morph sharing** — drag an image into the chat from the gallery so Morph can respond to its own work

---

### Lower Priority

#### 6. Idle feature granularity (per-behavior toggles)
Current settings only expose `idle_enabled` (all background activity) and `ambient_enabled` (journal + paint). Users may want finer control — e.g. allow carry-forward but disable downloads check.
**Approach:** Extend `HavenSettings` in `cmd/haven/settings.go` with per-behavior booleans (`journalEnabled`, `gardenEnabled`, `carryForwardEnabled`, etc.) and add corresponding toggles to the Idle Behaviors section in `SettingsView.swift`. Keep the master `idle_enabled` as a global kill switch.

---

## Architecture Notes for Next Agent

### Repo layout
```
~/Dev/morph/          — Go monorepo (Loopgate server + cmd/haven Wails app)
  cmd/haven/          — Wails frontend (LEGACY, being replaced by Swift)
  internal/loopgate/  — HTTP server that Swift Haven talks to
    server.go         — mux registration, Server struct
    server_haven_*.go — Haven-specific handlers
  cmd/loopgate/       — Binary that gets bundled in the DMG
~/Dev/Haven/          — Swift macOS app (active UI)
  Haven/App/          — App entry, launchers, seeders
  Haven/API/          — HTTP client + API models
  Haven/Windows/      — All SwiftUI views
  scripts/            — build-dmg.sh, future uninstall.sh
```

### Key invariants
- **Socket path:** Always `/tmp/haven-{uid}.sock` in production (≤20 chars, avoids 104-char Unix limit). Dev uses `{repoRoot}/runtime/state/loopgate.sock`.
- **Data directory:** `~/Library/Application Support/Haven/` in production (real home via `getpwuid`, NOT sandbox home). Dev uses `{repoRoot}/runtime/`.
- **Ambient default:** OFF for Anthropic and non-loopback OpenAI-compatible (`defaultAmbientEnabled()` / `havenDefaultAmbientEnabled()`). ON for local (Ollama/loopback).
- **Capability hints:** Live in `cmd/haven/capabilities.go`. `buildResidentCapabilityFacts()` injects them into every model turn. DO NOT put proactive instructions here for cloud providers — the idle system handles timing.
- **Settings persistence:** `runtime/state/haven_preferences.json`. All settings (morph_name, wallpaper, idle_enabled, ambient_enabled) go here. New settings fields require a key in this map AND default logic in both `cmd/haven/settings.go:GetSettings()` AND `internal/loopgate/server_haven_settings.go:readIdleSettings()`.
- **Authentication:** All `/v1/haven/settings/*` endpoints require HMAC-signed requests with actor label "haven". Follow the exact pattern in `server_haven_settings.go:handleHavenSettingsShellDev`.
- **Haven repo has no git remote** — local only. Push morph repo to `https://github.com/loop-root/morph.git`.

### How to test the idle settings
1. Build and run Loopgate: `go run ./cmd/loopgate` from `~/Dev/morph`
2. Open Haven (Swift app or Xcode)
3. Settings → Developer → "Idle Behaviors" — toggle "Background activity" off
4. Check `runtime/state/haven_preferences.json` — should have `"idle_enabled": false`
5. Toggle back on, verify `idle_enabled: true` written

### Chat regression verification
Send "organize my downloads folder" via the Anthropic provider. Before the panic recovery fix, this returns "I can't reach home base." After the fix, it should either succeed or return a clean fallback text. Check `runtime/logs/` for `haven.chat panic` entry with the panic value — that will reveal the exact root cause.
