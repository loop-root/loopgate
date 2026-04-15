package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/audit"
	"loopgate/internal/config"
	"loopgate/internal/ledger"
	"loopgate/internal/loopgate"
	modelruntime "loopgate/internal/modelruntime"
	"loopgate/internal/sandbox"
)

func TestApprovalPreview_HidesSensitiveContent(t *testing.T) {
	preview, hidden := approvalPreview("token=super-secret-value", 100)
	if !hidden {
		t.Fatal("expected hidden=true for sensitive content")
	}
	if preview != "" {
		t.Fatalf("expected empty preview when hidden, got %q", preview)
	}
}

func TestApprovalPreview_TruncatesLongContent(t *testing.T) {
	preview, hidden := approvalPreview("abcdefghijklmnopqrstuvwxyz", 10)
	if hidden {
		t.Fatal("expected hidden=false for non-sensitive content")
	}
	if preview != "abcdefghij... (truncated)" {
		t.Fatalf("unexpected preview: %q", preview)
	}
}

func TestFormatCapabilityResponse_DoesNotExposeApprovalDecisionNonce(t *testing.T) {
	formattedResponse := formatCapabilityResponse(loopgate.CapabilityResponse{
		Status: loopgate.ResponseStatusPendingApproval,
		Metadata: map[string]interface{}{
			"path":                    "guarded.txt",
			"approval_reason":         "write requires approval",
			"approval_decision_nonce": "super-secret-nonce",
		},
	})
	if strings.Contains(formattedResponse, "super-secret-nonce") {
		t.Fatalf("expected formatted response to omit approval decision nonce, got %q", formattedResponse)
	}
}

func TestFormatCapabilityResponse_AuditOnlyResultsDoNotRenderNormalOutput(t *testing.T) {
	formattedResponse := formatCapabilityResponse(loopgate.CapabilityResponse{
		Status:           loopgate.ResponseStatusSuccess,
		StructuredResult: map[string]interface{}{"output": "should not render"},
		FieldsMeta: map[string]loopgate.ResultFieldMetadata{
			"output": {
				Origin:         loopgate.ResultFieldOriginRemote,
				ContentType:    "text/plain",
				Trust:          loopgate.ResultFieldTrustDeterministic,
				Sensitivity:    loopgate.ResultFieldSensitivityTaintedText,
				SizeBytes:      len("should not render"),
				Kind:           loopgate.ResultFieldKindScalar,
				PromptEligible: false,
				MemoryEligible: false,
			},
		},
		Classification: loopgate.ResultClassification{
			Exposure: loopgate.ResultExposureAudit,
			Quarantine: loopgate.ResultQuarantine{
				Quarantined: true,
				Ref:         "quarantine://raw/http/1",
			},
		},
		QuarantineRef: "quarantine://raw/http/1",
	})
	if !strings.Contains(formattedResponse, "audit only") {
		t.Fatalf("expected audit-only message, got %q", formattedResponse)
	}
	if strings.Contains(formattedResponse, "should not render") {
		t.Fatalf("expected audit-only result to avoid rendering normal output, got %q", formattedResponse)
	}
}

func TestFormatCapabilityResponse_InvalidClassificationFailsClosed(t *testing.T) {
	formattedResponse := formatCapabilityResponse(loopgate.CapabilityResponse{
		Status:           loopgate.ResponseStatusSuccess,
		StructuredResult: map[string]interface{}{"content": "unsafe"},
		Classification: loopgate.ResultClassification{
			Exposure: loopgate.ResultExposureDisplay,
			Eligibility: loopgate.ResultEligibility{
				Prompt: true,
			},
		},
	})
	if !strings.Contains(formattedResponse, "invalid result classification") {
		t.Fatalf("expected invalid-classification error, got %q", formattedResponse)
	}
}

