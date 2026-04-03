**Last updated:** 2026-03-24

# Haven Capability MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Morph useful — model switching from UI, proactive memory, remove dead weight, and the foundation for real host file access.

**Architecture:** Six independent workstreams executed sequentially. Each produces a working, testable change. The desktop environment stays as-is (no dashboard pivot). Loopgate remains the sole authority for model inference and capability execution — Haven is a UI client, not a control plane.

**Tech Stack:** Go (Wails v2 backend), React/TypeScript (frontend), Loopgate Unix socket control plane, Ollama local model API

---

## Implementation status (engineering, 2026-03-22)

The items below were executed in the codebase; step checkboxes in each task are marked done for historical traceability. This is **not** a substitute for your own manual sign-off (especially Task 6 manual flow).

**Done in code**

- **Task 1 — Activity removed:** Backend and UI for the Activity app are gone; desktop icon strip and types updated.
- **Task 2 — Model settings:** `model_settings.go` + tests; Settings panel loads Ollama models and persists selection via `model_runtime.json`.
- **Task 3 — Proactive memory:** `memory_intent.go` is the **per-turn choke point**: `buildMemoryTurnDirective` injects proactive `memory.remember` guidance when the capability is present and no deterministic remember path fired. `buildRuntimeFacts()` in `chat.go` still carries baseline memory and continuity rules for every session.
- **Task 4 — Idle utilities:** Memory check-in, shared-folder exploration, and related registration in `idle.go` / `idle_behaviors.go`.
- **Task 5 — Host capabilities:** `host.folder.list`, `host.folder.read`, `host.organize.plan`, `host.plan.apply` registered end-to-end (Loopgate handlers, Haven allowlist, tool schema). Plan store and approval path for apply as designed.
- **Task 6 — Downloads loop:** Granted-folder lines in `buildRuntimeFacts()` (via `havenHasHostFolderCapabilities` + folder status), `idleCheckDownloads`, and desk-note prompt wired.

**Residual / follow-up (not blocking “code complete”)**

- **Manual E2E:** Operator should still run the Task 6 checklist against a real machine (Loopgate + Haven + granted Downloads).
- **Capability copy drift:** `havenCapabilityCatalog` and `buildResidentCapabilityFacts` in `cmd/haven/capabilities.go` must stay aligned with the Security UI (`LoopgateWindow.tsx`) and with `buildRuntimeFacts()` so the model does not see contradictory capability descriptions.
- **Plan storage:** In-memory / TTL limits for organization plans remain as implemented; cross-restart plan IDs are out of scope for this MVP.
- **Apply semantics:** Host apply uses the platform move/rename primitives available to the process; exotic cases (cross-volume, busy files) surface as operation errors rather than silent fallback.

**Expectation gap (vs “full local agent” / OpenClaw-style breadth)**

Users may expect a **broad** “run anything on my Mac” surface. This MVP intentionally separates:

- **`shell_exec`** — may be **policy-tight or absent**; even when allowed, it is not validated against **granted-folder roots** the way `host.*` is.
- **`host.folder.*` / `host.organize.plan` / `host.plan.apply`** — the **supported** path for touching real user files under **operator-granted** directories, with **plan vs apply** and **approval** on mutate.

The model should not treat these as interchangeable; resident capability facts in `capabilities.go` state that explicitly.

---

## File Structure

**Frontend layout:** The Haven desktop UI is modularized (`App.tsx` + `src/hooks/useHaven*.ts` + `components/HavenFloatingWindows.tsx`). See `docs/HavenOS/Haven_Frontend_Source_Map.md` before using stale line-number references from older notes.

### Files to Delete
- `cmd/haven/activitylog.go` — Activity log backend (506 lines)
- `cmd/haven/frontend/src/components/windows/ActivityWindow.tsx` — Activity window frontend (77 lines)

### Files to Create
- `cmd/haven/model_settings.go` — Backend for model listing + switching via Ollama API and model_runtime.json
- `cmd/haven/model_settings_test.go` — Tests for model listing and config persistence

