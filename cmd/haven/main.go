package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"morph/internal/config"
	"morph/internal/haven/threadstore"
	"morph/internal/loopgate"
	"morph/internal/sandbox"
	statepkg "morph/internal/state"
	"morph/internal/tools"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Resolve repo root — needed for Loopgate socket, persona, policy.
	repoRoot := os.Getenv("MORPH_REPO_ROOT")
	if repoRoot == "" {
		repoRoot = "."
	}

	// Resolve paths.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot determine home directory: %v\n", err)
		os.Exit(1)
	}

	havenDataDir := filepath.Join(homeDir, ".haven")
	// Default to the repo-local Loopgate socket (matches cmd/loopgate behavior).
	loopgateSocketPath := filepath.Join(repoRoot, "runtime", "state", "loopgate.sock")

	// Override from environment if set.
	if envData := os.Getenv("HAVEN_DATA_DIR"); envData != "" {
		havenDataDir = envData
	}
	if envSocket := os.Getenv("LOOPGATE_SOCKET"); envSocket != "" {
		loopgateSocketPath = envSocket
	}

	// Derive workspace identity from the absolute repo root path.
	// This deterministic hash ensures thread isolation per workspace and binds
	// the Loopgate session to a specific workspace for audit purposes.
	absRepoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve absolute repo root: %v\n", err)
		os.Exit(1)
	}
	workspaceID := deriveWorkspaceID(absRepoRoot)

	// Initialize thread store with workspace isolation.
	store, err := threadstore.NewStore(filepath.Join(havenDataDir, "threads"), workspaceID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "initialize thread store: %v\n", err)
		os.Exit(1)
	}

	// Connect to Loopgate and configure session.
	loopgateClient := loopgate.NewClient(loopgateSocketPath)
	loopgateClient.SetWorkspaceID(workspaceID)

	// Liveness only: GET /v1/health does not expose policy or capability inventory.
	healthDeadline := time.Now().Add(30 * time.Second)
	var healthErr error
	for time.Now().Before(healthDeadline) {
		_, healthErr = loopgateClient.Health(context.Background())
		if healthErr == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if healthErr != nil {
		fmt.Fprintf(os.Stderr, "ERROR: loopgate is unavailable at %s: %v\n", loopgateSocketPath, healthErr)
		fmt.Fprintln(os.Stderr, "Start Loopgate first with: go run ./cmd/loopgate")
		os.Exit(1)
	}

	sessionID := statepkg.MakeSessionID()
	// Security: Haven requests only the minimal capabilities needed for UI operations.
	// Capability names come from a static allowlist; Loopgate intersects with its registry
	// at session open. Full authoritative inventory requires a signed GET /v1/status after open.
	loopgateClient.ConfigureSession("haven", sessionID, havenCapabilityAllowlist())
	loopgateStatus, err := loopgateClient.Status(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: loopgate status at %s: %v\n", loopgateSocketPath, err)
		fmt.Fprintln(os.Stderr, "Start Loopgate first with: go run ./cmd/loopgate")
		os.Exit(1)
	}
	havenCapabilities := havenAllowedCapabilities(loopgateStatus.Capabilities)
	grantedHavenCapabilities := filterCapabilitySummaries(loopgateStatus.Capabilities, havenCapabilities)

	// Use policy from Loopgate (authoritative source).
	persona, _ := config.LoadPersona(repoRoot)
	pol := loopgateStatus.Policy

	// Ensure Morph's sandbox filesystem exists.
	sandboxPaths := sandbox.PathsForRepo(absRepoRoot)
	if err := sandboxPaths.Ensure(); err != nil {
		fmt.Fprintf(os.Stderr, "create sandbox directories: %v\n", err)
		os.Exit(1)
	}

	// Tool registry rooted in Morph's sandbox home — Morph operates in its own
	// virtual filesystem, not the user's real filesystem.
	toolRegistry, err := tools.NewSandboxRegistry(absRepoRoot, sandboxPaths.Home, pol)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create tool registry: %v\n", err)
		os.Exit(1)
	}
	for _, warningText := range buildHavenCapabilityAuditWarnings(havenCapabilityAllowlist(), loopgateStatus.Capabilities) {
		fmt.Fprintf(os.Stderr, "haven startup warning: %s\n", warningText)
	}
	validateCapabilityAllowlist(havenCapabilityAllowlistSet(), toolRegistry)

	// Event emitter — context is set in OnStartup when Wails runtime is ready.
	emitter := &wailsEmitter{}

	originalsDir := filepath.Join(havenDataDir, "originals")
	if err := os.MkdirAll(originalsDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "create originals dir: %v\n", err)
		os.Exit(1)
	}

	app := NewHavenApp(loopgateClient, store, toolRegistry, persona, pol, grantedHavenCapabilities, emitter, originalsDir, absRepoRoot)
	app.sandboxHome = sandboxPaths.Home
	app.sessionID = sessionID

	// Initialize presence system with Morph's name.
	morphName := loadMorphName(repoRoot)
	app.presence = NewPresenceManager(emitter, morphName)
	app.presence.snapshotPath = filepath.Join(absRepoRoot, "runtime", "state", "haven_presence.json")
	app.presence.persistPresenceSnapshot(app.GetPresence())

	// Load durable memory (wake-state) from Loopgate at startup.
	app.LoadWakeState()

	// Initialize idle behavior system.
	app.idleManager = NewIdleManager(app)
	// Load idle preference from saved settings.
	savedSettings := app.GetSettings()
	app.idleManager.mu.Lock()
	app.idleManager.enabled = savedSettings.IdleEnabled
	app.idleManager.ambientEnabled = savedSettings.AmbientEnabled
	app.idleManager.mu.Unlock()

	if err := wails.Run(&options.App{
		Title:  "Haven",
		Width:  1024,
		Height: 768,
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: func(ctx context.Context) {
			emitter.ctx = ctx
			app.SetWailsContext(ctx)

			// Verify workspace is reachable (non-fatal if it fails).
			if _, err := loopgateClient.SandboxList(ctx, loopgate.SandboxListRequest{SandboxPath: "workspace"}); err != nil {
				fmt.Fprintf(os.Stderr, "haven: workspace not reachable (sandbox dirs will be created on first import): %v\n", err)
			}
			if _, err := app.syncGrantedFolderAccess(context.Background()); err != nil {
				fmt.Fprintf(os.Stderr, "haven: folder sync unavailable: %v\n", err)
			}
			app.folderSync = NewFolderSyncManager(app)
		},
		OnShutdown: func(context.Context) {
			app.Shutdown()
		},
		Bind: []interface{}{
			app,
		},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "haven: %v\n", err)
		os.Exit(1)
	}
}

