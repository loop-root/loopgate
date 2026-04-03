package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	modelruntime "morph/internal/modelruntime"
)

// HavenSettings holds user-configurable settings.
type HavenSettings struct {
	MorphName      string `json:"morph_name"`
	Wallpaper      string `json:"wallpaper"`
	IdleEnabled    bool   `json:"idle_enabled"`
	AmbientEnabled bool   `json:"ambient_enabled"`
}

// SaveSettingsResult is returned by SaveSettings.
type SaveSettingsResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// GetSettings returns the current settings from preferences.
func (app *HavenApp) GetSettings() HavenSettings {
	prefs := app.loadPreferences()
	return HavenSettings{
		MorphName:      stringOrDefault(prefs["morph_name"], "Morph"),
		Wallpaper:      stringOrDefault(prefs["wallpaper"], "sahara"),
		IdleEnabled:    boolOrDefault(prefs["idle_enabled"], true),
		AmbientEnabled: app.defaultAmbientEnabled(prefs),
	}
}

// SaveSettings persists settings and applies in-memory changes.
func (app *HavenApp) SaveSettings(req HavenSettings) SaveSettingsResult {
	prefs := app.loadPreferences()
	prefs["morph_name"] = req.MorphName
	prefs["wallpaper"] = req.Wallpaper
	prefs["idle_enabled"] = req.IdleEnabled
	prefs["ambient_enabled"] = req.AmbientEnabled

	if err := app.savePreferences(prefs); err != nil {
		return SaveSettingsResult{Error: err.Error()}
	}

	// Apply in-memory: update presence manager's Morph name.
	if app.presence != nil && req.MorphName != "" {
		app.presence.mu.Lock()
		app.presence.morphName = req.MorphName
		app.presence.mu.Unlock()
	}

	// Apply in-memory: toggle idle behavior.
	if app.idleManager != nil {
		app.idleManager.mu.Lock()
		app.idleManager.enabled = req.IdleEnabled
		app.idleManager.ambientEnabled = req.AmbientEnabled
		app.idleManager.mu.Unlock()
	}

	return SaveSettingsResult{Success: true}
}

func (app *HavenApp) loadPreferences() map[string]interface{} {
	data, err := os.ReadFile(app.preferencesPath())
	if err != nil {
		return make(map[string]interface{})
	}
	var prefs map[string]interface{}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return make(map[string]interface{})
	}
	return prefs
}

func (app *HavenApp) savePreferences(prefs map[string]interface{}) error {
	path := app.preferencesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create preferences dir: %w", err)
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal preferences: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

func (app *HavenApp) preferencesPath() string {
	return filepath.Join(app.repoRoot, "runtime", "state", "haven_preferences.json")
}

func stringOrDefault(v interface{}, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func boolOrDefault(v interface{}, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

func (app *HavenApp) defaultAmbientEnabled(prefs map[string]interface{}) bool {
	if ambientEnabled, ok := prefs["ambient_enabled"].(bool); ok {
		return ambientEnabled
	}

	runtimeConfig, err := modelruntime.LoadPersistedConfig(modelruntime.ConfigPath(app.setupRepoRoot()))
	if err != nil {
		return true
	}
	if runtimeConfig.ProviderName == "anthropic" {
		return false
	}
	if runtimeConfig.ProviderName == "openai_compatible" && !modelruntime.IsLoopbackModelBaseURL(runtimeConfig.BaseURL) {
		return false
	}
	return true
}
