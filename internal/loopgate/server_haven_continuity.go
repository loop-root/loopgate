package loopgate

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"morph/internal/identifiers"
	"morph/internal/threadstore"
)

const (
	maxHavenContinuityInspectThreadBodyBytes = 8 * 1024

	havenContinuitySubmitStatusSubmitted       = "submitted"
	havenContinuitySubmitStatusSkippedNoEvents = "skipped_no_continuity_events"
)

func (server *Server) handleHavenContinuityInspectThread(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(tokenClaims.ActorLabel), "haven") {
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "continuity inspect-thread requires actor haven",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxHavenContinuityInspectThreadBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var wire HavenContinuityInspectThreadRequest
	if err := decodeJSONBytes(requestBodyBytes, &wire); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	threadID := strings.TrimSpace(wire.ThreadID)
	if err := identifiers.ValidateSafeIdentifier("thread_id", threadID); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	server.mu.Lock()
	sess, sessionFound := server.sessions[tokenClaims.ControlSessionID]
	server.mu.Unlock()
	if !sessionFound {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid capability token",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return
	}

	workspaceID := strings.TrimSpace(sess.WorkspaceID)
	if workspaceID == "" {
		workspaceID = server.deriveWorkspaceIDFromRepoRoot()
	}

	homeDir, err := server.resolveUserHomeDir()
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "cannot resolve home directory",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	threadRoot := filepath.Join(homeDir, ".haven", "threads")
	store, err := threadstore.NewStore(threadRoot, workspaceID)
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "thread store unavailable",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}

	events, err := store.LoadThread(threadID)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "unknown thread_id",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	sessionLabel := strings.TrimSpace(tokenClaims.ClientSessionLabel)
	if sessionLabel == "" {
		sessionLabel = strings.TrimSpace(tokenClaims.ControlSessionID)
	}

	continuityEvents := havenThreadstoreEventsToContinuityInputs(events, sessionLabel)
	if len(continuityEvents) == 0 {
		server.writeJSON(writer, http.StatusOK, HavenContinuityInspectThreadResponse{
			ThreadID:     threadID,
			SubmitStatus: havenContinuitySubmitStatusSkippedNoEvents,
		})
		return
	}

	inspectionID := makeHavenContinuityInspectionID()
	approxBytes := estimateHavenThreadPayloadBytes(events)
	approxTokens := approxBytes / 4
	if approxTokens < 1 {
		approxTokens = 1
	}

	sealedAt := server.now().UTC().Format(time.RFC3339Nano)
	inspectRequest := ContinuityInspectRequest{
		InspectionID:       inspectionID,
		ThreadID:           threadID,
		Scope:              memoryScopeGlobal,
		SealedAtUTC:        sealedAt,
		EventCount:         len(continuityEvents),
		ApproxPayloadBytes: approxBytes,
		ApproxPromptTokens: approxTokens,
		Tags:               []string{"haven", "conversation", "swift_submit"},
		Events:             continuityEvents,
	}

	inspectResponse, inspectErr := server.inspectContinuityThread(tokenClaims, inspectRequest)
	if inspectErr != nil {
		server.writeMemoryOperationError(writer, inspectErr)
		return
	}

	server.writeJSON(writer, http.StatusOK, HavenContinuityInspectThreadResponse{
		ThreadID:                  threadID,
		SubmitStatus:              havenContinuitySubmitStatusSubmitted,
		InspectionID:              inspectResponse.InspectionID,
		Outcome:                   inspectResponse.Outcome,
		DerivationOutcome:         inspectResponse.DerivationOutcome,
		ReviewStatus:              inspectResponse.ReviewStatus,
		LineageStatus:             inspectResponse.LineageStatus,
		DerivedDistillateIDs:      inspectResponse.DerivedDistillateIDs,
		DerivedResonateKeyIDs:     inspectResponse.DerivedResonateKeyIDs,
	})
}

func havenThreadstoreEventsToContinuityInputs(events []threadstore.ConversationEvent, sessionID string) []ContinuityEventInput {
	result := make([]ContinuityEventInput, 0, len(events))
	for eventIndex, event := range events {
		continuityType := havenThreadstoreTypeToContinuityType(event.Type)
		if continuityType == "" {
			continue
		}
		payload := make(map[string]interface{}, len(event.Data))
		for k, v := range event.Data {
			payload[k] = v
		}
		result = append(result, ContinuityEventInput{
			TimestampUTC:    event.TS,
			SessionID:       sessionID,
			Type:            continuityType,
			Scope:           memoryScopeGlobal,
			ThreadID:        event.ThreadID,
			EpistemicFlavor: "freshly_checked",
			LedgerSequence:  int64(eventIndex + 1),
			EventHash:       hashHavenThreadstoreEvent(event),
			Payload:         payload,
		})
	}
	return result
}

func havenThreadstoreTypeToContinuityType(eventType string) string {
	switch eventType {
	case threadstore.EventUserMessage:
		return "user_message"
	case threadstore.EventAssistantMessage:
		return "assistant_response"
	case threadstore.EventOrchToolResult:
		return "tool_executed"
	default:
		return ""
	}
}

func hashHavenThreadstoreEvent(event threadstore.ConversationEvent) string {
	data, _ := json.Marshal(event)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

func estimateHavenThreadPayloadBytes(events []threadstore.ConversationEvent) int {
	total := 0
	for _, event := range events {
		if text, ok := event.Data["text"].(string); ok {
			total += len(text)
		}
		if output, ok := event.Data["output"].(string); ok {
			total += len(output)
		}
	}
	if total < 1 {
		total = 1
	}
	return total
}

func makeHavenContinuityInspectionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("haven-insp-%s-%s", time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(b))
}
