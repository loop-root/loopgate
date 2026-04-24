package loopgate

import (
	"fmt"
	"strings"

	"loopgate/internal/config"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	policypkg "loopgate/internal/policy"
)

// ExplainClaudeCodeHookDecision evaluates a Claude Code-style PreToolUse
// request against the signed local policy without starting the daemon,
// appending audit, or creating approval state.
func ExplainClaudeCodeHookDecision(repoRoot string, req controlapipkg.HookPreValidateRequest) (controlapipkg.HookPreValidateResponse, error) {
	policyLoadResult, err := config.LoadPolicyWithHash(repoRoot)
	if err != nil {
		return controlapipkg.HookPreValidateResponse{}, fmt.Errorf("load signed policy: %w", err)
	}
	operatorOverrideLoadResult, err := config.LoadOperatorOverrideDocumentWithHash(repoRoot)
	if err != nil {
		return controlapipkg.HookPreValidateResponse{}, fmt.Errorf("load signed operator overrides: %w", err)
	}

	server := &Server{
		repoRoot:            repoRoot,
		policy:              policyLoadResult.Policy,
		policyContentSHA256: policyLoadResult.ContentSHA256,
		checker:             policypkg.NewChecker(policyLoadResult.Policy),
		operatorOverrideRuntime: serverOperatorOverrideRuntime{
			document:       operatorOverrideLoadResult.Document,
			contentSHA256:  operatorOverrideLoadResult.ContentSHA256,
			signatureKeyID: operatorOverrideLoadResult.SignatureKeyID,
			present:        operatorOverrideLoadResult.Present,
		},
	}

	toolDef, known := claudeCodeToolMap[strings.TrimSpace(req.ToolName)]
	if !known {
		if policyLoadResult.Policy.ClaudeCodeDenyUnknownTools() {
			return controlapipkg.HookPreValidateResponse{
				Decision:   "block",
				Reason:     "tool not in governance map - denied by default",
				ReasonCode: controlapipkg.HookReasonCodePolicyDenied,
				DenialCode: controlapipkg.DenialCodeHookUnknownTool,
			}, nil
		}
		return controlapipkg.HookPreValidateResponse{
			Decision:   "allow",
			ReasonCode: controlapipkg.HookReasonCodePolicyAllowed,
		}, nil
	}

	result := server.evaluateClaudeCodeHookPolicy(req, toolDef)
	operatorOverrideClass, operatorOverrideMaxDelegation, hasOperatorOverrideClass := policyLoadResult.Policy.ClaudeCodeToolOperatorOverride(req.ToolName)

	decision := "block"
	denialCode := controlapipkg.DenialCodePolicyDenied
	switch result.Decision {
	case policypkg.Allow:
		decision = "allow"
		denialCode = ""
	case policypkg.NeedsApproval:
		decision = "ask"
		denialCode = ""
	default:
		decision = "block"
	}
	decisionMetadata := buildHookDecisionMetadata(decision, result, operatorOverrideMaxDelegation)

	response := controlapipkg.HookPreValidateResponse{
		Decision:   decision,
		ReasonCode: decisionMetadata.reasonCode,
	}
	if strings.TrimSpace(result.Reason) != "" {
		response.Reason = result.Reason
	}
	if strings.TrimSpace(denialCode) != "" {
		response.DenialCode = denialCode
	}
	if decisionMetadata.approvalOwner != "" {
		response.ApprovalOwner = decisionMetadata.approvalOwner
	}
	if len(decisionMetadata.approvalOptions) > 0 {
		response.ApprovalOptions = decisionMetadata.approvalOptions
	}
	if hasOperatorOverrideClass {
		response.OperatorOverrideClass = operatorOverrideClass
		response.OperatorOverrideMaxDelegation = operatorOverrideMaxDelegation
	}
	return response, nil
}
