package loopgate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	modelpkg "morph/internal/model"
	modelruntime "morph/internal/modelruntime"
	"morph/internal/orchestrator"
	toolspkg "morph/internal/tools"
)

type sequenceModelProvider struct {
	mu        sync.Mutex
	responses []modelpkg.Response
	requests  []modelpkg.Request
}

func (provider *sequenceModelProvider) Generate(ctx context.Context, request modelpkg.Request) (modelpkg.Response, error) {
	_ = ctx
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.requests = append(provider.requests, request)
	if len(provider.responses) == 0 {
		return modelpkg.Response{AssistantText: "No response configured."}, nil
	}
	response := provider.responses[0]
	provider.responses = provider.responses[1:]
	return response, nil
}

func (provider *sequenceModelProvider) recordedRequests() []modelpkg.Request {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	recordedRequests := make([]modelpkg.Request, len(provider.requests))
	copy(recordedRequests, provider.requests)
	return recordedRequests
}

func TestBuildResidentCapabilityFacts_MemoryRememberHintMatchesRegistry(t *testing.T) {
	capabilitySummaries := []CapabilitySummary{
		{Name: "memory.remember"},
	}

	runtimeFacts := buildResidentCapabilityFacts(capabilitySummaries)
	var memoryFact string
	for _, runtimeFact := range runtimeFacts {
		if strings.Contains(runtimeFact, "memory.remember proposes durable facts") {
			memoryFact = runtimeFact
			break
		}
	}
	if memoryFact == "" {
		t.Fatalf("expected memory.remember runtime fact, got %#v", runtimeFacts)
	}
	if !strings.Contains(memoryFact, "work details") {
		t.Fatalf("expected work guidance in memory.remember runtime fact, got %q", memoryFact)
	}
	if !strings.Contains(memoryFact, "standing goals") {
		t.Fatalf("expected goals guidance in memory.remember runtime fact, got %q", memoryFact)
	}
	if !strings.Contains(memoryFact, "goal.current_sprint") {
		t.Fatalf("expected supported goal key example in memory.remember runtime fact, got %q", memoryFact)
	}
	if !strings.Contains(memoryFact, "work.focus_area") {
		t.Fatalf("expected supported work key example in memory.remember runtime fact, got %q", memoryFact)
	}
	if strings.Contains(memoryFact, "context.recent_topic") {
		t.Fatalf("did not expect unsupported context key example in memory.remember runtime fact, got %q", memoryFact)
	}
}

func TestHavenChat_EnablesToolsAndExecutesMemoryRemember(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	provider := &sequenceModelProvider{
		responses: []modelpkg.Response{
			{
				ToolUseBlocks: []modelpkg.ToolUseBlock{{
					ID:   "tool-memory-1",
					Name: "invoke_capability",
					Input: map[string]string{
						"capability":     "memory.remember",
						"arguments_json": `{"fact_key":"name","fact_value":"Ada"}`,
					},
				}},
				ProviderName: "stub",
				ModelName:    "stub",
				FinishReason: "tool_calls",
			},
			{
				AssistantText: "I'll remember that.",
				ProviderName:  "stub",
				ModelName:     "stub",
				FinishReason:  "stop",
			},
		},
	}
	server.newModelClientFromConfig = func(runtimeConfig modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error) {
		return modelpkg.NewClient(provider), modelruntime.Config{ProviderName: "stub", ModelName: "stub", Timeout: time.Second}, nil
	}

	client.ConfigureSession("haven", "haven-chat-test", capabilityNames(status.Capabilities))
	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	chatResponse, err := client.doHavenChatSSE(context.Background(), capabilityToken, havenChatRequest{
		Message: "Remember my name is Ada.",
	})
	if err != nil {
		t.Fatalf("POST /v1/chat: %v", err)
	}

	if chatResponse.AssistantText != "I'll remember that." {
		t.Fatalf("unexpected assistant text: %#v", chatResponse)
	}

	wakeState, err := client.LoadMemoryWakeState(context.Background())
	if err != nil {
		t.Fatalf("load memory wake state: %v", err)
	}
	foundRememberedFact := false
	for _, recentFact := range wakeState.RecentFacts {
		if recentFact.Name == "name" && recentFact.Value == "Ada" {
			foundRememberedFact = true
			break
		}
	}
	if !foundRememberedFact {
		t.Fatalf("expected remembered fact in wake state, got %#v", wakeState.RecentFacts)
	}

	recordedRequests := provider.recordedRequests()
	if len(recordedRequests) != 2 {
		t.Fatalf("expected 2 model requests, got %d", len(recordedRequests))
	}
	firstRuntimeFacts := strings.Join(recordedRequests[0].RuntimeFacts, "\n")
	if strings.Contains(firstRuntimeFacts, "Tool execution through Loopgate is not enabled") {
		t.Fatalf("expected Haven tool runtime facts, got %q", firstRuntimeFacts)
	}
	if len(recordedRequests[0].AvailableTools) == 0 {
		t.Fatalf("expected available tools in first request, got %#v", recordedRequests[0])
	}
	if len(recordedRequests[0].NativeToolDefs) == 0 {
		t.Fatalf("expected native tool defs in first request, got %#v", recordedRequests[0])
	}
}

