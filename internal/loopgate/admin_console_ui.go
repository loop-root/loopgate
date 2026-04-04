package loopgate

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"

	"morph/internal/config"
)

// adminField is one labeled value row with optional hover help (?).
type adminField struct {
	Label string
	Value string
	Tip   string
	// Widget: "text" | "bool" | "list" | "code" — affects presentation only (all read-only).
	Widget string
}

type adminSection struct {
	Title    string
	TitleTip string
	Fields   []adminField
}

type adminLayoutData struct {
	Title         string
	ActiveNav     string
	Body          template.HTML
	TenantSummary string
}

type adminPolicyPageData struct {
	Sections                   []adminSection
	SubordinateClassPolicyPath string
	SubordinateClassPolicyYAML string
}

type adminAuditTypeOption struct {
	Value    string
	Label    string
	Selected bool
}

type adminAuditPageData struct {
	TypeOptions   []adminAuditTypeOption
	TypeCustom    string
	UserID        string
	SelectedLimit int
	LimitOptions  []int
	CSVLink       string
	RowCount      int
	EventRows     []adminAuditRow
}

type adminUsersPageData struct {
	Rows []adminUserRow
}

// adminDashboardPageData powers the Dashboard page (live state + bounded audit tail).
type adminDashboardPageData struct {
	AdminListenAddr    string
	PolicyVersion      string
	ActiveSessions     int
	DistinctUsers      int
	PendingApprovals   int
	ActiveSubordinates int
	SubordinateMax     int
	LiveConnections    int
	ModelConnections   int
	CapabilityTokens   int
	AuditLedgerBytes   string
	AuditLedgerNote    string
	Goroutines         int
	PolicyGatesOn      int
	PolicyGatesTotal   int
	ModelChatTurns     int
	InputTokensWindow  int64
	OutputTokensWindow int64
	TotalTokensWindow  int64
	AuditWindowEvents  int
	AuditScanCap       int
}

type adminAuditRow struct {
	TS          string
	TypeDisplay string
	Session     string
	DataJSON    string
}

func adminBoolLabel(on bool) string {
	if on {
		return "On"
	}
	return "Off"
}

func adminJoinLines(lines []string) string {
	if len(lines) == 0 {
		return "—"
	}
	return strings.Join(lines, "\n")
}

