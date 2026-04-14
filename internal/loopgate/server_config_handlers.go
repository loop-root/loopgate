package loopgate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"loopgate/internal/config"
)

var validConfigSections = map[string]struct{}{
	"policy":      {},
	"runtime":     {},
	"connections": {},
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
		server.handleConfigPut(w, section, requestBodyBytes, tokenClaims)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (server *Server) handleConfigGet(w http.ResponseWriter, section string) {
	var result any
	policyRuntime := server.currentPolicyRuntime()

	switch section {
	case "policy":
		result = policyRuntime.policy
	case "runtime":
		result = server.runtimeConfig
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

func (server *Server) handleConfigPut(w http.ResponseWriter, section string, body []byte, tokenClaims capabilityToken) {
	switch section {
	case "policy":
		server.handleConfigPutPolicy(w, tokenClaims, body)
	case "runtime":
		server.handleConfigPutRuntime(w, body)
	case "connections":
		server.handleConfigPutConnections(w, body)
	}
}

func (server *Server) requireControlCapability(writer http.ResponseWriter, tokenClaims capabilityToken, requiredCapability string) bool {
	return server.requireScopedCapability(writer, tokenClaims, requiredCapability, "control-plane action")
}

func (server *Server) requireCapabilityScope(writer http.ResponseWriter, tokenClaims capabilityToken, requiredCapability string) bool {
	return server.requireScopedCapability(writer, tokenClaims, requiredCapability, "capability-scoped route")
}

func (server *Server) requireScopedCapability(writer http.ResponseWriter, tokenClaims capabilityToken, requiredCapability string, denialContext string) bool {
	if capabilityScopeAllowed(tokenClaims, requiredCapability) {
		return true
	}
	if err := server.logEvent("capability.denied", tokenClaims.ControlSessionID, map[string]interface{}{
		"capability":           requiredCapability,
		"reason":               fmt.Sprintf("capability token scope denied %s", denialContext),
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

func (server *Server) handleConfigPutPolicy(w http.ResponseWriter, tokenClaims capabilityToken, body []byte) {
	_ = body
	currentPolicyRuntime := server.currentPolicyRuntime()
	reloadedPolicyRuntime, err := server.reloadPolicyRuntimeFromDisk()
	if err != nil {
		http.Error(w, "reload signed policy: "+err.Error(), http.StatusConflict)
		return
	}

	policyChanged := currentPolicyRuntime.policyContentSHA256 != reloadedPolicyRuntime.policyContentSHA256
	if err := server.logEvent("config.policy.reloaded", tokenClaims.ControlSessionID, map[string]interface{}{
		"control_session_id":     tokenClaims.ControlSessionID,
		"actor_label":            tokenClaims.ActorLabel,
		"client_session_label":   tokenClaims.ClientSessionLabel,
		"previous_policy_sha256": currentPolicyRuntime.policyContentSHA256,
		"reloaded_policy_sha256": reloadedPolicyRuntime.policyContentSHA256,
		"policy_changed":         policyChanged,
	}); err != nil {
		server.writeJSON(w, http.StatusServiceUnavailable, auditUnavailableCapabilityResponse(""))
		return
	}

	server.storePolicyRuntime(reloadedPolicyRuntime)

	server.writeJSON(w, http.StatusOK, ConfigPolicyReloadResponse{
		Status:               "ok",
		PreviousPolicySHA256: currentPolicyRuntime.policyContentSHA256,
		PolicySHA256:         reloadedPolicyRuntime.policyContentSHA256,
		PolicyChanged:        policyChanged,
	})
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
