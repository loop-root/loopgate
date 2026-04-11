package loopgate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"morph/internal/config"
	modelruntime "morph/internal/modelruntime"
)

// havenShellDevResponse is the JSON body for GET and POST /v1/settings/shell-dev.
// The legacy /v1/haven/settings/shell-dev alias uses the same payload.
type havenShellDevResponse struct {
	Enabled bool   `json:"enabled"`
	Warning string `json:"warning,omitempty"`
}

type havenShellDevUpdateRequest struct {
	Enabled bool `json:"enabled"`
}

const havenShellDevWarningText = "Terminal access lets Morph run arbitrary shell commands inside the sandbox. " +
	"Enable only for development tasks that genuinely require the command line. " +
	"Disable again when not needed."

// handleHavenSettingsShellDev serves GET and POST /v1/settings/shell-dev.
//
// GET  — returns whether shell_exec is currently visible to the local chat surface.
// POST — enables or disables it by writing (or removing) the override file.
//
// Gating is applied at request time in handleHavenChat, so changes take effect
// immediately for new chat turns without restarting Loopgate.
func (server *Server) handleHavenSettingsShellDev(writer http.ResponseWriter, request *http.Request) {
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(tokenClaims.ActorLabel), "haven") {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "shell dev settings require actor haven",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return
	}

	switch request.Method {
	case http.MethodGet:
		if !server.requireControlCapability(writer, tokenClaims, controlCapabilityConfigRead) {
			return
		}
		if _, denial, ok := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !ok {
			server.writeJSON(writer, signedRequestHTTPStatus(denial.DenialCode), denial)
			return
		}
		enabled, err := config.IsShellDevModeEnabled(server.repoRoot)
		if err != nil {
			server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeExecutionFailed,
			})
			return
		}
		server.writeJSON(writer, http.StatusOK, shellDevResponseFor(enabled))

	case http.MethodPost:
		if !server.requireControlCapability(writer, tokenClaims, controlCapabilityConfigWrite) {
			return
		}
		body, denial, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
		if !ok {
			server.writeJSON(writer, signedRequestHTTPStatus(denial.DenialCode), denial)
			return
		}
		var req havenShellDevUpdateRequest
		if err := decodeJSONBytes(body, &req); err != nil {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusDenied,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
		var saveErr error
		if req.Enabled {
			saveErr = config.SaveShellDevOverride(server.repoRoot, true)
		} else {
			saveErr = config.RemoveShellDevOverride(server.repoRoot)
		}
		if saveErr != nil {
			server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: saveErr.Error(),
				DenialCode:   DenialCodeExecutionFailed,
			})
			return
		}
		server.writeJSON(writer, http.StatusOK, shellDevResponseFor(req.Enabled))

	default:
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func shellDevResponseFor(enabled bool) havenShellDevResponse {
	resp := havenShellDevResponse{Enabled: enabled}
	if enabled {
		resp.Warning = havenShellDevWarningText
	}
	return resp
}

// ---------------------------------------------------------------------------
// Idle settings — GET and POST /v1/settings/idle
// ---------------------------------------------------------------------------

// havenIdleSettingsResponse is the JSON body for GET and POST /v1/settings/idle.
// The legacy /v1/haven/settings/idle alias uses the same payload.
type havenIdleSettingsResponse struct {
	IdleEnabled    bool `json:"idle_enabled"`
	AmbientEnabled bool `json:"ambient_enabled"`
}

type havenIdleSettingsUpdateRequest struct {
	IdleEnabled    bool `json:"idle_enabled"`
	AmbientEnabled bool `json:"ambient_enabled"`
}

