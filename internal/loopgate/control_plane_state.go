package loopgate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type seenRequest struct {
	ControlSessionID string
	SeenAt           time.Time
}

type usedToken struct {
	TokenID           string
	ParentTokenID     string
	ControlSessionID  string
	Capability        string
	NormalizedArgHash string
	ConsumedAt        time.Time
}

func (server *Server) pruneExpiredLocked() {
	nowUTC := server.now().UTC()
	if server.expirySweepMaxInterval > 0 && !server.nextExpirySweepAt.IsZero() && nowUTC.Before(server.nextExpirySweepAt) {
		return
	}

	earliestNextSweepAt := time.Time{}
	noteNextSweepCandidate := func(candidateTime time.Time) {
		if candidateTime.IsZero() {
			return
		}
		candidateTime = candidateTime.UTC()
		if earliestNextSweepAt.IsZero() || candidateTime.Before(earliestNextSweepAt) {
			earliestNextSweepAt = candidateTime
		}
	}

	for tokenString, tokenClaims := range server.tokens {
		if nowUTC.After(tokenClaims.ExpiresAt) {
			delete(server.tokens, tokenString)
			continue
		}
		noteNextSweepCandidate(tokenClaims.ExpiresAt)
	}
	for controlSessionID, activeSession := range server.sessions {
		if nowUTC.After(activeSession.ExpiresAt) {
			delete(server.sessions, controlSessionID)
			delete(server.approvalTokenIndex, approvalTokenHash(activeSession.ApprovalToken))
			continue
		}
		noteNextSweepCandidate(activeSession.ExpiresAt)
	}
	for approvalID, pendingApproval := range server.approvals {
		if nowUTC.After(pendingApproval.ExpiresAt) {
			if pendingApproval.State == approvalStatePending {
				pendingApproval.State = approvalStateExpired
				server.approvals[approvalID] = pendingApproval
				noteNextSweepCandidate(pendingApproval.ExpiresAt.Add(requestReplayWindow))
				continue
			}
			if nowUTC.Sub(pendingApproval.ExpiresAt) > requestReplayWindow {
				delete(server.approvals, approvalID)
				continue
			}
			noteNextSweepCandidate(pendingApproval.ExpiresAt.Add(requestReplayWindow))
			continue
		}
		noteNextSweepCandidate(pendingApproval.ExpiresAt)
	}
	for approvalRequestID, approvalRequest := range server.mcpGatewayApprovalRequests {
		if nowUTC.After(approvalRequest.ExpiresAt) {
			if approvalRequest.State == approvalStatePending {
				approvalRequest.State = approvalStateExpired
				server.mcpGatewayApprovalRequests[approvalRequestID] = approvalRequest
				noteNextSweepCandidate(approvalRequest.ExpiresAt.Add(requestReplayWindow))
				continue
			}
			if nowUTC.Sub(approvalRequest.ExpiresAt) > requestReplayWindow {
				delete(server.mcpGatewayApprovalRequests, approvalRequestID)
				continue
			}
			noteNextSweepCandidate(approvalRequest.ExpiresAt.Add(requestReplayWindow))
			continue
		}
		noteNextSweepCandidate(approvalRequest.ExpiresAt)
	}
	for requestKey, seenRequest := range server.seenRequests {
		if nowUTC.Sub(seenRequest.SeenAt) > requestReplayWindow {
			delete(server.seenRequests, requestKey)
			continue
		}
		noteNextSweepCandidate(seenRequest.SeenAt.Add(requestReplayWindow))
	}
	for nonceKey, seenNonce := range server.seenAuthNonces {
		if nowUTC.Sub(seenNonce.SeenAt) > requestReplayWindow {
			delete(server.seenAuthNonces, nonceKey)
			continue
		}
		noteNextSweepCandidate(seenNonce.SeenAt.Add(requestReplayWindow))
	}
	for tokenID, consumedToken := range server.usedTokens {
		if nowUTC.Sub(consumedToken.ConsumedAt) > requestReplayWindow {
			delete(server.usedTokens, tokenID)
			continue
		}
		noteNextSweepCandidate(consumedToken.ConsumedAt.Add(requestReplayWindow))
	}
	if server.expirySweepMaxInterval <= 0 {
		server.nextExpirySweepAt = time.Time{}
		return
	}

	maxScheduledSweepAt := nowUTC.Add(server.expirySweepMaxInterval)
	switch {
	case earliestNextSweepAt.IsZero():
		server.nextExpirySweepAt = time.Time{}
	case earliestNextSweepAt.Before(nowUTC):
		server.nextExpirySweepAt = nowUTC
	case earliestNextSweepAt.Before(maxScheduledSweepAt):
		server.nextExpirySweepAt = earliestNextSweepAt
	default:
		server.nextExpirySweepAt = maxScheduledSweepAt
	}
}

