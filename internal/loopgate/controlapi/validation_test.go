package controlapi

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

	t.Run("decode accepts echoed tool metadata", func(t *testing.T) {
		raw := []byte(`{"request_id":"req_123","session_id":"control_123","actor":"operator","capability":"fs_list","arguments":{},"ToolName":"wrong.tool","tool_name":"also_wrong","toolName":"camel","tool_use_id":"u1","ToolUseID":"u2","tool_call_id":"c1","ToolCallID":"c2"}`)
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
	})

	t.Run("marshal json omits echoed provider fields", func(t *testing.T) {
		request := CapabilityRequest{
			RequestID:                 "r1",
			SessionID:                 "s1",
			Actor:                     "operator",
			Capability:                "fs_list",
			Arguments:                 map[string]string{},
			EchoedNativeToolName:      "should_not_appear",
			EchoedNativeToolNameCamel: "neither",
			EchoedNativeToolCallID:    "nor",
		}
		out, err := json.Marshal(request)
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

func TestValidateMCPGatewayInvocationRequest_NormalizesArgumentsDeterministically(t *testing.T) {
	validatedRequest, err := ValidateMCPGatewayInvocationRequest(MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"beta":  json.RawMessage(" 2 "),
			"alpha": json.RawMessage(` "loopgate" `),
		},
	})
	if err != nil {
		t.Fatalf("validate invocation request: %v", err)
	}

	expectedKeys := []string{"alpha", "beta"}
	if len(validatedRequest.ValidatedArgKeys) != len(expectedKeys) {
		t.Fatalf("validated key count = %d, want %d", len(validatedRequest.ValidatedArgKeys), len(expectedKeys))
	}
	for index := range expectedKeys {
		if validatedRequest.ValidatedArgKeys[index] != expectedKeys[index] {
			t.Fatalf("validated key %d = %q, want %q", index, validatedRequest.ValidatedArgKeys[index], expectedKeys[index])
		}
	}
	if string(validatedRequest.Arguments["alpha"]) != `"loopgate"` {
		t.Fatalf("alpha argument = %s", string(validatedRequest.Arguments["alpha"]))
	}
	if string(validatedRequest.Arguments["beta"]) != "2" {
		t.Fatalf("beta argument = %s", string(validatedRequest.Arguments["beta"]))
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
			},
			"extra": {
				Origin:         ResultFieldOriginRemote,
				ContentType:    "text/plain",
				Trust:          ResultFieldTrustDeterministic,
				Sensitivity:    ResultFieldSensitivityTaintedText,
				SizeBytes:      len("extra"),
				Kind:           ResultFieldKindScalar,
				PromptEligible: false,
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

func TestUIValidationHelpers_DenyBlankOrUnknownValues(t *testing.T) {
	t.Run("operator mount write grant update", func(t *testing.T) {
		if err := (UIOperatorMountWriteGrantUpdateRequest{
			RootPath: "/tmp/workspace",
			Action:   OperatorMountWriteGrantActionRenew,
		}).Validate(); err != nil {
			t.Fatalf("expected valid operator mount write grant update: %v", err)
		}
		if err := (UIOperatorMountWriteGrantUpdateRequest{
			RootPath: "/tmp/workspace",
			Action:   "delete",
		}).Validate(); err == nil {
			t.Fatal("expected unknown action to be denied")
		}
	})

	t.Run("folder access update", func(t *testing.T) {
		if err := (FolderAccessUpdateRequest{GrantedIDs: []string{"downloads", "desktop"}}).Validate(); err != nil {
			t.Fatalf("expected valid folder access update: %v", err)
		}
		if err := (FolderAccessUpdateRequest{GrantedIDs: []string{"downloads", ""}}).Validate(); err == nil {
			t.Fatal("expected blank granted id to be denied")
		}
	})

	t.Run("ui approval decision", func(t *testing.T) {
		approved := true
		if err := (UIApprovalDecisionRequest{Approved: &approved}).Validate(); err != nil {
			t.Fatalf("expected valid ui approval decision: %v", err)
		}
		if err := (UIApprovalDecisionRequest{}).Validate(); err == nil {
			t.Fatal("expected missing approval decision to be denied")
		}
	})

	t.Run("ui event envelope", func(t *testing.T) {
		if err := ValidateUIEventEnvelope(UIEventEnvelope{
			ID:   "evt_123",
			Type: UIEventTypeToolResult,
			Data: UIEventToolResult{RequestID: "req_123"},
		}); err != nil {
			t.Fatalf("expected valid ui event envelope: %v", err)
		}
		if err := ValidateUIEventEnvelope(UIEventEnvelope{
			ID:   "evt_123",
			Type: UIEventTypeToolResult,
		}); err == nil {
			t.Fatal("expected missing ui event data to be denied")
		}
	})
}

func TestSandboxValidationHelpers_RequirePaths(t *testing.T) {
	if err := (SandboxImportRequest{
		HostSourcePath:  "/tmp/input.txt",
		DestinationName: "input.txt",
	}).Validate(); err != nil {
		t.Fatalf("expected valid sandbox import request: %v", err)
	}
	if err := (SandboxImportRequest{DestinationName: "input.txt"}).Validate(); err == nil {
		t.Fatal("expected missing host_source_path to be denied")
	}
	if err := (SandboxStageRequest{SandboxSourcePath: "input.txt"}).Validate(); err == nil {
		t.Fatal("expected missing output_name to be denied")
	}
	if err := (SandboxMetadataRequest{}).Validate(); err == nil {
		t.Fatal("expected missing sandbox_source_path to be denied")
	}
	if err := (SandboxExportRequest{SandboxSourcePath: "input.txt"}).Validate(); err == nil {
		t.Fatal("expected missing host_destination_path to be denied")
	}
}