func TestHandleCommand_WriteUsesHardenedPath(t *testing.T) {
	repoRoot := t.TempDir()
	client, status := startTestLoopgate(t, repoRoot, testPolicyYAML(false))
	policy := status.Policy
	sandboxPaths := sandbox.PathsForRepo(repoRoot)

	commandResult := HandleCommand(CommandContext{
		RepoRoot:             repoRoot,
		Policy:               policy,
		CurrentRuntimeConfig: modelruntime.Config{},
		LoopgateClient:       client,
		LoopgateStatus:       status,
	}, "/write output.txt hello", nil, nil)
	if !commandResult.Handled {
		t.Fatal("expected command to be handled")
	}
	if !commandResult.ToolEventSeen {
		t.Fatal("expected write command to emit a tool event")
	}
	if !strings.Contains(commandResult.Output, "wrote 5 bytes to output.txt") {
		t.Fatalf("unexpected command output: %q", commandResult.Output)
	}

	writtenBytes, err := os.ReadFile(filepath.Join(sandboxPaths.Home, "output.txt"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(writtenBytes) != "hello" {
		t.Fatalf("unexpected file contents: %q", string(writtenBytes))
	}

	fileInfo, err := os.Stat(filepath.Join(sandboxPaths.Home, "output.txt"))
	if err != nil {
		t.Fatalf("stat written file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("expected hardened write permissions 0600, got %o", fileInfo.Mode().Perm())
	}
}

func TestHandleCommand_WriteDeniesMissingParentDirectory(t *testing.T) {
	repoRoot := t.TempDir()
	client, status := startTestLoopgate(t, repoRoot, testPolicyYAML(false))
	policy := status.Policy

	commandResult := HandleCommand(CommandContext{
		RepoRoot:             repoRoot,
		Policy:               policy,
		CurrentRuntimeConfig: modelruntime.Config{},
		LoopgateClient:       client,
		LoopgateStatus:       status,
	}, "/write missing/child.txt hello", nil, nil)
	if !commandResult.Handled {
		t.Fatal("expected command to be handled")
	}
	if !commandResult.ToolEventSeen {
		t.Fatal("expected denied write to emit a tool event")
	}
	if !strings.Contains(commandResult.Output, "parent_directory_not_resolved") {
		t.Fatalf("expected missing parent denial, got %q", commandResult.Output)
	}
}

func TestHandleCommand_WriteCannotSelfAuthorizeApprovalGatedAction(t *testing.T) {
	repoRoot := t.TempDir()
	client, status := startTestLoopgate(t, repoRoot, testPolicyYAML(true))

	commandResult := HandleCommand(CommandContext{
		RepoRoot:             repoRoot,
		Policy:               status.Policy,
		CurrentRuntimeConfig: modelruntime.Config{},
		LoopgateClient:       client,
		LoopgateStatus:       status,
	}, "/write guarded.txt hello", nil, nil)
	if !commandResult.Handled {
		t.Fatal("expected command to be handled")
	}
	if !strings.Contains(commandResult.Output, "requires approval") {
		t.Fatalf("expected approval denial, got %q", commandResult.Output)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "guarded.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected guarded file to remain unwritten, stat err=%v", err)
	}
}

func TestHandleCommand_SetupRequiresInteractivePrompt(t *testing.T) {
	repoRoot := t.TempDir()
	commandResult := HandleCommand(CommandContext{
		RepoRoot:             repoRoot,
		Policy:               config.Policy{},
		CurrentRuntimeConfig: modelruntime.Config{},
	}, "/setup", nil, nil)
	if !commandResult.Handled {
		t.Fatal("expected setup command to be handled")
	}
	if !strings.Contains(commandResult.Output, "interactive terminal prompt") {
		t.Fatalf("unexpected setup denial: %q", commandResult.Output)
	}
}

func TestHandleCommand_ModelShowsRuntimeSummary(t *testing.T) {
	repoRoot := t.TempDir()
	policy := config.Policy{}
	commandResult := HandleCommand(CommandContext{
		RepoRoot: repoRoot,
		Persona: config.Persona{
			Name:    "Loopgate",
			Version: "0.2.0",
		},
		Policy: policy,
		CurrentRuntimeConfig: modelruntime.Config{
			ProviderName:    "stub",
			ModelName:       "stub",
			MaxOutputTokens: 1024,
		},
	}, "/model", nil, nil)

	if !commandResult.Handled {
		t.Fatal("expected /model to be handled")
	}
	if !strings.Contains(commandResult.Output, "provider: stub") {
		t.Fatalf("expected model summary, got %q", commandResult.Output)
	}
}

func TestHandleCommand_ModelValidateUsesLoopgateValidation(t *testing.T) {
	repoRoot := t.TempDir()
	client, status := startTestLoopgate(t, repoRoot, testPolicyYAML(false))

	commandResult := HandleCommand(CommandContext{
		RepoRoot: repoRoot,
		Policy:   status.Policy,
		CurrentRuntimeConfig: modelruntime.Config{
			ProviderName: "stub",
			ModelName:    "stub",
		},
		LoopgateClient: client,
		LoopgateStatus: status,
	}, "/model validate", nil, nil)

	if !commandResult.Handled {
		t.Fatal("expected /model validate to be handled")
	}
	if !strings.Contains(commandResult.Output, "model config validated by Loopgate") {
		t.Fatalf("expected loopgate-backed validation summary, got %q", commandResult.Output)
	}
}

func TestHandleCommand_PersonaShowsTrustSummary(t *testing.T) {
	repoRoot := t.TempDir()
	policy := config.Policy{}
	persona := config.Persona{}
	persona.Name = "Loopgate"
	persona.Version = "0.2.0"
	persona.Communication.Tone = "calm"
	persona.Personality.Helpfulness = "high"
	persona.Personality.Honesty = "strict"
	persona.Personality.SafetyMindset = "high"
	persona.Personality.SecurityMindset = "high"
	persona.Personality.Directness = "high"
	persona.Personality.Pragmatism = "high"
	persona.Personality.Skepticism = "high"
	persona.Trust.TreatToolOutputAsUntrusted = true
	persona.Communication.StateUnknownsExplicitly = true
	persona.Communication.AvoidSpeculation = true

	commandResult := HandleCommand(CommandContext{
		RepoRoot: repoRoot,
		Persona:  persona,
		Policy:   policy,
	}, "/persona", nil, nil)
	if !strings.Contains(commandResult.Output, "treat_tool_output_as_untrusted: true") {
		t.Fatalf("expected persona trust summary, got %q", commandResult.Output)
	}
}

func TestHandleCommand_ToolsListsRegisteredTools(t *testing.T) {
	repoRoot := t.TempDir()
	client, status := startTestLoopgate(t, repoRoot, testPolicyYAML(false))
	policy := status.Policy
	commandResult := HandleCommand(CommandContext{
		RepoRoot:       repoRoot,
		Policy:         policy,
		LoopgateClient: client,
		LoopgateStatus: status,
	}, "/tools", nil, nil)
	if !strings.Contains(commandResult.Output, "fs_read: category=filesystem operation=read") {
		t.Fatalf("expected registered tool listing, got %q", commandResult.Output)
	}
}

func TestHandleCommand_SiteInspectSuggestsHTTPSWhenSchemeMissing(t *testing.T) {
	repoRoot := t.TempDir()
	client, status := startTestLoopgate(t, repoRoot, testPolicyYAML(false))
	commandResult := HandleCommand(CommandContext{
		RepoRoot:             repoRoot,
		Policy:               status.Policy,
		CurrentRuntimeConfig: modelruntime.Config{},
		LoopgateClient:       client,
		LoopgateStatus:       status,
	}, "/site inspect www.google.com", nil, nil)
	if !commandResult.Handled {
		t.Fatal("expected /site inspect to be handled")
	}
	if !strings.Contains(commandResult.Output, "Try: /site inspect https://www.google.com") {
		t.Fatalf("expected https guidance, got %q", commandResult.Output)
	}
}
func TestHandleCommand_ConnectionsShowsConfiguredConnections(t *testing.T) {
	repoRoot := t.TempDir()
	client, status := startTestLoopgate(t, repoRoot, testPolicyYAML(false))
	status.Connections = []loopgate.ConnectionStatus{{
		Provider:         "example",
		GrantType:        loopgate.GrantTypeClientCredentials,
		Subject:          "repo-bot",
		Scopes:           []string{"repo.read"},
		Status:           "stored",
		SecureStoreRefID: "example-secret-ref",
	}}

	commandResult := HandleCommand(CommandContext{
		RepoRoot:       repoRoot,
		Policy:         status.Policy,
		LoopgateClient: client,
		LoopgateStatus: status,
	}, "/connections", nil, nil)
	if !strings.Contains(commandResult.Output, "example/repo-bot: grant_type=client_credentials status=stored") {
		t.Fatalf("expected connection summary, got %q", commandResult.Output)
	}
}

func TestHandleCommand_SiteInspectShowsTrustDraftDetails(t *testing.T) {
	repoRoot := t.TempDir()
	client, status := startTestLoopgate(t, repoRoot, testPolicyYAML(false))
	status.Connections = nil

	commandResult := HandleCommand(CommandContext{
		RepoRoot: repoRoot,
		Policy:   status.Policy,
		LoopgateClient: &stubSiteClient{
			ControlPlaneClient: client,
			inspectionResponse: loopgate.SiteInspectionResponse{
				NormalizedURL:     "https://status.example.com/",
				Scheme:            "https",
				Host:              "status.example.com",
				Path:              "/",
				HTTPStatusCode:    200,
				ContentType:       "application/json",
				HTTPS:             true,
				TLSValid:          true,
				TrustDraftAllowed: true,
				DraftSuggestion: &loopgate.SiteTrustDraftSuggestion{
					Provider:       "status.example.com",
					Subject:        "root",
					CapabilityName: "status.example.com.status_get",
					ContentClass:   "structured_json",
					Extractor:      "json_nested_selector",
					CapabilityPath: "/",
				},
			},
		},
		LoopgateStatus: status,
	}, "/site inspect https://status.example.com/", nil, nil)
	if !strings.Contains(commandResult.Output, "trust_draft_allowed: true") {
		t.Fatalf("expected trust-draft detail, got %q", commandResult.Output)
	}
	if !strings.Contains(commandResult.Output, "suggested_extractor: json_nested_selector") {
		t.Fatalf("expected extractor detail, got %q", commandResult.Output)
	}
}

func TestHandleCommand_SiteTrustDraftRequiresInteractivePrompt(t *testing.T) {
	repoRoot := t.TempDir()
	client, status := startTestLoopgate(t, repoRoot, testPolicyYAML(false))

	commandResult := HandleCommand(CommandContext{
		RepoRoot: repoRoot,
		Policy:   status.Policy,
		LoopgateClient: &stubSiteClient{
			ControlPlaneClient: client,
			inspectionResponse: loopgate.SiteInspectionResponse{
				NormalizedURL:     "https://status.example.com/",
				Scheme:            "https",
				Host:              "status.example.com",
				Path:              "/",
				HTTPStatusCode:    200,
				ContentType:       "text/html",
				HTTPS:             true,
				TLSValid:          true,
				TrustDraftAllowed: true,
				DraftSuggestion: &loopgate.SiteTrustDraftSuggestion{
					Provider:       "status.example.com",
					Subject:        "root",
					CapabilityName: "status.example.com.page_get",
					ContentClass:   "html",
					Extractor:      "html_meta_allowlist",
					CapabilityPath: "/",
				},
			},
		},
		LoopgateStatus: status,
	}, "/site trust-draft https://status.example.com/", nil, nil)
	if !strings.Contains(commandResult.Output, "requires an interactive terminal prompt") {
		t.Fatalf("expected interactive prompt denial, got %q", commandResult.Output)
	}
}

func TestHandleCommand_SandboxImportCopiesIntoSandbox(t *testing.T) {
	repoRoot := t.TempDir()
	client, status := startTestLoopgate(t, repoRoot, testPolicyYAML(false))
	hostRootPath := t.TempDir()
	configureHavenSandboxSession(client, status, "shell-sandbox-import", hostRootPath)
	hostSourcePath := filepath.Join(hostRootPath, "notes.txt")
	if err := os.WriteFile(hostSourcePath, []byte("hello sandbox"), 0o600); err != nil {
		t.Fatalf("write host source: %v", err)
	}

	commandResult := HandleCommand(CommandContext{
		RepoRoot:       repoRoot,
		Policy:         status.Policy,
		LoopgateClient: client,
		LoopgateStatus: status,
	}, fmt.Sprintf("/sandbox import %s", hostSourcePath), nil, nil)
	if !commandResult.Handled {
		t.Fatal("expected command to be handled")
	}
	if !strings.Contains(commandResult.Output, "action: import") {
		t.Fatalf("expected import output, got %q", commandResult.Output)
	}
	importedPath := filepath.Join(repoRoot, "runtime", "sandbox", "root", "home", "imports", "notes.txt")
	importedBytes, err := os.ReadFile(importedPath)
	if err != nil {
		t.Fatalf("read imported path: %v", err)
	}
	if string(importedBytes) != "hello sandbox" {
		t.Fatalf("unexpected imported contents: %q", string(importedBytes))
	}
}

func TestHandleCommand_SandboxStageCopiesIntoOutputs(t *testing.T) {
	repoRoot := t.TempDir()
	client, status := startTestLoopgate(t, repoRoot, testPolicyYAML(false))
	hostRootPath := t.TempDir()
	configureHavenSandboxSession(client, status, "shell-sandbox-stage", hostRootPath)
	hostSourcePath := filepath.Join(hostRootPath, "stage-me.txt")
	if err := os.WriteFile(hostSourcePath, []byte("stage contents"), 0o600); err != nil {
		t.Fatalf("write host source: %v", err)
	}
	if _, err := client.SandboxImport(context.Background(), loopgate.SandboxImportRequest{
		HostSourcePath:  hostSourcePath,
		DestinationName: "stage-me.txt",
	}); err != nil {
		t.Fatalf("sandbox import: %v", err)
	}

	commandResult := HandleCommand(CommandContext{
		RepoRoot:       repoRoot,
		Policy:         status.Policy,
		LoopgateClient: client,
		LoopgateStatus: status,
	}, "/sandbox stage /morph/home/imports/stage-me.txt staged.txt", nil, nil)
	if !commandResult.Handled {
		t.Fatal("expected command to be handled")
	}
	if !strings.Contains(commandResult.Output, "action: stage") {
		t.Fatalf("expected stage output, got %q", commandResult.Output)
	}
	if strings.Contains(commandResult.Output, "runtime/sandbox/root/home") {
		t.Fatalf("expected virtual sandbox paths only, got %q", commandResult.Output)
	}
	if !strings.Contains(commandResult.Output, "sandbox_absolute_path: /morph/home/outputs/staged.txt") {
		t.Fatalf("expected virtual sandbox path in output, got %q", commandResult.Output)
	}
	stagedPath := filepath.Join(repoRoot, "runtime", "sandbox", "root", "home", "outputs", "staged.txt")
	stagedBytes, err := os.ReadFile(stagedPath)
	if err != nil {
		t.Fatalf("read staged output: %v", err)
	}
	if string(stagedBytes) != "stage contents" {
		t.Fatalf("unexpected staged contents: %q", string(stagedBytes))
	}
	if !strings.Contains(commandResult.Output, "artifact_ref: staged://artifacts/") {
		t.Fatalf("expected staged artifact ref in output, got %q", commandResult.Output)
	}
}

func TestHandleCommand_SandboxMetadataShowsStagedArtifact(t *testing.T) {
	repoRoot := t.TempDir()
	client, status := startTestLoopgate(t, repoRoot, testPolicyYAML(false))
	hostRootPath := t.TempDir()
	configureHavenSandboxSession(client, status, "shell-sandbox-metadata", hostRootPath)
	hostSourcePath := filepath.Join(hostRootPath, "stage-me.txt")
	if err := os.WriteFile(hostSourcePath, []byte("stage contents"), 0o600); err != nil {
		t.Fatalf("write host source: %v", err)
	}
	if _, err := client.SandboxImport(context.Background(), loopgate.SandboxImportRequest{
		HostSourcePath:  hostSourcePath,
		DestinationName: "stage-me.txt",
	}); err != nil {
		t.Fatalf("sandbox import: %v", err)
	}
	if _, err := client.SandboxStage(context.Background(), loopgate.SandboxStageRequest{
		SandboxSourcePath: "/morph/home/imports/stage-me.txt",
		OutputName:        "staged.txt",
	}); err != nil {
		t.Fatalf("sandbox stage: %v", err)
	}

	commandResult := HandleCommand(CommandContext{
		RepoRoot:       repoRoot,
		Policy:         status.Policy,
		LoopgateClient: client,
		LoopgateStatus: status,
	}, "/sandbox metadata /morph/home/outputs/staged.txt", nil, nil)
	if !commandResult.Handled {
		t.Fatal("expected command to be handled")
	}
	if !strings.Contains(commandResult.Output, "artifact_ref: staged://artifacts/") {
		t.Fatalf("expected artifact metadata output, got %q", commandResult.Output)
	}
	if !strings.Contains(commandResult.Output, "source_sandbox_path: /morph/home/imports/stage-me.txt") {
		t.Fatalf("expected virtual source sandbox path, got %q", commandResult.Output)
	}
	if !strings.Contains(commandResult.Output, "review_action: review staged artifact metadata before export") {
		t.Fatalf("expected review action output, got %q", commandResult.Output)
	}
}

func TestHandleCommand_SandboxExportRequiresInteractivePrompt(t *testing.T) {
	repoRoot := t.TempDir()
	client, status := startTestLoopgate(t, repoRoot, testPolicyYAML(false))

	commandResult := HandleCommand(CommandContext{
		RepoRoot:       repoRoot,
		Policy:         status.Policy,
		LoopgateClient: client,
		LoopgateStatus: status,
	}, "/sandbox export /morph/home/outputs/staged.txt /tmp/out.txt", nil, nil)
	if !strings.Contains(commandResult.Output, "requires an interactive terminal prompt") {
		t.Fatalf("expected interactive prompt denial, got %q", commandResult.Output)
	}
}

func TestHandleCommand_HelpIncludesAllCommands(t *testing.T) {
	commandResult := HandleCommand(CommandContext{}, "/help", nil, nil)
	for _, expected := range []string{
		"/agent", "/model", "/quarantine", "/site",
		"/sandbox",
		"/connections", "/man",
	} {
		if !strings.Contains(commandResult.Output, expected) {
			t.Fatalf("expected help to mention %s, got %q", expected, commandResult.Output)
		}
	}
}

type stubSiteClient struct {
	loopgate.ControlPlaneClient
	inspectionResponse loopgate.SiteInspectionResponse
}

func (stubClient *stubSiteClient) InspectSite(context.Context, loopgate.SiteInspectionRequest) (loopgate.SiteInspectionResponse, error) {
	return stubClient.inspectionResponse, nil
}

func startTestLoopgate(t *testing.T, repoRoot string, policyYAML string) (*loopgate.Client, loopgate.StatusResponse) {
	t.Helper()

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte(policyYAML), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	testExecutablePath, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	runtimeConfig := config.DefaultRuntimeConfig()
	runtimeConfig.ControlPlane.ExpectedSessionClientExecutable = testExecutablePath
	if err := config.WriteRuntimeConfigYAML(repoRoot, runtimeConfig); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("lg-shell-%d-%d.sock", os.Getpid(), time.Now().UnixNano()))
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	server, err := loopgate.NewServerForIntegrationHarness(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new loopgate server: %v", err)
	}

	serverContext, cancel := context.WithCancel(context.Background())
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = server.Serve(serverContext)
	}()
	t.Cleanup(func() {
		cancel()
		<-serverDone
	})

	client := loopgate.NewClient(socketPath)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err = client.Health(context.Background())
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("wait for loopgate health: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	client.ConfigureSession("test", "session-test", []string{"fs_list"})
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("bootstrap loopgate status: %v", err)
	}
	client.ConfigureSession("test", "session-test", capabilityNamesFromStatus(status))
	return client, status
}

