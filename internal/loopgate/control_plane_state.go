package loopgate

import (
	"bytes"
	"encoding/json"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"slices"
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

	for tokenString, tokenClaims := range server.sessionState.tokens {
		if nowUTC.After(tokenClaims.ExpiresAt) {
			delete(server.sessionState.tokens, tokenString)
			continue
		}
		noteNextSweepCandidate(tokenClaims.ExpiresAt)
	}
	for controlSessionID, activeSession := range server.sessionState.sessions {
		if nowUTC.After(activeSession.ExpiresAt) {
			delete(server.sessionState.sessions, controlSessionID)
			delete(server.approvalState.tokenIndex, approvalTokenHash(activeSession.ApprovalToken))
			continue
		}
		noteNextSweepCandidate(activeSession.ExpiresAt)
	}
	for approvalID, pendingApproval := range server.approvalState.records {
		if nowUTC.After(pendingApproval.ExpiresAt) {
			if pendingApproval.State == approvalStatePending {
				expiredApproval, transitionErr := setApprovalStateLocked(server.approvalState.records, approvalID, pendingApproval, approvalStateExpired)
				if transitionErr != nil {
					noteNextSweepCandidate(pendingApproval.ExpiresAt)
					continue
				}
				pendingApproval = expiredApproval
				noteNextSweepCandidate(pendingApproval.ExpiresAt.Add(requestReplayWindow))
				continue
			}
			if nowUTC.Sub(pendingApproval.ExpiresAt) > requestReplayWindow {
				delete(server.approvalState.records, approvalID)
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
				expiredApprovalRequest, transitionErr := setMCPGatewayApprovalStateLocked(server.mcpGatewayApprovalRequests, approvalRequestID, approvalRequest, approvalStateExpired)
				if transitionErr != nil {
					noteNextSweepCandidate(approvalRequest.ExpiresAt)
					continue
				}
				approvalRequest = expiredApprovalRequest
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
	for requestKey, seenRequest := range server.replayState.seenRequests {
		if nowUTC.Sub(seenRequest.SeenAt) > requestReplayWindow {
			delete(server.replayState.seenRequests, requestKey)
			continue
		}
		noteNextSweepCandidate(seenRequest.SeenAt.Add(requestReplayWindow))
	}
	for nonceKey, seenNonce := range server.replayState.seenAuthNonces {
		if nowUTC.Sub(seenNonce.SeenAt) > requestReplayWindow {
			delete(server.replayState.seenAuthNonces, nonceKey)
			continue
		}
		noteNextSweepCandidate(seenNonce.SeenAt.Add(requestReplayWindow))
	}
	for tokenID, consumedToken := range server.replayState.usedTokens {
		if nowUTC.Sub(consumedToken.ConsumedAt) > requestReplayWindow {
			delete(server.replayState.usedTokens, tokenID)
			continue
		}
		noteNextSweepCandidate(consumedToken.ConsumedAt.Add(requestReplayWindow))
	}
	for controlSessionID, readTimestamps := range server.replayState.sessionReadCounts {
		prunedReadTimestamps := readTimestamps[:0]
		for _, readTimestamp := range readTimestamps {
			if nowUTC.Sub(readTimestamp) >= fsReadRateWindow {
				continue
			}
			prunedReadTimestamps = append(prunedReadTimestamps, readTimestamp)
			noteNextSweepCandidate(readTimestamp.Add(fsReadRateWindow))
		}
		if len(prunedReadTimestamps) == 0 {
			delete(server.replayState.sessionReadCounts, controlSessionID)
			continue
		}
		server.replayState.sessionReadCounts[controlSessionID] = prunedReadTimestamps
	}
	for peerUID, hookTimestamps := range server.replayState.hookPreValidateCounts {
		prunedHookTimestamps := hookTimestamps[:0]
		for _, hookTimestamp := range hookTimestamps {
			if nowUTC.Sub(hookTimestamp) >= server.hookPreValidateRateWindow {
				continue
			}
			prunedHookTimestamps = append(prunedHookTimestamps, hookTimestamp)
			noteNextSweepCandidate(hookTimestamp.Add(server.hookPreValidateRateWindow))
		}
		if len(prunedHookTimestamps) == 0 {
			delete(server.replayState.hookPreValidateCounts, peerUID)
			continue
		}
		server.replayState.hookPreValidateCounts[peerUID] = prunedHookTimestamps
	}
	for rateLimitKey, failureTimestamps := range server.replayState.hookPeerAuthFailureCounts {
		prunedFailureTimestamps := failureTimestamps[:0]
		for _, failureTimestamp := range failureTimestamps {
			if nowUTC.Sub(failureTimestamp) >= server.hookPeerAuthFailureWindow {
				continue
			}
			prunedFailureTimestamps = append(prunedFailureTimestamps, failureTimestamp)
			noteNextSweepCandidate(failureTimestamp.Add(server.hookPeerAuthFailureWindow))
		}
		if len(prunedFailureTimestamps) == 0 {
			delete(server.replayState.hookPeerAuthFailureCounts, rateLimitKey)
			continue
		}
		server.replayState.hookPeerAuthFailureCounts[rateLimitKey] = prunedFailureTimestamps
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

type authNonceReplayStore interface {
	Load(nowUTC time.Time) (map[string]seenRequest, error)
	Record(nonceKey string, seenNonce seenRequest) error
	Compact(seenAuthNonces map[string]seenRequest) error
}

type snapshotNonceReplayStore struct {
	path string
}

func (store snapshotNonceReplayStore) Load(nowUTC time.Time) (map[string]seenRequest, error) {
	rawBytes, err := os.ReadFile(store.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]seenRequest{}, nil
		}
		return nil, fmt.Errorf("read nonce replay state: %w", err)
	}
	var stateFile nonceReplayFile
	if err := json.Unmarshal(rawBytes, &stateFile); err != nil {
		return nil, fmt.Errorf("decode nonce replay state: %w", err)
	}
	loadedNonces := make(map[string]seenRequest, len(stateFile.Nonces))
	for nonceKey, entry := range stateFile.Nonces {
		seenAt, parseErr := time.Parse(time.RFC3339Nano, entry.SeenAt)
		if parseErr != nil {
			continue
		}
		if nowUTC.Sub(seenAt) > requestReplayWindow {
			continue
		}
		loadedNonces[nonceKey] = seenRequest{
			ControlSessionID: entry.ControlSessionID,
			SeenAt:           seenAt,
		}
	}
	return loadedNonces, nil
}

func (store snapshotNonceReplayStore) Record(nonceKey string, seenNonce seenRequest) error {
	seenAuthNonces, err := store.Load(seenNonce.SeenAt.UTC())
	if err != nil {
		return err
	}
	seenAuthNonces[nonceKey] = seenNonce
	return store.Compact(seenAuthNonces)
}

func (store snapshotNonceReplayStore) Compact(seenAuthNonces map[string]seenRequest) error {
	stateFile := nonceReplaySnapshot(seenAuthNonces)
	jsonBytes, err := json.Marshal(stateFile)
	if err != nil {
		return fmt.Errorf("marshal nonce replay state: %w", err)
	}
	if err := atomicWritePrivateJSON(store.path, jsonBytes); err != nil {
		return fmt.Errorf("persist nonce replay state: %w", err)
	}
	return nil
}

type nonceReplayLogRecord struct {
	NonceKey         string `json:"nonce_key"`
	ControlSessionID string `json:"control_session_id"`
	SeenAt           string `json:"seen_at"`
}

type appendOnlyNonceReplayStore struct {
	path               string
	legacySnapshotPath string
}

func (store appendOnlyNonceReplayStore) Load(nowUTC time.Time) (map[string]seenRequest, error) {
	if _, err := os.Stat(store.path); err == nil {
		return loadAppendOnlyNonceReplayLog(store.path, nowUTC)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat nonce replay log: %w", err)
	}
	if strings.TrimSpace(store.legacySnapshotPath) != "" {
		return snapshotNonceReplayStore{path: store.legacySnapshotPath}.Load(nowUTC)
	}
	return map[string]seenRequest{}, nil
}

func (store appendOnlyNonceReplayStore) Record(nonceKey string, seenNonce seenRequest) error {
	record := nonceReplayLogRecord{
		NonceKey:         nonceKey,
		ControlSessionID: seenNonce.ControlSessionID,
		SeenAt:           seenNonce.SeenAt.UTC().Format(time.RFC3339Nano),
	}
	recordBytes, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal nonce replay log record: %w", err)
	}
	recordBytes = append(recordBytes, '\n')
	return appendPrivateStateLine(store.path, recordBytes)
}

func (store appendOnlyNonceReplayStore) Compact(seenAuthNonces map[string]seenRequest) error {
	nonceKeys := make([]string, 0, len(seenAuthNonces))
	for nonceKey := range seenAuthNonces {
		nonceKeys = append(nonceKeys, nonceKey)
	}
	slices.Sort(nonceKeys)

	logBytes := make([]byte, 0, len(nonceKeys)*96)
	for _, nonceKey := range nonceKeys {
		seenNonce := seenAuthNonces[nonceKey]
		recordBytes, err := json.Marshal(nonceReplayLogRecord{
			NonceKey:         nonceKey,
			ControlSessionID: seenNonce.ControlSessionID,
			SeenAt:           seenNonce.SeenAt.UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			return fmt.Errorf("marshal compacted nonce replay log record: %w", err)
		}
		logBytes = append(logBytes, recordBytes...)
		logBytes = append(logBytes, '\n')
	}
	if err := atomicWritePrivateStateFile(store.path, logBytes); err != nil {
		return fmt.Errorf("persist compacted nonce replay log: %w", err)
	}
	return nil
}

func (server *Server) currentNonceReplayStore() authNonceReplayStore {
	if server.nonceReplayStore != nil {
		return server.nonceReplayStore
	}
	return snapshotNonceReplayStore{path: server.noncePath}
}

func copySeenRequests(source map[string]seenRequest) map[string]seenRequest {
	copied := make(map[string]seenRequest, len(source))
	for key, seen := range source {
		copied[key] = seen
	}
	return copied
}

func (server *Server) loadNonceReplayState() error {
	loadedNonces, err := server.currentNonceReplayStore().Load(server.now().UTC())
	if err != nil {
		return err
	}
	for nonceKey, seenNonce := range loadedNonces {
		server.replayState.seenAuthNonces[nonceKey] = seenNonce
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

func loadAppendOnlyNonceReplayLog(path string, nowUTC time.Time) (map[string]seenRequest, error) {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]seenRequest{}, nil
		}
		return nil, fmt.Errorf("read nonce replay log: %w", err)
	}
	if len(rawBytes) == 0 {
		return map[string]seenRequest{}, nil
	}

	lines := bytes.Split(rawBytes, []byte{'\n'})
	hasTrailingNewline := len(rawBytes) > 0 && rawBytes[len(rawBytes)-1] == '\n'
	loadedNonces := make(map[string]seenRequest)
	for lineIndex, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		isLastLine := lineIndex == len(lines)-1
		var record nonceReplayLogRecord
		if err := json.Unmarshal(line, &record); err != nil {
			if isLastLine && !hasTrailingNewline {
				continue
			}
			return nil, fmt.Errorf("decode nonce replay log line %d: %w", lineIndex+1, err)
		}
		seenAt, err := time.Parse(time.RFC3339Nano, record.SeenAt)
		if err != nil {
			if isLastLine && !hasTrailingNewline {
				continue
			}
			return nil, fmt.Errorf("parse nonce replay log timestamp on line %d: %w", lineIndex+1, err)
		}
		if nowUTC.Sub(seenAt) > requestReplayWindow {
			continue
		}
		loadedNonces[record.NonceKey] = seenRequest{
			ControlSessionID: record.ControlSessionID,
			SeenAt:           seenAt.UTC(),
		}
	}
	return loadedNonces, nil
}

func atomicWritePrivateStateFile(path string, fileBytes []byte) error {
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
	if _, err := tempFile.Write(fileBytes); err != nil {
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

func atomicWritePrivateJSON(path string, jsonBytes []byte) error {
	return atomicWritePrivateStateFile(path, jsonBytes)
}

func appendPrivateStateLine(path string, line []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	createdFile := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		createdFile = true
	} else if err != nil {
		return fmt.Errorf("stat state file: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open state file for append: %w", err)
	}
	if _, err := file.Write(line); err != nil {
		_ = file.Close()
		return fmt.Errorf("append state file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync state file append: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close state file append: %w", err)
	}
	if createdFile {
		if stateDir, err := os.Open(filepath.Dir(path)); err == nil {
			_ = stateDir.Sync()
			_ = stateDir.Close()
		}
	}
	return nil
}

func (server *Server) saveNonceReplayState() error {
	server.mu.Lock()
	stateSnapshot := copySeenRequests(server.replayState.seenAuthNonces)
	server.mu.Unlock()
	return server.currentNonceReplayStore().Compact(stateSnapshot)
}

func (server *Server) countPendingApprovalsForSessionLocked(controlSessionID string) int {
	pendingCount := 0
	for _, pendingApproval := range server.approvalState.records {
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
func (server *Server) recordRequest(controlSessionID string, capabilityRequest controlapipkg.CapabilityRequest) *controlapipkg.CapabilityResponse {
	requestKey := controlSessionID + ":" + capabilityRequest.RequestID
	server.mu.Lock()
	defer server.mu.Unlock()
	server.pruneExpiredLocked()
	if _, found := server.replayState.seenRequests[requestKey]; found {
		return &controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "duplicate request_id was rejected",
			DenialCode:   controlapipkg.DenialCodeRequestReplayDetected,
		}
	}
	if len(server.replayState.seenRequests) >= server.maxSeenRequestReplayEntries {
		return &controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "request replay store is at capacity",
			DenialCode:   controlapipkg.DenialCodeReplayStateSaturated,
		}
	}
	server.replayState.seenRequests[requestKey] = seenRequest{
		ControlSessionID: controlSessionID,
		SeenAt:           server.now().UTC(),
	}
	server.noteReplayWindowCandidateLocked(server.replayState.seenRequests[requestKey].SeenAt)
	return nil
}

func (server *Server) consumeExecutionToken(tokenClaims capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) (controlapipkg.CapabilityResponse, bool) {
	if strings.TrimSpace(tokenClaims.BoundCapability) != "" && tokenClaims.BoundCapability != capabilityRequest.Capability {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "capability token binding does not match requested capability",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenBindingInvalid,
		}, true
	}
	if strings.TrimSpace(tokenClaims.BoundArgumentHash) != "" && tokenClaims.BoundArgumentHash != normalizedArgumentHash(capabilityRequest.Arguments) {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "capability token binding does not match normalized arguments",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenBindingInvalid,
		}, true
	}
	if !tokenClaims.SingleUse {
		return controlapipkg.CapabilityResponse{}, false
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	server.pruneExpiredLocked()
	if _, alreadyUsed := server.replayState.usedTokens[tokenClaims.TokenID]; alreadyUsed {
		return controlapipkg.CapabilityResponse{
			RequestID:    capabilityRequest.RequestID,
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "single-use capability token was already consumed",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenReused,
		}, true
	}
	server.replayState.usedTokens[tokenClaims.TokenID] = usedToken{
		TokenID:           tokenClaims.TokenID,
		ParentTokenID:     tokenClaims.ParentTokenID,
		ControlSessionID:  tokenClaims.ControlSessionID,
		Capability:        capabilityRequest.Capability,
		NormalizedArgHash: normalizedArgumentHash(capabilityRequest.Arguments),
		ConsumedAt:        server.now().UTC(),
	}
	server.noteReplayWindowCandidateLocked(server.replayState.usedTokens[tokenClaims.TokenID].ConsumedAt)
	return controlapipkg.CapabilityResponse{}, false
}

// recordAuthNonce returns nil if the nonce is new and recorded, a denial for replay, or a
// denial when the nonce map is saturated (fail closed).
func (server *Server) recordAuthNonce(controlSessionID string, requestNonce string) *controlapipkg.CapabilityResponse {
	nonceKey := controlSessionID + ":" + requestNonce
	server.mu.Lock()
	server.pruneExpiredLocked()
	if _, found := server.replayState.seenAuthNonces[nonceKey]; found {
		server.mu.Unlock()
		return &controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "request nonce replay was rejected",
			DenialCode:   controlapipkg.DenialCodeRequestNonceReplayDetected,
		}
	}
	if len(server.replayState.seenAuthNonces) >= server.maxAuthNonceReplayEntries {
		server.mu.Unlock()
		return &controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "request nonce replay store is at capacity",
			DenialCode:   controlapipkg.DenialCodeReplayStateSaturated,
		}
	}
	server.replayState.seenAuthNonces[nonceKey] = seenRequest{
		ControlSessionID: controlSessionID,
		SeenAt:           server.now().UTC(),
	}
	recordedNonce := server.replayState.seenAuthNonces[nonceKey]
	server.noteReplayWindowCandidateLocked(recordedNonce.SeenAt)
	server.mu.Unlock()

	if err := server.currentNonceReplayStore().Record(nonceKey, recordedNonce); err != nil {
		server.mu.Lock()
		delete(server.replayState.seenAuthNonces, nonceKey)
		server.mu.Unlock()
		return &controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "nonce replay state is unavailable",
			DenialCode:   controlapipkg.DenialCodeAuditUnavailable,
		}
	}
	return nil
}
