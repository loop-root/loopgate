package loopgate

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"morph/internal/config"
	"morph/internal/ledger"
	"morph/internal/secrets"

	"golang.org/x/crypto/bcrypt"
)

// See docs/adr/0016-admin-console-v0-auth.md.
const (
	adminSessionCookieName = "lg_admin_session"
	minAdminTokenRunes     = 24
	adminSessionTTL        = 8 * time.Hour
	adminAuditExportLimit  = 5000
)

// ConfiguredAdminListenAddr returns the TCP address from config when this process started the admin
// console (--admin). Empty when the admin HTTP server is not active.
func (server *Server) ConfiguredAdminListenAddr() string {
	if server == nil || server.adminHTTPServer == nil {
		return ""
	}
	return server.adminListenAddr
}

func (server *Server) initAdminConsole() error {
	rawToken := strings.TrimSpace(os.Getenv("LOOPGATE_ADMIN_TOKEN"))
	if rawToken == "" {
		return fmt.Errorf("loopgate: LOOPGATE_ADMIN_TOKEN is required when starting with --admin (see docs/setup/ADMIN_CONSOLE.md)")
	}
	if utf8.RuneCountInString(rawToken) < minAdminTokenRunes {
		return fmt.Errorf("loopgate: LOOPGATE_ADMIN_TOKEN must be at least %d characters", minAdminTokenRunes)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(rawToken), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("loopgate: hash admin token: %w", err)
	}
	rawToken = ""

	server.adminPasswordHash = passwordHash
	server.adminListenAddr = strings.TrimSpace(server.runtimeConfig.AdminConsole.ListenAddr)
	server.adminSessions = make(map[string]time.Time)

	mux := http.NewServeMux()
	mux.HandleFunc("/admin/login", server.handleAdminLogin)
	mux.HandleFunc("/admin/logout", server.handleAdminLogout)
	mux.HandleFunc("/admin", server.handleAdminBarePathRedirect)
	mux.HandleFunc("/admin/", server.handleAdminProtectedSubtree)

	server.adminHTTPServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    maxHeaderBytes,
	}
	return nil
}

func (server *Server) adminDeploymentTenantID() string {
	return strings.TrimSpace(server.runtimeConfig.Tenancy.DeploymentTenantID)
}

func (server *Server) adminDiagInfo(msg string, attrs ...any) {
	if server.diagnostic == nil || server.diagnostic.Server == nil {
		return
	}
	prefix := append([]any{
		"deployment_tenant_id", server.adminDeploymentTenantID(),
	}, attrs...)
	server.diagnostic.Server.Info(msg, prefix...)
}

func (server *Server) adminDiagWarn(msg string, attrs ...any) {
	if server.diagnostic == nil || server.diagnostic.Server == nil {
		return
	}
	prefix := append([]any{
		"deployment_tenant_id", server.adminDeploymentTenantID(),
	}, attrs...)
	server.diagnostic.Server.Warn(msg, prefix...)
}

func (server *Server) pruneAdminSessionsLocked(now time.Time) {
	for token, expires := range server.adminSessions {
		if !expires.After(now) {
			delete(server.adminSessions, token)
		}
	}
}

func (server *Server) adminSessionTokenFromRequest(request *http.Request) string {
	cookie, err := request.Cookie(adminSessionCookieName)
	if err != nil || cookie == nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func (server *Server) adminSessionValid(sessionToken string) bool {
	if sessionToken == "" {
		return false
	}
	server.adminSessionMu.Lock()
	defer server.adminSessionMu.Unlock()
	now := server.now()
	server.pruneAdminSessionsLocked(now)
	expires, ok := server.adminSessions[sessionToken]
	return ok && expires.After(now)
}

func (server *Server) handleAdminBarePathRedirect(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Redirect(response, request, "/admin/", http.StatusSeeOther)
		return
	}
	http.Redirect(response, request, "/admin/", http.StatusSeeOther)
}