func TestHavenChat_KeepsApprovalRequiredToolPending(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }
	server.registry.Register(fakeLoopgateTool{
		name:        "external_write",
		category:    "filesystem",
		operation:   toolspkg.OpWrite,
		description: "test-only write that should still require approval for the local chat route",
		output:      "pending",
	})

	provider := &sequenceModelProvider{
		responses: []modelpkg.Response{
			{
				AssistantText: `<tool_call>{"name":"external_write","args":{}}</tool_call>`,
				ProviderName:  "stub",
				ModelName:     "stub",
				FinishReason:  "tool_calls",
			},
		},
	}
	server.newModelClientFromConfig = func(runtimeConfig modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error) {
		return modelpkg.NewClient(provider), modelruntime.Config{ProviderName: "stub", ModelName: "stub", Timeout: time.Second}, nil
	}

	client.ConfigureSession("haven", "haven-chat-approval-test", append(capabilityNames(status.Capabilities), "external_write"))
	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	chatResponse, err := client.doHavenChatSSE(context.Background(), capabilityToken, havenChatRequest{
		Message: "Write a file called pending.txt in my workspace.",
	})
	if err != nil {
		t.Fatalf("POST /v1/chat: %v", err)
	}

	if chatResponse.Status != "approval_required" {
		t.Fatalf("expected status approval_required, got %#v", chatResponse)
	}
	if strings.TrimSpace(chatResponse.ApprovalID) == "" {
		t.Fatalf("expected approval_id in chat response, got %#v", chatResponse)
	}
	if chatResponse.ApprovalCapability != "external_write" {
		t.Fatalf("expected approval_capability external_write, got %#v", chatResponse)
	}
	if !strings.Contains(chatResponse.AssistantText, "security prompt") || !strings.Contains(chatResponse.AssistantText, "Haven") {
		t.Fatalf("expected operator-facing assistant text about approving in Haven, got %#v", chatResponse.AssistantText)
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.approvals) != 1 {
		t.Fatalf("expected 1 pending approval in authoritative server state, got %#v", server.approvals)
	}
}