### Files to Modify
- `cmd/haven/frontend/src/components/SettingsPanel.tsx` — Add model dropdown section
- `cmd/haven/frontend/src/components/DesktopSurface.tsx` — Remove `"activity"` from icon array
- `cmd/haven/frontend/src/lib/haven.tsx` — Remove activity from `ICON_MAP`, `WIN_DEFAULTS`, `AppID`
- `cmd/haven/frontend/src/App.tsx` — Remove ActivityWindow import, state, and rendering; if a dedicated window map exists, remove the activity branch from `HavenFloatingWindows.tsx` as well
- `cmd/haven/frontend/src/App.css` — Remove `.activity-*` CSS rules
- `cmd/haven/main.go` — Remove `GetActivityLog` usage if any startup references exist
- `cmd/haven/memory_intent.go` — Per-turn proactive memory directive via `buildMemoryTurnDirective` (primary injection point)
- `cmd/haven/chat.go` — Baseline memory and continuity rules in `buildRuntimeFacts()` (session-wide; not a substitute for per-turn directives)
- `cmd/haven/idle_behaviors.go` — Add new utility-class idle behaviors

---

## Task 1: Remove Activity App

**Files:**
- Delete: `cmd/haven/activitylog.go`
- Delete: `cmd/haven/frontend/src/components/windows/ActivityWindow.tsx`
- Modify: `cmd/haven/frontend/src/components/DesktopSurface.tsx:80`
- Modify: `cmd/haven/frontend/src/components/DesktopSurface.tsx:216-229` (morphAvatarStyleForAnchor)
- Modify: `cmd/haven/frontend/src/lib/haven.tsx` (ICON_MAP, WIN_DEFAULTS, AppID type)
- Modify: `cmd/haven/frontend/src/App.tsx` (remove ActivityWindow import, state, rendering)
- Modify: `cmd/haven/frontend/src/App.css` (remove .activity-* rules)

- [x] **Step 1: Remove activity from the desktop icon array**

In `cmd/haven/frontend/src/components/DesktopSurface.tsx`, line 80, change:
```typescript
(["morph", "loopgate", "workspace", "activity", "todo", "notes", "journal", "paint"] as AppID[])
```
to:
```typescript
(["morph", "loopgate", "workspace", "todo", "notes", "journal", "paint"] as AppID[])
```

Also remove `"activity"` from the `morphAvatarStyleForAnchor` switch cases (line 220).

- [x] **Step 2: Remove ActivityWindow from App.tsx**

In `cmd/haven/frontend/src/App.tsx`:
- Remove the `import ActivityWindow` line
- Remove the `activityLog` and `activityLogExpanded` state variables
- Remove the `GetActivityLog()` call in any useEffect
- Remove the `<ActivityWindow ... />` JSX rendering and its containing window wrapper
- Remove the `onToggleEntry` handler

- [x] **Step 3: Remove activity from haven.tsx type definitions**

In `cmd/haven/frontend/src/lib/haven.tsx`:
- Remove `"activity"` from the `AppID` type union
- Remove the activity entry from `ICON_MAP`
- Remove the activity entry from `WIN_DEFAULTS`

- [x] **Step 4: Delete the backend and frontend files**

```bash
rm cmd/haven/activitylog.go
rm cmd/haven/frontend/src/components/windows/ActivityWindow.tsx
```

- [x] **Step 5: Remove .activity-* CSS rules from App.css**

In `cmd/haven/frontend/src/App.css`, remove all CSS rules that start with `.activity-` (the activity hero, timeline, entries, dots, pills, empty state, etc.).

- [x] **Step 6: Verify the build compiles**

```bash
cd cmd/haven/frontend && npm run build && cd ../../.. && go build ./cmd/haven/
```
Expected: No errors. Activity references fully removed.

- [x] **Step 7: Run existing tests**

```bash
go test ./cmd/haven/ -v -count=1
```
Expected: All tests pass. No test references `GetActivityLog`.

- [x] **Step 8: Commit**

```bash
git add -A cmd/haven/activitylog.go cmd/haven/frontend/src/components/windows/ActivityWindow.tsx cmd/haven/frontend/src/components/DesktopSurface.tsx cmd/haven/frontend/src/lib/haven.tsx cmd/haven/frontend/src/App.tsx cmd/haven/frontend/src/App.css
git commit -m "feat: remove Activity Monitor app from Haven

The Activity app added complexity without clear MVP value.
Activity information is already visible through Loopgate's
Security window and desk notes. Removing it simplifies the
desktop and focuses the surface area."
```

---

