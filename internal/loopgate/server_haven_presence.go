package loopgate

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var havenPresenceAllowedStates = map[string]string{
	"blocked":   "Morph is blocked",
	"focused":   "Morph is focused",
	"idle":      "Morph is idle",
	"paused":    "Morph is paused",
	"planning":  "Morph is planning",
	"reviewing": "Morph is reviewing",
	"sleeping":  "Morph is sleeping",
	"working":   "Morph is working",
}

var havenPresenceAllowedAnchors = map[string]string{
	"chat":      "in chat",
	"desk":      "at desk",
	"memory":    "in memory",
	"project":   "in a project",
	"review":    "in review",
	"settings":  "in settings",
	"task":      "on a task",
	"workspace": "in workspace",
}

func (server *Server) havenPresencePath() string {
	return filepath.Join(server.repoRoot, "runtime", "state", "haven_presence.json")
}

func (server *Server) handleHavenPresence(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityUIRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	server.writeJSON(writer, http.StatusOK, server.loadHavenPresenceSnapshot())
}

func (server *Server) handleHavenMorphSleep(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityUIRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	presence := server.loadHavenPresenceSnapshot()
	isSleeping := presence.State == "sleeping"
	server.writeJSON(writer, http.StatusOK, HavenMorphSleepResponse{
		State:      presence.State,
		StatusText: presence.StatusText,
		DetailText: presence.DetailText,
		Anchor:     presence.Anchor,
		IsSleeping: isSleeping,
		IsResting:  isSleeping,
	})
}

func (server *Server) loadHavenPresenceSnapshot() HavenPresenceResponse {
	path := server.havenPresencePath()
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return normalizedHavenPresenceSnapshot(HavenPresenceResponse{})
	}
	var snapshot HavenPresenceResponse
	if err := json.Unmarshal(rawBytes, &snapshot); err != nil {
		return normalizedHavenPresenceSnapshot(HavenPresenceResponse{})
	}
	return normalizedHavenPresenceSnapshot(snapshot)
}

func normalizedHavenPresenceSnapshot(rawSnapshot HavenPresenceResponse) HavenPresenceResponse {
	normalizedState := normalizeHavenPresenceState(rawSnapshot.State)
	normalizedAnchor := normalizeHavenPresenceAnchor(rawSnapshot.Anchor)
	normalizedSnapshot := HavenPresenceResponse{
		State:      normalizedState,
		StatusText: havenPresenceAllowedStates[normalizedState],
		Anchor:     normalizedAnchor,
	}
	if detailText, found := havenPresenceAllowedAnchors[normalizedAnchor]; found && detailText != "" {
		normalizedSnapshot.DetailText = detailText
	}
	return normalizedSnapshot
}

func normalizeHavenPresenceState(rawState string) string {
	normalizedState := strings.ToLower(strings.TrimSpace(rawState))
	if _, found := havenPresenceAllowedStates[normalizedState]; found {
		return normalizedState
	}
	return "idle"
}

func normalizeHavenPresenceAnchor(rawAnchor string) string {
	normalizedAnchor := strings.ToLower(strings.TrimSpace(rawAnchor))
	if _, found := havenPresenceAllowedAnchors[normalizedAnchor]; found {
		return normalizedAnchor
	}
	return "desk"
}
