package loopgate

const (
	ApprovalClassReadSandboxPath    = "read_sandbox_path"
	ApprovalClassReadHostFolder     = "read_host_folder"
	ApprovalClassWriteHostFolder    = "write_host_folder"
	ApprovalClassApplyHostPlan      = "apply_host_organization_plan"
	ApprovalClassWriteSandboxPath   = "write_sandbox_path"
	ApprovalClassExportSandboxArt   = "export_sandbox_artifact"
	ApprovalClassLaunchMorphling    = "launch_morphling"
	ApprovalClassMCPGatewayInvoke   = "mcp_gateway_invoke"
	ApprovalClassProviderCapability = "provider_capability"
	ApprovalClassCreateTrustDraft   = "create_trust_draft"
)

func ApprovalClassLabel(approvalClass string) string {
	switch approvalClass {
	case ApprovalClassReadSandboxPath:
		return "read sandbox path"
	case ApprovalClassReadHostFolder:
		return "read host folder"
	case ApprovalClassWriteHostFolder:
		return "write host folder"
	case ApprovalClassApplyHostPlan:
		return "apply host organization plan"
	case ApprovalClassWriteSandboxPath:
		return "write sandbox path"
	case ApprovalClassExportSandboxArt:
		return "export sandbox artifact"
	case ApprovalClassLaunchMorphling:
		return "launch morphling"
	case ApprovalClassMCPGatewayInvoke:
		return "invoke MCP gateway tool"
	case ApprovalClassProviderCapability:
		return "provider capability"
	case ApprovalClassCreateTrustDraft:
		return "create trust draft"
	default:
		return ""
	}
}

func (server *Server) approvalClassForCapability(capabilityName string) string {
	switch capabilityName {
	case "fs_read", "fs_list":
		return ApprovalClassReadSandboxPath
	case "host.folder.list", "host.folder.read", "host.organize.plan":
		return ApprovalClassReadHostFolder
	case "operator_mount.fs_write", "operator_mount.fs_mkdir":
		return ApprovalClassWriteHostFolder
	case "host.plan.apply":
		return ApprovalClassApplyHostPlan
	case "fs_write":
		return ApprovalClassWriteSandboxPath
	default:
		if _, found := server.configuredCapabilities[capabilityName]; found {
			return ApprovalClassProviderCapability
		}
		return ""
	}
}