## Task 2: Model Dropdown in Settings

This lets users switch between Ollama models from the Haven UI instead of editing JSON by hand.

**Files:**
- Create: `cmd/haven/model_settings.go`
- Create: `cmd/haven/model_settings_test.go`
- Modify: `cmd/haven/frontend/src/components/SettingsPanel.tsx`

### Sub-task 2a: Backend — List Available Models

- [x] **Step 1: Write the test for ListAvailableModels**

Create `cmd/haven/model_settings_test.go`:
```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListAvailableModels_ReturnsModelsFromOllama(t *testing.T) {
	// Simulate Ollama /api/tags response.
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []map[string]interface{}{
				{"name": "qwen2.5:7b", "size": 4700000000},
				{"name": "llama3:8b", "size": 4800000000},
			},
		})
	}))
	defer ollamaServer.Close()

	models, err := listOllamaModels(ollamaServer.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].Name != "qwen2.5:7b" {
		t.Errorf("expected first model qwen2.5:7b, got %s", models[0].Name)
	}
}

func TestListAvailableModels_HandlesOllamaDown(t *testing.T) {
	models, err := listOllamaModels("http://127.0.0.1:1") // unreachable
	if err != nil {
		t.Fatalf("should not error, got: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected 0 models when Ollama is down, got %d", len(models))
	}
}
```

- [x] **Step 2: Run test to verify it fails**

```bash
go test ./cmd/haven/ -run TestListAvailableModels -v -count=1
```
Expected: FAIL — `listOllamaModels` not defined.

- [x] **Step 3: Implement model listing backend**

Create `cmd/haven/model_settings.go`:
```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	modelruntime "morph/internal/modelruntime"
)

// OllamaModel represents a model available in the local Ollama instance.
type OllamaModel struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// ModelSettingsResponse is returned by GetModelSettings.
type ModelSettingsResponse struct {
	CurrentModel    string        `json:"current_model"`
	ProviderName    string        `json:"provider_name"`
	BaseURL         string        `json:"base_url"`
	AvailableModels []OllamaModel `json:"available_models"`
}

// SaveModelRequest is the request payload for SaveModelSelection.
type SaveModelRequest struct {
	ModelName string `json:"model_name"`
}

// GetModelSettings returns the current model config and available Ollama models.
func (app *HavenApp) GetModelSettings() ModelSettingsResponse {
	configPath := modelruntime.ConfigPath(app.setupRepoRoot())
	runtimeConfig, err := modelruntime.LoadPersistedConfig(configPath)
	if err != nil {
		return ModelSettingsResponse{CurrentModel: "unknown", ProviderName: "unknown"}
	}

	baseURL := runtimeConfig.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	// Strip /v1 suffix for Ollama API calls.
	ollamaBase := baseURL
	if len(ollamaBase) > 3 && ollamaBase[len(ollamaBase)-3:] == "/v1" {
		ollamaBase = ollamaBase[:len(ollamaBase)-3]
	}

	models, _ := listOllamaModels(ollamaBase)

	return ModelSettingsResponse{
		CurrentModel:    runtimeConfig.ModelName,
		ProviderName:    runtimeConfig.ProviderName,
		BaseURL:         runtimeConfig.BaseURL,
		AvailableModels: models,
	}
}

// SaveModelSelection updates the model_runtime.json with the selected model.
func (app *HavenApp) SaveModelSelection(req SaveModelRequest) SaveSettingsResult {
	configPath := modelruntime.ConfigPath(app.setupRepoRoot())
	runtimeConfig, err := modelruntime.LoadPersistedConfig(configPath)
	if err != nil {
		return SaveSettingsResult{Error: fmt.Sprintf("load model config: %v", err)}
	}

	runtimeConfig.ModelName = req.ModelName

	if err := modelruntime.SavePersistedConfig(configPath, runtimeConfig); err != nil {
		return SaveSettingsResult{Error: fmt.Sprintf("save model config: %v", err)}
	}

	return SaveSettingsResult{Success: true}
}

// listOllamaModels queries the Ollama /api/tags endpoint for available models.
// Returns an empty slice (not an error) if Ollama is unreachable.
func listOllamaModels(ollamaBaseURL string) ([]OllamaModel, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ollamaBaseURL+"/api/tags", nil)
	if err != nil {
		return nil, nil
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil // Ollama not running — not an error
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var result struct {
		Models []OllamaModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil
	}

	return result.Models, nil
}
```