func buildAdminPolicySections(pol config.Policy) []adminSection {
	return []adminSection{
		{
			Title:    "Overview",
			TitleTip: "High-level policy version loaded from core/policy/policy.yaml and enforced by Loopgate.",
			Fields: []adminField{
				{Label: "Policy version", Value: pol.Version, Tip: "Semantic version string from the YAML file.", Widget: "text"},
			},
		},
		{
			Title:    "Filesystem tools",
			TitleTip: "Governs sandboxed file read/write for tool capabilities. Paths are validated against allowed roots.",
			Fields: []adminField{
				{Label: "Read enabled", Value: adminBoolLabel(pol.Tools.Filesystem.ReadEnabled), Tip: "When on, read-style capabilities may access files under allowed roots.", Widget: "bool"},
				{Label: "Write enabled", Value: adminBoolLabel(pol.Tools.Filesystem.WriteEnabled), Tip: "When on, write-style capabilities may modify files under policy.", Widget: "bool"},
				{Label: "Write requires approval", Value: adminBoolLabel(pol.Tools.Filesystem.WriteRequiresApproval), Tip: "If on, mutating writes route through the approval flow when policy demands it.", Widget: "bool"},
				{Label: "Allowed roots", Value: adminJoinLines(pol.Tools.Filesystem.AllowedRoots), Tip: "Virtual or repo-relative roots tools may access. Empty with reads/writes enabled is invalid at load time.", Widget: "list"},
				{Label: "Denied paths", Value: adminJoinLines(pol.Tools.Filesystem.DeniedPaths), Tip: "Explicit path prefixes blocked even inside allowed roots.", Widget: "list"},
			},
		},
		{
			Title:    "HTTP tools",
			TitleTip: "Outbound HTTP from governed tools: domains, timeouts, and approval posture.",
			Fields: []adminField{
				{Label: "Enabled", Value: adminBoolLabel(pol.Tools.HTTP.Enabled), Tip: "Master switch for HTTP tool capabilities.", Widget: "bool"},
				{Label: "Requires approval", Value: adminBoolLabel(pol.Tools.HTTP.RequiresApproval), Tip: "When on, HTTP calls may require operator approval per policy class.", Widget: "bool"},
				{Label: "Timeout (seconds)", Value: fmt.Sprintf("%d", pol.Tools.HTTP.TimeoutSeconds), Tip: "Upper bound on outbound HTTP request time.", Widget: "text"},
				{Label: "Allowed domains", Value: adminJoinLines(pol.Tools.HTTP.AllowedDomains), Tip: "Host allowlist for tool HTTP. Empty usually means no outbound calls.", Widget: "list"},
			},
		},
		{
			Title:    "Shell tools",
			TitleTip: "If enabled, shell execution is limited to an explicit command allowlist.",
			Fields: []adminField{
				{Label: "Enabled", Value: adminBoolLabel(pol.Tools.Shell.Enabled), Tip: "Whether shell-style capabilities are permitted at all.", Widget: "bool"},
				{Label: "Requires approval", Value: adminBoolLabel(pol.Tools.Shell.RequiresApproval), Tip: "Shell runs may require explicit approval when true.", Widget: "bool"},
				{Label: "Allowed commands", Value: adminJoinLines(pol.Tools.Shell.AllowedCommands), Tip: "Exact command names or paths permitted for shell tools.", Widget: "list"},
			},
		},
		{
			Title:    "Subordinate runtimes",
			TitleTip: "Bounded delegated execution contexts: spawn limits and class template requirements. Authoritative fields live under the delegated-runtime section of capability policy YAML.",
			Fields: []adminField{
				{Label: "Spawn enabled", Value: adminBoolLabel(pol.Tools.Morphlings.SpawnEnabled), Tip: "Whether new subordinate runtimes may be created under policy.", Widget: "bool"},
				{Label: "Max active", Value: fmt.Sprintf("%d", pol.Tools.Morphlings.MaxActive), Tip: "Upper bound on concurrently active subordinate runtimes.", Widget: "text"},
				{Label: "Require template", Value: adminBoolLabel(pol.Tools.Morphlings.RequireTemplate), Tip: "Spawn requests must reference an approved class template.", Widget: "bool"},
			},
		},
		{
			Title:    "Logging (policy)",
			TitleTip: "Policy-side switches for command, tool, and memory promotion logging (distinct from audit ledger).",
			Fields: []adminField{
				{Label: "Log commands", Value: adminBoolLabel(pol.Logging.LogCommands), Tip: "Record command-style events when policy allows.", Widget: "bool"},
				{Label: "Log tool calls", Value: adminBoolLabel(pol.Logging.LogToolCalls), Tip: "Record tool invocations for operator visibility.", Widget: "bool"},
				{Label: "Log memory promotions", Value: adminBoolLabel(pol.Logging.LogMemoryPromotions), Tip: "Record memory promotion decisions.", Widget: "bool"},
			},
		},
		{
			Title:    "Memory",
			TitleTip: "Distillation and continuity review gates for governed memory.",
			Fields: []adminField{
				{Label: "Auto distillate", Value: adminBoolLabel(pol.Memory.AutoDistillate), Tip: "Automatic distillation of memory artifacts when enabled.", Widget: "bool"},
				{Label: "Require promotion approval", Value: adminBoolLabel(pol.Memory.RequirePromotionApproval), Tip: "Promotions need explicit approval.", Widget: "bool"},
				{Label: "Continuity review required", Value: adminBoolLabel(pol.Memory.ContinuityReviewRequired), Tip: "Continuity mutations may require review.", Widget: "bool"},
				{Label: "Submit previous min events", Value: fmt.Sprintf("%d", pol.Memory.SubmitPreviousMinEvents), Tip: "Minimum prior events included when submitting continuity context.", Widget: "text"},
				{Label: "Submit previous min payload bytes", Value: fmt.Sprintf("%d", pol.Memory.SubmitPreviousMinPayloadBytes), Tip: "Minimum payload size heuristic for prior context.", Widget: "text"},
				{Label: "Submit previous min prompt tokens", Value: fmt.Sprintf("%d", pol.Memory.SubmitPreviousMinPromptTokens), Tip: "Token floor for prior prompt material.", Widget: "text"},
			},
		},
		{
			Title:    "Safety",
			TitleTip: "Hard guards on who may change persona or policy through governed paths.",
			Fields: []adminField{
				{Label: "Allow persona modification", Value: adminBoolLabel(pol.Safety.AllowPersonaModification), Tip: "Whether persona edits are permitted through controlled APIs.", Widget: "bool"},
				{Label: "Allow policy modification", Value: adminBoolLabel(pol.Safety.AllowPolicyModification), Tip: "Whether policy mutation is permitted (rare; usually operator-only).", Widget: "bool"},
			},
		},
	}
}

var memoryBackendChoices = []string{"continuity_tcl", "rag_baseline", "hybrid"}