// handleAdminProtectedSubtree serves authenticated /admin/* routes (except login/logout, registered separately).
func (server *Server) handleAdminProtectedSubtree(response http.ResponseWriter, request *http.Request) {
	if !server.adminSessionValid(server.adminSessionTokenFromRequest(request)) {
		http.Redirect(response, request, "/admin/login", http.StatusSeeOther)
		return
	}
	switch request.URL.Path {
	case "/admin/":
		server.handleAdminRoot(response, request)
	case "/admin/policy":
		server.handleAdminPolicy(response, request)
	case "/admin/audit":
		server.handleAdminAudit(response, request)
	case "/admin/users":
		server.handleAdminUsers(response, request)
	default:
		http.NotFound(response, request)
	}
}

func (server *Server) handleAdminLogin(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		if server.adminSessionValid(server.adminSessionTokenFromRequest(request)) {
			http.Redirect(response, request, "/admin/", http.StatusSeeOther)
			return
		}
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		_ = adminLoginPageTemplate.Execute(response, nil)
	case http.MethodPost:
		if err := request.ParseForm(); err != nil {
			http.Error(response, "bad form", http.StatusBadRequest)
			return
		}
		submitted := strings.TrimSpace(request.FormValue("token"))
		if err := bcrypt.CompareHashAndPassword(server.adminPasswordHash, []byte(submitted)); err != nil {
			server.adminDiagWarn("admin_console_login_denied", "reason", "token_mismatch")
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			response.WriteHeader(http.StatusUnauthorized)
			_ = adminLoginPageTemplate.Execute(response, map[string]any{"Error": "Invalid token."})
			return
		}
		sessionToken, err := randomHex(32)
		if err != nil {
			http.Error(response, "session mint failed", http.StatusInternalServerError)
			return
		}
		expires := server.now().Add(adminSessionTTL)
		server.adminSessionMu.Lock()
		server.pruneAdminSessionsLocked(server.now())
		server.adminSessions[sessionToken] = expires
		server.adminSessionMu.Unlock()

		http.SetCookie(response, &http.Cookie{
			Name:     adminSessionCookieName,
			Value:    sessionToken,
			Path:     "/admin",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Expires:  expires.UTC(),
		})
		server.adminDiagInfo("admin_console_login_ok")
		http.Redirect(response, request, "/admin/", http.StatusSeeOther)
	default:
		response.Header().Set("Allow", "GET, POST")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (server *Server) handleAdminLogout(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost && request.Method != http.MethodGet {
		response.Header().Set("Allow", "GET, POST")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := server.adminSessionTokenFromRequest(request)
	if token != "" {
		server.adminSessionMu.Lock()
		delete(server.adminSessions, token)
		server.adminSessionMu.Unlock()
	}
	http.SetCookie(response, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.Redirect(response, request, "/admin/login", http.StatusSeeOther)
}

func (server *Server) handleAdminRoot(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/admin/" {
		http.NotFound(response, request)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_ = adminShellTemplate.Execute(response, map[string]any{
		"Title":   "Loopgate admin",
		"Content": template.HTML(`<p class="nav"><a href="/admin/policy">Policy</a> · <a href="/admin/audit">Audit</a> · <a href="/admin/users">Sessions</a></p><p>Deployment tenant filter: <code>` + template.HTMLEscapeString(adminTenantLabel(server.adminDeploymentTenantID())) + `</code></p><form method="post" action="/admin/logout"><button type="submit">Sign out</button></form>`),
	})
}

func adminTenantLabel(tenantID string) string {
	if tenantID == "" {
		return "(personal mode — all tenants visible)"
	}
	return tenantID
}

func (server *Server) handleAdminPolicy(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	server.adminDiagInfo("admin_console_policy_view")
	policyJSON, err := config.PolicyToJSON(server.policy)
	if err != nil {
		http.Error(response, "serialize policy", http.StatusInternalServerError)
		return
	}
	morphPath := filepath.Join(server.repoRoot, "core", "policy", "morphling_classes.yaml")
	morphYAML, err := os.ReadFile(morphPath)
	if err != nil {
		http.Error(response, "read morphling class policy", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_ = adminShellTemplate.Execute(response, map[string]any{
		"Title": "Policy",
		"Content": template.HTML(
			`<h2>Active policy (JSON)</h2><pre>` + template.HTMLEscapeString(string(policyJSON)) + `</pre>` +
				`<h2>Morphling classes (YAML seed)</h2><pre>` + template.HTMLEscapeString(string(morphYAML)) + `</pre>`,
		),
	})
}

func (server *Server) handleAdminUsers(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	deploymentTenant := server.adminDeploymentTenantID()
	server.mu.Lock()
	rows := make([]adminUserRow, 0, len(server.sessions))
	for _, session := range server.sessions {
		if deploymentTenant != "" && strings.TrimSpace(session.TenantID) != deploymentTenant {
			continue
		}
		rows = append(rows, adminUserRow{
			SessionID:        session.ID,
			ActorLabel:       session.ActorLabel,
			ClientSession:    session.ClientSessionLabel,
			TenantID:         session.TenantID,
			UserID:           session.UserID,
			CreatedAtRFC3339: session.CreatedAt.UTC().Format(time.RFC3339),
			ExpiresAtRFC3339: session.ExpiresAt.UTC().Format(time.RFC3339),
			PeerUID:          session.PeerIdentity.UID,
		})
	}
	server.mu.Unlock()

	var tableHTML strings.Builder
	tableHTML.WriteString(`<table class="grid"><thead><tr><th>Session</th><th>Actor</th><th>Client</th><th>Tenant</th><th>User</th><th>Peer UID</th><th>Created</th><th>Expires</th></tr></thead><tbody>`)
	for _, row := range rows {
		tableHTML.WriteString("<tr><td>")
		tableHTML.WriteString(template.HTMLEscapeString(row.SessionID))
		tableHTML.WriteString("</td><td>")
		tableHTML.WriteString(template.HTMLEscapeString(row.ActorLabel))
		tableHTML.WriteString("</td><td>")
		tableHTML.WriteString(template.HTMLEscapeString(row.ClientSession))
		tableHTML.WriteString("</td><td>")
		tableHTML.WriteString(template.HTMLEscapeString(row.TenantID))
		tableHTML.WriteString("</td><td>")
		tableHTML.WriteString(template.HTMLEscapeString(row.UserID))
		tableHTML.WriteString("</td><td>")
		tableHTML.WriteString(strconv.FormatUint(uint64(row.PeerUID), 10))
		tableHTML.WriteString("</td><td>")
		tableHTML.WriteString(template.HTMLEscapeString(row.CreatedAtRFC3339))
		tableHTML.WriteString("</td><td>")
		tableHTML.WriteString(template.HTMLEscapeString(row.ExpiresAtRFC3339))
		tableHTML.WriteString("</td></tr>")
	}
	tableHTML.WriteString("</tbody></table>")

	server.adminDiagInfo("admin_console_users_view", "row_count", len(rows))
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_ = adminShellTemplate.Execute(response, map[string]any{
		"Title":   "Control sessions",
		"Content": template.HTML(tableHTML.String()),
	})
}

type adminUserRow struct {
	SessionID        string
	ActorLabel       string
	ClientSession    string
	TenantID         string
	UserID           string
	CreatedAtRFC3339 string
	ExpiresAtRFC3339 string
	PeerUID          uint32
}

func (server *Server) handleAdminAudit(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := request.URL.Query()
	typeFilter := strings.TrimSpace(query.Get("type"))
	userFilter := strings.TrimSpace(query.Get("user_id"))
	limit := 200
	if rawLimit := strings.TrimSpace(query.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > adminAuditExportLimit {
			http.Error(response, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	wantCSV := strings.TrimSpace(query.Get("format")) == "csv"

	events, err := server.scanAuditEventsForAdmin(typeFilter, userFilter, limit)
	if err != nil {
		http.Error(response, "read audit ledger", http.StatusInternalServerError)
		return
	}
	server.adminDiagInfo("admin_console_audit_view", "row_count", len(events), "format_csv", wantCSV)

	if wantCSV {
		response.Header().Set("Content-Type", "text/csv; charset=utf-8")
		response.Header().Set("Content-Disposition", `attachment; filename="loopgate_audit_export.csv"`)
		writer := csv.NewWriter(response)
		_ = writer.Write([]string{"ts", "type", "session", "data_json_redacted"})
		for _, event := range events {
			redactedData := secrets.RedactStructuredFields(event.Data)
			dataBytes, err := json.Marshal(redactedData)
			if err != nil {
				http.Error(response, "encode row", http.StatusInternalServerError)
				return
			}
			_ = writer.Write([]string{event.TS, event.Type, event.Session, string(dataBytes)})
		}
		writer.Flush()
		return
	}

	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	var b strings.Builder
	b.WriteString(`<p><a href="/admin/audit?format=csv">Download CSV</a> (redacted)</p>`)
	b.WriteString(`<table class="grid"><thead><tr><th>Time</th><th>Type</th><th>Session</th><th>Data (redacted JSON)</th></tr></thead><tbody>`)
	for _, event := range events {
		redactedData := secrets.RedactStructuredFields(event.Data)
		dataBytes, _ := json.Marshal(redactedData)
		b.WriteString("<tr><td>")
		b.WriteString(template.HTMLEscapeString(event.TS))
		b.WriteString("</td><td>")
		b.WriteString(template.HTMLEscapeString(event.Type))
		b.WriteString("</td><td>")
		b.WriteString(template.HTMLEscapeString(event.Session))
		b.WriteString("</td><td><pre>")
		b.WriteString(template.HTMLEscapeString(string(dataBytes)))
		b.WriteString("</pre></td></tr>")
	}
	b.WriteString("</tbody></table>")
	_ = adminShellTemplate.Execute(response, map[string]any{
		"Title":   "Audit",
		"Content": template.HTML(b.String()),
	})
}

func (server *Server) scanAuditEventsForAdmin(typeFilter, userFilter string, limit int) ([]ledger.Event, error) {
	deploymentTenant := server.adminDeploymentTenantID()
	file, err := os.Open(server.auditPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	const maxAuditLineBytes = 4 * 1024 * 1024
	scanner.Buffer(make([]byte, 64*1024), maxAuditLineBytes)

	var ring []ledger.Event
	for scanner.Scan() {
		line := bytesTrimSpaceCopy(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event ledger.Event
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if typeFilter != "" && event.Type != typeFilter {
			continue
		}
		if userFilter != "" {
			if event.Data == nil {
				continue
			}
			uid, _ := event.Data["user_id"].(string)
			if strings.TrimSpace(uid) != userFilter {
				continue
			}
		}
		if deploymentTenant != "" {
			if event.Data == nil {
				continue
			}
			tid, _ := event.Data["tenant_id"].(string)
			if strings.TrimSpace(tid) != deploymentTenant {
				continue
			}
		}
		ring = append(ring, event)
		if len(ring) > adminAuditExportLimit {
			ring = ring[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(ring) > limit {
		ring = ring[len(ring)-limit:]
	}
	return ring, nil
}

func bytesTrimSpaceCopy(b []byte) []byte {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\t' || b[start] == '\n' || b[start] == '\r') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\n' || b[end-1] == '\r') {
		end--
	}
	out := make([]byte, end-start)
	copy(out, b[start:end])
	return out
}

var adminLoginPageTemplate = template.Must(template.New("adminLogin").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>Loopgate admin login</title>
<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:2rem auto}label{display:block;margin:.5rem 0 .25rem}input{width:100%;padding:.4rem}.err{color:#b00020;margin-top:1rem}</style>
</head><body>
<h1>Loopgate admin</h1>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<form method="post" action="/admin/login"><label for="token">Admin token</label>
<input id="token" name="token" type="password" autocomplete="current-password" required>
<button type="submit">Sign in</button></form>
</body></html>`))

var adminShellTemplate = template.Must(template.New("adminShell").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>{{.Title}} — Loopgate admin</title>
<style>body{font-family:system-ui,sans-serif;max-width:60rem;margin:2rem auto}pre{white-space:pre-wrap;word-break:break-word;background:#f6f8fa;padding:1rem;border-radius:6px}table.grid{border-collapse:collapse;width:100%}table.grid th,table.grid td{border:1px solid #ccc;padding:.35rem .5rem;vertical-align:top;font-size:.9rem}a{color:#0366d6}.nav{margin-bottom:1rem}</style>
</head><body>
<p class="nav"><a href="/admin/">Home</a> · <a href="/admin/policy">Policy</a> · <a href="/admin/audit">Audit</a> · <a href="/admin/users">Sessions</a></p>
<h1>{{.Title}}</h1>
{{.Content}}
</body></html>`))