- [x] **Step 4: Run tests to verify they pass**

```bash
go test ./cmd/haven/ -run TestListAvailableModels -v -count=1
```
Expected: PASS

- [x] **Step 5: Commit backend**

```bash
git add cmd/haven/model_settings.go cmd/haven/model_settings_test.go
git commit -m "feat: add model listing and switching backend for Haven settings

Queries Ollama /api/tags for available models. Updates model_runtime.json
on selection. Loopgate reads this file per-request so changes take effect
on the next chat turn without restart."
```

### Sub-task 2b: Frontend — Model Dropdown in Settings

- [x] **Step 6: Add model dropdown to SettingsPanel.tsx**

In `cmd/haven/frontend/src/components/SettingsPanel.tsx`, add:

1. Import the new Wails functions:
```typescript
import {
  GetSettings,
  GetSharedFolderStatus,
  GetModelSettings,
  SaveModelSelection,
  SaveSettings,
  SyncSharedFolder,
  type SharedFolderStatusResponse,
  type ModelSettingsResponse,
} from "../../wailsjs/go/main/HavenApp";
```

2. Add state:
```typescript
const [modelSettings, setModelSettings] = useState<ModelSettingsResponse | null>(null);
const [selectedModel, setSelectedModel] = useState("");
const [modelSaving, setModelSaving] = useState(false);
const [modelSaved, setModelSaved] = useState(false);
```

3. Load model settings in the existing `useEffect`:
```typescript
Promise.all([GetSettings(), GetSharedFolderStatus(), GetModelSettings()])
  .then(([settings, sharedFolderStatus, modelSettingsResponse]) => {
    setMorphName(settings.morph_name);
    setIdleEnabled(settings.idle_enabled);
    setAmbientEnabled(settings.ambient_enabled);
    setSharedFolder(sharedFolderStatus);
    setModelSettings(modelSettingsResponse);
    setSelectedModel(modelSettingsResponse.current_model);
    setLoaded(true);
  })
  .catch(() => setLoaded(true));
```

4. Add save handler:
```typescript
const handleModelSave = async () => {
  if (!selectedModel || selectedModel === modelSettings?.current_model) return;
  setModelSaving(true);
  try {
    await SaveModelSelection({ model_name: selectedModel });
    setModelSettings((prev) => prev ? { ...prev, current_model: selectedModel } : prev);
    setModelSaved(true);
    setTimeout(() => setModelSaved(false), 2000);
  } catch {
    // Keep current UI state.
  } finally {
    setModelSaving(false);
  }
};
```

5. Add the Model section JSX after the Identity section:
```tsx
<div className="settings-section">
  <div className="settings-section-title">Model</div>
  <label className="settings-label">Active model</label>
  {modelSettings && modelSettings.available_models.length > 0 ? (
    <>
      <select
        className="settings-input"
        value={selectedModel}
        onChange={(e) => setSelectedModel(e.target.value)}
      >
        {modelSettings.available_models.map((m) => (
          <option key={m.name} value={m.name}>{m.name}</option>
        ))}
        {!modelSettings.available_models.some((m) => m.name === selectedModel) && (
          <option value={selectedModel}>{selectedModel} (not installed)</option>
        )}
      </select>
      <div className="settings-hint">
        Changing the model takes effect on the next message. Provider: {modelSettings.provider_name}
      </div>
      {selectedModel !== modelSettings.current_model && (
        <button className="retro-btn" type="button" disabled={modelSaving} onClick={handleModelSave}>
          {modelSaved ? "Switched!" : modelSaving ? "Switching..." : "Switch Model"}
        </button>
      )}
    </>
  ) : (
    <div className="settings-hint">
      {modelSettings?.current_model
        ? `Using ${modelSettings.current_model} (${modelSettings.provider_name})`
        : "No local models found. Is Ollama running?"}
    </div>
  )}
</div>
```

- [x] **Step 7: Build and verify**

```bash
cd cmd/haven/frontend && npm run build && cd ../../.. && go build ./cmd/haven/
```
Expected: Compiles cleanly. Wails generates TypeScript bindings for `GetModelSettings` and `SaveModelSelection`.