func configureHavenSandboxSession(client *loopgate.Client, status loopgate.StatusResponse, sessionID string, hostRootPath string) {
	client.SetOperatorMountPaths([]string{hostRootPath}, hostRootPath)
	client.ConfigureSession("operator", sessionID, capabilityNamesFromStatus(status))
}

func capabilityNamesFromStatus(status loopgate.StatusResponse) []string {
	names := make([]string, 0, len(status.Capabilities)+len(status.ControlCapabilities))
	for _, capability := range status.Capabilities {
		names = append(names, capability.Name)
	}
	// Shell integration tests need the same full control-session envelope as the live
	// client surface. Restricting this helper to executable capabilities silently drops
	// scoped control-plane routes like site.inspect and causes false-negative test drift.
	for _, capability := range status.ControlCapabilities {
		names = append(names, capability.Name)
	}
	return names
}

func writeContinuityLedger(t *testing.T, repoRoot string, ledgerEvents ...ledger.Event) {
	t.Helper()

	ledgerPath := filepath.Join(repoRoot, "core", "memory", "ledger", "ledger.jsonl")
	if err := os.MkdirAll(filepath.Dir(ledgerPath), 0o700); err != nil {
		t.Fatalf("mkdir ledger dir: %v", err)
	}

	for _, ledgerEvent := range ledgerEvents {
		if err := audit.RecordMustPersist(ledgerPath, ledgerEvent); err != nil {
			t.Fatalf("write ledger event: %v", err)
		}
	}
}

func testPolicyYAML(writeRequiresApproval bool) string {
	approvalValue := "false"
	if writeRequiresApproval {
		approvalValue = "true"
	}

	return "version: 0.1.0\n\n" +
		"tools:\n" +
		"  filesystem:\n" +
		"    allowed_roots:\n" +
		"      - \".\"\n" +
		"    denied_paths: []\n" +
		"    read_enabled: true\n" +
		"    write_enabled: true\n" +
		"    write_requires_approval: " + approvalValue + "\n" +
		"  http:\n" +
		"    enabled: false\n" +
		"    allowed_domains: []\n" +
		"    requires_approval: true\n" +
		"    timeout_seconds: 10\n" +
		"  shell:\n" +
		"    enabled: false\n" +
		"    allowed_commands: []\n" +
		"    requires_approval: true\n" +
		"logging:\n" +
		"  log_commands: true\n" +
		"  log_tool_calls: true\n" +
		"safety:\n" +
		"  allow_persona_modification: false\n" +
		"  allow_policy_modification: false\n"
}
