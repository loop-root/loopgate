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
	havenThreadEventSourceKind                 = "haven_thread_event"
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

	sealedAtUTC := server.now().UTC().Format(time.RFC3339Nano)
	observedInspectRequest := buildObservedContinuityInspectRequestFromHavenThread(threadID, sessionLabel, makeHavenContinuityInspectionID(), sealedAtUTC, events)
	if len(observedInspectRequest.ObservedPacket.Events) == 0 {
		server.writeJSON(writer, http.StatusOK, HavenContinuityInspectThreadResponse{
			ThreadID:     threadID,
			SubmitStatus: havenContinuitySubmitStatusSkippedNoEvents,
		})
		return
	}

	inspectResponse, inspectErr := server.inspectObservedContinuity(tokenClaims, observedInspectRequest)
	if inspectErr != nil {
		server.writeMemoryOperationError(writer, inspectErr)
		return
	}

	server.writeJSON(writer, http.StatusOK, HavenContinuityInspectThreadResponse{
		ThreadID:              threadID,
		SubmitStatus:          havenContinuitySubmitStatusSubmitted,
		InspectionID:          inspectResponse.InspectionID,
		Outcome:               inspectResponse.Outcome,
		DerivationOutcome:     inspectResponse.DerivationOutcome,
		ReviewStatus:          inspectResponse.ReviewStatus,
		LineageStatus:         inspectResponse.LineageStatus,
		DerivedDistillateIDs:  inspectResponse.DerivedDistillateIDs,
		DerivedResonateKeyIDs: inspectResponse.DerivedResonateKeyIDs,
	})
}

func buildObservedContinuityInspectRequestFromHavenThread(threadID string, sessionID string, inspectionID string, sealedAtUTC string, events []threadstore.ConversationEvent) ObservedContinuityInspectRequest {
	return ObservedContinuityInspectRequest{
		InspectionID:   inspectionID,
		ThreadID:       strings.TrimSpace(threadID),
		Scope:          memoryScopeGlobal,
		SealedAtUTC:    strings.TrimSpace(sealedAtUTC),
		Tags:           []string{"haven", "conversation", "swift_submit"},
		ObservedPacket: buildObservedContinuityPacketFromHavenThread(threadID, sessionID, sealedAtUTC, events),
	}
}

func buildObservedContinuityPacketFromHavenThread(threadID string, sessionID string, sealedAtUTC string, events []threadstore.ConversationEvent) continuityObservedPacket {
	result := continuityObservedPacket{
		ThreadID:    strings.TrimSpace(threadID),
		Scope:       memoryScopeGlobal,
		SealedAtUTC: strings.TrimSpace(sealedAtUTC),
		Tags:        []string{"haven", "conversation", "swift_submit"},
		Events:      make([]continuityObservedEventRecord, 0, len(events)),
	}
	for eventIndex, event := range events {
		observedEvent, ok := buildObservedContinuityEventFromHavenThread(event, sessionID, eventIndex+1, threadID)
		if !ok {
			continue
		}
		result.Events = append(result.Events, observedEvent)
	}
	return result
}

func buildObservedContinuityEventFromHavenThread(event threadstore.ConversationEvent, sessionID string, ledgerSequence int, threadID string) (continuityObservedEventRecord, bool) {
	continuityType := havenThreadstoreTypeToContinuityType(event.Type)
	if continuityType == "" {
		return continuityObservedEventRecord{}, false
	}

	eventThreadID := strings.TrimSpace(event.ThreadID)
	if eventThreadID == "" {
		eventThreadID = strings.TrimSpace(threadID)
	}

	// Threadstore JSONL files are append-only, so the event index is a stable local
	// provenance handle until Loopgate grows first-class thread event IDs.
	return continuityObservedEventRecord{
		TimestampUTC:    strings.TrimSpace(event.TS),
		SessionID:       strings.TrimSpace(sessionID),
		Type:            continuityType,
		Scope:           memoryScopeGlobal,
		ThreadID:        eventThreadID,
		EpistemicFlavor: "freshly_checked",
		LedgerSequence:  int64(ledgerSequence),
		EventHash:       hashHavenThreadstoreEvent(event),
		SourceRefs: []continuityArtifactSourceRef{{
			Kind: havenThreadEventSourceKind,
			Ref:  fmt.Sprintf("%s:%d", strings.TrimSpace(threadID), ledgerSequence),
		}},
		Payload: buildObservedContinuityEventPayload(event.Data),
	}, true
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

func makeHavenContinuityInspectionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("haven-insp-%s-%s", time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(b))
}
