package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"morph/internal/loopgate"
	modelruntime "morph/internal/modelruntime"
)

func TestCompleteSetupAnthropicStoresModelConnectionAndPersistsRuntimeConfig(t *testing.T) {
	client := &fakeLoopgateClient{}
	app, _ := testApp(t, client)

	var storedRequest loopgate.ModelConnectionStoreRequest
	var folderAccessRequest loopgate.FolderAccessUpdateRequest
	storeCalls := 0
	client.storeModelConnectionFn = func(_ context.Context, request loopgate.ModelConnectionStoreRequest) (loopgate.ModelConnectionStatus, error) {
		storeCalls++
		storedRequest = request
		return loopgate.ModelConnectionStatus{
			ConnectionID:     request.ConnectionID,
			ProviderName:     request.ProviderName,
			BaseURL:          request.BaseURL,
			SecureStoreRefID: "model-" + request.ConnectionID,
		}, nil
	}

	validateCalls := 0
	client.validateModelConfigFn = func(_ context.Context, runtimeConfig modelruntime.Config) (modelruntime.Config, error) {
		validateCalls++
		if runtimeConfig.ProviderName != "anthropic" {
			t.Fatalf("unexpected provider name: %q", runtimeConfig.ProviderName)
		}
		if runtimeConfig.ModelConnectionID == "" {
			t.Fatal("expected model connection id to be set")
		}
		if runtimeConfig.APIKeyEnvVar != "" {
			t.Fatalf("expected no legacy api key env var, got %q", runtimeConfig.APIKeyEnvVar)
		}
		return runtimeConfig, nil
	}
	client.updateFolderAccessFn = func(_ context.Context, request loopgate.FolderAccessUpdateRequest) (loopgate.FolderAccessStatusResponse, error) {
		folderAccessRequest = request
		return loopgate.FolderAccessStatusResponse{}, nil
	}

	response := app.CompleteSetup(SetupRequest{
		ProviderName:     "anthropic",
		ModelName:        "claude-sonnet-4-5",
		APIKey:           "sk-ant-test-secret",
		MorphName:        "Nova",
		Wallpaper:        "harbor",
		GrantedFolderIDs: []string{"downloads", "documents"},
		AmbientEnabled:   false,
		RunInBackground:  true,
	})
	if !response.Success {
		t.Fatalf("expected setup success, got error: %s", response.Error)
	}
	if storeCalls != 1 {
		t.Fatalf("expected 1 stored model connection call, got %d", storeCalls)
	}
	if validateCalls != 1 {
		t.Fatalf("expected 1 validate call, got %d", validateCalls)
	}
	if len(folderAccessRequest.GrantedIDs) != 2 || folderAccessRequest.GrantedIDs[0] != "downloads" || folderAccessRequest.GrantedIDs[1] != "documents" {
		t.Fatalf("unexpected granted folder request: %#v", folderAccessRequest)
	}
	if storedRequest.ProviderName != "anthropic" {
		t.Fatalf("unexpected stored provider: %q", storedRequest.ProviderName)
	}
	if storedRequest.BaseURL != "https://api.anthropic.com/v1" {
		t.Fatalf("unexpected stored base url: %q", storedRequest.BaseURL)
	}
	if storedRequest.SecretValue != "sk-ant-test-secret" {
		t.Fatalf("unexpected stored secret value: %q", storedRequest.SecretValue)
	}
	if storedRequest.ConnectionID == "" {
		t.Fatal("expected stored connection id to be populated")
	}

	persistedConfig, err := modelruntime.LoadPersistedConfig(modelruntime.ConfigPath(app.repoRoot))
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if persistedConfig.ProviderName != "anthropic" {
		t.Fatalf("unexpected persisted provider: %q", persistedConfig.ProviderName)
	}
	if persistedConfig.ModelConnectionID != storedRequest.ConnectionID {
		t.Fatalf("unexpected persisted model connection id: got %q want %q", persistedConfig.ModelConnectionID, storedRequest.ConnectionID)
	}
	if persistedConfig.APIKeyEnvVar != "" {
		t.Fatalf("expected no persisted api key env var, got %q", persistedConfig.APIKeyEnvVar)
	}

	prefs := app.loadPreferences()
	if prefs["morph_name"] != "Nova" {
		t.Fatalf("unexpected morph name preference: %#v", prefs["morph_name"])
	}
	if prefs["wallpaper"] != "harbor" {
		t.Fatalf("unexpected wallpaper preference: %#v", prefs["wallpaper"])
	}
	if prefs["run_in_background"] != true {
		t.Fatalf("unexpected run_in_background preference: %#v", prefs["run_in_background"])
	}
	if prefs["ambient_enabled"] != false {
		t.Fatalf("unexpected ambient_enabled preference: %#v", prefs["ambient_enabled"])
	}
}

