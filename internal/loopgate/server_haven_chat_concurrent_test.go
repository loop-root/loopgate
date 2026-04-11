package loopgate

// Integration tests for executeHavenToolCallsConcurrent.
//
// Design rationale:
//  - We use artificial delays in fake tools to distinguish serial (~N*delay)
//    from parallel (~1*delay) execution without relying on goroutine schedules.
//  - 60 ms per read tool: on even a heavily loaded CI box, two concurrent reads
//    finish in < 120 ms with overhead; the serial path takes >= 120 ms.
//  - Threshold of 110 ms: leaves 50 ms headroom above the parallel ideal (~60 ms)
//    while still cleanly rejecting the serial case (>= 120 ms).
//  - Write tools are never parallelised — ordering of side effects is observable.

import (
	"context"
	"sync"
	"testing"
	"time"

	modelpkg "morph/internal/model"
	modelruntime "morph/internal/modelruntime"
	"morph/internal/orchestrator"
	toolspkg "morph/internal/tools"
)

// ── Slow fake tool ─────────────────────────────────────────────────────────────

// slowFakeLoopgateTool wraps fakeLoopgateTool with a configurable Execute delay.
// Used to make parallel vs serial timing differences measurable in tests.
type slowFakeLoopgateTool struct {
	fakeLoopgateTool
	delay time.Duration
}