func buildAdminRuntimeSections(cfg config.RuntimeConfig) []adminSection {
	diagLevel := cfg.Logging.Diagnostic.DefaultLevel
	if strings.TrimSpace(diagLevel) == "" {
		diagLevel = "(default)"
	}
	verifySeg := "true"
	if cfg.Logging.AuditLedger.VerifyClosedSegmentsOnStartup != nil && !*cfg.Logging.AuditLedger.VerifyClosedSegmentsOnStartup {
		verifySeg = "false"
	}
	return []adminSection{
		{
			Title:    "Deployment",
			TitleTip: "Values from config/runtime.yaml. Tenant and user IDs are stamped onto control sessions at open — never from client JSON.",
			Fields: []adminField{
				{Label: "Config version", Value: cfg.Version, Tip: "Runtime config schema version string.", Widget: "text"},
				{Label: "Deployment tenant ID", Value: nullish(cfg.Tenancy.DeploymentTenantID), Tip: "Empty means personal/desktop mode. When set, audit and session views filter to this tenant.", Widget: "text"},
				{Label: "Deployment user ID", Value: nullish(cfg.Tenancy.DeploymentUserID), Tip: "Optional deployment-scoped user label applied at session open.", Widget: "text"},
			},
		},
		{
			Title:    "Admin console",
			TitleTip: "Loopback operator UI. TCP listens only when you start loopgate with --admin and set LOOPGATE_ADMIN_TOKEN.",
			Fields: []adminField{
				{Label: "Enabled in config", Value: adminBoolLabel(cfg.AdminConsole.Enabled), Tip: "Must be true together with the --admin flag for the TCP listener to open.", Widget: "bool"},
				{Label: "Listen address", Value: nullish(cfg.AdminConsole.ListenAddr), Tip: "Loopback bind (e.g. 127.0.0.1:9847). Non-loopback addresses are rejected.", Widget: "code"},
			},
		},
		{
			Title:    "Audit ledger",
			TitleTip: "Hash-chained JSONL settings for loopgate_events.jsonl and segment rotation.",
			Fields: []adminField{
				{Label: "Max event bytes", Value: fmt.Sprintf("%d", cfg.Logging.AuditLedger.MaxEventBytes), Tip: "Maximum serialized size per audit event.", Widget: "text"},
				{Label: "Rotate at bytes", Value: fmt.Sprintf("%d", cfg.Logging.AuditLedger.RotateAtBytes), Tip: "When the active segment exceeds this size, rotation occurs.", Widget: "text"},
				{Label: "Segment directory", Value: cfg.Logging.AuditLedger.SegmentDir, Tip: "Repo-relative directory for closed segments.", Widget: "code"},
				{Label: "Manifest path", Value: cfg.Logging.AuditLedger.ManifestPath, Tip: "Segment manifest JSONL location.", Widget: "code"},
				{Label: "Verify closed segments on startup", Value: verifySeg, Tip: "Integrity check for closed segments when Loopgate starts.", Widget: "text"},
			},
		},
		{
			Title:    "Diagnostic logging",
			TitleTip: "Optional slog text files under runtime/logs — not a substitute for the audit ledger.",
			Fields: []adminField{
				{Label: "Enabled", Value: adminBoolLabel(cfg.Logging.Diagnostic.Enabled), Tip: "Writes channel log files for local troubleshooting.", Widget: "bool"},
				{Label: "Default level", Value: diagLevel, Tip: "Baseline verbosity: error, warn, info, debug, trace.", Widget: "text"},
				{Label: "Directory", Value: cfg.Logging.Diagnostic.ResolvedDirectory(), Tip: "Resolved path relative to repo root.", Widget: "code"},
			},
		},
		{
			Title:    "Memory tuning",
			TitleTip: "Scoring and planner preferences for the active memory backend (see Memory backend card above).",
			Fields: []adminField{
				{Label: "Candidate panel size", Value: fmt.Sprintf("%d", cfg.Memory.CandidatePanelSize), Tip: "How many memory candidates the scorer surfaces.", Widget: "text"},
				{Label: "Soft subordinate concurrency", Value: fmt.Sprintf("%d", cfg.Memory.SoftMorphlingConcurrency), Tip: "Concurrency hint for soft delegated work; see memory tuning in runtime YAML for the field name.", Widget: "text"},
				{Label: "Decomposition preference", Value: cfg.Memory.DecompositionPreference, Tip: "Planner decomposition mode string.", Widget: "code"},
				{Label: "Review preference", Value: cfg.Memory.ReviewPreference, Tip: "Review workflow preference string.", Widget: "code"},
				{Label: "Batching preference", Value: cfg.Memory.BatchingPreference, Tip: "How memory batches behave on failure.", Widget: "code"},
			},
		},
	}
}