func TestCompleteSetupLoopbackModelSkipsSecureStorage(t *testing.T) {
	client := &fakeLoopgateClient{}
	app, _ := testApp(t, client)

	storeCalls := 0
	client.storeModelConnectionFn = func(_ context.Context, request loopgate.ModelConnectionStoreRequest) (loopgate.ModelConnectionStatus, error) {
		storeCalls++
		return loopgate.ModelConnectionStatus{ConnectionID: request.ConnectionID}, nil
	}

	response := app.CompleteSetup(SetupRequest{
		ProviderName:   "openai_compatible",
		ModelName:      "llama3",
		BaseURL:        "http://localhost:11434/v1",
		MorphName:      "Morph",
		AmbientEnabled: true,
	})
	if !response.Success {
		t.Fatalf("expected setup success, got error: %s", response.Error)
	}
	if storeCalls != 0 {
		t.Fatalf("expected loopback setup to skip secure model connection storage, got %d calls", storeCalls)
	}

	persistedConfig, err := modelruntime.LoadPersistedConfig(modelruntime.ConfigPath(app.repoRoot))
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if persistedConfig.ModelConnectionID != "" {
		t.Fatalf("expected no model connection id for loopback setup, got %q", persistedConfig.ModelConnectionID)
	}
	if persistedConfig.BaseURL != "http://localhost:11434/v1" {
		t.Fatalf("unexpected persisted base url: %q", persistedConfig.BaseURL)
	}
}

func TestCompleteSetupRejectsMissingRemoteAPIKey(t *testing.T) {
	client := &fakeLoopgateClient{}
	app, _ := testApp(t, client)

	response := app.CompleteSetup(SetupRequest{
		ProviderName: "anthropic",
		ModelName:    "claude-sonnet-4-5",
	})
	if response.Success {
		t.Fatal("expected setup failure for missing api key")
	}
	if response.Error == "" {
		t.Fatal("expected setup failure message")
	}
}

func TestCompleteSetupCreatesDownloadsOfferWhenFolderGrantHasItems(t *testing.T) {
	client := &fakeLoopgateClient{}
	app, _ := testApp(t, client)

	client.updateFolderAccessFn = func(_ context.Context, request loopgate.FolderAccessUpdateRequest) (loopgate.FolderAccessStatusResponse, error) {
		if len(request.GrantedIDs) != 1 || request.GrantedIDs[0] != "downloads" {
			t.Fatalf("unexpected granted ids: %#v", request.GrantedIDs)
		}
		return loopgate.FolderAccessStatusResponse{
			Folders: []loopgate.FolderAccessStatus{
				{
					ID:         "downloads",
					Name:       "Downloads",
					Granted:    true,
					HostExists: true,
					EntryCount: 12,
				},
			},
		}, nil
	}

	response := app.CompleteSetup(SetupRequest{
		ProviderName:     "openai_compatible",
		ModelName:        "llama3",
		BaseURL:          "http://localhost:11434/v1",
		GrantedFolderIDs: []string{"downloads"},
		AmbientEnabled:   true,
	})
	if !response.Success {
		t.Fatalf("expected setup success, got error: %s", response.Error)
	}

	deskNotes, err := app.ListDeskNotes()
	if err != nil {
		t.Fatalf("list desk notes: %v", err)
	}
	if len(deskNotes) != 1 {
		t.Fatalf("expected one onboarding desk note, got %d", len(deskNotes))
	}
	if !strings.Contains(deskNotes[0].Title, "Downloads") {
		t.Fatalf("unexpected onboarding note title: %q", deskNotes[0].Title)
	}
	if deskNotes[0].Action == nil {
		t.Fatal("expected onboarding note action to be present")
	}
	if !strings.Contains(deskNotes[0].Action.Message, "host.organize.plan") {
		t.Fatalf("expected onboarding note to route through host organize planning, got %q", deskNotes[0].Action.Message)
	}
	if strings.Contains(deskNotes[0].Action.Message, "without touching the originals") {
		t.Fatalf("expected onboarding note to permit host-file changes via approval flow, got %q", deskNotes[0].Action.Message)
	}
	prefs := app.loadPreferences()
	if prefs["initial_offer_created"] != true {
		t.Fatalf("expected initial_offer_created preference, got %#v", prefs["initial_offer_created"])
	}
}

