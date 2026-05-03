package loopgate

import (
	"strings"
	"time"
)

func (server *Server) checkFsReadRateLimit(controlSessionID string) bool {
	server.mu.Lock()
	defer server.mu.Unlock()

	if server.fsReadRateLimit <= 0 {
		return false
	}

	nowUTC := server.now().UTC()
	cutoff := nowUTC.Add(-fsReadRateWindow)

	timestamps := server.replayState.sessionReadCounts[controlSessionID]
	// Prune old entries.
	pruned := make([]time.Time, 0, len(timestamps))
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			pruned = append(pruned, ts)
		}
	}
	if len(pruned) >= server.fsReadRateLimit {
		server.replayState.sessionReadCounts[controlSessionID] = pruned
		return true
	}
	server.replayState.sessionReadCounts[controlSessionID] = append(pruned, nowUTC)
	return false
}

func (server *Server) checkHookPreValidateRateLimit(peerUID uint32) bool {
	server.mu.Lock()
	defer server.mu.Unlock()

	if server.hookPreValidateRateLimit <= 0 || server.hookPreValidateRateWindow <= 0 {
		return false
	}

	nowUTC := server.now().UTC()
	cutoff := nowUTC.Add(-server.hookPreValidateRateWindow)

	timestamps := server.replayState.hookPreValidateCounts[peerUID]
	pruned := make([]time.Time, 0, len(timestamps))
	for _, timestamp := range timestamps {
		if timestamp.After(cutoff) {
			pruned = append(pruned, timestamp)
		}
	}
	if len(pruned) >= server.hookPreValidateRateLimit {
		server.replayState.hookPreValidateCounts[peerUID] = pruned
		return true
	}
	server.replayState.hookPreValidateCounts[peerUID] = append(pruned, nowUTC)
	return false
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

	nowUTC := server.now().UTC()
	cutoff := nowUTC.Add(-server.hookPeerAuthFailureWindow)

	timestamps := server.replayState.hookPeerAuthFailureCounts[trimmedRateLimitKey]
	pruned := make([]time.Time, 0, len(timestamps))
	for _, timestamp := range timestamps {
		if timestamp.After(cutoff) {
			pruned = append(pruned, timestamp)
		}
	}
	if len(pruned) >= server.hookPeerAuthFailureRateLimit {
		server.replayState.hookPeerAuthFailureCounts[trimmedRateLimitKey] = pruned
		return true
	}
	server.replayState.hookPeerAuthFailureCounts[trimmedRateLimitKey] = append(pruned, nowUTC)
	return false
}
