package loopgate

const (
	maxHavenChatBodyBytes  = 8 * 1024 * 1024 // 8 MB — allows image attachments (base64)
	maxHavenChatTurns      = 20
	maxHavenToolIterations = 12
	// When the model answers host-folder organize asks with prose only (no tools), re-prompt
	// it this many times before returning the last assistant text to the client.
	// Each nudge is an extra full model round-trip in the same HTTP request — high values
	// multiply latency (e.g. 4 here ⇒ up to 5 sequential Reply calls). Keep this small.
	maxHavenHostFolderProseOnlyNudges = 2
	// After host.organize.plan succeeds, the model often answers with prose claiming Loopgate
	// approval is pending — but approvals are only created when host.plan.apply runs. Nudge apply
	// this many times before giving up (each nudge is one extra model round-trip).
	maxHavenHostPlanApplyNudges = 2
	useCompactHavenNativeTools  = true
)

const havenToolLoopContinuationFact = "You are after tool results in the thread. Continue from the user's **latest** message; those outputs are authoritative. Do not re-run tools that already succeeded in this thread unless the user explicitly asks to redo or refresh. Do **not** call host.folder.*, host.organize.plan, or host.plan.apply unless (a) the user's latest message is clearly about listing/organizing a granted Mac folder, or (b) you are in the middle of that same workflow in **this** request (e.g. you just listed and must plan, or plan just returned plan_id and apply is next). If the user narrowed scope (e.g. only memory, or said organizing is not needed), answer with text or the tools that match—do not drag in host organize. Do not restart with a generic greeting or onboarding menu unless they changed topic."

// havenToolLoopSlimOrganizeFact is injected only while a host-folder organize workflow is active
// for this HTTP request (see runHavenChatToolLoop). It must not run on every follow-up iteration when
// the toolkit is merely available — that steered models to re-list/re-plan after unrelated tools (e.g. memory.remember).
//
// The full capability catalog is not re-sent after the first call to keep later iterations fast.
const havenToolLoopSlimOrganizeFact = "NEXT STEP (host folders): if the folder was just listed → call host.organize.plan NOW with a plan_json that includes EVERY non-folder entry from the listing — every single file must have a move or mkdir operation, do not leave any files unorganized in the root; if a plan_id was just returned → call host.plan.apply with that plan_id. Do not ask for confirmation — the Loopgate popup IS the approval step."

const havenToolFollowupUserNudge = "Continue from my prior request using the tool results in the thread above. Give the next concrete step or answer with no greeting, and do not ask me to restate the goal unless it was truly ambiguous. Do not start host-folder organize tools unless my latest message asked for that or you are mid list→plan→apply from this same turn."

// havenHostFolderActNowNudge is injected when the model hasn't listed the folder yet.
const havenHostFolderActNowNudge = `Do NOT describe what you will do — call invoke_capability RIGHT NOW. Use capability="host.folder.list" and arguments_json="{\"folder_name\":\"downloads\"}" (or the correct preset). arguments_json must be a JSON-encoded string. Emit a structured tool call only — no prose.`

// havenHostFolderPlanNowNudge is injected when the folder has already been listed
// (conversation has prior assistant analysis) but the model returned prose instead of calling host.organize.plan.
const havenHostFolderPlanNowNudge = `Folder contents are already in the thread above. Do NOT ask the user for confirmation — the Loopgate popup that appears after host.plan.apply IS the approval step. Call invoke_capability RIGHT NOW with capability="host.organize.plan". Your plan_json MUST include a move or mkdir operation for EVERY file in the listing — do not skip any files or leave them in the root. arguments_json must be a JSON-encoded string, e.g. "{\"folder_name\":\"downloads\",\"plan_json\":\"[{\\\"kind\\\":\\\"mkdir\\\",\\\"path\\\":\\\"Archives\\\"},{\\\"kind\\\":\\\"move\\\",\\\"from\\\":\\\"file.zip\\\",\\\"to\\\":\\\"Archives/file.zip\\\"}]\"}". Do not invent capabilities like host.folder.mkdir — all folder creation goes inside plan_json as {\"kind\":\"mkdir\",\"path\":\"Name\"}. Emit a tool call only.`

// havenHostPlanApplyActNowNudge is sent when host.organize.plan already returned a plan_id but the
// model answered with prose instead of calling host.plan.apply (the only step that enqueues Loopgate approval).
const havenHostPlanApplyActNowNudge = "host.organize.plan already returned a plan_id in the tool results above. Loopgate does not open an approval prompt until you call host.plan.apply with that plan_id. The operator already confirmed in Messenger — call invoke_capability for host.plan.apply now using the plan_id from the organize.plan result. Do not tell the user approval is waiting until host.plan.apply returns pending_approval."

const defaultHavenToolResultMaxRunes = 20000

// Optional UX hints for Haven clients (e.g. sidebar follow-ups). Not security-sensitive.
const (
	havenUXSignalHostOrganizeApprovalPending = "host_organize_approval_pending"
	havenUXSignalHostOrganizeApplied         = "host_organize_applied"
)

