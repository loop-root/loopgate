package loopgate

import (
	"loopgate/internal/controlruntime"
	"strings"
)

func (server *Server) checkFsReadRateLimit(controlSessionID string) bool {
	server.mu.Lock()
	defer server.mu.Unlock()

	if server.fsReadRateLimit <= 0 {
		return false
	}

	decision := controlruntime.CheckSlidingWindowRateLimit(server.replayState.sessionReadCounts[controlSessionID], server.fsReadRateLimit, fsReadRateWindow, server.now().UTC())
	server.replayState.sessionReadCounts[controlSessionID] = decision.Timestamps
	return decision.Denied
}

func (server *Server) checkHookPreValidateRateLimit(peerUID uint32) bool {
	server.mu.Lock()
	defer server.mu.Unlock()

	if server.hookPreValidateRateLimit <= 0 || server.hookPreValidateRateWindow <= 0 {
		return false
	}

	decision := controlruntime.CheckSlidingWindowRateLimit(server.replayState.hookPreValidateCounts[peerUID], server.hookPreValidateRateLimit, server.hookPreValidateRateWindow, server.now().UTC())
	server.replayState.hookPreValidateCounts[peerUID] = decision.Timestamps
	return decision.Denied
}

func (server *Server) checkHookPeerAuthFailureRateLimit(rateLimitKey string) bool {
	server.mu.Lock()
	defer server.mu.Unlock()

	if server.hookPeerAuthFailureRateLimit <= 0 || server.hookPeerAuthFailureWindow <= 0 {
		return false
	}

	trimmedRateLimitKey := strings.TrimSpace(rateLimitKey)
	if trimmedRateLimitKey == "" {
		trimmedRateLimitKey = "unknown"
	}

	decision := controlruntime.CheckSlidingWindowRateLimit(server.replayState.hookPeerAuthFailureCounts[trimmedRateLimitKey], server.hookPeerAuthFailureRateLimit, server.hookPeerAuthFailureWindow, server.now().UTC())
	server.replayState.hookPeerAuthFailureCounts[trimmedRateLimitKey] = decision.Timestamps
	return decision.Denied
}
