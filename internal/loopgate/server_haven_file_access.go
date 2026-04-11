package loopgate

import (
	"context"
	"fmt"

	"morph/internal/secrets"
)

func (server *Server) havenReadFileViaCapability(ctx context.Context, tokenClaims capabilityToken, sandboxPath string) (string, error) {
	capReq := CapabilityRequest{
		RequestID:  fmt.Sprintf("haven-ui-fs-read-%d", server.now().UnixNano()),
		Capability: "fs_read",
		Arguments: map[string]string{
			"path": sandboxPath,
		},
	}
	resp := server.executeCapabilityRequest(ctx, tokenClaims, capReq, false)
	if resp.Status != ResponseStatusSuccess {
		reason := resp.DenialReason
		if reason == "" {
			reason = "read denied"
		}
		return "", fmt.Errorf("%s", secrets.RedactText(reason))
	}
	content, _ := resp.StructuredResult["content"].(string)
	return content, nil
}