// wailsEmitter implements EventEmitter using the Wails runtime.
type wailsEmitter struct {
	ctx context.Context
}

func (e *wailsEmitter) Emit(eventName string, data interface{}) {
	if e.ctx != nil {
		wailsruntime.EventsEmit(e.ctx, eventName, data)
	}
}

// loadMorphName reads the morph name from haven preferences.
func loadMorphName(repoRoot string) string {
	prefsPath := filepath.Join(repoRoot, "runtime", "state", "haven_preferences.json")
	data, err := os.ReadFile(prefsPath)
	if err != nil {
		return "Morph"
	}
	var prefs map[string]interface{}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return "Morph"
	}
	if name, ok := prefs["morph_name"].(string); ok && name != "" {
		return name
	}
	return "Morph"
}

// deriveWorkspaceID computes a deterministic workspace identity from the absolute
// repo root path. The result is a SHA-256 hex digest that uniquely identifies this
// workspace. Using a hash (not the raw path) prevents path-based injection and
// ensures consistent length regardless of path depth.
func deriveWorkspaceID(absRepoRoot string) string {
	hash := sha256.Sum256([]byte(absRepoRoot))
	return hex.EncodeToString(hash[:])
}

// havenAllowedCapabilities returns the minimal set of capabilities Haven needs.
// Haven is a UI client, not a second control plane. Rather than requesting all
// available capabilities (which would escalate authority if new capabilities are
// added), Haven declares an explicit allowlist intersected with what Loopgate offers.
//
// Security invariant: Haven's capability scope must be explicitly declared and
// must not automatically expand when new capabilities are registered in Loopgate.
func havenAllowedCapabilities(available []loopgate.CapabilitySummary) []string {
	allowlist := havenCapabilityAllowlist()
	availableSet := make(map[string]struct{}, len(available))
	for _, cap := range available {
		availableSet[cap.Name] = struct{}{}
	}

	var result []string
	for _, capabilityName := range allowlist {
		if _, ok := availableSet[capabilityName]; ok {
			result = append(result, capabilityName)
		}
	}
	return result
}

