package loopgate

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestValidateGrantType(t *testing.T) {
	validGrantTypes := []string{
		GrantTypePublicRead,
		GrantTypePKCE,
		GrantTypeAuthorizationCode,
		GrantTypeClientCredentials,
	}
	for _, validGrantType := range validGrantTypes {
		if err := ValidateGrantType(validGrantType); err != nil {
			t.Fatalf("expected grant type %q to validate: %v", validGrantType, err)
		}
	}

	if err := ValidateGrantType("../../oauth"); err == nil {
		t.Fatal("expected unsupported traversal-like grant type to be denied")
	}
}

func TestConnectionStatusValidate_DeniesUnsafeIdentifiers(t *testing.T) {
	validConnectionStatus := ConnectionStatus{
		Provider:           "github",
		GrantType:          GrantTypePKCE,
		Subject:            "user_123",
		Scopes:             []string{"repo.read", "issue.write"},
		Status:             "connected",
		SecureStoreRefID:   "ref_abc123",
		LastValidatedAtUTC: "2026-03-08T00:00:00Z",
		LastUsedAtUTC:      "2026-03-08T00:00:00Z",
		LastRotatedAtUTC:   "2026-03-08T00:00:00Z",
	}
	if err := validConnectionStatus.Validate(); err != nil {
		t.Fatalf("expected valid connection status: %v", err)
	}

	if err := (ConnectionStatus{
		Provider:  "../../etc",
		GrantType: GrantTypePKCE,
	}).Validate(); err == nil {
		t.Fatal("expected traversal-like provider to be denied")
	}

	if err := (ConnectionStatus{
		Provider:  "github",
		GrantType: GrantTypePKCE,
		Subject:   "$whoami",
	}).Validate(); err == nil {
		t.Fatal("expected shell-like subject to be denied")
	}
}

func TestOpenSessionRequestValidate_DeniesUnsafeRequestedCapability(t *testing.T) {
	validOpenSessionRequest := OpenSessionRequest{
		Actor:                 "morph",
		SessionID:             "session_123",
		RequestedCapabilities: []string{"fs_read", "fs_list"},
		CorrelationID:         "corr_123",
	}
	if err := validOpenSessionRequest.Validate(); err != nil {
		t.Fatalf("expected valid session request: %v", err)
	}

	invalidOpenSessionRequest := OpenSessionRequest{
		Actor:                 "morph",
		SessionID:             "session_123",
		RequestedCapabilities: []string{"../../etc"},
	}
	if err := invalidOpenSessionRequest.Validate(); err == nil {
		t.Fatal("expected traversal-like capability request to be denied")
	}
}

func TestCapabilityRequestValidate_DeniesUnsafeMetadataAndArgumentNames(t *testing.T) {
	validCapabilityRequest := CapabilityRequest{
		RequestID:     "req_123",
		SessionID:     "control_123",
		Actor:         "morph",
		Capability:    "fs_read",
		Arguments:     map[string]string{"path": "README.md"},
		CorrelationID: "corr_123",
	}
	if err := validCapabilityRequest.Validate(); err != nil {
		t.Fatalf("expected valid capability request: %v", err)
	}

	t.Run("decode_accepts_echoed_tool_name_then_normalize_strips", func(t *testing.T) {
		raw := []byte(`{"request_id":"req_123","session_id":"control_123","actor":"haven","capability":"fs_list","arguments":{},"ToolName":"wrong.tool","tool_name":"also_wrong","toolName":"camel","tool_use_id":"u1","ToolUseID":"u2","tool_call_id":"c1","ToolCallID":"c2"}`)
		var decoded CapabilityRequest
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if decoded.EchoedNativeToolName != "wrong.tool" || decoded.EchoedNativeToolNameSnake != "also_wrong" ||
			decoded.EchoedNativeToolNameCamel != "camel" || decoded.EchoedNativeToolUseIDSnake != "u1" ||
			decoded.EchoedNativeToolUseID != "u2" || decoded.EchoedNativeToolCallID != "c1" ||
			decoded.EchoedNativeToolCallIDAlt != "c2" {
			t.Fatalf("expected echoed fields populated: %+v", decoded)
		}
		normalized := normalizeCapabilityRequest(decoded)
		if normalized.EchoedNativeToolName != "" || normalized.EchoedNativeToolNameSnake != "" ||
			normalized.EchoedNativeToolNameCamel != "" || normalized.EchoedNativeToolUseID != "" ||
			normalized.EchoedNativeToolUseIDSnake != "" || normalized.EchoedNativeToolCallID != "" ||
			normalized.EchoedNativeToolCallIDAlt != "" {
			t.Fatalf("normalize should strip echoed tool metadata")
		}
		if normalized.Capability != "fs_list" {
			t.Fatalf("capability: %q", normalized.Capability)
		}
	})

	t.Run("marshal_json_omits_echoed_provider_fields", func(t *testing.T) {
		req := CapabilityRequest{
			RequestID:                 "r1",
			SessionID:                 "s1",
			Actor:                     "haven",
			Capability:                "fs_list",
			Arguments:                 map[string]string{},
			EchoedNativeToolName:      "should_not_appear",
			EchoedNativeToolNameCamel: "neither",
			EchoedNativeToolCallID:    "nor",
		}
		out, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(out, []byte("ToolName")) || bytes.Contains(out, []byte("toolName")) || bytes.Contains(out, []byte("tool_call_id")) {
			t.Fatalf("marshal leaked provider echo keys: %s", string(out))
		}
	})

	if err := (CapabilityRequest{
		RequestID:  "req_123",
		Capability: "$dump_tokens",
		Arguments:  map[string]string{"path": "README.md"},
	}).Validate(); err == nil {
		t.Fatal("expected shell-like capability name to be denied")
	}

	if err := (CapabilityRequest{
		RequestID:  "req_123",
		Capability: "fs_read",
		Arguments:  map[string]string{"../../key": "README.md"},
	}).Validate(); err == nil {
		t.Fatal("expected traversal-like argument name to be denied")
	}
}

