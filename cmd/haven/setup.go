package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"morph/internal/loopgate"
	modelruntime "morph/internal/modelruntime"
	setupwizard "morph/internal/setup"
)

// SetupStatus represents the current setup state of Haven.
type SetupStatus struct {
	NeedsSetup bool   `json:"needs_setup"`
	RepoRoot   string `json:"repo_root"`
}

// SetupRequest contains the wizard form data.
// First-run UI intentionally collects only model connection + folder grants; other fields
// use defaults here and can be changed later in Settings (see master plan Phase 3 Task 9).
type SetupRequest struct {
	ProviderName     string   `json:"provider_name"` // "anthropic" or "openai_compatible" (for Ollama)
	ModelName        string   `json:"model_name"`
	APIKey           string   `json:"api_key"`    // empty for loopback/local providers
	BaseURL          string   `json:"base_url"`   // e.g. http://localhost:11434/v1 for Ollama
	MorphName        string   `json:"morph_name"` // personalization
	Wallpaper        string   `json:"wallpaper"`
	GrantedFolderIDs []string `json:"granted_folder_ids"`
	AmbientEnabled   bool     `json:"ambient_enabled"`
	RunInBackground  bool     `json:"run_in_background"`
}

// SetupResponse is returned after setup completes.
type SetupResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// CheckSetup returns whether first-run setup is needed.
func (app *HavenApp) CheckSetup() SetupStatus {
	repoRoot := app.setupRepoRoot()
	prefs := app.loadPreferences()
	setupCompletedAt := strings.TrimSpace(stringOrDefault(prefs["setup_completed"], ""))
	needsSetup := setupCompletedAt == ""
	if !needsSetup {
		runtimeConfig, err := modelruntime.LoadPersistedConfig(modelruntime.ConfigPath(repoRoot))
		if err != nil {
			needsSetup = true
		} else if strings.TrimSpace(runtimeConfig.ProviderName) == "" || strings.TrimSpace(runtimeConfig.ModelName) == "" {
			needsSetup = true
		}
	}
	return SetupStatus{
		NeedsSetup: needsSetup,
		RepoRoot:   repoRoot,
	}
}

// DetectOllama checks if an Ollama instance is running locally.
func (app *HavenApp) DetectOllama() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:11434/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// ListLocalModels returns OpenAI-compatible loopback models available at the given base URL.
func (app *HavenApp) ListLocalModels(baseURL string) ([]string, error) {
	trimmedBaseURL := strings.TrimSpace(baseURL)
	if trimmedBaseURL == "" {
		trimmedBaseURL = "http://localhost:11434/v1"
	}

	requestContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	modelNames, err := setupwizard.ProbeOpenAICompatibleModels(requestContext, trimmedBaseURL)
	if err != nil {
		return nil, err
	}
	return modelNames, nil
}

// CompleteSetup writes the model runtime config and optional persona name.
func (app *HavenApp) CompleteSetup(req SetupRequest) SetupResponse {
	repoRoot := app.setupRepoRoot()

	runtimeConfig := modelruntime.Config{
		ProviderName:    strings.TrimSpace(req.ProviderName),
		ModelName:       strings.TrimSpace(req.ModelName),
		BaseURL:         strings.TrimSpace(req.BaseURL),
		Temperature:     0.7,
		MaxOutputTokens: 4096,
		Timeout:         120 * time.Second,
	}

	if runtimeConfig.ProviderName == "anthropic" {
		runtimeConfig.ModelConnectionID = setupModelConnectionID(runtimeConfig.ProviderName)
	}
	if runtimeConfig.ProviderName == "openai_compatible" && !modelruntime.IsLoopbackModelBaseURL(runtimeConfig.BaseURL) && strings.TrimSpace(req.APIKey) != "" {
		runtimeConfig.ModelConnectionID = setupModelConnectionID(runtimeConfig.ProviderName)
	}

	normalizedConfig, err := modelruntime.NormalizeConfig(runtimeConfig)
	if err != nil {
		return SetupResponse{Error: fmt.Sprintf("normalize setup config: %v", err)}
	}

	if normalizedConfig.ModelConnectionID != "" {
		if strings.TrimSpace(req.APIKey) == "" {
			return SetupResponse{Error: "api key is required for this provider"}
		}
		// Keys are stored only through Loopgate (OS secure store / keychain); see StoreModelConnection in loopgate.
		if _, err := app.loopgateClient.StoreModelConnection(context.Background(), loopgate.ModelConnectionStoreRequest{
			ConnectionID: normalizedConfig.ModelConnectionID,
			ProviderName: normalizedConfig.ProviderName,
			BaseURL:      normalizedConfig.BaseURL,
			SecretValue:  req.APIKey,
		}); err != nil {
			return SetupResponse{Error: fmt.Sprintf("store model credential: %v", err)}
		}
	}

	validatedConfig, err := app.loopgateClient.ValidateModelConfig(context.Background(), normalizedConfig)
	if err != nil {
		return SetupResponse{Error: fmt.Sprintf("validate setup config: %v", err)}
	}

	if err := modelruntime.SavePersistedConfig(modelruntime.ConfigPath(repoRoot), validatedConfig); err != nil {
		return SetupResponse{Error: fmt.Sprintf("save model runtime config: %v", err)}
	}

	folderAccessContext, cancelFolderAccess := withFolderAccessTimeout(context.Background())
	defer cancelFolderAccess()
	folderAccessStatus, err := app.loopgateClient.UpdateFolderAccess(folderAccessContext, loopgate.FolderAccessUpdateRequest{
		GrantedIDs: append([]string(nil), req.GrantedFolderIDs...),
	})
	if err != nil {
		return SetupResponse{Error: fmt.Sprintf("configure folder access: %v", err)}
	}

	prefs := app.loadPreferences()
	prefs["morph_name"] = defaultSetupMorphName(req.MorphName)
	prefs["wallpaper"] = defaultSetupWallpaper(req.Wallpaper)
	prefs["ambient_enabled"] = req.AmbientEnabled
	prefs["run_in_background"] = req.RunInBackground
	prefs["setup_completed"] = time.Now().UTC().Format(time.RFC3339)
	if err := app.savePreferences(prefs); err != nil {
		return SetupResponse{Error: fmt.Sprintf("save haven preferences: %v", err)}
	}

	if app.presence != nil {
		app.presence.mu.Lock()
		app.presence.morphName = defaultSetupMorphName(req.MorphName)
		app.presence.mu.Unlock()
	}
	if app.idleManager != nil {
		app.idleManager.mu.Lock()
		app.idleManager.ambientEnabled = req.AmbientEnabled
		app.idleManager.mu.Unlock()
	}

	if err := app.maybeCreateSetupDeskNote(folderAccessStatus); err != nil {
		return SetupResponse{Error: fmt.Sprintf("create onboarding note: %v", err)}
	}

	return SetupResponse{Success: true}
}