func nullish(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// countPolicyGatesEnabled counts boolean enforcement switches in the loaded policy (read-only dashboard metric).
func countPolicyGatesEnabled(pol config.Policy) (enabled, total int) {
	bools := []bool{
		pol.Tools.Filesystem.ReadEnabled,
		pol.Tools.Filesystem.WriteEnabled,
		pol.Tools.Filesystem.WriteRequiresApproval,
		pol.Tools.HTTP.Enabled,
		pol.Tools.HTTP.RequiresApproval,
		pol.Tools.Shell.Enabled,
		pol.Tools.Shell.RequiresApproval,
		pol.Tools.Morphlings.SpawnEnabled,
		pol.Tools.Morphlings.RequireTemplate,
		pol.Logging.LogCommands,
		pol.Logging.LogToolCalls,
		pol.Logging.LogMemoryPromotions,
		pol.Memory.AutoDistillate,
		pol.Memory.RequirePromotionApproval,
		pol.Memory.ContinuityReviewRequired,
		pol.Safety.AllowPersonaModification,
		pol.Safety.AllowPolicyModification,
	}
	total = len(bools)
	for _, on := range bools {
		if on {
			enabled++
		}
	}
	return enabled, total
}

// adminMemoryBackendOptions returns backend names for a read-only select; current marked selected in template.
func adminMemoryBackendOptions(current string) []adminSelectOption {
	current = strings.TrimSpace(current)
	inList := false
	for _, choice := range memoryBackendChoices {
		if choice == current {
			inList = true
		}
	}
	var out []adminSelectOption
	if current != "" && !inList {
		out = append(out, adminSelectOption{
			Value:    current,
			Label:    current + " (custom)",
			Selected: true,
		})
	}
	for _, choice := range memoryBackendChoices {
		out = append(out, adminSelectOption{
			Value:    choice,
			Label:    choice,
			Selected: inList && choice == current,
		})
	}
	return out
}

type adminSelectOption struct {
	Value    string
	Label    string
	Selected bool
}

type adminRuntimePageData struct {
	Sections       []adminSection
	BackendOptions []adminSelectOption
	CurrentBackend string
}

var (
	adminAppLayout      *template.Template
	adminPolicyContent  *template.Template
	adminRuntimeContent *template.Template
	adminAuditContent   *template.Template
	adminHomeContent    *template.Template
	adminUsersContent   *template.Template
	adminLoginStyled    *template.Template
)

func init() {
	const adminCSS = `:root{--bg:#f5f5f7;--surface:#fff;--text:#1d1d1f;--text2:#6e6e73;--line:rgba(0,0,0,.08);--accent:#0071e3;--accent-soft:rgba(0,113,227,.08);--radius:12px;--shadow:0 2px 12px rgba(0,0,0,.06);--font:-apple-system,BlinkMacSystemFont,"SF Pro Text","Segoe UI",system-ui,sans-serif}
*{box-sizing:border-box}
body.admin-app{margin:0;font-family:var(--font);background:var(--bg);color:var(--text);min-height:100vh;display:flex;-webkit-font-smoothing:antialiased}
aside.sidebar{width:240px;flex-shrink:0;background:var(--surface);border-right:1px solid var(--line);padding:28px 20px;position:sticky;top:0;align-self:flex-start;height:100vh}
.brand{font-size:13px;font-weight:600;letter-spacing:.04em;text-transform:uppercase;color:var(--text2);margin-bottom:8px}
.product{font-size:20px;font-weight:600;letter-spacing:-.02em;margin-bottom:28px}
nav.side a{display:block;padding:10px 12px;margin:2px 0;border-radius:8px;font-size:14px;color:var(--text);text-decoration:none}
nav.side a:hover{background:var(--bg)}
nav.side a.active{background:var(--accent-soft);color:var(--accent);font-weight:500}
.tenant-pill{margin-top:auto;padding-top:24px;font-size:12px;color:var(--text2);line-height:1.4;border-top:1px solid var(--line);margin-top:32px;padding-top:16px}
main.main{flex:1;padding:36px 48px;max-width:1240px;min-width:0}
.page-head{margin-bottom:28px}
.page-head h1{font-size:28px;font-weight:600;letter-spacing:-.03em;margin:0 0 6px}
.page-head p{margin:0;font-size:14px;color:var(--text2);max-width:52ch;line-height:1.5}
.card{background:var(--surface);border-radius:var(--radius);box-shadow:var(--shadow);border:1px solid var(--line);margin-bottom:20px;overflow:visible}
.card.card--contain{overflow:hidden}
.card-h{padding:16px 20px;border-bottom:1px solid var(--line);display:flex;align-items:center;gap:10px;background:linear-gradient(180deg,rgba(255,255,255,.9),var(--surface))}
.card-h h2{margin:0;font-size:15px;font-weight:600;letter-spacing:-.01em}
.card-b{padding:8px 0}
.field-row{display:grid;grid-template-columns:minmax(200px,280px) 1fr;gap:16px 24px;padding:14px 20px;border-bottom:1px solid var(--line);align-items:start}
.field-row:last-child{border-bottom:none}
.field-label{display:flex;align-items:center;gap:8px;font-size:13px;font-weight:500;color:var(--text2)}
.help{display:inline-flex;align-items:center;justify-content:center;width:18px;height:18px;border-radius:50%;background:#e8e8ed;color:#424245;font-size:11px;font-weight:700;cursor:help;flex-shrink:0;border:none;padding:0;line-height:1;position:relative;z-index:20}
.help:focus{outline:2px solid var(--accent);outline-offset:2px}
.help::after{content:attr(data-tip);position:absolute;left:0;bottom:calc(100% + 10px);transform:none;width:min(300px,max(220px,calc(100vw - 40px)));max-width:min(300px,calc(100vw - 24px));padding:12px 14px;background:#1d1d1f;color:#f5f5f7;font-size:12px;font-weight:400;line-height:1.45;border-radius:10px;white-space:normal;text-align:left;opacity:0;pointer-events:none;transition:opacity .18s ease;z-index:100;box-shadow:0 8px 24px rgba(0,0,0,.25)}
.help:hover::after,.help:focus-visible::after{opacity:1}
.field-value{font-size:15px;color:var(--text);word-break:break-word}
.field-widget-bool .pill{display:inline-block;padding:4px 12px;border-radius:100px;font-size:12px;font-weight:600;letter-spacing:.02em}
.pill-on{background:#e8f7ec;color:#1b5e20}
.pill-off{background:#f0f0f2;color:var(--text2)}
.field-widget-list{white-space:pre-wrap;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px;background:var(--bg);padding:12px 14px;border-radius:8px;border:1px solid var(--line);max-height:200px;overflow:auto}
.field-widget-code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px;background:var(--bg);padding:10px 12px;border-radius:8px;border:1px solid var(--line)}
select.fake-select, input.fake-input{font:inherit;font-size:14px;padding:10px 12px;border-radius:8px;border:1px solid var(--line);background:var(--bg);color:var(--text2);width:100%;max-width:420px;cursor:not-allowed}
.form-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(220px,1fr));gap:16px 20px;margin-bottom:20px;align-items:end}
.form-field label{display:flex;align-items:center;gap:8px;font-size:12px;font-weight:600;color:var(--text2);text-transform:uppercase;letter-spacing:.04em;margin-bottom:8px}
.form-field select,.form-field input{font:inherit;width:100%;padding:10px 12px;border-radius:8px;border:1px solid var(--line);background:var(--surface)}
.btn-row{display:flex;gap:12px;flex-wrap:wrap;align-items:center;margin-bottom:20px}
.btn{font:inherit;font-size:14px;font-weight:500;padding:10px 20px;border-radius:980px;border:none;cursor:pointer;background:var(--accent);color:#fff}
.btn:hover{filter:brightness(1.05)}
.btn.secondary{background:var(--surface);color:var(--accent);border:1px solid var(--line)}
.btn.secondary:hover{background:var(--bg)}
.muted{font-size:13px;color:var(--text2)}
.dash-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:14px;margin-bottom:28px}
.dash-tile{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:18px 20px;box-shadow:var(--shadow)}
.dash-tile .k{font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:.06em;color:var(--text2);margin:0 0 6px;display:flex;align-items:center;gap:6px}
.dash-tile .v{font-size:26px;font-weight:600;letter-spacing:-.03em;margin:0;line-height:1.15}
.dash-tile .sub{font-size:12px;color:var(--text2);margin:8px 0 0;line-height:1.4}
.dash-section{margin:8px 0 14px;font-size:13px;font-weight:600;color:var(--text2);letter-spacing:.02em}
.quick-links{display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:12px;margin-top:8px}
.quick-links a.card{text-decoration:none;color:inherit;padding:18px 20px}
.audit-scroll{margin:0;overflow-x:auto;-webkit-overflow-scrolling:touch;max-width:100%}
.audit-table{width:100%;border-collapse:collapse;font-size:13px;min-width:720px}
.audit-table th{text-align:left;padding:10px 12px;border-bottom:1px solid var(--line);color:var(--text2);font-weight:600;font-size:11px;text-transform:uppercase;letter-spacing:.05em;white-space:nowrap}
.audit-table td{padding:12px;border-bottom:1px solid var(--line);vertical-align:top}
.audit-table td.audit-ts{white-space:nowrap}
.audit-table tr:hover td{background:rgba(0,0,0,.015)}
details.raw-json{margin-top:8px}
details.raw-json summary{cursor:pointer;font-size:12px;color:var(--accent);user-select:none}
details.raw-json pre{margin:8px 0 0;font-size:11px;background:var(--bg);padding:12px;border-radius:8px;overflow:auto;max-height:280px;border:1px solid var(--line);white-space:pre-wrap;word-break:break-word;max-width:min(960px,92vw)}
.login-page{min-height:100vh;display:flex;align-items:center;justify-content:center;background:var(--bg);font-family:var(--font)}
.login-card{background:var(--surface);padding:40px 44px;border-radius:var(--radius);box-shadow:var(--shadow);border:1px solid var(--line);width:100%;max-width:380px}
.login-card h1{margin:0 0 8px;font-size:24px;font-weight:600;letter-spacing:-.02em}
.login-card .sub{margin:0 0 28px;font-size:14px;color:var(--text2);line-height:1.5}
.login-card label{display:flex;align-items:center;gap:8px;font-size:13px;font-weight:500;color:var(--text2);margin-bottom:8px}
.login-card input{width:100%;padding:12px 14px;font:inherit;border-radius:8px;border:1px solid var(--line);margin-bottom:20px}
.login-card button{width:100%;padding:12px;font:inherit;font-weight:600;border-radius:980px;border:none;background:var(--accent);color:#fff;cursor:pointer}
.login-card .err{color:#c62828;font-size:14px;margin-bottom:16px}
.users-table{width:100%;border-collapse:collapse;font-size:13px}
.users-table th{text-align:left;padding:10px 12px;border-bottom:1px solid var(--line);font-size:11px;text-transform:uppercase;letter-spacing:.05em;color:var(--text2)}
.users-table td{padding:12px;border-bottom:1px solid var(--line);vertical-align:top}
.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}
`

	master := template.New("admin").Funcs(template.FuncMap{
		"adminCSS": func() template.CSS { return template.CSS(adminCSS) },
	})

	adminAppLayout = template.Must(master.Parse(`
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · Loopgate</title>
<style>{{adminCSS}}</style>
</head>
<body class="admin-app">
<aside class="sidebar">
<div class="brand">Control plane</div>
<div class="product">Loopgate</div>
<nav class="side">
<a href="/admin/" {{if eq .ActiveNav "home"}}class="active"{{end}}>Dashboard</a>
<a href="/admin/policy" {{if eq .ActiveNav "policy"}}class="active"{{end}}>Capability policy</a>
<a href="/admin/config" {{if eq .ActiveNav "config"}}class="active"{{end}}>Configuration</a>
<a href="/admin/audit" {{if eq .ActiveNav "audit"}}class="active"{{end}}>Audit log</a>
<a href="/admin/users" {{if eq .ActiveNav "users"}}class="active"{{end}}>Sessions</a>
</nav>
<div class="tenant-pill"><strong>Tenant scope</strong><br>{{.TenantSummary}}</div>
</aside>
<main class="main">
{{.Body}}
</main>
</body>
</html>`))

	adminPolicyContent = template.Must(template.New("policy").Parse(`
<div class="page-head">
<h1>Capability policy</h1>
<p>Authoritative tool, memory, and safety settings loaded from <span class="mono">core/policy/policy.yaml</span>. Read-only here — edit the file and restart Loopgate (or use your governed config workflow).</p>
</div>
{{range .Sections}}
<section class="card">
<div class="card-h">
<h2>{{.Title}}</h2>
<span class="help" tabindex="0" role="button" aria-label="{{.TitleTip}}" data-tip="{{.TitleTip}}">?</span>
</div>
<div class="card-b">
{{range .Fields}}
<div class="field-row">
<div class="field-label">
<span>{{.Label}}</span>
<span class="help" tabindex="0" role="button" aria-label="{{.Tip}}" data-tip="{{.Tip}}">?</span>
</div>
<div class="field-value field-widget-{{.Widget}}">{{if eq .Widget "bool"}}{{if eq .Value "On"}}<span class="pill pill-on">On</span>{{else}}<span class="pill pill-off">Off</span>{{end}}{{else}}{{.Value}}{{end}}</div>
</div>
{{end}}
</div>
</section>
{{end}}
<section class="card">
<div class="card-h"><h2>Subordinate class policy</h2><span class="help" tabindex="0" role="button" aria-label="YAML class definitions merged with registry defaults at load. Filename on disk is historical." data-tip="YAML class definitions merged with registry defaults at load. On-disk filename is historical; edit the file shown below to change classes.">?</span></div>
<div class="card-b" style="padding:20px">
<p class="muted" style="margin:0 0 12px">Source file: <span class="mono">{{.SubordinateClassPolicyPath}}</span></p>
<details class="raw-json" open><summary>Source YAML</summary><pre>{{.SubordinateClassPolicyYAML}}</pre></details>
</div>
</section>
<form method="post" action="/admin/logout" style="margin-top:32px"><button type="submit" class="btn secondary">Sign out</button></form>
`))

	adminRuntimeContent = template.Must(template.New("runtime").Parse(`
<div class="page-head">
<h1>Configuration</h1>
<p>Live values from <span class="mono">config/runtime.yaml</span> (plus defaults). Changes require editing that file and restarting Loopgate — this console does not write config yet.</p>
</div>
<section class="card">
<div class="card-h"><h2>Memory backend</h2><span class="help" tabindex="0" role="button" aria-label="Which continuity backend is active. Switches affect recall, scoring, and storage layout." data-tip="Which continuity backend is active. Switches affect recall, scoring, and storage layout.">?</span></div>
<div class="card-b" style="padding:20px">
<label class="field-label" style="padding:0 20px 8px;display:flex"><span>Active backend</span><span class="help" tabindex="0" data-tip="Read-only view of the running backend. Edit memory.backend in config/runtime.yaml to change.">?</span></label>
<select class="fake-select" disabled aria-readonly="true">{{range .BackendOptions}}<option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Label}}</option>{{end}}</select>
<p class="muted" style="margin:12px 20px 0">Running: <strong class="mono">{{.CurrentBackend}}</strong></p>
</div>
</section>
{{range .Sections}}
<section class="card">
<div class="card-h"><h2>{{.Title}}</h2><span class="help" tabindex="0" role="button" aria-label="{{.TitleTip}}" data-tip="{{.TitleTip}}">?</span></div>
<div class="card-b">
{{range .Fields}}
<div class="field-row">
<div class="field-label"><span>{{.Label}}</span><span class="help" tabindex="0" role="button" aria-label="{{.Tip}}" data-tip="{{.Tip}}">?</span></div>
<div class="field-value field-widget-{{.Widget}}">{{if eq .Widget "bool"}}{{if eq .Value "On"}}<span class="pill pill-on">On</span>{{else}}<span class="pill pill-off">Off</span>{{end}}{{else}}{{.Value}}{{end}}</div>
</div>
{{end}}
</div>
</section>
{{end}}
<form method="post" action="/admin/logout" style="margin-top:32px"><button type="submit" class="btn secondary">Sign out</button></form>
`))

	adminAuditContent = template.Must(template.New("audit").Parse(`
<div class="page-head">
<h1>Audit log</h1>
<p>Recent hash-chained events from <span class="mono">loopgate_events.jsonl</span>. Payloads are redacted for display. Export CSV for analysis.</p>
</div>
<form class="card" method="get" action="/admin/audit" style="padding:24px;margin-bottom:24px">
<div class="form-grid">
<div class="form-field">
<label>Event type <span class="help" tabindex="0" data-tip="Filter to one event type string (exact match). Choose a preset or type your own.">?</span></label>
<select name="type_preset" id="type_preset">{{range .TypeOptions}}<option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Label}}</option>{{end}}</select>
</div>
<div class="form-field">
<label>Custom type <span class="help" tabindex="0" data-tip="When set, overrides the preset dropdown (exact match).">?</span></label>
<input type="text" name="type_custom" value="{{.TypeCustom}}" placeholder="e.g. capability.executed" autocomplete="off">
</div>
<div class="form-field">
<label>User ID <span class="help" tabindex="0" data-tip="Filter events whose data.user_id matches this deployment-scoped user label.">?</span></label>
<input type="text" name="user_id" value="{{.UserID}}" placeholder="Optional" autocomplete="off">
</div>
<div class="form-field">
<label>Row limit <span class="help" tabindex="0" data-tip="Maximum rows loaded from the tail of the ledger (newest first within the scan window).">?</span></label>
<select name="limit">{{range .LimitOptions}}<option value="{{.}}" {{if eq $.SelectedLimit .}}selected{{end}}>{{.}} rows</option>{{end}}</select>
</div>
</div>
<div class="btn-row">
<button type="submit" class="btn">Apply filters</button>
<a class="btn secondary" href="{{.CSVLink}}" style="text-decoration:none;display:inline-block;line-height:normal">Download CSV</a>
<span class="muted">Showing {{.RowCount}} events</span>
</div>
</form>
<section class="card card--contain" style="padding:0">
<div class="audit-scroll">
<table class="audit-table">
<thead><tr><th>Time</th><th>Type</th><th>Session</th><th>Details</th></tr></thead>
<tbody>
{{range .EventRows}}
<tr>
<td class="mono audit-ts">{{.TS}}</td>
<td>{{.TypeDisplay}}</td>
<td class="mono">{{.Session}}</td>
<td>
<details class="raw-json"><summary>Redacted JSON</summary><pre>{{.DataJSON}}</pre></details>
</td>
</tr>
{{else}}
<tr><td colspan="4" class="muted" style="padding:24px">No events match these filters.</td></tr>
{{end}}
</tbody>
</table>
</div>
</section>
<form method="post" action="/admin/logout" style="margin-top:32px"><button type="submit" class="btn secondary">Sign out</button></form>
`))

	adminHomeContent = template.Must(template.New("home").Parse(`
<div class="page-head">
<h1>Dashboard</h1>
<p>Live snapshot of this Loopgate process and the latest slice of the audit ledger. Numbers refresh on each page load; nothing here mutates policy or config.</p>
<p class="muted" style="margin-top:10px;font-size:13px">Admin console: <span class="mono">{{.AdminListenAddr}}</span> · Policy <span class="mono">{{.PolicyVersion}}</span></p>
</div>

<p class="dash-section">What is running</p>
<div class="dash-grid">
<div class="dash-tile">
<p class="k">Active control sessions <span class="help" tabindex="0" data-tip="Open control-plane sessions in this process, after tenant filter. Each session is a connected client (IDE, MCP host, desktop shell, etc.).">?</span></p>
<p class="v">{{.ActiveSessions}}</p>
</div>
<div class="dash-tile">
<p class="k">Distinct deployment users <span class="help" tabindex="0" data-tip="Unique non-empty user_id values on active sessions (from tenancy at session open), tenant-scoped.">?</span></p>
<p class="v">{{.DistinctUsers}}</p>
</div>
<div class="dash-tile">
<p class="k">Pending approvals <span class="help" tabindex="0" data-tip="Operator decisions waiting in the approval queue.">?</span></p>
<p class="v">{{.PendingApprovals}}</p>
</div>
<div class="dash-tile">
<p class="k">Active subordinate runtimes <span class="help" tabindex="0" data-tip="Delegated execution contexts currently consuming capacity vs the configured maximum active (spawn limits in capability policy).">?</span></p>
<p class="v">{{.ActiveSubordinates}}<span style="font-size:16px;font-weight:500;color:var(--text2)"> / {{.SubordinateMax}}</span></p>
</div>
<div class="dash-tile">
<p class="k">Live tool connections <span class="help" tabindex="0" data-tip="Tracked integration / tool connection records held in memory.">?</span></p>
<p class="v">{{.LiveConnections}}</p>
</div>
<div class="dash-tile">
<p class="k">Model API connections <span class="help" tabindex="0" data-tip="Configured model client connections in this process.">?</span></p>
<p class="v">{{.ModelConnections}}</p>
</div>
<div class="dash-tile">
<p class="k">Capability tokens (memory) <span class="help" tabindex="0" data-tip="Unexpired capability tokens currently held server-side (not secret values).">?</span></p>
<p class="v">{{.CapabilityTokens}}</p>
</div>
</div>

<p class="dash-section">Input / output / tokens <span class="help" tabindex="0" data-tip="Summed from completed model-chat audit events in the trailing window only. Other code paths may not emit input_tokens/output_tokens.">?</span></p>
<div class="dash-grid">
<div class="dash-tile">
<p class="k">Model chat turns (window)</p>
<p class="v">{{.ModelChatTurns}}</p>
<p class="sub">Completed governed model chat events in the rolling tail ({{.AuditWindowEvents}} events scanned, cap {{.AuditScanCap}}). Failed turns may omit token fields.</p>
</div>
<div class="dash-tile">
<p class="k">Input tokens (window)</p>
<p class="v">{{.InputTokensWindow}}</p>
</div>
<div class="dash-tile">
<p class="k">Output tokens (window)</p>
<p class="v">{{.OutputTokensWindow}}</p>
</div>
<div class="dash-tile">
<p class="k">Total tokens (window)</p>
<p class="v">{{.TotalTokensWindow}}</p>
</div>
</div>

<p class="dash-section">Resources & policy surface</p>
<div class="dash-grid">
<div class="dash-tile">
<p class="k">Go routines <span class="help" tabindex="0" data-tip="Runtime goroutine count for this process (rough load indicator).">?</span></p>
<p class="v">{{.Goroutines}}</p>
</div>
<div class="dash-tile">
<p class="k">Audit ledger file <span class="help" tabindex="0" data-tip="Size of the primary JSONL ledger on disk for this repo (not segment archives).">?</span></p>
<p class="v" style="font-size:20px">{{.AuditLedgerBytes}}</p>
<p class="sub">{{.AuditLedgerNote}}</p>
</div>
<div class="dash-tile">
<p class="k">Policy gates enabled <span class="help" tabindex="0" data-tip="Count of boolean tool, logging, memory, and safety switches set to true in the loaded policy YAML.">?</span></p>
<p class="v">{{.PolicyGatesOn}}<span style="font-size:16px;font-weight:500;color:var(--text2)"> / {{.PolicyGatesTotal}}</span></p>
</div>
</div>

<p class="dash-section">Shortcuts</p>
<div class="quick-links">
<a href="/admin/policy" class="card"><h2 style="margin:0 0 6px;font-size:16px">Capability policy</h2><p class="muted" style="margin:0;font-size:13px">Filesystem, HTTP, shell, subordinate runtimes, memory, safety.</p></a>
<a href="/admin/config" class="card"><h2 style="margin:0 0 6px;font-size:16px">Configuration</h2><p class="muted" style="margin:0;font-size:13px">Runtime YAML, memory backend, ledger paths.</p></a>
<a href="/admin/audit" class="card"><h2 style="margin:0 0 6px;font-size:16px">Audit log</h2><p class="muted" style="margin:0;font-size:13px">Filter, redacted JSON, CSV export.</p></a>
<a href="/admin/users" class="card"><h2 style="margin:0 0 6px;font-size:16px">Sessions</h2><p class="muted" style="margin:0;font-size:13px">Session table and identity columns.</p></a>
</div>
<form method="post" action="/admin/logout" style="margin-top:36px"><button type="submit" class="btn secondary">Sign out</button></form>
`))

	adminUsersContent = template.Must(template.New("users").Parse(`
<div class="page-head">
<h1>Control sessions</h1>
<p>Active sessions accepted by Loopgate. MAC keys and tokens are never shown here.</p>
</div>
<section class="card card--contain" style="padding:0;overflow:auto">
<table class="users-table">
<thead><tr><th>Session</th><th>Actor</th><th>Client</th><th>Tenant</th><th>User</th><th>Peer UID</th><th>Created</th><th>Expires</th></tr></thead>
<tbody>
{{range .Rows}}
<tr>
<td class="mono">{{.SessionID}}</td>
<td>{{.ActorLabel}}</td>
<td>{{.ClientSession}}</td>
<td>{{.TenantID}}</td>
<td>{{.UserID}}</td>
<td>{{.PeerUID}}</td>
<td class="mono">{{.CreatedAtRFC3339}}</td>
<td class="mono">{{.ExpiresAtRFC3339}}</td>
</tr>
{{else}}
<tr><td colspan="8" class="muted" style="padding:24px">No active sessions in this tenant scope.</td></tr>
{{end}}
</tbody>
</table>
</section>
<form method="post" action="/admin/logout" style="margin-top:32px"><button type="submit" class="btn secondary">Sign out</button></form>
`))

	adminLoginStyled = template.Must(template.New("login").Funcs(template.FuncMap{
		"adminCSS": func() template.CSS { return template.CSS(adminCSS) },
	}).Parse(`
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sign in · Loopgate</title>
<style>{{adminCSS}}</style>
</head>
<body class="login-page">
<div class="login-card">
<h1>Loopgate</h1>
<p class="sub">Administrator access. Enter the token configured as <span class="mono">LOOPGATE_ADMIN_TOKEN</span> for this process.</p>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<form method="post" action="/admin/login">
<label for="token">Admin token <span class="help" tabindex="0" data-tip="Same high-entropy value as LOOPGATE_ADMIN_TOKEN (24+ characters). Never committed to git.">?</span></label>
<input id="token" name="token" type="password" autocomplete="current-password" required>
<button type="submit">Continue</button>
</form>
</div>
</body>
</html>`))
}

// adminAuditTypePresets are common event types for the audit filter dropdown (exact match on wire type).
var adminAuditTypePresets = []string{
	"session.opened",
	"capability.executed",
	"tool.success",
	"tool.denied",
	"memory.remember",
	adminAuditLedgerTypeModelChatDone,
	"morphling.spawned",
	"morphling.terminated",
}

// adminFriendlyAuditTypeLabel maps stable ledger type strings to operator-facing labels (product-agnostic UI).
func adminFriendlyAuditTypeLabel(eventType string) string {
	switch eventType {
	case "":
		return ""
	case adminAuditLedgerTypeModelChatDone:
		return "Model chat (completed)"
	case "haven.chat.error":
		return "Model chat (error)"
	case "haven.chat.denied":
		return "Model chat (denied)"
	default:
		if strings.HasPrefix(eventType, "morphling.") {
			suffix := strings.TrimPrefix(eventType, "morphling.")
			if suffix == "" {
				return "Subordinate runtime"
			}
			return "Subordinate runtime · " + strings.ReplaceAll(suffix, "_", " ")
		}
		return eventType
	}
}

func buildAdminAuditTypeOptions(effectiveFilter string, discovered []string) []adminAuditTypeOption {
	seen := make(map[string]struct{})
	opts := []adminAuditTypeOption{
		{Value: "", Label: "All types", Selected: effectiveFilter == ""},
	}
	seen[""] = struct{}{}

	add := func(value string) {
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		opts = append(opts, adminAuditTypeOption{
			Value:    value,
			Label:    adminFriendlyAuditTypeLabel(value),
			Selected: effectiveFilter == value,
		})
	}
	for _, preset := range adminAuditTypePresets {
		add(preset)
	}
	sort.Strings(discovered)
	for _, eventType := range discovered {
		add(eventType)
	}
	return opts
}

func executeAdminTemplate(content *template.Template, data any) (template.HTML, error) {
	var buf bytes.Buffer
	if err := content.Execute(&buf, data); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

func (server *Server) renderAdminLayout(response http.ResponseWriter, title, activeNav string, body template.HTML) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_ = adminAppLayout.Execute(response, adminLayoutData{
		Title:         title,
		ActiveNav:     activeNav,
		Body:          body,
		TenantSummary: adminTenantLabel(server.adminDeploymentTenantID()),
	})
}