func (t slowFakeLoopgateTool) Execute(ctx context.Context, _ map[string]string) (string, error) {
	select {
	case <-time.After(t.delay):
		return t.output, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// ── Execution-order tracker ────────────────────────────────────────────────────

// trackingFakeLoopgateTool records the wall-clock start time of each Execute
// call so tests can verify that serial write tools do not overlap.
type trackingFakeLoopgateTool struct {
	fakeLoopgateTool
	delay  time.Duration
	mu     *sync.Mutex
	starts *[]time.Time
}

func (t trackingFakeLoopgateTool) Execute(ctx context.Context, _ map[string]string) (string, error) {
	t.mu.Lock()
	*t.starts = append(*t.starts, time.Now())
	t.mu.Unlock()
	select {
	case <-time.After(t.delay):
		return t.output, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestHavenChat_ConcurrentReadOnlyToolsRunFasterThanSerial verifies that two
// read-only tools in a single model response execute in parallel.
//
// This test is the primary gate for executeHavenToolCallsConcurrent.
// When RED: the executor is still running reads serially (>= 120 ms for 2×60 ms tools).
// When GREEN: reads truly overlap (< 110 ms total).
func TestHavenChat_ConcurrentReadOnlyToolsRunFasterThanSerial(t *testing.T) {
	const toolDelay = 60 * time.Millisecond
	// Serial: >= 120 ms.  Parallel ideal: ~60 ms.  Threshold: 110 ms.
	const parallelThreshold = 110 * time.Millisecond

	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	server.registry.Register(slowFakeLoopgateTool{
		fakeLoopgateTool: fakeLoopgateTool{
			name: "slow_read_a", category: "test", operation: toolspkg.OpRead, output: "result-a",
		},
		delay: toolDelay,
	})
	server.registry.Register(slowFakeLoopgateTool{
		fakeLoopgateTool: fakeLoopgateTool{
			name: "slow_read_b", category: "test", operation: toolspkg.OpRead, output: "result-b",
		},
		delay: toolDelay,
	})

	// Model calls both reads in one response via invoke_capability, then returns text.
	// ExtractStructuredCalls expands invoke_capability blocks into the real capability names
	// so executeHavenToolCallsConcurrent sees Name="slow_read_a" / "slow_read_b".
	provider := &sequenceModelProvider{
		responses: []modelpkg.Response{
			{
				ToolUseBlocks: []modelpkg.ToolUseBlock{
					{ID: "call-a", Name: "invoke_capability", Input: map[string]string{
						"capability":     "slow_read_a",
						"arguments_json": `{}`,
					}},
					{ID: "call-b", Name: "invoke_capability", Input: map[string]string{
						"capability":     "slow_read_b",
						"arguments_json": `{}`,
					}},
				},
				ProviderName: "stub",
				ModelName:    "stub",
				FinishReason: "tool_calls",
			},
			{
				AssistantText: "Both reads done.",
				ProviderName:  "stub",
				ModelName:     "stub",
				FinishReason:  "stop",
			},
		},
	}
	server.newModelClientFromConfig = func(_ modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error) {
		return modelpkg.NewClient(provider), modelruntime.Config{ProviderName: "stub", ModelName: "stub", Timeout: 5 * time.Second}, nil
	}

	client.ConfigureSession("haven", "concurrent-reads-test",
		append(advertisedSessionCapabilityNames(status), "slow_read_a", "slow_read_b"))
	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	start := time.Now()
	_, err = client.doHavenChatSSE(context.Background(), capabilityToken, havenChatRequest{
		Message: "run both reads",
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("POST /v1/chat: %v", err)
	}
	if elapsed >= parallelThreshold {
		t.Errorf("expected concurrent reads to complete in < %v, got %v — reads may still be serial", parallelThreshold, elapsed)
	}
}

// TestHavenChat_SerialWriteToolsRunInOrder verifies that write-classified tools
// do not overlap. We check their start times are staggered by at least toolDelay
// minus a small jitter allowance. This is a safety invariant: parallel writes
// would produce interleaved side effects in the conversation history.
func TestHavenChat_SerialWriteToolsRunInOrder(t *testing.T) {
	const toolDelay = 40 * time.Millisecond
	// Serial: second start >= toolDelay after first. Allow 15 ms jitter.
	const minSerialGap = 25 * time.Millisecond

	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	var mu sync.Mutex
	var starts []time.Time

	// trusted=true so the tools are auto-allowed via shouldAutoAllowTrustedSandboxCapability
	// without needing to match a policy category rule. Without trusted=true, a
	// category:"test" write tool would not match the "filesystem" policy and be
	// silently denied before Execute is ever reached.
	server.registry.Register(trackingFakeLoopgateTool{
		fakeLoopgateTool: fakeLoopgateTool{
			name: "tracked_write_a", category: "filesystem", operation: toolspkg.OpWrite, output: "wrote-a",
			trusted: true,
		},
		delay: toolDelay, mu: &mu, starts: &starts,
	})
	server.registry.Register(trackingFakeLoopgateTool{
		fakeLoopgateTool: fakeLoopgateTool{
			name: "tracked_write_b", category: "filesystem", operation: toolspkg.OpWrite, output: "wrote-b",
			trusted: true,
		},
		delay: toolDelay, mu: &mu, starts: &starts,
	})

	provider := &sequenceModelProvider{
		responses: []modelpkg.Response{
			{
				ToolUseBlocks: []modelpkg.ToolUseBlock{
					{ID: "w-1", Name: "invoke_capability", Input: map[string]string{
						"capability":     "tracked_write_a",
						"arguments_json": `{}`,
					}},
					{ID: "w-2", Name: "invoke_capability", Input: map[string]string{
						"capability":     "tracked_write_b",
						"arguments_json": `{}`,
					}},
				},
				ProviderName: "stub", ModelName: "stub", FinishReason: "tool_calls",
			},
			{
				AssistantText: "Both writes done.",
				ProviderName:  "stub", ModelName: "stub", FinishReason: "stop",
			},
		},
	}
	server.newModelClientFromConfig = func(_ modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error) {
		return modelpkg.NewClient(provider), modelruntime.Config{ProviderName: "stub", ModelName: "stub", Timeout: 5 * time.Second}, nil
	}

	client.ConfigureSession("haven", "serial-writes-test",
		append(advertisedSessionCapabilityNames(status), "tracked_write_a", "tracked_write_b"))
	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	_, err = client.doHavenChatSSE(context.Background(), capabilityToken, havenChatRequest{
		Message: "run both writes",
	})
	if err != nil {
		t.Fatalf("POST /v1/chat: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(starts) != 2 {
		t.Fatalf("expected 2 tool executions, got %d", len(starts))
	}
	gap := starts[1].Sub(starts[0])
	if gap < minSerialGap {
		t.Errorf("write tools appear to have run in parallel: start gap %v < minSerialGap %v", gap, minSerialGap)
	}
}

// TestHavenChat_ToolResultsRetainInputOrder verifies that the result slice
// returned by executeHavenToolCallsConcurrent is in the same order as the input
// calls, even when reads finish in arbitrary order. The model conversation
// depends on positional correlation between call IDs and results.
func TestHavenChat_ToolResultsRetainInputOrder(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	// Two reads with opposite delays so they naturally finish in reverse order.
	// If ordering is not preserved, the result at index 0 will have callID "call-b".
	server.registry.Register(slowFakeLoopgateTool{
		fakeLoopgateTool: fakeLoopgateTool{
			name: "slow_first", category: "test", operation: toolspkg.OpRead, output: "first-result",
		},
		delay: 50 * time.Millisecond,
	})
	server.registry.Register(slowFakeLoopgateTool{
		fakeLoopgateTool: fakeLoopgateTool{
			name: "fast_second", category: "test", operation: toolspkg.OpRead, output: "second-result",
		},
		delay: 5 * time.Millisecond,
	})

	provider := &sequenceModelProvider{
		responses: []modelpkg.Response{
			{
				ToolUseBlocks: []modelpkg.ToolUseBlock{
					{ID: "call-a", Name: "invoke_capability", Input: map[string]string{
						"capability":     "slow_first",
						"arguments_json": `{}`,
					}},
					{ID: "call-b", Name: "invoke_capability", Input: map[string]string{
						"capability":     "fast_second",
						"arguments_json": `{}`,
					}},
				},
				ProviderName: "stub", ModelName: "stub", FinishReason: "tool_calls",
			},
			{
				AssistantText: "Order verified.",
				ProviderName:  "stub", ModelName: "stub", FinishReason: "stop",
			},
		},
	}
	server.newModelClientFromConfig = func(_ modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error) {
		return modelpkg.NewClient(provider), modelruntime.Config{ProviderName: "stub", ModelName: "stub", Timeout: 5 * time.Second}, nil
	}

	client.ConfigureSession("haven", "result-order-test",
		append(advertisedSessionCapabilityNames(status), "slow_first", "fast_second"))
	capTok, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	// Intercept the tool results before they reach the model to check ordering.
	// We do this by observing the SSE stream of tool_result events via the
	// recorded model requests: the second model request gets both tool results
	// as ToolResultBlocks in the conversation turn. Check their order there.
	chatResponse, err := client.doHavenChatSSE(context.Background(), capTok, havenChatRequest{
		Message: "check order",
	})
	if err != nil {
		t.Fatalf("POST /v1/chat: %v", err)
	}
	if chatResponse.AssistantText != "Order verified." {
		t.Fatalf("unexpected assistant text: %q", chatResponse.AssistantText)
	}

	// The second model request should contain tool results in call-a, call-b order.
	reqs := provider.recordedRequests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 model requests, got %d", len(reqs))
	}
	resultTurn := reqs[1].Conversation
	if len(resultTurn) == 0 {
		t.Fatal("expected conversation turns in second model request")
	}
	// Find the tool-result turn (role "user" with ToolResults set).
	var toolResultBlocks []modelpkg.ToolResultBlock
	for _, turn := range resultTurn {
		if len(turn.ToolResults) > 0 {
			toolResultBlocks = turn.ToolResults
			break
		}
	}
	if len(toolResultBlocks) != 2 {
		t.Fatalf("expected 2 tool result blocks in second request conversation, got %d", len(toolResultBlocks))
	}
	if toolResultBlocks[0].ToolUseID != "call-a" {
		t.Errorf("expected first result ToolUseID=call-a, got %q (order not preserved)", toolResultBlocks[0].ToolUseID)
	}
	if toolResultBlocks[1].ToolUseID != "call-b" {
		t.Errorf("expected second result ToolUseID=call-b, got %q (order not preserved)", toolResultBlocks[1].ToolUseID)
	}

	// Execute both the known-concurrent reads test using executeHavenToolCallsConcurrent
	// directly to verify index-based result ordering without HTTP overhead.
	parsedCalls := []orchestrator.ToolCall{
		{ID: "direct-a", Name: "slow_first", Args: map[string]string{}},
		{ID: "direct-b", Name: "fast_second", Args: map[string]string{}},
	}
	// Build a minimal token for the direct call (actor must be "haven" for auto-allow).
	token := capabilityToken{
		ActorLabel:          "haven",
		ControlSessionID:    "test-direct",
		AllowedCapabilities: map[string]struct{}{"slow_first": {}, "fast_second": {}},
	}
	results := server.executeHavenToolCallsConcurrent(context.Background(), token, parsedCalls, nil)
	if len(results) != 2 {
		t.Fatalf("direct call: expected 2 results, got %d", len(results))
	}
	if results[0].CallID != "direct-a" {
		t.Errorf("direct call: results[0].CallID = %q, want direct-a", results[0].CallID)
	}
	if results[1].CallID != "direct-b" {
		t.Errorf("direct call: results[1].CallID = %q, want direct-b", results[1].CallID)
	}
}