- [x] **Step 8: Commit frontend**

```bash
git add cmd/haven/frontend/src/components/SettingsPanel.tsx
git commit -m "feat: add model dropdown to Haven Settings panel

Users can now switch between Ollama models from the Settings UI.
Shows all locally installed models via Ollama /api/tags.
Changes take effect on next message without restart."
```

---

## Task 3: Proactive Memory — Model-Inferred Saves

Currently memory only saves on explicit "remember" requests. This adds a **per-turn** directive (in `memory_intent.go`) that encourages the model to call `memory.remember` when it detects durable facts, preferences, or routines — even without the user saying "remember." `buildRuntimeFacts()` in `chat.go` remains the place for **session-wide** baseline memory rules (continuity, when to prefer `memory.remember` over hand-waving); it does **not** replace the per-turn proactive hint.

**Files:**
- Modify: `cmd/haven/memory_intent.go:26-68` — `buildMemoryTurnDirective` (proactive `RuntimeFacts` when no deterministic remember fired)
- Modify: `cmd/haven/chat.go` — keep `buildRuntimeFacts()` baseline memory lines consistent with the above (optional wording tweaks only)

- [x] **Step 1: Read the current buildRuntimeFacts to understand the injection point**

Read `cmd/haven/chat.go` — find the `buildRuntimeFacts()` method and understand what runtime facts are already injected.

- [x] **Step 2: Add proactive memory directive to buildMemoryTurnDirective**

In `cmd/haven/memory_intent.go`, modify `buildMemoryTurnDirective` to always append a proactive memory hint when `memory.remember` is available and there's no explicit remember request:

After line 46 (the existing `return memoryTurnDirective{}` for no-explicit-intent), replace:
```go
return memoryTurnDirective{}
```
with:
```go
return memoryTurnDirective{
    RuntimeFacts: []string{
        "You have durable memory via memory.remember. If the user shares a stable fact about themselves (name, role, preference, routine, project detail, or ongoing goal), save it proactively. Do NOT ask permission — just call memory.remember with a clear fact_key and fact_value. Only save durable facts, not ephemeral conversation details. If unsure whether something is durable, err on the side of saving — the user can always correct it.",
    },
}
```

This means:
- Explicit "remember" requests still get deterministic handling (fast path, no model involved)
- When there's no explicit request but `memory.remember` is available, the model gets a nudge to infer saves
- When `memory.remember` is not available, no directive is added (existing behavior)

- [x] **Step 3: Verify the build compiles and tests pass**

```bash
go build ./cmd/haven/ && go test ./cmd/haven/ -v -count=1
```
Expected: All pass. The change is additive — existing explicit-remember tests still work.

- [x] **Step 4: Commit**

```bash
git add cmd/haven/memory_intent.go
git commit -m "feat: add proactive memory directive for model-inferred saves

When memory.remember is available, the model is encouraged to
save durable facts proactively without requiring explicit
'remember' keywords. Explicit remember requests still use the
fast deterministic path. This makes memory feel natural instead
of requiring magic words."
```

---

## Task 4: Enhanced Idle Loops — More Useful Ambient Behavior

Add new utility-class idle behaviors so Morph does more useful things when idle, not just journal and create art.

**Files:**
- Modify: `cmd/haven/idle_behaviors.go`
- Modify: `cmd/haven/idle.go` (register new behaviors)

- [x] **Step 1: Read idle.go to understand behavior registration**

Read the full `idle.go` to find where behaviors are registered and how `nextBehavior()` selects them.

- [x] **Step 2: Add idleReviewMemory behavior**

In `cmd/haven/idle_behaviors.go`, the `idleReviewMemory` function already has a stub. Implement it:

```go
func idleReviewMemory(_ context.Context, app *HavenApp) error {
	wakeState := app.currentWakeSnapshot()
	if len(wakeState.Facts) == 0 {
		return nil // Nothing to review
	}

	factSummary := fmt.Sprintf("I have %d facts in memory.", len(wakeState.Facts))
	if len(wakeState.ActiveGoals) > 0 {
		factSummary += fmt.Sprintf(" There are %d active goals.", len(wakeState.ActiveGoals))
	}

	if app.hasActiveDeskNoteTitle("Memory check-in") {
		return nil
	}

	_, err := app.createDeskNote(DeskNoteDraft{
		Kind:  "update",
		Title: "Memory check-in",
		Body:  factSummary + " Want me to review what I know and clean up anything stale?",
		Action: &DeskNoteAction{
			Kind:    "send_message",
			Label:   "Review memories",
			Message: "Please review your current durable memories and list what you know about me. Flag anything that seems stale or wrong so I can correct it.",
		},
	})
	return err
}
```

