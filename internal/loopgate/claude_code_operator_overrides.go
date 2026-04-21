package loopgate

import (
	"loopgate/internal/config"
	controlapipkg "loopgate/internal/loopgate/controlapi"
)

func (server *Server) matchClaudeCodeOperatorOverride(req controlapipkg.HookPreValidateRequest, overrideClass string) (config.OperatorOverrideGrant, bool) {
	if overrideClass != config.OperatorOverrideClassRepoEditSafe {
		return config.OperatorOverrideGrant{}, false
	}

	targetPaths, ok := hookTargetPaths(req)
	if !ok || len(targetPaths) == 0 {
		return config.OperatorOverrideGrant{}, false
	}

	overrideRuntime := server.currentOperatorOverrideRuntime()
	activeGrants := config.ActiveOperatorOverrideGrants(overrideRuntime.document, overrideClass)
	for _, grant := range activeGrants {
		matchedAllTargets := true
		for _, targetPath := range targetPaths {
			resolvedTargetPath := resolveHookTargetPath(targetPath, req.CWD, server.repoRoot)
			if resolvedTargetPath == "" || !config.OperatorOverrideGrantMatchesPath(grant, resolvedTargetPath, server.repoRoot) {
				matchedAllTargets = false
				break
			}
		}
		if matchedAllTargets {
			return grant, true
		}
	}

	return config.OperatorOverrideGrant{}, false
}