func TestHavenUserMessageLikelyHostFolderAction(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"cleanup my downloads folder", true},
		{"Can you help me cleanup my downloads folder?", true},
		{"organize my Desktop files", true},
		{"tidy my Downloads", true},
		{"list files in my downloads folder", true},
		{"how are you today", false},
		{"organize my life goals", false},
		{"cleanup the codebase", false},
	}
	for _, tt := range tests {
		if got := havenUserMessageLikelyHostFolderAction(tt.msg); got != tt.want {
			t.Fatalf("havenUserMessageLikelyHostFolderAction(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestHavenIsShortAffirmation(t *testing.T) {
	if !havenIsShortAffirmation("yes") || !havenIsShortAffirmation(" OK ") || !havenIsShortAffirmation("go ahead") {
		t.Fatalf("expected common affirmations to match")
	}
	if havenIsShortAffirmation("maybe later") || havenIsShortAffirmation("") {
		t.Fatalf("expected non-affirmations to be false")
	}
}

func TestHavenHostFolderProseNudgeApplies(t *testing.T) {
	prior := []modelpkg.ConversationTurn{
		{Role: "user", Content: "organize my downloads folder"},
		{Role: "assistant", Content: "What sort order?"},
	}
	cur := append(append([]modelpkg.ConversationTurn{}, prior...), modelpkg.ConversationTurn{Role: "user", Content: "yes"})
	if !havenHostFolderProseNudgeApplies("yes", cur) {
		t.Fatalf("expected yes + prior host intent to nudge")
	}
	if !havenHostFolderProseNudgeApplies("just make them nicer", cur) {
		t.Fatalf("expected stylistic follow-up to nudge")
	}
	if havenHostFolderProseNudgeApplies("yes", []modelpkg.ConversationTurn{{Role: "user", Content: "hello"}}) {
		t.Fatalf("expected bare yes without host thread not to nudge")
	}
}

func TestHavenChat_HostFolderProseNudgeTriggersToolRound(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	baseCaps := capabilityNames(status.Capabilities)
	hostCaps := []string{"host.folder.list", "host.folder.read", "host.organize.plan", "host.plan.apply"}
	allCaps := append(append([]string{}, baseCaps...), hostCaps...)

	provider := &sequenceModelProvider{
		responses: []modelpkg.Response{
			{
				AssistantText: "Let's start by listing the contents of your downloads folder.",
				ProviderName:  "stub",
				ModelName:     "stub",
				FinishReason:  "stop",
			},
			{
				ToolUseBlocks: []modelpkg.ToolUseBlock{{
					ID:   "call-list-1",
					Name: "invoke_capability",
					Input: map[string]string{
						"capability":     "host.folder.list",
						"arguments_json": `{"folder_name":"downloads","path":"."}`,
					},
				}},
				ProviderName: "stub",
				ModelName:    "stub",
				FinishReason: "tool_calls",
			},
			{
				AssistantText: "Here is what I found (after listing).",
				ProviderName:  "stub",
				ModelName:     "stub",
				FinishReason:  "stop",
			},
		},
	}
	server.newModelClientFromConfig = func(runtimeConfig modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error) {
		return modelpkg.NewClient(provider), modelruntime.Config{ProviderName: "stub", ModelName: "stub", Timeout: time.Second}, nil
	}

	client.ConfigureSession("haven", "haven-chat-host-prose-nudge", allCaps)
	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	chatResponse, err := client.doHavenChatSSE(context.Background(), capabilityToken, havenChatRequest{
		Message: "Can you help me cleanup my downloads folder?",
	})
	if err != nil {
		t.Fatalf("POST /v1/chat: %v", err)
	}
	if !strings.Contains(chatResponse.AssistantText, "Here is what I found") {
		t.Fatalf("expected final assistant text after tool round, got %#v", chatResponse.AssistantText)
	}

	reqs := provider.recordedRequests()
	if len(reqs) < 2 {
		t.Fatalf("expected prose nudge to schedule a second model call, got %d requests", len(reqs))
	}
	if !strings.Contains(reqs[1].UserMessage, "invoke_capability") {
		t.Fatalf("expected synthetic user nudge to demand invoke_capability, got %q", reqs[1].UserMessage)
	}
}

// TestHavenChat_OrganizePlanAutoAppliesAndRequiresApproval verifies that the
// loop auto-applies the plan immediately after host.organize.plan succeeds,
// without an extra model round-trip. host.plan.apply always requires operator
// approval regardless of the write_requires_approval policy setting.
func TestHavenChat_OrganizePlanProseNudgesHostPlanApply(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }
	sharedHostPath := filepath.Join(repoRoot, defaultSharedFolderName)
	if err := os.MkdirAll(sharedHostPath, 0o755); err != nil {
		t.Fatalf("mkdir shared host folder: %v", err)
	}

	baseCaps := capabilityNames(status.Capabilities)
	hostCaps := []string{"host.folder.list", "host.folder.read", "host.organize.plan", "host.plan.apply"}
	allCaps := append(append([]string{}, baseCaps...), hostCaps...)

	planOps, err := json.Marshal([]map[string]string{{"kind": "mkdir", "path": "_haven_chat_test_dir"}})
	if err != nil {
		t.Fatalf("marshal plan ops: %v", err)
	}
	organizeArgs, err := json.Marshal(map[string]string{
		"folder_name": "shared",
		"plan_json":   string(planOps),
		"summary":     "test plan",
	})
	if err != nil {
		t.Fatalf("marshal organize args: %v", err)
	}

	provider := &sequenceModelProvider{
		responses: []modelpkg.Response{
			{
				ToolUseBlocks: []modelpkg.ToolUseBlock{{
					ID:   "oplan-1",
					Name: "invoke_capability",
					Input: map[string]string{
						"capability":     "host.organize.plan",
						"arguments_json": string(organizeArgs),
					},
				}},
				ProviderName: "stub",
				ModelName:    "stub",
				FinishReason: "tool_calls",
			},
		},
	}
	server.newModelClientFromConfig = func(runtimeConfig modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error) {
		return modelpkg.NewClient(provider), modelruntime.Config{ProviderName: "stub", ModelName: "stub", Timeout: time.Second}, nil
	}

	client.ConfigureSession("haven", "haven-chat-organize-auto-apply", allCaps)
	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	chatResponse, err := client.doHavenChatSSE(context.Background(), capabilityToken, havenChatRequest{
		Message: "yes — go ahead and apply the plan to my shared folder",
	})
	if err != nil {
		t.Fatalf("POST /v1/chat: %v", err)
	}

	// Auto-apply fires host.plan.apply immediately after organize.plan succeeds.
	// host.plan.apply always requires operator approval, so the response is
	// approval_required — no additional model call is needed.
	if chatResponse.Status != "approval_required" {
		t.Fatalf("expected status approval_required from auto-apply, got %#v", chatResponse)
	}
	if chatResponse.ApprovalCapability != "host.plan.apply" {
		t.Fatalf("expected approval for host.plan.apply, got %#v", chatResponse.ApprovalCapability)
	}
	if strings.TrimSpace(chatResponse.ApprovalID) == "" {
		t.Fatalf("expected approval_id in response, got %#v", chatResponse)
	}

	reqs := provider.recordedRequests()
	if len(reqs) != 1 {
		t.Fatalf("expected exactly 1 model call (organize.plan only; auto-apply skips the extra round-trip), got %d", len(reqs))
	}
}

func TestHavenIsNonUserFacingAssistantPlaceholder(t *testing.T) {
	if !havenIsNonUserFacingAssistantPlaceholder("(no text in model response)") {
		t.Fatalf("expected exact placeholder to match")
	}
	if !havenIsNonUserFacingAssistantPlaceholder("  (NO TEXT IN MODEL RESPONSE)  ") {
		t.Fatalf("expected case/space-insensitive match")
	}
	if havenIsNonUserFacingAssistantPlaceholder("I'll list your Downloads next.") {
		t.Fatalf("expected normal text not to match")
	}
}

func TestHavenChat_PlaceholderAssistantTextBecomesFriendlyReply(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	provider := &sequenceModelProvider{
		responses: []modelpkg.Response{
			{
				AssistantText: "(no text in model response)",
				ProviderName:  "stub",
				ModelName:     "stub",
				FinishReason:  "stop",
			},
		},
	}
	server.newModelClientFromConfig = func(runtimeConfig modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error) {
		return modelpkg.NewClient(provider), modelruntime.Config{ProviderName: "stub", ModelName: "stub", Timeout: time.Second}, nil
	}

	client.ConfigureSession("haven", "haven-chat-placeholder-test", capabilityNames(status.Capabilities))
	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	chatResponse, err := client.doHavenChatSSE(context.Background(), capabilityToken, havenChatRequest{
		Message: "yes that's fine",
	})
	if err != nil {
		t.Fatalf("POST /v1/chat: %v", err)
	}

	if strings.Contains(strings.ToLower(chatResponse.AssistantText), "no text in model response") {
		t.Fatalf("expected placeholder stripped from operator text, got %#v", chatResponse.AssistantText)
	}
	if strings.TrimSpace(chatResponse.AssistantText) == "" {
		t.Fatalf("expected non-empty friendly assistant text, got %#v", chatResponse.AssistantText)
	}
}

func TestHavenToolResultContent_IncludesDenialCode(t *testing.T) {
	out := havenToolResultContent(orchestrator.ToolResult{
		CallID:     "call-1",
		Capability: "memory.remember",
		Status:     orchestrator.StatusDenied,
		Reason:     "explicit memory write could not be analyzed safely and was not stored",
		DenialCode: DenialCodeMemoryCandidateInvalid,
	})
	if !strings.Contains(out, "denial_code") || !strings.Contains(out, DenialCodeMemoryCandidateInvalid) {
		t.Fatalf("expected denial_code suffix in model-facing tool content, got %q", out)
	}
}