- [x] **Step 3: Add idleExploreWorkspace behavior**

Add a new behavior that checks if there are new files in the workspace or shared folders:

```go
func idleExploreWorkspace(_ context.Context, app *HavenApp) error {
	folderStatus := app.currentFolderAccessStatus()

	var newItems []string
	for _, folder := range folderStatus.Folders {
		if folder.Granted && folder.MirrorReady && folder.EntryCount > 0 {
			newItems = append(newItems, fmt.Sprintf("%s (%d items)", folder.Name, folder.EntryCount))
		}
	}

	if len(newItems) == 0 {
		return nil
	}

	noteTitle := "Shared folders have content"
	if app.hasActiveDeskNoteTitle(noteTitle) {
		return nil
	}

	_, err := app.createDeskNote(DeskNoteDraft{
		Kind:  "update",
		Title: noteTitle,
		Body:  fmt.Sprintf("I can see: %s. Want me to take a look and suggest how to organize things?", strings.Join(newItems, ", ")),
		Action: &DeskNoteAction{
			Kind:    "send_message",
			Label:   "Yes, take a look",
			Message: "Please browse the shared folders in my workspace and give me a summary of what you see. If anything looks like it could be organized better, suggest a plan.",
		},
	})
	return err
}
```

- [x] **Step 4: Register new behaviors in idle.go**

In `idle.go`, `nextBehavior()` (line 180) currently only has `carryForwardIdleBehavior` as a utility behavior, then falls through to ambient random selection. Expand the utility section to check for memory review and workspace exploration before falling through to ambient:

After line 184 (`return carryForwardIdleBehavior, true`), add additional utility checks:
```go
// Check if memory review is useful (has facts to review).
wakeState := im.app.currentWakeSnapshot()
if len(wakeState.Facts) > 0 && im.cooldownReady(IdleBehaviorUtility) {
    return reviewMemoryIdleBehavior, true
}

// Check if shared folders have content worth exploring.
folderStatus := im.app.currentFolderAccessStatus()
for _, folder := range folderStatus.Folders {
    if folder.Granted && folder.MirrorReady && folder.EntryCount > 0 {
        return exploreWorkspaceIdleBehavior, true
    }
}
```

Also define the `reviewMemoryIdleBehavior` and `exploreWorkspaceIdleBehavior` as `IdleBehavior` structs following the existing `carryForwardIdleBehavior` pattern (with Name, Description, Class, Presence, Execute fields).

- [x] **Step 5: Build and test**

```bash
go build ./cmd/haven/ && go test ./cmd/haven/ -v -count=1
```
Expected: All pass.

- [x] **Step 6: Commit**

```bash
git add cmd/haven/idle_behaviors.go cmd/haven/idle.go
git commit -m "feat: add memory review and workspace exploration idle behaviors

Morph now reviews its memory and checks shared folders during
idle time, offering desk notes with actionable next steps.
These are utility-class behaviors that take priority over
ambient journaling/art."
```

---

## Task 5: Granted Folder Host Access Foundation

This is the critical capability gap. Currently Morph can only see mirrored copies of host files. This task adds typed host capabilities so Morph can read, plan, and (with approval) apply changes to files in granted folders on the real host filesystem.

This task lays the Loopgate + Haven foundation. The actual Downloads Organizer workflow (Task 6) builds on top.

