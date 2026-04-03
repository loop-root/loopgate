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

// havenShellDevResponse is the JSON body for GET and POST /v1/haven/settings/shell-dev.
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

// handleHavenSettingsShellDev serves GET and POST /v1/haven/settings/shell-dev.
//
// GET  — returns whether shell_exec is currently visible to Haven chat.
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
// Idle settings — GET and POST /v1/haven/settings/idle
// ---------------------------------------------------------------------------

// havenIdleSettingsResponse is the JSON body for GET and POST /v1/haven/settings/idle.
type havenIdleSettingsResponse struct {
	IdleEnabled    bool `json:"idle_enabled"`
	AmbientEnabled bool `json:"ambient_enabled"`
}

type havenIdleSettingsUpdateRequest struct {
	IdleEnabled    bool `json:"idle_enabled"`
	AmbientEnabled bool `json:"ambient_enabled"`
}

// handleHavenSettingsIdle serves GET and POST /v1/haven/settings/idle.
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
		if _, denial, ok := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !ok {
			server.writeJSON(writer, signedRequestHTTPStatus(denial.DenialCode), denial)
			return
		}
		server.writeJSON(writer, http.StatusOK, server.readIdleSettings())

	case http.MethodPost:
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

func (server *Server) readIdleSettings() havenIdleSettingsResponse {
	server.havenPreferencesMu.Lock()
	defer server.havenPreferencesMu.Unlock()
	prefs := server.loadHavenPrefs()
	return havenIdleSettingsResponse{
		IdleEnabled:    boolHavenPref(prefs, "idle_enabled", true),
		AmbientEnabled: boolHavenPref(prefs, "ambient_enabled", server.havenDefaultAmbientEnabled()),
	}
}

func (server *Server) writeIdleSettings(idleEnabled, ambientEnabled bool) error {
	server.havenPreferencesMu.Lock()
	defer server.havenPreferencesMu.Unlock()
	prefs := server.loadHavenPrefs()
	prefs["idle_enabled"] = idleEnabled
	prefs["ambient_enabled"] = ambientEnabled
	return server.saveHavenPrefs(prefs)
}

func (server *Server) loadHavenPrefs() map[string]interface{} {
	data, err := os.ReadFile(server.havenPrefsPath())
	if err != nil {
		return make(map[string]interface{})
	}
	var prefs map[string]interface{}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return make(map[string]interface{})
	}
	return prefs
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
	return os.WriteFile(path, data, 0o644)
}

func (server *Server) havenPrefsPath() string {
	return filepath.Join(server.repoRoot, "runtime", "state", "haven_preferences.json")
}

// havenDefaultAmbientEnabled mirrors the logic in cmd/haven settings.go:
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
