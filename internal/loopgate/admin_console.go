package loopgate

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"morph/internal/ledger"
	"morph/internal/secrets"

	"golang.org/x/crypto/bcrypt"
)

// See docs/adr/0016-admin-console-v0-auth.md.
const (
	adminSessionCookieName    = "lg_admin_session"
	minAdminTokenRunes        = 24
	adminSessionTTL           = 8 * time.Hour
	adminAuditExportLimit     = 5000
	adminDashboardAuditRetain = 5000
	// adminAuditLedgerTypeModelChatDone is the stable audit ledger type for completed governed model chat turns
	// (carries input_tokens / output_tokens). Wire id is historical; admin UI surfaces a product-agnostic label.
	adminAuditLedgerTypeModelChatDone = "haven.chat"
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
	case "/admin/config":
		server.handleAdminRuntimeConfig(response, request)
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
		_ = adminLoginStyled.Execute(response, nil)
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
			_ = adminLoginStyled.Execute(response, map[string]any{"Error": "Invalid token."})
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
	dash, err := server.buildAdminDashboardPageData()
	if err != nil {
		http.Error(response, "build dashboard", http.StatusInternalServerError)
		return
	}
	server.adminDiagInfo("admin_console_dashboard_view",
		"active_sessions", dash.ActiveSessions,
		"model_chat_turns_window", dash.ModelChatTurns,
	)
	body, err := executeAdminTemplate(adminHomeContent, dash)
	if err != nil {
		http.Error(response, "render page", http.StatusInternalServerError)
		return
	}
	server.renderAdminLayout(response, "Dashboard", "home", body)
}

func adminCoerceInt64(v interface{}) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case int64:
		return x
	case uint64:
		return int64(x)
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

func adminFormatByteSize(n int64) string {
	if n < 0 {
		return "—"
	}
	const kb = 1024
	switch {
	case n < kb:
		return fmt.Sprintf("%d B", n)
	case n < kb*kb:
		return fmt.Sprintf("%.1f KiB", float64(n)/kb)
	case n < kb*kb*kb:
		return fmt.Sprintf("%.1f MiB", float64(n)/(kb*kb))
	default:
		return fmt.Sprintf("%.2f GiB", float64(n)/(kb*kb*kb))
	}
}

