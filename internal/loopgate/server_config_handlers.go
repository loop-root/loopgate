package loopgate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"morph/internal/config"
	policypkg "morph/internal/policy"
	toolspkg "morph/internal/tools"
)

var validConfigSections = map[string]struct{}{
	"policy":            {},
	"morphling_classes": {},
	"runtime":           {},
	"goal_aliases":      {},
	"connections":       {},
}

func (server *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	section := strings.TrimPrefix(r.URL.Path, "/v1/config/")
	section = strings.TrimSuffix(section, "/")
	if _, ok := validConfigSections[section]; !ok {
		http.Error(w, fmt.Sprintf("unknown config section %q", section), http.StatusNotFound)
		return
	}

	tokenClaims, ok := server.authenticate(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !server.requireControlCapability(w, tokenClaims, controlCapabilityConfigRead) {
			return
		}
		if _, denialResponse, verified := server.verifySignedRequestWithoutBody(r, tokenClaims.ControlSessionID); !verified {
			server.writeJSON(w, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
			return
		}
		server.handleConfigGet(w, section)
	case http.MethodPut:
		if !server.requireControlCapability(w, tokenClaims, controlCapabilityConfigWrite) {
			return
		}
		requestBodyBytes, denialResponse, verified := server.readAndVerifySignedBody(w, r, 2<<20, tokenClaims.ControlSessionID)
		if !verified {
			server.writeJSON(w, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
			return
		}
		server.handleConfigPut(w, section, requestBodyBytes)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (server *Server) handleConfigGet(w http.ResponseWriter, section string) {
	var result any

	switch section {
	case "policy":
		result = server.policy
	case "morphling_classes":
		server.mu.Lock()
		mcp := server.morphlingClassPolicy
		server.mu.Unlock()
		// Convert to the file representation for API consumers.
		classList := make([]morphlingClassYAMLDef, 0, len(mcp.Classes))
		for _, cls := range mcp.Classes {
			classList = append(classList, validatedClassToYAMLDef(cls))
		}
		result = morphlingClassPolicyFile{Version: mcp.Version, Classes: classList}
	case "runtime":
		result = server.runtimeConfig
	case "goal_aliases":
		result = server.goalAliases
	case "connections":
		server.providerTokenMu.Lock()
		conns := server.configuredConnections
		caps := server.configuredCapabilities
		server.providerTokenMu.Unlock()
		result = connectionsToConfigFiles(conns, caps)
	}

	w.Header().Set("Content-Type", contentTypeApplicationJSON)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}

func (server *Server) handleConfigPut(w http.ResponseWriter, section string, body []byte) {
	switch section {
	case "policy":
		server.handleConfigPutPolicy(w, body)
	case "morphling_classes":
		server.handleConfigPutMorphlingClasses(w, body)
	case "runtime":
		server.handleConfigPutRuntime(w, body)
	case "goal_aliases":
		server.handleConfigPutGoalAliases(w, body)
	case "connections":
		server.handleConfigPutConnections(w, body)
	}
}

func (server *Server) requireControlCapability(writer http.ResponseWriter, tokenClaims capabilityToken, requiredCapability string) bool {
	if capabilityScopeAllowed(tokenClaims, requiredCapability) {
		return true
	}
	if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
		"capability":           requiredCapability,
		"reason":               "capability token scope denied control-plane action",
		"denial_code":          DenialCodeCapabilityTokenScopeDenied,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"control_session_id":   tokenClaims.ControlSessionID,
	}); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, auditUnavailableCapabilityResponse(""))
		return false
	}
	server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
		Status:       ResponseStatusDenied,
		DenialReason: "capability token scope denied requested capability",
		DenialCode:   DenialCodeCapabilityTokenScopeDenied,
	})
	return false
}