func TestApprovalDecisionRequestValidate_DeniesUnsafeNonce(t *testing.T) {
	if err := (ApprovalDecisionRequest{Approved: true, DecisionNonce: "nonce_123"}).Validate(); err != nil {
		t.Fatalf("expected valid approval decision nonce: %v", err)
	}

	if err := (ApprovalDecisionRequest{Approved: true, DecisionNonce: "$replay"}).Validate(); err == nil {
		t.Fatal("expected shell-like decision nonce to be denied")
	}
}

func TestMCPGatewayInvocationRequestValidate_DeniesUnsafeArgumentName(t *testing.T) {
	validInvocationRequest := MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"query": json.RawMessage(`"loopgate"`),
		},
	}
	if err := validInvocationRequest.Validate(); err != nil {
		t.Fatalf("expected valid MCP gateway invocation request: %v", err)
	}

	if err := (MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"../../query": json.RawMessage(`"loopgate"`),
		},
	}).Validate(); err == nil {
		t.Fatal("expected traversal-like MCP gateway argument name to be denied")
	}

	if err := (MCPGatewayInvocationRequest{
		ServerID:  "github",
		ToolName:  "search_repositories",
		Arguments: nil,
	}).Validate(); err == nil {
		t.Fatal("expected missing MCP gateway arguments object to be denied")
	}
}

func TestCapabilityResponseResultClassification_RejectsMissingFieldMetadata(t *testing.T) {
	_, err := (CapabilityResponse{
		Status:           ResponseStatusSuccess,
		StructuredResult: map[string]interface{}{"message": "unsafe"},
		Classification: ResultClassification{
			Exposure: ResultExposureDisplay,
		},
	}).ResultClassification()
	if err == nil {
		t.Fatal("expected missing fields_meta to be rejected")
	}
}

func TestCapabilityResponseResultClassification_RejectsExtraFieldMetadata(t *testing.T) {
	_, err := (CapabilityResponse{
		Status:           ResponseStatusSuccess,
		StructuredResult: map[string]interface{}{"message": "unsafe"},
		FieldsMeta: map[string]ResultFieldMetadata{
			"message": {
				Origin:         ResultFieldOriginRemote,
				ContentType:    "text/plain",
				Trust:          ResultFieldTrustDeterministic,
				Sensitivity:    ResultFieldSensitivityTaintedText,
				SizeBytes:      len("unsafe"),
				Kind:           ResultFieldKindScalar,
				PromptEligible: false,
				MemoryEligible: false,
			},
			"extra": {
				Origin:         ResultFieldOriginRemote,
				ContentType:    "text/plain",
				Trust:          ResultFieldTrustDeterministic,
				Sensitivity:    ResultFieldSensitivityTaintedText,
				SizeBytes:      len("extra"),
				Kind:           ResultFieldKindScalar,
				PromptEligible: false,
				MemoryEligible: false,
			},
		},
		Classification: ResultClassification{
			Exposure: ResultExposureDisplay,
		},
	}).ResultClassification()
	if err == nil {
		t.Fatal("expected extra fields_meta entry to be rejected")
	}
}

func TestModelConnectionStoreRequest_MarshalJSON_RedactsSecretValue(t *testing.T) {
	req := ModelConnectionStoreRequest{
		ConnectionID: "conn_1",
		ProviderName: "openai",
		BaseURL:      "https://api.example.com",
		SecretValue:  "super-secret-key-must-not-appear",
	}
	out, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(out, []byte("super-secret-key-must-not-appear")) {
		t.Fatalf("marshaled json leaks secret: %s", string(out))
	}
}