var havenToolResultMaxRunesByCapability = map[string]int{
	"fs_read":                 16000,
	"fs_list":                 12000,
	"operator_mount.fs_read":  16000,
	"operator_mount.fs_list":  12000,
	"operator_mount.fs_write": 16000,
	"operator_mount.fs_mkdir": 8000,
	"shell_exec":              12000,
	"haven.operator_context":  12000,
	"host.folder.list":        4000,
	"host.folder.read":        16000,
	"host.organize.plan":      20000,
	"host.plan.apply":         20000,
}

type havenCapabilityDescriptor struct {
	DisplayName string
	RuntimeHint string
}

var havenCapabilityCatalog = map[string]havenCapabilityDescriptor{
	"fs_list":                 {DisplayName: "Browse Files", RuntimeHint: "browse folders and see what is in your Haven sandbox workspace"},
	"fs_read":                 {DisplayName: "Read Documents", RuntimeHint: "read files inside your Haven sandbox workspace"},
	"fs_write":                {DisplayName: "Save Work", RuntimeHint: "create and update files in your Haven sandbox workspace"},
	"fs_mkdir":                {DisplayName: "Create Folders", RuntimeHint: "create new folders in your Haven sandbox workspace"},
	"operator_mount.fs_list":  {DisplayName: "Granted host project", RuntimeHint: "list files under operator-granted host directories (paths relative to each grant root)"},
	"operator_mount.fs_read":  {DisplayName: "Granted host project", RuntimeHint: "read files under operator-granted host directories"},
	"operator_mount.fs_write": {DisplayName: "Granted host project", RuntimeHint: "write files under operator-granted host directories (may require approval)"},
	"operator_mount.fs_mkdir": {DisplayName: "Granted host project", RuntimeHint: "create directories under operator-granted host paths (may require approval)"},
	"journal.list":            {DisplayName: "Journal", RuntimeHint: "review your private journal entries"},
	"journal.read":            {DisplayName: "Journal", RuntimeHint: "read a private journal entry"},
	"journal.write":           {DisplayName: "Journal", RuntimeHint: "write a private journal entry when the user asks for reflection or journaling"},
	"haven.operator_context":  {DisplayName: "Operator guide", RuntimeHint: "fetch authoritative Haven harness documentation for troubleshooting"},
	"notes.list":              {DisplayName: "Notes", RuntimeHint: "review your private working notes"},
	"notes.read":              {DisplayName: "Notes", RuntimeHint: "read a working note from your notebook"},
	"notes.write":             {DisplayName: "Notes", RuntimeHint: "save a working note for plans, scratch work, or research"},
	"memory.remember":         {DisplayName: "Remember Things", RuntimeHint: "propose short structured continuity (preferences, routines, profile, goals); Loopgate accepts or rejects; do not invent facts or store secrets"},
	"paint.list":              {DisplayName: "Paint", RuntimeHint: "review the paintings in your gallery"},
	"paint.save":              {DisplayName: "Paint", RuntimeHint: "create a painting from explicit strokes and save it to your gallery"},
	"note.create":             {DisplayName: "Sticky Notes", RuntimeHint: "leave a sticky note on the desktop for the user"},
	"desktop.organize":        {DisplayName: "Desktop Layout", RuntimeHint: "rearrange the desktop icons to tidy up Haven"},
	"todo.add":                {DisplayName: "Task Board", RuntimeHint: "add a task when the user wants a reminder or explicitly asks to track something across sessions"},
	"todo.complete":           {DisplayName: "Task Board", RuntimeHint: "mark a task as done when it no longer needs attention"},
	"todo.list":               {DisplayName: "Task Board", RuntimeHint: "review your open tasks and active goals"},
	"goal.set":                {DisplayName: "Goals", RuntimeHint: "set a named persistent goal for ongoing work or a multi-session objective the user wants to track"},
	"goal.close":              {DisplayName: "Goals", RuntimeHint: "close a goal when the objective has been achieved or the user no longer wants to track it"},
	"shell_exec":              {DisplayName: "Terminal Commands", RuntimeHint: "run terminal commands when a task genuinely requires the command line"},
	"host.folder.list":        {DisplayName: "Granted host folders", RuntimeHint: "list files in a user-granted folder on the real host filesystem"},
	"host.folder.read":        {DisplayName: "Granted host folders", RuntimeHint: "read a file under a granted host folder on disk"},
	"host.organize.plan":      {DisplayName: "Granted host folders", RuntimeHint: "draft a move or mkdir plan for a granted folder (no host writes until apply)"},
	"host.plan.apply":         {DisplayName: "Granted host folders", RuntimeHint: "execute an approved organization plan on the real host filesystem"},
	"invoke_capability":       {DisplayName: "Capability Dispatcher", RuntimeHint: "dispatch a single allowed Haven capability"},
}
