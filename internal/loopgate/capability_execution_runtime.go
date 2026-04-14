package loopgate

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	policypkg "morph/internal/policy"
	toolspkg "morph/internal/tools"
)

func isHighRiskCapability(tool toolspkg.Tool, policyDecision policypkg.CheckResult) bool {
	if policyDecision.Decision == policypkg.NeedsApproval {
		return true
	}
	if trustedTool, ok := tool.(interface{ TrustedSandboxLocal() bool }); ok && trustedTool.TrustedSandboxLocal() {
		return false
	}
	return tool.Operation() == toolspkg.OpWrite
}

func (server *Server) hasTrustedHavenSession(tokenClaims capabilityToken) bool {
	if !strings.EqualFold(strings.TrimSpace(tokenClaims.ActorLabel), "haven") {
		return false
	}
	server.mu.Lock()
	controlSession, sessionFound := server.sessions[tokenClaims.ControlSessionID]
	server.mu.Unlock()
	if !sessionFound {
		return false
	}
	return controlSession.TrustedHavenClient
}

func (server *Server) requireTrustedHavenSession(writer http.ResponseWriter, tokenClaims capabilityToken, denialReason string) bool {
	if server.hasTrustedHavenSession(tokenClaims) {
		return true
	}
	normalizedDenialReason := strings.TrimSpace(denialReason)
	if normalizedDenialReason == "" {
		normalizedDenialReason = "trusted Haven session required"
	}
	server.writeJSON(writer, 403, CapabilityResponse{
		Status:       ResponseStatusDenied,
		DenialReason: normalizedDenialReason,
		DenialCode:   DenialCodeCapabilityTokenInvalid,
	})
	return false
}

func (server *Server) shouldAutoAllowTrustedSandboxCapability(tokenClaims capabilityToken, capabilityName string, tool toolspkg.Tool, policyDecision policypkg.CheckResult) bool {
	if policyDecision.Decision != policypkg.NeedsApproval {
		return false
	}
	policyRuntime := server.currentPolicyRuntime()
	// Host-mounted project tools are never "trusted sandbox" work. They reach the
	// real host filesystem under operator-granted roots and must preserve normal
	// approval semantics even if someone accidentally tags them trusted later.
	if strings.HasPrefix(strings.TrimSpace(capabilityName), "operator_mount.") {
		return false
	}
	if !policyRuntime.policy.HavenTrustedSandboxAutoAllowEnabled() {
		return false
	}
	if !policyRuntime.policy.HavenTrustedSandboxAutoAllowMatchesCapability(capabilityName) {
		return false
	}
	if !server.hasTrustedHavenSession(tokenClaims) {
		return false
	}
	trustedTool, ok := tool.(interface{ TrustedSandboxLocal() bool })
	return ok && trustedTool.TrustedSandboxLocal()
}

func deriveExecutionToken(baseToken capabilityToken, capabilityRequest CapabilityRequest) capabilityToken {
	derivedTokenID := "exec:" + baseToken.TokenID + ":" + capabilityRequest.RequestID
	return capabilityToken{
		TokenID:             derivedTokenID,
		ControlSessionID:    baseToken.ControlSessionID,
		ActorLabel:          baseToken.ActorLabel,
		ClientSessionLabel:  baseToken.ClientSessionLabel,
		AllowedCapabilities: capabilitySet([]string{capabilityRequest.Capability}),
		PeerIdentity:        baseToken.PeerIdentity,
		TenantID:            baseToken.TenantID,
		UserID:              baseToken.UserID,
		ExpiresAt:           baseToken.ExpiresAt,
		SingleUse:           true,
		ApprovedExecution:   baseToken.ApprovedExecution,
		BoundCapability:     capabilityRequest.Capability,
		BoundArgumentHash:   normalizedArgumentHash(capabilityRequest.Arguments),
		ParentTokenID:       baseToken.TokenID,
	}
}