func havenCapabilityAllowlist() []string {
	// Capabilities Haven needs for chat orchestration (model-requested tool calls).
	// These are the tool names that the model may invoke during a conversation.
	// Filesystem tools are the core set; add new tool names here explicitly
	// as new tools are registered — do NOT enumerate automatically.
	return []string{
		"fs_read",
		"fs_write",
		"fs_list",
		"fs_mkdir",
		"journal.list",
		"journal.read",
		"journal.write",
		"notes.list",
		"notes.read",
		"notes.write",
		"memory.remember",
		"paint.list",
		"paint.save",
		"note.create",
		"desktop.organize",
		"todo.add",
		"todo.complete",
		"todo.list",
		"shell_exec",
		"host.folder.list",
		"host.folder.read",
		"host.organize.plan",
		"host.plan.apply",
	}
}

func havenCapabilityAllowlistSet() map[string]struct{} {
	allowlistSet := make(map[string]struct{}, len(havenCapabilityAllowlist()))
	for _, capabilityName := range havenCapabilityAllowlist() {
		allowlistSet[capabilityName] = struct{}{}
	}
	return allowlistSet
}

func buildHavenCapabilityAuditWarnings(allowlist []string, available []loopgate.CapabilitySummary) []string {
	availableSet := make(map[string]struct{}, len(available))
	for _, cap := range available {
		availableSet[cap.Name] = struct{}{}
	}

	warningTexts := make([]string, 0)
	for _, capabilityName := range allowlist {
		if _, ok := availableSet[capabilityName]; !ok {
			warningTexts = append(warningTexts, fmt.Sprintf("allowlisted capability %q is not offered by Loopgate", capabilityName))
		}
	}
	return warningTexts
}

func validateCapabilityAllowlist(allowlist map[string]struct{}, registry *tools.Registry) {
	for _, warningText := range buildCapabilityAllowlistWarnings(allowlist, registry) {
		fmt.Fprintf(os.Stderr, "haven: WARNING: %s\n", warningText)
	}
}

func buildCapabilityAllowlistWarnings(allowlist map[string]struct{}, registry *tools.Registry) []string {
	if registry == nil {
		return nil
	}

	warningTexts := make([]string, 0)
	for capabilityName := range allowlist {
		if !isLocalRegistryCapability(capabilityName) {
			continue
		}
		if !registry.Has(capabilityName) {
			warningTexts = append(warningTexts, fmt.Sprintf("capability %q is in the allowlist but not registered in the tool registry — this tool will not work", capabilityName))
		}
	}
	return warningTexts
}

func isLocalRegistryCapability(capabilityName string) bool {
	switch capabilityName {
	case "journal.list",
		"journal.read",
		"journal.write",
		"notes.list",
		"notes.read",
		"notes.write",
		"memory.remember",
		"paint.list",
		"paint.save",
		"note.create",
		"desktop.organize",
		"todo.add",
		"todo.complete",
		"todo.list",
		"shell_exec",
		"host.folder.list",
		"host.folder.read",
		"host.organize.plan",
		"host.plan.apply":
		return true
	default:
		return false
	}
}
