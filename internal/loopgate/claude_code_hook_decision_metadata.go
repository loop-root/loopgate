package loopgate

import (
	"strings"

	"loopgate/internal/config"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	policypkg "loopgate/internal/policy"
)

type hookDecisionMetadata struct {
	reasonCode      string
	approvalOwner   string
	approvalOptions []string
}

func buildHookDecisionMetadata(decision string, result policypkg.CheckResult, maxDelegation string) hookDecisionMetadata {
	metadata := hookDecisionMetadata{
		reasonCode: hookReasonCode(decision, result),
	}
	if decision == "ask" {
		metadata.approvalOwner = controlapipkg.HookApprovalOwnerHarness
		metadata.approvalOptions = hookApprovalOptions(maxDelegation)
	}
	return metadata
}

func hookReasonCode(decision string, result policypkg.CheckResult) string {
	switch decision {
	case "allow":
		if strings.Contains(result.Reason, "delegated operator override") {
			return controlapipkg.HookReasonCodeOperatorOverrideAllowed
		}
		return controlapipkg.HookReasonCodePolicyAllowed
	case "ask":
		return controlapipkg.HookReasonCodeApprovalRequired
	case "block":
		return controlapipkg.HookReasonCodePolicyDenied
	default:
		return ""
	}
}

func hookApprovalOptions(maxDelegation string) []string {
	options := []string{controlapipkg.HookApprovalOptionOnce}
	switch strings.TrimSpace(maxDelegation) {
	case config.OperatorOverrideDelegationSession:
		options = append(options, controlapipkg.HookApprovalOptionSession)
	case config.OperatorOverrideDelegationPersistent:
		options = append(options, controlapipkg.HookApprovalOptionPersistent)
	}
	return options
}