func normalizedArgumentHash(arguments map[string]string) string {
	if len(arguments) == 0 {
		return ""
	}
	argumentKeys := make([]string, 0, len(arguments))
	for argumentKey := range arguments {
		argumentKeys = append(argumentKeys, argumentKey)
	}
	sort.Strings(argumentKeys)

	hasher := sha256.New()
	for _, argumentKey := range argumentKeys {
		_, _ = hasher.Write([]byte(argumentKey))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(arguments[argumentKey]))
		_, _ = hasher.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func normalizeCapabilityRequest(capabilityRequest CapabilityRequest) CapabilityRequest {
	capabilityRequest.RequestID = strings.TrimSpace(capabilityRequest.RequestID)
	capabilityRequest.SessionID = strings.TrimSpace(capabilityRequest.SessionID)
	capabilityRequest.Actor = strings.TrimSpace(capabilityRequest.Actor)
	capabilityRequest.Capability = strings.TrimSpace(capabilityRequest.Capability)
	capabilityRequest.CorrelationID = strings.TrimSpace(capabilityRequest.CorrelationID)
	capabilityRequest.EchoedNativeToolName = ""
	capabilityRequest.EchoedNativeToolNameSnake = ""
	capabilityRequest.EchoedNativeToolNameCamel = ""
	capabilityRequest.EchoedNativeToolUseID = ""
	capabilityRequest.EchoedNativeToolUseIDSnake = ""
	capabilityRequest.EchoedNativeToolCallID = ""
	capabilityRequest.EchoedNativeToolCallIDAlt = ""

	if capabilityRequest.Arguments == nil {
		return capabilityRequest
	}

	normalizedArguments := make(map[string]string, len(capabilityRequest.Arguments))
	for argumentKey, rawArgumentValue := range capabilityRequest.Arguments {
		normalizedValue := rawArgumentValue
		if argumentKey != "content" {
			normalizedValue = strings.TrimSpace(normalizedValue)
		}
		if argumentKey == "path" && strings.TrimSpace(normalizedValue) != "" {
			normalizedValue = filepath.Clean(normalizedValue)
		}
		normalizedArguments[argumentKey] = normalizedValue
	}
	capabilityRequest.Arguments = normalizedArguments
	return capabilityRequest
}

func capabilitySet(capabilities []string) map[string]struct{} {
	set := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if trimmedCapability := strings.TrimSpace(capability); trimmedCapability != "" {
			set[trimmedCapability] = struct{}{}
		}
	}
	return set
}

func normalizedCapabilityList(capabilities []string) []string {
	normalized := make([]string, 0, len(capabilities))
	seenCapabilities := make(map[string]struct{}, len(capabilities))
	for _, rawCapability := range capabilities {
		trimmedCapability := strings.TrimSpace(rawCapability)
		if trimmedCapability == "" {
			continue
		}
		if _, seen := seenCapabilities[trimmedCapability]; seen {
			continue
		}
		seenCapabilities[trimmedCapability] = struct{}{}
		normalized = append(normalized, trimmedCapability)
	}
	return normalized
}

func isSecretExportCapabilityHeuristic(capability string) bool {
	lowerCapability := strings.ToLower(strings.TrimSpace(capability))
	if lowerCapability == "" {
		return false
	}

	sensitivePrefixes := []string{
		"secret.",
		"token.",
		"credential.",
		"credentials.",
		"key.",
	}
	for _, sensitivePrefix := range sensitivePrefixes {
		if strings.HasPrefix(lowerCapability, sensitivePrefix) {
			return true
		}
	}

	if strings.Contains(lowerCapability, "export") && (strings.Contains(lowerCapability, "token") || strings.Contains(lowerCapability, "secret") || strings.Contains(lowerCapability, "credential") || strings.Contains(lowerCapability, "key")) {
		return true
	}
	return false
}

func copyCapabilitySet(input map[string]struct{}) map[string]struct{} {
	if len(input) == 0 {
		return map[string]struct{}{}
	}
	copied := make(map[string]struct{}, len(input))
	for capability := range input {
		copied[capability] = struct{}{}
	}
	return copied
}