func (app *HavenApp) setupRepoRoot() string {
	if strings.TrimSpace(app.repoRoot) != "" {
		return app.repoRoot
	}
	repoRoot := os.Getenv("MORPH_REPO_ROOT")
	if repoRoot == "" {
		return "."
	}
	return repoRoot
}

func defaultSetupMorphName(rawName string) string {
	trimmedName := strings.TrimSpace(rawName)
	if trimmedName == "" {
		return "Morph"
	}
	return trimmedName
}

func setupModelConnectionID(providerName string) string {
	return fmt.Sprintf("%s:%d", strings.TrimSpace(providerName), time.Now().UTC().UnixNano())
}

func defaultSetupWallpaper(rawWallpaper string) string {
	trimmedWallpaper := strings.TrimSpace(rawWallpaper)
	if trimmedWallpaper == "" {
		return "sahara"
	}
	return trimmedWallpaper
}

func (app *HavenApp) maybeCreateSetupDeskNote(folderAccessStatus loopgate.FolderAccessStatusResponse) error {
	prefs := app.loadPreferences()
	if boolOrDefault(prefs["initial_offer_created"], false) {
		return nil
	}

	var draft *DeskNoteDraft
	for _, folderStatus := range folderAccessStatus.Folders {
		if !folderStatus.Granted || !folderStatus.HostExists || folderStatus.EntryCount <= 0 {
			continue
		}
		switch folderStatus.ID {
		case "downloads":
			draft = &DeskNoteDraft{
				Kind:  "reminder",
				Title: "Downloads could use a glance",
				Body:  fmt.Sprintf("I noticed %d item%s in Downloads. If you want, I can review the real folder, draft an organization plan, and ask for approval before changing anything.", folderStatus.EntryCount, pluralSuffix(folderStatus.EntryCount)),
				Action: &DeskNoteAction{
					Kind:    "send_message",
					Label:   "Yes, do it",
					Message: "Please look through my Downloads folder using host.folder.list. Categorize what you find and create an organization plan using host.organize.plan. Show me the plan before applying anything.",
				},
			}
		case "desktop":
			draft = &DeskNoteDraft{
				Kind:  "reminder",
				Title: "Your desktop is connected",
				Body:  fmt.Sprintf("I can now review your real Desktop folder. There %s %d item%s there if you want me to draft a cleanup plan and ask before making changes.", pluralVerb(folderStatus.EntryCount), folderStatus.EntryCount, pluralSuffix(folderStatus.EntryCount)),
				Action: &DeskNoteAction{
					Kind:    "send_message",
					Label:   "Yes, do it",
					Message: "Please review my Desktop folder using host.folder.list. Note what looks cluttered or out of place, create a cleanup plan using host.organize.plan, and show me the plan before applying anything.",
				},
			}
		case "documents":
			draft = &DeskNoteDraft{
				Kind:  "reminder",
				Title: "Documents are ready when you are",
				Body:  fmt.Sprintf("I can now review a mirrored copy of Documents in Haven. There %s %d item%s there if you want help finding a starting point.", pluralVerb(folderStatus.EntryCount), folderStatus.EntryCount, pluralSuffix(folderStatus.EntryCount)),
				Action: &DeskNoteAction{
					Kind:    "send_message",
					Label:   "Yes, do it",
					Message: "Please take a first pass through the mirrored Documents folder in Haven. Surface likely starting points, summarize what stands out, and ask me one short question if you need direction.",
				},
			}
		}
		if draft != nil {
			break
		}
	}

	if draft == nil {
		return nil
	}
	if _, err := app.createDeskNote(*draft); err != nil {
		return err
	}
	prefs["initial_offer_created"] = true
	return app.savePreferences(prefs)
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func pluralVerb(count int) string {
	if count == 1 {
		return "is"
	}
	return "are"
}