func (server *Server) handleConfigPutPolicy(w http.ResponseWriter, body []byte) {
	pol, err := config.LoadPolicyFromJSON(body)
	if err != nil {
		http.Error(w, "invalid policy: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := config.WritePolicyYAML(server.repoRoot, pol); err != nil {
		http.Error(w, "save policy: "+err.Error(), http.StatusInternalServerError)
		return
	}
	reloaded, err := config.LoadPolicy(server.repoRoot)
	if err != nil {
		http.Error(w, "reload policy: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Hot-reload: update policy, recreate checker and registry.
	registry, err := toolspkg.NewDefaultRegistry(server.repoRoot, reloaded)
	if err != nil {
		http.Error(w, "recreate registry: "+err.Error(), http.StatusInternalServerError)
		return
	}

	server.mu.Lock()
	server.policy = reloaded
	server.checker = policypkg.NewChecker(reloaded)
	server.registry = registry
	server.mu.Unlock()

	w.Header().Set("Content-Type", contentTypeApplicationJSON)
	fmt.Fprintln(w, `{"status":"ok"}`)
}

func (server *Server) handleConfigPutMorphlingClasses(w http.ResponseWriter, body []byte) {
	server.mu.Lock()
	registry := server.registry
	server.mu.Unlock()

	mcp, err := loadMorphlingClassPolicyFromJSON(body, registry)
	if err != nil {
		http.Error(w, "invalid morphling classes: "+err.Error(), http.StatusBadRequest)
		return
	}

	var fileData morphlingClassPolicyFile
	if jsonErr := json.Unmarshal(body, &fileData); jsonErr != nil {
		http.Error(w, "decode morphling classes: "+jsonErr.Error(), http.StatusBadRequest)
		return
	}
	if err := config.SaveJSONConfig(server.configStateDir, "morphling_classes", fileData); err != nil {
		http.Error(w, "save morphling classes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	server.mu.Lock()
	server.morphlingClassPolicy = mcp
	server.mu.Unlock()

	w.Header().Set("Content-Type", contentTypeApplicationJSON)
	fmt.Fprintln(w, `{"status":"ok"}`)
}

func (server *Server) handleConfigPutRuntime(w http.ResponseWriter, body []byte) {
	var rc config.RuntimeConfig
	if err := json.Unmarshal(body, &rc); err != nil {
		http.Error(w, "invalid runtime config: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := config.WriteRuntimeConfigYAML(server.repoRoot, rc); err != nil {
		http.Error(w, "save runtime config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	reloaded, err := config.LoadRuntimeConfig(server.repoRoot)
	if err != nil {
		http.Error(w, "reload runtime config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	server.mu.Lock()
	server.runtimeConfig = reloaded
	server.mu.Unlock()

	w.Header().Set("Content-Type", contentTypeApplicationJSON)
	fmt.Fprintln(w, `{"status":"ok"}`)
}

func (server *Server) handleConfigPutGoalAliases(w http.ResponseWriter, body []byte) {
	var ga config.GoalAliases
	if err := json.Unmarshal(body, &ga); err != nil {
		http.Error(w, "invalid goal aliases: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := config.WriteGoalAliasesYAML(server.repoRoot, ga); err != nil {
		http.Error(w, "save goal aliases: "+err.Error(), http.StatusInternalServerError)
		return
	}
	reloaded, err := config.LoadGoalAliases(server.repoRoot)
	if err != nil {
		http.Error(w, "reload goal aliases: "+err.Error(), http.StatusInternalServerError)
		return
	}

	server.mu.Lock()
	server.goalAliases = reloaded
	server.mu.Unlock()

	w.Header().Set("Content-Type", contentTypeApplicationJSON)
	fmt.Fprintln(w, `{"status":"ok"}`)
}

func (server *Server) handleConfigPutConnections(w http.ResponseWriter, body []byte) {
	conns, caps, err := loadConfiguredConnectionsFromJSON(body)
	if err != nil {
		http.Error(w, "invalid connections: "+err.Error(), http.StatusBadRequest)
		return
	}

	var configFiles []connectionConfigFile
	if jsonErr := json.Unmarshal(body, &configFiles); jsonErr != nil {
		http.Error(w, "decode connections: "+jsonErr.Error(), http.StatusBadRequest)
		return
	}
	if err := config.SaveJSONConfig(server.configStateDir, "connections", configFiles); err != nil {
		http.Error(w, "save connections: "+err.Error(), http.StatusInternalServerError)
		return
	}

	server.providerTokenMu.Lock()
	server.configuredConnections = conns
	server.configuredCapabilities = caps
	server.providerTokenMu.Unlock()

	// Re-register capabilities.
	if err := server.registerConfiguredCapabilities(); err != nil {
		http.Error(w, "re-register capabilities: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentTypeApplicationJSON)
	fmt.Fprintln(w, `{"status":"ok"}`)
}

// validatedClassToYAMLDef converts a validated morphling class back to its file representation.
func validatedClassToYAMLDef(cls validatedMorphlingClass) morphlingClassYAMLDef {
	return morphlingClassYAMLDef{
		Name:        cls.Name,
		Description: cls.Description,
		Capabilities: struct {
			Allowed []string `yaml:"allowed" json:"allowed"`
		}{Allowed: cls.AllowedCapabilities},
		Sandbox: struct {
			AllowedZones []string `yaml:"allowed_zones" json:"allowed_zones"`
		}{AllowedZones: cls.AllowedZones},
		ResourceLimits: struct {
			MaxTimeSeconds int   `yaml:"max_time_seconds" json:"max_time_seconds"`
			MaxTokens      int   `yaml:"max_tokens" json:"max_tokens"`
			MaxDiskBytes   int64 `yaml:"max_disk_bytes" json:"max_disk_bytes"`
		}{MaxTimeSeconds: cls.MaxTimeSeconds, MaxTokens: cls.MaxTokens, MaxDiskBytes: cls.MaxDiskBytes},
		TTL: struct {
			SpawnApprovalTTLSeconds   int `yaml:"spawn_approval_ttl_seconds" json:"spawn_approval_ttl_seconds"`
			CapabilityTokenTTLSeconds int `yaml:"capability_token_ttl_seconds" json:"capability_token_ttl_seconds"`
			ReviewTTLSeconds          int `yaml:"review_ttl_seconds" json:"review_ttl_seconds"`
		}{SpawnApprovalTTLSeconds: cls.SpawnApprovalTTLSeconds, CapabilityTokenTTLSeconds: cls.CapabilityTokenTTLSeconds, ReviewTTLSeconds: cls.ReviewTTLSeconds},
		SpawnRequiresApproval:    cls.SpawnRequiresApproval,
		CompletionRequiresReview: cls.CompletionRequiresReview,
		MaxConcurrent:            cls.MaxConcurrent,
	}
}

// connectionsToConfigFiles converts the runtime maps back to the array of connectionConfigFile.
func connectionsToConfigFiles(conns map[string]configuredConnection, caps map[string]configuredCapability) []connectionConfigFile {
	// Group capabilities by connection key.
	capsByConn := make(map[string][]connectionCapabilityConfig)
	for _, cap := range caps {
		capConfig := connectionCapabilityConfig{
			Name:         cap.Name,
			Description:  cap.Description,
			Method:       cap.Method,
			Path:         cap.Path,
			ContentClass: cap.ContentClass,
			Extractor:    cap.Extractor,
		}
		fields := make([]connectionCapabilityFieldConfig, 0, len(cap.ResponseFields))
		for _, f := range cap.ResponseFields {
			fields = append(fields, connectionCapabilityFieldConfig(f))
		}
		capConfig.ResponseFields = fields
		capsByConn[cap.ConnectionKey] = append(capsByConn[cap.ConnectionKey], capConfig)
	}

	var result []connectionConfigFile
	for connKey, conn := range conns {
		allowedHosts := make([]string, 0, len(conn.AllowedHosts))
		for h := range conn.AllowedHosts {
			allowedHosts = append(allowedHosts, h)
		}
		var authURL, tokenURL, apiBaseURL string
		if conn.AuthorizationURL != nil {
			authURL = conn.AuthorizationURL.String()
		}
		if conn.TokenURL != nil {
			tokenURL = conn.TokenURL.String()
		}
		if conn.APIBaseURL != nil {
			apiBaseURL = conn.APIBaseURL.String()
		}
		result = append(result, connectionConfigFile{
			Provider:         conn.Registration.Provider,
			GrantType:        conn.Registration.GrantType,
			Subject:          conn.Registration.Subject,
			ClientID:         conn.ClientID,
			AuthorizationURL: authURL,
			TokenURL:         tokenURL,
			RedirectURL:      conn.RedirectURL,
			APIBaseURL:       apiBaseURL,
			AllowedHosts:     allowedHosts,
			Scopes:           conn.Registration.Scopes,
			Credential:       conn.Registration.Credential,
			Capabilities:     capsByConn[connKey],
		})
	}
	return result
}
