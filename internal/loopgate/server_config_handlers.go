package loopgate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"path/filepath"
	"sort"
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
		result = server.currentRuntimeConfigSnapshot()
	case "connections":
		conns, caps := server.currentConfiguredProviderSnapshot()
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
		server.handleConfigPutRuntime(w, body, tokenClaims)
	case "connections":
		server.handleConfigPutConnections(w, body, tokenClaims)
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
		"denial_code":          controlapipkg.DenialCodeCapabilityTokenScopeDenied,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"control_session_id":   tokenClaims.ControlSessionID,
	}); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, auditUnavailableCapabilityResponse(""))
		return false
	}
	server.writeJSON(writer, http.StatusForbidden, controlapipkg.CapabilityResponse{
		Status:       controlapipkg.ResponseStatusDenied,
		DenialReason: "capability token scope denied requested capability",
		DenialCode:   controlapipkg.DenialCodeCapabilityTokenScopeDenied,
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

	server.writeJSON(w, http.StatusOK, controlapipkg.ConfigPolicyReloadResponse{
		Status:               "ok",
		PreviousPolicySHA256: currentPolicyRuntime.policyContentSHA256,
		PolicySHA256:         reloadedPolicyRuntime.policyContentSHA256,
		PolicyChanged:        policyChanged,
	})
}

func (server *Server) handleConfigPutRuntime(w http.ResponseWriter, body []byte, tokenClaims capabilityToken) {
	var rc config.RuntimeConfig
	if err := decodeJSONBytes(body, &rc); err != nil {
		http.Error(w, "invalid runtime config: "+err.Error(), http.StatusBadRequest)
		return
	}
	previousRuntimeConfig := server.currentRuntimeConfigSnapshot()
	if err := config.WriteRuntimeConfigYAML(server.repoRoot, rc); err != nil {
		http.Error(w, "save runtime config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	reloaded, err := config.LoadRuntimeConfig(server.repoRoot)
	if err != nil {
		rollbackErr := config.WriteRuntimeConfigYAML(server.repoRoot, previousRuntimeConfig)
		http.Error(w, formatConfigUpdateFailure("reload runtime config", err, rollbackErr), http.StatusInternalServerError)
		return
	}

	auditData, err := buildRuntimeConfigAuditData(previousRuntimeConfig, reloaded, tokenClaims)
	if err != nil {
		rollbackErr := config.WriteRuntimeConfigYAML(server.repoRoot, previousRuntimeConfig)
		http.Error(w, formatConfigUpdateFailure("hash runtime config for audit", err, rollbackErr), http.StatusInternalServerError)
		return
	}
	if err := server.logEvent("config.runtime.updated", tokenClaims.ControlSessionID, auditData); err != nil {
		rollbackErr := config.WriteRuntimeConfigYAML(server.repoRoot, previousRuntimeConfig)
		if rollbackErr != nil {
			http.Error(w, formatConfigUpdateFailure("rollback runtime config after audit failure", err, rollbackErr), http.StatusInternalServerError)
			return
		}
		server.writeJSON(w, http.StatusServiceUnavailable, auditUnavailableCapabilityResponse(""))
		return
	}

	server.applyRuntimeConfigReloaded(reloaded)

	server.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) handleConfigPutConnections(w http.ResponseWriter, body []byte, tokenClaims capabilityToken) {
	conns, caps, err := loadConfiguredConnectionsFromJSON(body)
	if err != nil {
		http.Error(w, "invalid connections: "+err.Error(), http.StatusBadRequest)
		return
	}

	configFiles := connectionsToConfigFiles(conns, caps)
	nextPolicyRuntime, err := server.buildPolicyRuntimeForConfiguredCapabilities(caps)
	if err != nil {
		http.Error(w, "build configured capability runtime: "+err.Error(), http.StatusInternalServerError)
		return
	}

	previousConnections, previousCapabilities := server.currentConfiguredProviderSnapshot()
	previousConfigFiles := connectionsToConfigFiles(previousConnections, previousCapabilities)
	auditData, err := buildConnectionsConfigAuditData(previousConfigFiles, configFiles, tokenClaims)
	if err != nil {
		http.Error(w, "hash connections config for audit: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := config.SaveJSONConfig(server.configStateDir, "connections", configFiles); err != nil {
		http.Error(w, "save connections: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := server.logEvent("config.connections.updated", tokenClaims.ControlSessionID, auditData); err != nil {
		rollbackErr := config.SaveJSONConfig(server.configStateDir, "connections", previousConfigFiles)
		if rollbackErr != nil {
			http.Error(w, formatConfigUpdateFailure("rollback connections config after audit failure", err, rollbackErr), http.StatusInternalServerError)
			return
		}
		server.writeJSON(w, http.StatusServiceUnavailable, auditUnavailableCapabilityResponse(""))
		return
	}

	server.applyConfiguredConnectionsReloaded(conns, caps)
	server.storePolicyRuntime(nextPolicyRuntime)

	server.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	for connectionKey := range capsByConn {
		sort.Slice(capsByConn[connectionKey], func(leftIndex int, rightIndex int) bool {
			return capsByConn[connectionKey][leftIndex].Name < capsByConn[connectionKey][rightIndex].Name
		})
	}

	var result []connectionConfigFile
	for connKey, conn := range conns {
		allowedHosts := make([]string, 0, len(conn.AllowedHosts))
		for h := range conn.AllowedHosts {
			allowedHosts = append(allowedHosts, h)
		}
		sort.Strings(allowedHosts)
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
	sort.Slice(result, func(leftIndex int, rightIndex int) bool {
		if result[leftIndex].Provider != result[rightIndex].Provider {
			return result[leftIndex].Provider < result[rightIndex].Provider
		}
		return result[leftIndex].Subject < result[rightIndex].Subject
	})
	if result == nil {
		return []connectionConfigFile{}
	}
	return result
}

func configSHA256(value any) (string, error) {
	encodedBytes, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	contentHash := sha256.Sum256(encodedBytes)
	return hex.EncodeToString(contentHash[:]), nil
}

func buildRuntimeConfigAuditData(previousRuntimeConfig config.RuntimeConfig, appliedRuntimeConfig config.RuntimeConfig, tokenClaims capabilityToken) (map[string]interface{}, error) {
	previousRuntimeConfigSHA256, err := configSHA256(previousRuntimeConfig)
	if err != nil {
		return nil, err
	}
	appliedRuntimeConfigSHA256, err := configSHA256(appliedRuntimeConfig)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"control_session_id":          tokenClaims.ControlSessionID,
		"actor_label":                 tokenClaims.ActorLabel,
		"client_session_label":        tokenClaims.ClientSessionLabel,
		"config_section":              "runtime",
		"previous_config_sha256":      previousRuntimeConfigSHA256,
		"applied_config_sha256":       appliedRuntimeConfigSHA256,
		"config_changed":              previousRuntimeConfigSHA256 != appliedRuntimeConfigSHA256,
		"audit_export_enabled":        appliedRuntimeConfig.Logging.AuditExport.Enabled,
		"diagnostic_logging_enabled":  appliedRuntimeConfig.Logging.Diagnostic.Enabled,
		"expected_client_pin_present": strings.TrimSpace(appliedRuntimeConfig.ControlPlane.ExpectedSessionClientExecutable) != "",
	}, nil
}

func buildConnectionsConfigAuditData(previousConfigFiles []connectionConfigFile, appliedConfigFiles []connectionConfigFile, tokenClaims capabilityToken) (map[string]interface{}, error) {
	previousConfigSHA256, err := configSHA256(previousConfigFiles)
	if err != nil {
		return nil, err
	}
	appliedConfigSHA256, err := configSHA256(appliedConfigFiles)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"control_session_id":        tokenClaims.ControlSessionID,
		"actor_label":               tokenClaims.ActorLabel,
		"client_session_label":      tokenClaims.ClientSessionLabel,
		"config_section":            "connections",
		"previous_config_sha256":    previousConfigSHA256,
		"applied_config_sha256":     appliedConfigSHA256,
		"config_changed":            previousConfigSHA256 != appliedConfigSHA256,
		"previous_connection_count": len(previousConfigFiles),
		"applied_connection_count":  len(appliedConfigFiles),
		"previous_capability_count": countConfiguredConnectionCapabilities(previousConfigFiles),
		"applied_capability_count":  countConfiguredConnectionCapabilities(appliedConfigFiles),
	}, nil
}

func countConfiguredConnectionCapabilities(configFiles []connectionConfigFile) int {
	totalCount := 0
	for _, configFile := range configFiles {
		totalCount += len(configFile.Capabilities)
	}
	return totalCount
}

func formatConfigUpdateFailure(action string, cause error, rollbackErr error) string {
	if rollbackErr == nil {
		return action + ": " + cause.Error()
	}
	return fmt.Sprintf("%s: %v (rollback failed: %v)", action, cause, rollbackErr)
}

func (server *Server) currentRuntimeConfigSnapshot() config.RuntimeConfig {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.runtimeConfig
}

func (server *Server) applyRuntimeConfigReloaded(reloadedRuntimeConfig config.RuntimeConfig) {
	server.mu.Lock()
	server.runtimeConfig = reloadedRuntimeConfig
	server.auditExportStatePath = filepath.Join(server.repoRoot, reloadedRuntimeConfig.Logging.AuditExport.StatePath)
	server.expectedClientPath = normalizeSessionExecutablePinPath(reloadedRuntimeConfig.ControlPlane.ExpectedSessionClientExecutable)
	server.mu.Unlock()
}

func (server *Server) applyConfiguredConnectionsReloaded(configuredConnections map[string]configuredConnection, configuredCapabilities map[string]configuredCapability) {
	server.providerRuntime.mu.Lock()
	server.providerRuntime.tokens = make(map[string]providerAccessToken)
	server.providerRuntime.configuredConnections = cloneConfiguredConnections(configuredConnections)
	server.providerRuntime.configuredCapabilities = cloneConfiguredCapabilities(configuredCapabilities)
	server.providerRuntime.mu.Unlock()
}