**Files:**
- Modify: `cmd/haven/main.go:219-244` (havenAllowedCapabilities — add host.* capabilities)
- Modify: `internal/model/toolschema.go:12-32` (nativeToolAllowlist — add host.* tools)
- New capability registrations in Loopgate tool registry (exact files depend on Loopgate's tool registration pattern)

**Important:** This task requires understanding and modifying Loopgate's capability registration, which is the most complex subsystem. Each sub-step should be verified carefully.

- [x] **Step 1: Understand the tool registration pattern**

Read the tool registry at `internal/tools/` to understand how tools like `fs_read`, `fs_write` are registered. We need to add parallel `host.folder.list`, `host.folder.read`, `host.organize.plan`, and `host.plan.apply` tools that operate on granted host paths instead of sandbox paths.

- [x] **Step 2: Define the host tool schemas**

The host tools need these capabilities (from the Host Access design doc):

- `host.folder.list` — List contents of a granted host folder
  - Args: `folder_name` (string, e.g. "downloads"), `path` (string, optional subfolder)
  - Returns: directory listing with file names, sizes, dates

- `host.folder.read` — Read a file from a granted host folder
  - Args: `folder_name` (string), `path` (string, relative path within folder)
  - Returns: file contents (text) or metadata (binary)

- `host.organize.plan` — Create an organization plan for files in a granted folder
  - Args: `folder_name` (string), `plan` (array of moves/renames/mkdir operations)
  - Returns: plan ID, summary of proposed changes

- `host.plan.apply` — Apply a previously created organization plan
  - Args: `plan_id` (string)
  - Returns: results of each operation

- [x] **Step 3: Register host tools in the tool registry**

Create tool definitions following the existing pattern in `internal/tools/`. Each tool should:
- Validate that the target folder is in the granted folders list
- Route through Loopgate for execution (not bypass the security model)
- Return structured results

- [x] **Step 4: Add host capabilities to Haven's allowlist**

In `cmd/haven/main.go:224-243`, add to `havenAllowlist`:
```go
"host.folder.list":   {},
"host.folder.read":   {},
"host.organize.plan": {},
"host.plan.apply":    {},
```

- [x] **Step 5: Add host tools to the native tool allowlist**

In `internal/model/toolschema.go:12-32`, add to `nativeToolAllowlist`:
```go
"host.folder.list":   true,
"host.folder.read":   true,
"host.organize.plan": true,
"host.plan.apply":    true,
```

- [x] **Step 6: Implement host.folder.list capability handler**

This is the simplest host capability — it reads the granted folder listing from the real filesystem (not the sandbox mirror). Implementation should:
1. Resolve the folder name to a host path via Loopgate's granted folder registry
2. Validate the folder is actually granted
3. List directory contents with metadata (name, size, modified date, is_dir)
4. Return structured result

- [x] **Step 7: Implement host.folder.read capability handler**

Similar to fs_read but operates on granted host paths:
1. Resolve folder + relative path to absolute host path
2. Validate the resolved path is within a granted folder (path traversal prevention)
3. Read and return file contents (text files) or metadata (binary files)

- [x] **Step 8: Implement host.organize.plan capability handler**

This is the planning step — Morph proposes changes, nothing is executed yet:
1. Accept a structured plan (array of operations: move, rename, mkdir)
2. Validate all source paths exist in granted folders
3. Validate all target paths are within granted folders
4. Store the plan with a unique ID for later review/apply
5. Return plan summary for the user to review

- [x] **Step 9: Implement host.plan.apply capability handler**

This executes a previously stored plan:
1. Load the plan by ID
2. Re-validate all paths (files may have changed since planning)
3. Execute operations in order (mkdir first, then moves/renames)
4. Return results for each operation (success/failure)
5. This capability should require Loopgate approval (not standing-granted)

- [x] **Step 10: Build and test**

```bash
go build ./cmd/haven/ && go test ./cmd/haven/ -v -count=1
go test ./internal/tools/ -v -count=1
```

- [x] **Step 11: Commit**

```bash
git add internal/tools/ internal/model/toolschema.go cmd/haven/main.go
git commit -m "feat: add typed host folder capabilities for granted folder access

Adds host.folder.list, host.folder.read, host.organize.plan,
and host.plan.apply capabilities. These operate on real host
files in folders granted during setup, not sandbox mirrors.
Plan/apply separation ensures Morph proposes changes visibly
before executing them. host.plan.apply requires Loopgate approval."
```

---

## Task 6: Downloads Organizer MVP Flow

This wires everything together into the first real product loop: Morph reads, plans, and organizes the user's Downloads folder.

**Files:**
- Modify: `cmd/haven/idle_behaviors.go` — Add downloads-specific idle behavior
- Modify: `cmd/haven/chat.go` — Add runtime facts about granted folders
- Potentially modify: `cmd/haven/desknotes.go` — Add plan-review desk note type

- [x] **Step 1: Add downloads awareness to runtime facts**

In the `buildRuntimeFacts()` method (in `chat.go`), add awareness of granted folders:

```go
// Add granted folder context.
folderStatus := app.currentFolderAccessStatus()
for _, folder := range folderStatus.Folders {
    if folder.Granted && folder.MirrorReady {
        runtimeFacts = append(runtimeFacts, fmt.Sprintf(
            "You have access to the user's %s folder (%d items). You can use host.folder.list and host.folder.read to inspect it, and host.organize.plan + host.plan.apply to organize files.",
            folder.Name, folder.EntryCount,
        ))
    }
}
```

- [x] **Step 2: Add downloads organizer idle behavior**

In `cmd/haven/idle_behaviors.go`, add a behavior that specifically checks Downloads:

```go
func idleCheckDownloads(_ context.Context, app *HavenApp) error {
    folderStatus := app.currentFolderAccessStatus()

    var downloadsFolder *loopgate.FolderAccessStatus
    for i := range folderStatus.Folders {
        if folderStatus.Folders[i].Name == "downloads" && folderStatus.Folders[i].Granted {
            downloadsFolder = &folderStatus.Folders[i]
            break
        }
    }
    if downloadsFolder == nil || downloadsFolder.EntryCount == 0 {
        return nil
    }

    noteTitle := "Your Downloads folder could use some tidying"
    if app.hasActiveDeskNoteTitle(noteTitle) {
        return nil
    }

    _, err := app.createDeskNote(DeskNoteDraft{
        Kind:  "update",
        Title: noteTitle,
        Body:  fmt.Sprintf("I can see %d items in your Downloads. Want me to take a look and suggest how to organize them?", downloadsFolder.EntryCount),
        Action: &DeskNoteAction{
            Kind:    "send_message",
            Label:   "Yes, organize Downloads",
            Message: "Please look through my Downloads folder using host.folder.list. Categorize what you find and create an organization plan using host.organize.plan. Show me the plan before applying anything.",
        },
    })
    return err
}
```

- [x] **Step 3: Register idleCheckDownloads as a utility behavior**

Add it to the idle behavior selection in `idle.go` as a utility-class behavior. It should run when:
- Downloads folder is granted
- Downloads folder has content
- No active desk note already covers this

- [x] **Step 4: Build and test**

```bash
go build ./cmd/haven/ && go test ./cmd/haven/ -v -count=1
```

- [x] **Step 5: Manual test flow**

1. Start Loopgate: `go run ./cmd/loopgate`
2. Start Haven: `go run ./cmd/haven`
3. Verify Downloads folder appears in granted folders
4. Wait for idle behavior to trigger, or send a message asking Morph to organize Downloads
5. Verify Morph can list Downloads contents
6. Verify Morph creates an organization plan
7. Verify plan requires approval before applying

- [x] **Step 6: Commit**

```bash
git add cmd/haven/idle_behaviors.go cmd/haven/idle.go cmd/haven/chat.go
git commit -m "feat: add Downloads organizer MVP flow

Morph proactively checks Downloads folder and offers to organize.
The flow is: list → categorize → plan → review → apply.
Plan/apply requires visible approval. This is the first real
product loop demonstrating Morph's utility on host files."
```

---

## Execution Notes

**Priority order (original plan):** Tasks 1–3 were parallel-friendly. Task 5 required Loopgate work; Task 6 layered on Task 5. **As implemented:** all six landed in the repo; use the **Implementation status** section above for what remains manual or follow-up.

**Risk areas:**
- Task 5 (host access) is the most complex — it touches Loopgate's security model. The tool registration and capability execution path needs careful study before implementation.
- Task 2 (model dropdown) depends on Ollama being available. The UI should gracefully handle Ollama being down.
- Task 3 (proactive memory) is small but high leverage; keep `memory_intent.go` and `capabilities.go` / `chat.go` wording aligned so the model gets one coherent story.

**What this plan does NOT include:**
- Dashboard pivot (user confirmed: keep desktop)
- Browser/webfetch capability (Phase 2)
- Terminal/m-shell (Phase 2)
- Projects system (Phase 2)
- Aesthetic/retro redesign (later)
- Loopgate settings UI (separate path, not through Haven)
