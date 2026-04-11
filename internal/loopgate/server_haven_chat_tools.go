package loopgate

import (
	"context"
	"sync"

	modelpkg "morph/internal/model"
	"morph/internal/orchestrator"
	"morph/internal/threadstore"
)

func (server *Server) executeHavenToolCalls(ctx context.Context, tokenClaims capabilityToken, parsedCalls []orchestrator.ToolCall) []orchestrator.ToolResult {
	toolResults := make([]orchestrator.ToolResult, 0, len(parsedCalls))
	for _, parsedCall := range parsedCalls {
		capabilityResponse := server.executeCapabilityRequest(ctx, tokenClaims, CapabilityRequest{
			RequestID:  parsedCall.ID,
			Actor:      tokenClaims.ActorLabel,
			SessionID:  tokenClaims.ControlSessionID,
			Capability: parsedCall.Name,
			Arguments:  parsedCall.Args,
		}, true)
		toolResult, toolResultErr := havenToolResultFromCapabilityResponse(parsedCall.ID, parsedCall.Name, capabilityResponse)
		if toolResultErr != nil {
			toolResult.Status = orchestrator.StatusError
			toolResult.Reason = "invalid Loopgate tool result"
		}
		toolResult.Capability = parsedCall.Name
		toolResults = append(toolResults, toolResult)
	}
	return toolResults
}

// executeHavenToolCallsConcurrent runs read-only tool calls in parallel and
// write/execute tool calls serially, preserving the original input order in
// the returned results slice.
//
// Why two phases:
//   - Read-only tools (OpRead) have no observable ordering constraints between
//     themselves; fanning them out reduces latency proportional to the count.
//   - Write and execute tools have side effects that may be visible to later
//     calls (e.g. fs_write followed by fs_read must see the new content).
//     They must remain serial.
//   - Unknown capabilities (not in registry) default to serial — fail-closed.
//
// The emitter is goroutine-safe (see havenSSEEmitter.mu) so each read goroutine
// can stream its own tool_result event as it finishes, giving the operator
// live feedback rather than a single batch after all reads complete.
//
// Concurrency invariants (AGENTS.md §7):
//   - All goroutines are scoped to this function; wg.Wait() ensures they all
//     complete before the function returns. No goroutine outlives the HTTP handler.
//   - Result slots are pre-allocated and each goroutine writes to its own index
//     (i), so there is no data race on the toolResults slice itself.
//   - Approval detection (checking results for StatusPendingApproval) happens
//     after wg.Wait(), so the caller always sees the complete, stable result set.
func (server *Server) executeHavenToolCallsConcurrent(ctx context.Context, tokenClaims capabilityToken, parsedCalls []orchestrator.ToolCall, emitter *havenSSEEmitter) []orchestrator.ToolResult {
	toolResults := make([]orchestrator.ToolResult, len(parsedCalls))

	// Partition into read-only (parallel) and serial (write/execute/unknown) groups.
	// We keep the original index so results can be written to the correct slot.
	type indexedCall struct {
		idx  int
		call orchestrator.ToolCall
	}
	var readGroup []indexedCall
	var serialGroup []indexedCall
	for i, call := range parsedCalls {
		cls := classifyCapability(server.registry, call.Name)
		if cls.readOnly {
			readGroup = append(readGroup, indexedCall{i, call})
		} else {
			serialGroup = append(serialGroup, indexedCall{i, call})
		}
	}

	// executeSingle runs one capability request and stores the result at toolResults[idx].
	// It also emits a tool_result SSE event immediately on completion so the operator
	// sees progress rather than a silent pause while tools run.
	executeSingle := func(idx int, call orchestrator.ToolCall) {
		capabilityResponse := server.executeCapabilityRequest(ctx, tokenClaims, CapabilityRequest{
			RequestID:  call.ID,
			Actor:      tokenClaims.ActorLabel,
			SessionID:  tokenClaims.ControlSessionID,
			Capability: call.Name,
			Arguments:  call.Args,
		}, true)
		result, err := havenToolResultFromCapabilityResponse(call.ID, call.Name, capabilityResponse)
		if err != nil {
			result.Status = orchestrator.StatusError
			result.Reason = "invalid Loopgate tool result"
		}
		result.Capability = call.Name
		toolResults[idx] = result
		// Emit immediately — emitter.emit is goroutine-safe.
		emitter.emit(havenSSEEvent{Type: "tool_result", ToolResult: &havenSSEToolResult{
			CallID:  result.CallID,
			Preview: havenSSEPreviewForToolResult(result),
			Status:  string(result.Status),
		}})
	}

	// Phase 1: Fan out all read-only calls concurrently.
	// Each goroutine writes to its own pre-allocated index — no mutex needed on toolResults.
	var wg sync.WaitGroup
	for _, ic := range readGroup {
		wg.Add(1)
		go func(idx int, call orchestrator.ToolCall) {
			defer wg.Done()
			executeSingle(idx, call)
		}(ic.idx, ic.call)
	}
	wg.Wait()

	// Phase 2: Execute write/unknown calls serially in original input order.
	// We must wait for all reads to complete first to preserve the invariant that
	// any write that could observe a prior read's side effects sees the final state.
	for _, ic := range serialGroup {
		executeSingle(ic.idx, ic.call)
	}

	return toolResults
}

func havenToolResultTurn(toolResults []orchestrator.ToolResult, useStructuredPath bool) (modelpkg.ConversationTurn, bool) {
	if useStructuredPath {
		return havenStructuredToolResultTurn(toolResults), true
	}

	eligibleResults := havenPromptEligibleToolResults(toolResults)
	if len(eligibleResults) == 0 {
		return modelpkg.ConversationTurn{}, false
	}

	return modelpkg.ConversationTurn{
		Role:      "tool",
		Content:   orchestrator.FormatResults(eligibleResults),
		Timestamp: threadstore.NowUTC(),
	}, true
}
