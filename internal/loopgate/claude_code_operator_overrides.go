package loopgate

import (
	"loopgate/internal/config"
	controlapipkg "loopgate/internal/loopgate/controlapi"
)

func (server *Server) matchClaudeCodeOperatorOverride(req controlapipkg.HookPreValidateRequest, overrideClass string) (config.OperatorOverrideGrant, bool) {
	switch overrideClass {
	case config.OperatorOverrideClassRepoReadSearch,
		config.OperatorOverrideClassRepoEditSafe,
		config.OperatorOverrideClassRepoWriteSafe,
		config.OperatorOverrideClassRepoBashSafe:
	default:
		return config.OperatorOverrideGrant{}, false
	}

	targetPaths, ok := hookTargetPaths(req)
	if !ok || len(targetPaths) == 0 {
		if overrideClass != config.OperatorOverrideClassRepoBashSafe || req.CWD == "" {
			return config.OperatorOverrideGrant{}, false
		}
		targetPaths = []string{req.CWD}
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