func (server *Server) noteExpiryCandidateLocked(candidateTime time.Time) {
	if server.expirySweepMaxInterval <= 0 || candidateTime.IsZero() {
		return
	}
	candidateTime = candidateTime.UTC()
	if server.nextExpirySweepAt.IsZero() || candidateTime.Before(server.nextExpirySweepAt) {
		server.nextExpirySweepAt = candidateTime
	}
}

func (server *Server) noteReplayWindowCandidateLocked(seenAt time.Time) {
	if seenAt.IsZero() {
		return
	}
	server.noteExpiryCandidateLocked(seenAt.UTC().Add(requestReplayWindow))
}

type persistedNonce struct {
	ControlSessionID string `json:"control_session_id"`
	SeenAt           string `json:"seen_at"`
}

type nonceReplayFile struct {
	Nonces map[string]persistedNonce `json:"nonces"`
}

func (server *Server) loadNonceReplayState() error {
	rawBytes, err := os.ReadFile(server.noncePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read nonce replay state: %w", err)
	}
	var stateFile nonceReplayFile
	if err := json.Unmarshal(rawBytes, &stateFile); err != nil {
		return fmt.Errorf("decode nonce replay state: %w", err)
	}
	nowUTC := server.now().UTC()
	for nonceKey, entry := range stateFile.Nonces {
		seenAt, parseErr := time.Parse(time.RFC3339Nano, entry.SeenAt)
		if parseErr != nil {
			continue
		}
		if nowUTC.Sub(seenAt) > requestReplayWindow {
			continue
		}
		server.seenAuthNonces[nonceKey] = seenRequest{
			ControlSessionID: entry.ControlSessionID,
			SeenAt:           seenAt,
		}
	}
	return nil
}

func nonceReplaySnapshot(seenAuthNonces map[string]seenRequest) nonceReplayFile {
	entries := make(map[string]persistedNonce, len(seenAuthNonces))
	for nonceKey, seen := range seenAuthNonces {
		entries[nonceKey] = persistedNonce{
			ControlSessionID: seen.ControlSessionID,
			SeenAt:           seen.SeenAt.UTC().Format(time.RFC3339Nano),
		}
	}
	return nonceReplayFile{Nonces: entries}
}

func atomicWritePrivateJSON(path string, jsonBytes []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create state temp file: %w", err)
	}
	tempPath := tempFile.Name()
	cleanupTemp := func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}
	if err := tempFile.Chmod(0o600); err != nil {
		cleanupTemp()
		return fmt.Errorf("chmod state temp file: %w", err)
	}
	if _, err := tempFile.Write(jsonBytes); err != nil {
		cleanupTemp()
		return fmt.Errorf("write state temp file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		cleanupTemp()
		return fmt.Errorf("sync state temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close state temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("commit state file: %w", err)
	}
	if stateDir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = stateDir.Sync()
		_ = stateDir.Close()
	}
	return nil
}

func (server *Server) saveNonceReplayState() error {
	server.mu.Lock()
	stateFile := nonceReplaySnapshot(server.seenAuthNonces)
	server.mu.Unlock()

	jsonBytes, err := json.Marshal(stateFile)
	if err != nil {
		return fmt.Errorf("marshal nonce replay state: %w", err)
	}
	if err := atomicWritePrivateJSON(server.noncePath, jsonBytes); err != nil {
		return fmt.Errorf("persist nonce replay state: %w", err)
	}
	return nil
}

func (server *Server) countPendingApprovalsForSessionLocked(controlSessionID string) int {
	pendingCount := 0
	for _, pendingApproval := range server.approvals {
		if pendingApproval.ControlSessionID != controlSessionID {
			continue
		}
		if pendingApproval.State == approvalStatePending {
			pendingCount++
		}
	}
	return pendingCount
}

func (server *Server) countPendingMCPGatewayApprovalRequestsForSessionLocked(controlSessionID string) int {
	pendingCount := 0
	for _, approvalRequest := range server.mcpGatewayApprovalRequests {
		if approvalRequest.ControlSessionID != controlSessionID {
			continue
		}
		if approvalRequest.State != approvalStatePending {
			continue
		}
		pendingCount++
	}
	return pendingCount
}

