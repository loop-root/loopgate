package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"loopgate/internal/identifiers"
)

type CapabilityRequest struct {
	RequestID     string            `json:"request_id"`
	SessionID     string            `json:"session_id"`
	Actor         string            `json:"actor"`
	Capability    string            `json:"capability"`
	Arguments     map[string]string `json:"arguments"`
	CorrelationID string            `json:"correlation_id"`
	// The following fields accept mistaken copies of provider-native tool metadata
	// (OpenAI/Kimi/Moonshot shapes) into this envelope. They are stripped in
	// normalizeCapabilityRequest and must never influence policy — Capability is canonical.
	EchoedNativeToolName       string `json:"ToolName,omitempty"`
	EchoedNativeToolNameSnake  string `json:"tool_name,omitempty"`
	EchoedNativeToolNameCamel  string `json:"toolName,omitempty"`
	EchoedNativeToolUseID      string `json:"ToolUseID,omitempty"`
	EchoedNativeToolUseIDSnake string `json:"tool_use_id,omitempty"`
	EchoedNativeToolCallID     string `json:"tool_call_id,omitempty"`
	EchoedNativeToolCallIDAlt  string `json:"ToolCallID,omitempty"`
}

// MarshalJSON emits only the canonical capability-execute fields so provider-echo
// metadata decoded into the struct is never sent back on the wire (defense in depth).
func (request CapabilityRequest) MarshalJSON() ([]byte, error) {
	type capabilityRequestWire struct {
		RequestID     string            `json:"request_id"`
		SessionID     string            `json:"session_id"`
		Actor         string            `json:"actor"`
		Capability    string            `json:"capability"`
		Arguments     map[string]string `json:"arguments"`
		CorrelationID string            `json:"correlation_id"`
	}
	return json.Marshal(capabilityRequestWire{
		RequestID:     request.RequestID,
		SessionID:     request.SessionID,
		Actor:         request.Actor,
		Capability:    request.Capability,
		Arguments:     request.Arguments,
		CorrelationID: request.CorrelationID,
	})
}

func (request CapabilityRequest) Validate() error {
	if strings.TrimSpace(request.RequestID) != "" {
		if err := identifiers.ValidateSafeIdentifier("request_id", request.RequestID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(request.SessionID) != "" {
		if err := identifiers.ValidateSafeIdentifier("session_id", request.SessionID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(request.Actor) != "" {
		if err := identifiers.ValidateSafeIdentifier("actor", request.Actor); err != nil {
			return err
		}
	}
	if err := identifiers.ValidateSafeIdentifier("capability", request.Capability); err != nil {
		return err
	}
	if strings.TrimSpace(request.CorrelationID) != "" {
		if err := identifiers.ValidateSafeIdentifier("correlation_id", request.CorrelationID); err != nil {
			return err
		}
	}
	for argumentKey := range request.Arguments {
		if err := identifiers.ValidateSafeIdentifier("capability argument name", argumentKey); err != nil {
			return err
		}
	}
	return nil
}

func CloneCapabilityRequest(request CapabilityRequest) CapabilityRequest {
	clonedRequest := request
	if request.Arguments != nil {
		clonedRequest.Arguments = make(map[string]string, len(request.Arguments))
		for argumentKey, argumentValue := range request.Arguments {
			clonedRequest.Arguments[argumentKey] = argumentValue
		}
	}
	return clonedRequest
}

type ApprovalDecisionRequest struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
	// DecisionNonce is the single-use nonce issued at approval creation time. Required.
	DecisionNonce string `json:"decision_nonce"`
	// ApprovalManifestSHA256 is the canonical approval manifest hash per AMP RFC 0005 §6.
	// When provided, the server verifies it matches the manifest computed at approval creation
	// time, binding the decision to the exact method, path, and request body that was approved.
	// The server computes the manifest from the stored approval; the operator obtains this value
	// from the pending approval response and must include it to prove they reviewed the manifest.
	ApprovalManifestSHA256 string `json:"approval_manifest_sha256,omitempty"`
}

func (request ApprovalDecisionRequest) Validate() error {
	if strings.TrimSpace(request.DecisionNonce) == "" {
		return nil
	}
	return identifiers.ValidateSafeIdentifier("approval decision nonce", request.DecisionNonce)
}

func RequestBodySHA256(requestBody any) (string, error) {
	requestBytes, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("marshal request body: %w", err)
	}
	sum := sha256.Sum256(requestBytes)
	return hex.EncodeToString(sum[:]), nil
}