func TestCompleteSetupUsesExtendedTimeoutForFolderAccess(t *testing.T) {
	client := &fakeLoopgateClient{}
	app, _ := testApp(t, client)

	client.updateFolderAccessFn = func(ctx context.Context, request loopgate.FolderAccessUpdateRequest) (loopgate.FolderAccessStatusResponse, error) {
		if len(request.GrantedIDs) != 1 || request.GrantedIDs[0] != "downloads" {
			t.Fatalf("unexpected granted ids: %#v", request.GrantedIDs)
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("expected folder access update context to carry a deadline")
		}
		remaining := time.Until(deadline)
		if remaining < 110*time.Second || remaining > 121*time.Second {
			t.Fatalf("expected extended folder access timeout, got remaining=%s", remaining)
		}
		return loopgate.FolderAccessStatusResponse{}, nil
	}

	response := app.CompleteSetup(SetupRequest{
		ProviderName:     "openai_compatible",
		ModelName:        "llama3",
		BaseURL:          "http://localhost:11434/v1",
		GrantedFolderIDs: []string{"downloads"},
		AmbientEnabled:   true,
	})
	if !response.Success {
		t.Fatalf("expected setup success, got error: %s", response.Error)
	}
}

func TestCheckSetup_RequiresOnboardingWithoutCompletedPreferences(t *testing.T) {
	client := &fakeLoopgateClient{}
	app, _ := testApp(t, client)

	if err := modelruntime.SavePersistedConfig(modelruntime.ConfigPath(app.repoRoot), modelruntime.Config{
		ProviderName:    "openai_compatible",
		ModelName:       "llama3",
		BaseURL:         "http://localhost:11434/v1",
		Temperature:     0.7,
		MaxOutputTokens: 4096,
		Timeout:         120 * time.Second,
	}); err != nil {
		t.Fatalf("save persisted config: %v", err)
	}

	status := app.CheckSetup()
	if !status.NeedsSetup {
		t.Fatal("expected setup to still be required until onboarding preferences are completed")
	}
}

func TestCheckSetup_DoesNotRequireOnboardingAfterCompletion(t *testing.T) {
	client := &fakeLoopgateClient{}
	app, _ := testApp(t, client)

	if err := modelruntime.SavePersistedConfig(modelruntime.ConfigPath(app.repoRoot), modelruntime.Config{
		ProviderName:    "openai_compatible",
		ModelName:       "llama3",
		BaseURL:         "http://localhost:11434/v1",
		Temperature:     0.7,
		MaxOutputTokens: 4096,
		Timeout:         120 * time.Second,
	}); err != nil {
		t.Fatalf("save persisted config: %v", err)
	}
	if err := app.savePreferences(map[string]interface{}{
		"setup_completed": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("save preferences: %v", err)
	}

	status := app.CheckSetup()
	if status.NeedsSetup {
		t.Fatal("expected completed onboarding to suppress setup wizard")
	}
}