// recordRequest returns nil when the request_id is accepted for replay tracking, or a denial
// when duplicate or when the replay map is saturated (fail closed — no eviction).
func (server *Server) recordRequest(controlSessionID string, capabilityRequest CapabilityRequest) *CapabilityResponse {
	requestKey := controlSessionID + ":" + capabilityRequest.RequestID
	server.mu.Lock()
	defer server.mu.Unlock()
	server.pruneExpiredLocked()
	if _, found := server.seenRequests[requestKey]; found {
		return &CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "duplicate request_id was rejected",
			DenialCode:   DenialCodeRequestReplayDetected,
		}
	}
	if len(server.seenRequests) >= server.maxSeenRequestReplayEntries {
		return &CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "request replay store is at capacity",
			DenialCode:   DenialCodeReplayStateSaturated,
		}
	}
	server.seenRequests[requestKey] = seenRequest{
		ControlSessionID: controlSessionID,
		SeenAt:           server.now().UTC(),
	}
	server.noteReplayWindowCandidateLocked(server.seenRequests[requestKey].SeenAt)
	return nil
}

func (server *Server) consumeExecutionToken(tokenClaims capabilityToken, capabilityRequest CapabilityRequest) (CapabilityResponse, bool) {
	if strings.TrimSpace(tokenClaims.BoundCapability) != "" && tokenClaims.BoundCapability != capabilityRequest.Capability {
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "capability token binding does not match requested capability",
			DenialCode:   DenialCodeCapabilityTokenBindingInvalid,
		}, true
	}
	if strings.TrimSpace(tokenClaims.BoundArgumentHash) != "" && tokenClaims.BoundArgumentHash != normalizedArgumentHash(capabilityRequest.Arguments) {
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "capability token binding does not match normalized arguments",
			DenialCode:   DenialCodeCapabilityTokenBindingInvalid,
		}, true
	}
	if !tokenClaims.SingleUse {
		return CapabilityResponse{}, false
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	server.pruneExpiredLocked()
	if _, alreadyUsed := server.usedTokens[tokenClaims.TokenID]; alreadyUsed {
		return CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       ResponseStatusDenied,
			DenialReason: "single-use capability token was already consumed",
			DenialCode:   DenialCodeCapabilityTokenReused,
		}, true
	}
	server.usedTokens[tokenClaims.TokenID] = usedToken{
		TokenID:           tokenClaims.TokenID,
		ParentTokenID:     tokenClaims.ParentTokenID,
		ControlSessionID:  tokenClaims.ControlSessionID,
		Capability:        capabilityRequest.Capability,
		NormalizedArgHash: normalizedArgumentHash(capabilityRequest.Arguments),
		ConsumedAt:        server.now().UTC(),
	}
	server.noteReplayWindowCandidateLocked(server.usedTokens[tokenClaims.TokenID].ConsumedAt)
	return CapabilityResponse{}, false
}

// recordAuthNonce returns nil if the nonce is new and recorded, a denial for replay, or a
// denial when the nonce map is saturated (fail closed).
func (server *Server) recordAuthNonce(controlSessionID string, requestNonce string) *CapabilityResponse {
	nonceKey := controlSessionID + ":" + requestNonce
	server.mu.Lock()
	server.pruneExpiredLocked()
	if _, found := server.seenAuthNonces[nonceKey]; found {
		server.mu.Unlock()
		return &CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request nonce replay was rejected",
			DenialCode:   DenialCodeRequestNonceReplayDetected,
		}
	}
	if len(server.seenAuthNonces) >= server.maxAuthNonceReplayEntries {
		server.mu.Unlock()
		return &CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request nonce replay store is at capacity",
			DenialCode:   DenialCodeReplayStateSaturated,
		}
	}
	server.seenAuthNonces[nonceKey] = seenRequest{
		ControlSessionID: controlSessionID,
		SeenAt:           server.now().UTC(),
	}
	server.noteReplayWindowCandidateLocked(server.seenAuthNonces[nonceKey].SeenAt)
	stateFile := nonceReplaySnapshot(server.seenAuthNonces)
	server.mu.Unlock()

	jsonBytes, err := json.Marshal(stateFile)
	if err != nil {
		server.mu.Lock()
		delete(server.seenAuthNonces, nonceKey)
		server.mu.Unlock()
		return &CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "nonce replay state is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		}
	}
	if err := atomicWritePrivateJSON(server.noncePath, jsonBytes); err != nil {
		server.mu.Lock()
		delete(server.seenAuthNonces, nonceKey)
		server.mu.Unlock()
		return &CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "nonce replay state is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		}
	}
	return nil
}