// scanAuditTailForDashboard retains the last retainCap events (tenant-filtered) and aggregates token fields
// from completed model chat audit rows (see adminAuditLedgerTypeModelChatDone).
func (server *Server) scanAuditTailForDashboard(retainCap int) (modelChatTurns int, inputSum, outputSum int64, retained int, err error) {
	deploymentTenant := server.adminDeploymentTenantID()
	file, err := os.Open(server.auditPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, 0, 0, nil
		}
		return 0, 0, 0, 0, err
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
		if len(ring) > retainCap {
			ring = ring[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, 0, 0, err
	}
	retained = len(ring)
	for _, e := range ring {
		if e.Type != adminAuditLedgerTypeModelChatDone || e.Data == nil {
			continue
		}
		modelChatTurns++
		inputSum += adminCoerceInt64(e.Data["input_tokens"])
		outputSum += adminCoerceInt64(e.Data["output_tokens"])
	}
	return modelChatTurns, inputSum, outputSum, retained, nil
}

func (server *Server) buildAdminDashboardPageData() (adminDashboardPageData, error) {
	modelChatTurns, inTok, outTok, retained, err := server.scanAuditTailForDashboard(adminDashboardAuditRetain)
	if err != nil {
		return adminDashboardPageData{}, err
	}

	gatesOn, gatesTotal := countPolicyGatesEnabled(server.policy)
	subordinateActive := server.activeMorphlingCount(server.now().UTC())
	subordinateMax := server.policy.Tools.Morphlings.MaxActive

	listen := strings.TrimSpace(server.adminListenAddr)
	if listen == "" {
		listen = "—"
	}

	var ledgerBytes int64 = -1
	if fi, statErr := os.Stat(server.auditPath); statErr == nil {
		ledgerBytes = fi.Size()
	}

	server.connectionsMu.Lock()
	liveConn := len(server.connections)
	server.connectionsMu.Unlock()

	server.modelConnectionsMu.Lock()
	modelConn := len(server.modelConnections)
	server.modelConnectionsMu.Unlock()

	deploymentTenant := server.adminDeploymentTenantID()
	server.mu.Lock()
	pending := 0
	for _, approvalRecord := range server.approvals {
		if approvalRecord.State == "pending" {
			pending++
		}
	}
	tokensHeld := len(server.tokens)
	userSeen := make(map[string]struct{})
	activeSessions := 0
	for _, session := range server.sessions {
		if deploymentTenant != "" && strings.TrimSpace(session.TenantID) != deploymentTenant {
			continue
		}
		activeSessions++
		uid := strings.TrimSpace(session.UserID)
		if uid != "" {
			userSeen[uid] = struct{}{}
		}
	}
	server.mu.Unlock()

	return adminDashboardPageData{
		AdminListenAddr:    listen,
		PolicyVersion:      server.policy.Version,
		ActiveSessions:     activeSessions,
		DistinctUsers:      len(userSeen),
		PendingApprovals:   pending,
		ActiveSubordinates: subordinateActive,
		SubordinateMax:     subordinateMax,
		LiveConnections:    liveConn,
		ModelConnections:   modelConn,
		CapabilityTokens:   tokensHeld,
		AuditLedgerBytes:   adminFormatByteSize(ledgerBytes),
		AuditLedgerNote:    fmt.Sprintf("Trailing window: %d events retained for token math (cap %d).", retained, adminDashboardAuditRetain),
		Goroutines:         runtime.NumGoroutine(),
		PolicyGatesOn:      gatesOn,
		PolicyGatesTotal:   gatesTotal,
		ModelChatTurns:     modelChatTurns,
		InputTokensWindow:  inTok,
		OutputTokensWindow: outTok,
		TotalTokensWindow:  inTok + outTok,
		AuditWindowEvents:  retained,
		AuditScanCap:       adminDashboardAuditRetain,
	}, nil
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
	classPolicyPath := filepath.Join(server.repoRoot, "core", "policy", "morphling_classes.yaml")
	classPolicyYAML, err := os.ReadFile(classPolicyPath)
	if err != nil {
		http.Error(response, "read subordinate class policy", http.StatusInternalServerError)
		return
	}
	pageData := adminPolicyPageData{
		Sections:                   buildAdminPolicySections(server.policy),
		SubordinateClassPolicyPath: classPolicyPath,
		SubordinateClassPolicyYAML: string(classPolicyYAML),
	}
	body, err := executeAdminTemplate(adminPolicyContent, pageData)
	if err != nil {
		http.Error(response, "render policy page", http.StatusInternalServerError)
		return
	}
	server.renderAdminLayout(response, "Capability policy", "policy", body)
}

func (server *Server) handleAdminRuntimeConfig(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	server.adminDiagInfo("admin_console_runtime_config_view")
	cfg := server.runtimeConfig
	pageData := adminRuntimePageData{
		Sections:       buildAdminRuntimeSections(cfg),
		BackendOptions: adminMemoryBackendOptions(cfg.Memory.Backend),
		CurrentBackend: cfg.Memory.Backend,
	}
	body, err := executeAdminTemplate(adminRuntimeContent, pageData)
	if err != nil {
		http.Error(response, "render configuration page", http.StatusInternalServerError)
		return
	}
	server.renderAdminLayout(response, "Configuration", "config", body)
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

	server.adminDiagInfo("admin_console_users_view", "row_count", len(rows))
	body, err := executeAdminTemplate(adminUsersContent, adminUsersPageData{Rows: rows})
	if err != nil {
		http.Error(response, "render sessions page", http.StatusInternalServerError)
		return
	}
	server.renderAdminLayout(response, "Sessions", "users", body)
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
	typeCustom := strings.TrimSpace(query.Get("type_custom"))
	typePreset := strings.TrimSpace(query.Get("type_preset"))
	legacyType := strings.TrimSpace(query.Get("type"))
	typeFilter := ""
	switch {
	case typeCustom != "":
		typeFilter = typeCustom
	case legacyType != "":
		typeFilter = legacyType
	default:
		typeFilter = typePreset
	}
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

	discoveredTypes, err := server.collectAuditEventTypesForAdmin(100000)
	if err != nil {
		http.Error(response, "scan audit types", http.StatusInternalServerError)
		return
	}
	typeCustomInput := typeCustom
	if typeCustomInput == "" && legacyType != "" && legacyType == typeFilter {
		typeCustomInput = legacyType
	}
	csvParams := url.Values{}
	csvParams.Set("format", "csv")
	csvParams.Set("limit", strconv.Itoa(limit))
	if userFilter != "" {
		csvParams.Set("user_id", userFilter)
	}
	if typeFilter != "" {
		csvParams.Set("type", typeFilter)
	}
	auditRows := make([]adminAuditRow, 0, len(events))
	for _, event := range events {
		redactedData := secrets.RedactStructuredFields(event.Data)
		dataBytes, err := json.Marshal(redactedData)
		if err != nil {
			http.Error(response, "encode row", http.StatusInternalServerError)
			return
		}
		auditRows = append(auditRows, adminAuditRow{
			TS:          event.TS,
			TypeDisplay: adminFriendlyAuditTypeLabel(event.Type),
			Session:     event.Session,
			DataJSON:    string(dataBytes),
		})
	}
	pageData := adminAuditPageData{
		TypeOptions:   buildAdminAuditTypeOptions(typeFilter, discoveredTypes),
		TypeCustom:    typeCustomInput,
		UserID:        userFilter,
		SelectedLimit: limit,
		LimitOptions:  []int{50, 100, 200, 500, 1000, adminAuditExportLimit},
		CSVLink:       "/admin/audit?" + csvParams.Encode(),
		RowCount:      len(auditRows),
		EventRows:     auditRows,
	}
	body, err := executeAdminTemplate(adminAuditContent, pageData)
	if err != nil {
		http.Error(response, "render audit page", http.StatusInternalServerError)
		return
	}
	server.renderAdminLayout(response, "Audit log", "audit", body)
}

func (server *Server) collectAuditEventTypesForAdmin(maxLines int) ([]string, error) {
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

	seen := make(map[string]struct{})
	linesRead := 0
	for scanner.Scan() {
		linesRead++
		if linesRead > maxLines {
			break
		}
		line := bytesTrimSpaceCopy(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event ledger.Event
		if err := json.Unmarshal(line, &event); err != nil {
			continue
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
		if event.Type != "" {
			seen[event.Type] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seen))
	for eventType := range seen {
		out = append(out, eventType)
	}
	sort.Strings(out)
	return out, nil
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
