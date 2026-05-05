package loopgate

import (
	"strings"
	"testing"
)

func TestBuildMCPGatewayJSONRPCFrame_OK(t *testing.T) {
	frameBytes, err := buildMCPGatewayJSONRPCFrame([]byte(`{"jsonrpc":"2.0"}`))
	if err != nil {
		t.Fatalf("build frame: %v", err)
	}
	frameText := string(frameBytes)
	if !strings.HasPrefix(frameText, "Content-Length: 17\r\n\r\n") {
		t.Fatalf("expected content-length header, got %q", frameText)
	}
	if !strings.HasSuffix(frameText, `{"jsonrpc":"2.0"}`) {
		t.Fatalf("expected frame body, got %q", frameText)
	}
}

func TestBuildMCPGatewayJSONRPCFrame_RejectsOversizeBody(t *testing.T) {
	bodyBytes := make([]byte, mcpGatewayMaxMessageBodyBytes+1)
	if _, err := buildMCPGatewayJSONRPCFrame(bodyBytes); err == nil || !strings.Contains(err.Error(), "body exceeds maximum size") {
		t.Fatalf("expected body size error, got %v", err)
	}
}
