package tools

import (
	"context"
	"strings"
	"time"
)

// HavenOperatorContextGuideVersion is bumped when the operator-facing text changes
// materially (Morph may cite it when helping troubleshoot).
const HavenOperatorContextGuideVersion = "2026-04-08"

// havenOperatorGuideBody is static, operator-safe documentation. It must not
// embed secrets, host paths from the environment, or instructions that widen
// authority — only describe how Haven and Loopgate already behave.
const havenOperatorGuideBody = `Haven operator guide (from Loopgate; authoritative for harness behavior)

MOUNTS / ADDITIONAL DIRECTORIES
- The Haven app may send additional_paths on each chat turn so you can help with files under those directories (typically via shell_exec after operator approval).
- Directory grants are intended to be time-bounded; when a path disappears from the request, treat it as no longer in scope for new work until the operator re-grants it in Haven (/adir or setup). Loopgate may enforce its own grant store in a future release.
- Paths are normalized on the client (symlinks resolved; unsafe system roots rejected). If the operator hits "not permitted", they chose a blocked path.

TUI LAYOUT
- Side panels (tasks, goals, approvals, journal list) are OFF by default for a minimal chat view. The operator presses Ctrl+B to toggle them on.
- Tab switches focus between chat and the sidebar when the sidebar is visible.
- Bracketed paste of folder paths: pasting one or more absolute directory paths (or file:/// URLs) into the chat input can register mounts automatically (same rules as /adir).

SHORTCUTS / COMMANDS (Haven TUI)
- /adir <path> — grant a resolved directory (TTL managed in Haven config until Loopgate stores grants).
- /desktop — on macOS, refresh the Desktop symlink to sandbox "projects" if Loopgate exposes host-layout.
- /help — full slash list; /status — session summary.

TROUBLESHOOTING
- "No access to files": check shell_exec is enabled if they need terminal reads; check additional_paths and project path in /status; host Mac folders use host.folder.* only when those presets are granted in Loopgate setup — not the same as arbitrary paths.
- Approvals: operator uses /approve or the sidebar (when visible) or haven approve in another terminal.
- Journal sidebar lists sandbox journal files; full entry opens on Enter (separate from working notes).

Morph: call haven.operator_context again when the user needs the latest version of this text.`

// HavenOperatorContext returns the static operator guide string for the given UTC time
// (for freshness line only).
func HavenOperatorContext(nowUTC time.Time) string {
	var b strings.Builder
	b.WriteString("VERSION: ")
	b.WriteString(HavenOperatorContextGuideVersion)
	b.WriteString("\n\n")
	b.WriteString(strings.TrimSpace(havenOperatorGuideBody))
	b.WriteString("\n\nRetrieved at (UTC): ")
	b.WriteString(nowUTC.Format(time.RFC3339))
	return b.String()
}

// HavenOperatorContextTool is a read-only capability: returns the operator guide above.
type HavenOperatorContextTool struct{}

func (t *HavenOperatorContextTool) Name() string      { return "haven.operator_context" }
func (t *HavenOperatorContextTool) Category() string  { return "haven" }
func (t *HavenOperatorContextTool) Operation() string { return OpRead }

func (t *HavenOperatorContextTool) Schema() Schema {
	return Schema{
		Description: "Return the Haven operator guide (mounts, TUI layout, shortcuts, troubleshooting). No arguments. Call when the operator asks how Haven works or needs accurate harness documentation.",
	}
}

func (t *HavenOperatorContextTool) Execute(_ context.Context, _ map[string]string) (string, error) {
	return HavenOperatorContext(time.Now().UTC()), nil
}

var _ Tool = (*HavenOperatorContextTool)(nil)