// handleHavenSettingsIdle serves GET and POST /v1/settings/idle.
//
// GET  — returns current idle_enabled and ambient_enabled from haven_preferences.json.
// POST — updates them; changes are picked up by the idle manager within one tick (~30 s).
func (server *Server) handleHavenSettingsIdle(writer http.ResponseWriter, request *http.Request) {
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(tokenClaims.ActorLabel), "haven") {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "idle settings require actor haven",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return
	}

	switch request.Method {
	case http.MethodGet:
		if !server.requireControlCapability(writer, tokenClaims, controlCapabilityConfigRead) {
			return
		}
		if _, denial, ok := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !ok {
			server.writeJSON(writer, signedRequestHTTPStatus(denial.DenialCode), denial)
			return
		}
		response, err := server.readIdleSettings()
		if err != nil {
			server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeExecutionFailed,
			})
			return
		}
		server.writeJSON(writer, http.StatusOK, response)

	case http.MethodPost:
		if !server.requireControlCapability(writer, tokenClaims, controlCapabilityConfigWrite) {
			return
		}
		body, denial, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
		if !ok {
			server.writeJSON(writer, signedRequestHTTPStatus(denial.DenialCode), denial)
			return
		}
		var req havenIdleSettingsUpdateRequest
		if err := decodeJSONBytes(body, &req); err != nil {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusDenied,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
		if err := server.writeIdleSettings(req.IdleEnabled, req.AmbientEnabled); err != nil {
			server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeExecutionFailed,
			})
			return
		}
		server.writeJSON(writer, http.StatusOK, havenIdleSettingsResponse{
			IdleEnabled:    req.IdleEnabled,
			AmbientEnabled: req.AmbientEnabled,
		})

	default:
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (server *Server) readIdleSettings() (havenIdleSettingsResponse, error) {
	server.havenPreferencesMu.Lock()
	defer server.havenPreferencesMu.Unlock()
	prefs, err := server.loadHavenPrefs()
	if err != nil {
		return havenIdleSettingsResponse{}, err
	}
	return havenIdleSettingsResponse{
		IdleEnabled:    boolHavenPref(prefs, "idle_enabled", true),
		AmbientEnabled: boolHavenPref(prefs, "ambient_enabled", server.havenDefaultAmbientEnabled()),
	}, nil
}

func (server *Server) writeIdleSettings(idleEnabled, ambientEnabled bool) error {
	server.havenPreferencesMu.Lock()
	defer server.havenPreferencesMu.Unlock()
	prefs, err := server.loadHavenPrefs()
	if err != nil {
		return err
	}
	prefs["idle_enabled"] = idleEnabled
	prefs["ambient_enabled"] = ambientEnabled
	return server.saveHavenPrefs(prefs)
}

func (server *Server) loadHavenPrefs() (map[string]interface{}, error) {
	data, err := os.ReadFile(server.havenPrefsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]interface{}), nil
		}
		return nil, fmt.Errorf("read preferences: %w", err)
	}
	var prefs map[string]interface{}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return nil, fmt.Errorf("parse preferences: %w", err)
	}
	return prefs, nil
}

func (server *Server) saveHavenPrefs(prefs map[string]interface{}) error {
	path := server.havenPrefsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create preferences dir: %w", err)
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal preferences: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

func (server *Server) havenPrefsPath() string {
	return filepath.Join(server.repoRoot, "runtime", "state", "haven_preferences.json")
}

// havenDefaultAmbientEnabled preserves the historical default for ambient mode:
// ambient is off by default for Anthropic and non-loopback OpenAI-compatible.
func (server *Server) havenDefaultAmbientEnabled() bool {
	cfg, err := modelruntime.LoadPersistedConfig(modelruntime.ConfigPath(server.repoRoot))
	if err != nil {
		return true
	}
	if cfg.ProviderName == "anthropic" {
		return false
	}
	if cfg.ProviderName == "openai_compatible" && !modelruntime.IsLoopbackModelBaseURL(cfg.BaseURL) {
		return false
	}
	return true
}

func boolHavenPref(prefs map[string]interface{}, key string, def bool) bool {
	if v, ok := prefs[key].(bool); ok {
		return v
	}
	return def
}
