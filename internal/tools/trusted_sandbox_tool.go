package tools

// trustedSandboxLocalTool marks a tool instance as confined to Morph's own
// Haven sandbox, allowing the policy layer to keep in-world actions
// low-friction without weakening host-rooted registries.
type trustedSandboxLocalTool struct {
	Tool
}

func (tool trustedSandboxLocalTool) TrustedSandboxLocal() bool {
	return true
}

// WrapTrustedSandboxLocal marks a tool as a trusted Haven-native sandbox tool.
// Use this only for tools whose effects remain inside Morph's own environment.
func WrapTrustedSandboxLocal(tool Tool) Tool {
	return trustedSandboxLocalTool{Tool: tool}
}
