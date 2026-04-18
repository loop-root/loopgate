package loopgate

import (
	"crypto/sha256"
	"encoding/hex"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"path/filepath"
	"sort"
	"strings"

	policypkg "loopgate/internal/policy"
	toolspkg "loopgate/internal/tools"
)

func isHighRiskCapability(tool toolspkg.Tool, policyDecision policypkg.CheckResult) bool {
	if policyDecision.Decision == policypkg.NeedsApproval {
		return true
	}
	return tool.Operation() == toolspkg.OpWrite
}

func deriveExecutionToken(baseToken capabilityToken, capabilityRequest controlapipkg.CapabilityRequest) capabilityToken {
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

func normalizeCapabilityRequest(capabilityRequest controlapipkg.CapabilityRequest) controlapipkg.CapabilityRequest {
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
