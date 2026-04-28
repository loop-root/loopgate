package loopgate

import (
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"loopgate/internal/config"
	policypkg "loopgate/internal/policy"
	"loopgate/internal/safety"
)

func (server *Server) evaluateClaudeCodeHookPolicy(req controlapipkg.HookPreValidateRequest, toolDef struct {
	category  string
	operation string
}) policypkg.CheckResult {
	policyRuntime := server.currentPolicyRuntime()
	baseResult := policyRuntime.checker.Check(hookToolInfo{
		name:     req.ToolName,
		category: toolDef.category,
		op:       toolDef.operation,
	})

	toolPolicy, hasToolPolicy := policyRuntime.policy.ClaudeCodeToolPolicy(req.ToolName)
	if !hasToolPolicy {
		toolPolicy = config.ClaudeCodeToolPolicy{}
	}

	if toolPolicy.Enabled != nil && !*toolPolicy.Enabled {
		return policypkg.CheckResult{
			Decision: policypkg.Deny,
			Reason:   fmt.Sprintf("%s is disabled by Claude Code tool policy", req.ToolName),
		}
	}

	if constraintResult, constrained := server.applyClaudeCodeHookConstraints(req, toolPolicy); constrained {
		return constraintResult
	}

	overrideEligible := false
	switch {
	case toolPolicy.RequiresApproval != nil && *toolPolicy.RequiresApproval && baseResult.Decision != policypkg.Deny:
		overrideEligible = true
	case toolPolicy.RequiresApproval == nil && baseResult.Decision == policypkg.NeedsApproval:
		overrideEligible = true
	}
	if overrideEligible {
		if overrideClass, maxDelegation, hasOverrideClass := policyRuntime.policy.ClaudeCodeToolOperatorOverride(req.ToolName); hasOverrideClass && maxDelegation == config.OperatorOverrideDelegationPersistent {
			if grant, matched := server.matchClaudeCodeOperatorOverride(req, overrideClass); matched {
				return policypkg.CheckResult{
					Decision: policypkg.Allow,
					Reason:   fmt.Sprintf("%s allowed by delegated operator override %s", req.ToolName, grant.ID),
				}
			}
		}
	}

	if toolPolicy.RequiresApproval != nil {
		if *toolPolicy.RequiresApproval {
			return policypkg.CheckResult{
				Decision: policypkg.NeedsApproval,
				Reason:   fmt.Sprintf("%s requires approval by Claude Code tool policy", req.ToolName),
			}
		}
		return policypkg.CheckResult{Decision: policypkg.Allow}
	}

	if toolPolicy.Enabled != nil && *toolPolicy.Enabled {
		return policypkg.CheckResult{Decision: policypkg.Allow}
	}
	if !hasToolPolicy {
		return baseResult
	}

	return baseResult
}

func (server *Server) applyClaudeCodeHookConstraints(req controlapipkg.HookPreValidateRequest, toolPolicy config.ClaudeCodeToolPolicy) (policypkg.CheckResult, bool) {
	switch req.ToolName {
	case "Bash":
		return server.applyClaudeCodeBashConstraints(req, toolPolicy)
	case "Read", "Write", "Edit", "MultiEdit", "Glob", "Grep":
		return server.applyClaudeCodePathConstraints(req, toolPolicy)
	case "WebFetch":
		return server.applyClaudeCodeWebFetchConstraints(req, toolPolicy)
	default:
		return policypkg.CheckResult{}, false
	}
}

func (server *Server) applyClaudeCodeBashConstraints(req controlapipkg.HookPreValidateRequest, toolPolicy config.ClaudeCodeToolPolicy) (policypkg.CheckResult, bool) {
	if len(toolPolicy.AllowedCommandPrefixes) == 0 && len(toolPolicy.DeniedCommandPrefixes) == 0 {
		return policypkg.CheckResult{}, false
	}

	commandText, ok := hookInputString(req.ToolInput, "command")
	if !ok {
		return policypkg.CheckResult{
			Decision: policypkg.Deny,
			Reason:   "bash command policy requires tool_input.command",
		}, true
	}
	normalizedCommandText := normalizeWhitespaceForPrefixMatch(commandText)

	for _, deniedPrefix := range toolPolicy.DeniedCommandPrefixes {
		if strings.HasPrefix(normalizedCommandText, normalizeWhitespaceForPrefixMatch(deniedPrefix)) {
			return policypkg.CheckResult{
				Decision: policypkg.Deny,
				Reason:   fmt.Sprintf("bash command matches denied prefix %q", deniedPrefix),
			}, true
		}
	}

	if len(toolPolicy.AllowedCommandPrefixes) == 0 {
		return policypkg.CheckResult{}, false
	}

	for _, allowedPrefix := range toolPolicy.AllowedCommandPrefixes {
		if strings.HasPrefix(normalizedCommandText, normalizeWhitespaceForPrefixMatch(allowedPrefix)) {
			return policypkg.CheckResult{}, false
		}
	}

	return policypkg.CheckResult{
		Decision: policypkg.Deny,
		Reason:   "bash command does not match any allowed prefix",
	}, true
}

func normalizeWhitespaceForPrefixMatch(rawValue string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(rawValue)), " ")
}

func (server *Server) applyClaudeCodePathConstraints(req controlapipkg.HookPreValidateRequest, toolPolicy config.ClaudeCodeToolPolicy) (policypkg.CheckResult, bool) {
	policyRuntime := server.currentPolicyRuntime()
	allowedRoots := toolPolicy.AllowedRoots
	if len(allowedRoots) == 0 {
		allowedRoots = policyRuntime.policy.Tools.Filesystem.AllowedRoots
	}
	deniedPaths := toolPolicy.DeniedPaths
	if len(deniedPaths) == 0 {
		deniedPaths = policyRuntime.policy.Tools.Filesystem.DeniedPaths
	}

	if len(allowedRoots) == 0 && len(deniedPaths) == 0 {
		return policypkg.CheckResult{}, false
	}

	targetPaths, ok := hookTargetPaths(req)
	if !ok || len(targetPaths) == 0 {
		return policypkg.CheckResult{
			Decision: policypkg.Deny,
			Reason:   fmt.Sprintf("%s path policy requires a target path", req.ToolName),
		}, true
	}

	for _, targetPath := range targetPaths {
		resolvedTargetPath, err := server.resolveClaudeCodeHookPolicyPath(req, targetPath, allowedRoots, deniedPaths)
		if err != nil {
			displayPath := resolvedTargetPath
			if strings.TrimSpace(displayPath) == "" {
				displayPath = strings.TrimSpace(targetPath)
			}
			switch {
			case strings.Contains(err.Error(), "path is outside allowed roots"):
				return policypkg.CheckResult{
					Decision: policypkg.Deny,
					Reason:   fmt.Sprintf("%s target %q is outside allowed roots", req.ToolName, displayPath),
				}, true
			case strings.Contains(err.Error(), "path is denied by policy"):
				return policypkg.CheckResult{
					Decision: policypkg.Deny,
					Reason:   fmt.Sprintf("%s target %q matches denied path policy", req.ToolName, displayPath),
				}, true
			case strings.Contains(err.Error(), "symlink path"):
				return policypkg.CheckResult{
					Decision: policypkg.Deny,
					Reason:   fmt.Sprintf("%s target %q uses a symlink path", req.ToolName, displayPath),
				}, true
			default:
				return policypkg.CheckResult{
					Decision: policypkg.Deny,
					Reason:   fmt.Sprintf("%s path policy could not resolve target path", req.ToolName),
				}, true
			}
		}
		if resolvedTargetPath == "" {
			return policypkg.CheckResult{
				Decision: policypkg.Deny,
				Reason:   fmt.Sprintf("%s path policy could not resolve target path", req.ToolName),
			}, true
		}
	}

	return policypkg.CheckResult{}, false
}

func (server *Server) applyClaudeCodeWebFetchConstraints(req controlapipkg.HookPreValidateRequest, toolPolicy config.ClaudeCodeToolPolicy) (policypkg.CheckResult, bool) {
	policyRuntime := server.currentPolicyRuntime()
	allowedDomains := toolPolicy.AllowedDomains
	if len(allowedDomains) == 0 {
		allowedDomains = policyRuntime.policy.Tools.HTTP.AllowedDomains
	}
	if len(allowedDomains) == 0 {
		return policypkg.CheckResult{}, false
	}

	requestURL, ok := hookInputString(req.ToolInput, "url")
	if !ok {
		return policypkg.CheckResult{
			Decision: policypkg.Deny,
			Reason:   "web fetch domain policy requires tool_input.url",
		}, true
	}

	parsedURL, err := url.Parse(requestURL)
	if err != nil || parsedURL.Host == "" {
		return policypkg.CheckResult{
			Decision: policypkg.Deny,
			Reason:   "web fetch domain policy requires a valid url",
		}, true
	}

	requestHost := strings.ToLower(parsedURL.Hostname())
	for _, allowedDomain := range allowedDomains {
		if hostMatchesDomain(requestHost, strings.ToLower(allowedDomain)) {
			return policypkg.CheckResult{}, false
		}
	}

	return policypkg.CheckResult{
		Decision: policypkg.Deny,
		Reason:   fmt.Sprintf("web fetch host %q is outside allowed domains", requestHost),
	}, true
}

func hookInputString(toolInput map[string]interface{}, fieldName string) (string, bool) {
	rawValue, found := toolInput[fieldName]
	if !found {
		return "", false
	}
	stringValue, ok := rawValue.(string)
	if !ok {
		return "", false
	}
	trimmedValue := strings.TrimSpace(stringValue)
	if trimmedValue == "" {
		return "", false
	}
	return trimmedValue, true
}

func hookTargetPaths(req controlapipkg.HookPreValidateRequest) ([]string, bool) {
	switch req.ToolName {
	case "Read", "Write", "Edit", "MultiEdit":
		filePath, ok := hookInputString(req.ToolInput, "file_path")
		if !ok {
			return nil, false
		}
		return []string{filePath}, true
	case "Glob", "Grep":
		searchPath, ok := hookInputString(req.ToolInput, "path")
		if ok {
			return []string{searchPath}, true
		}
		if strings.TrimSpace(req.CWD) == "" {
			return nil, false
		}
		return []string{req.CWD}, true
	default:
		return nil, false
	}
}

func resolveHookTargetPath(rawTargetPath string, cwd string, repoRoot string) string {
	candidatePath, err := hookTargetCandidatePath(rawTargetPath, cwd, repoRoot)
	if err != nil {
		return ""
	}
	return candidatePath
}

func hookTargetCandidatePath(rawTargetPath string, cwd string, repoRoot string) (string, error) {
	trimmedTargetPath := strings.TrimSpace(rawTargetPath)
	if trimmedTargetPath == "" {
		return "", fmt.Errorf("empty target path")
	}
	var candidatePath string
	if filepath.IsAbs(trimmedTargetPath) {
		candidatePath = trimmedTargetPath
	} else if strings.TrimSpace(cwd) != "" {
		trimmedCWD := strings.TrimSpace(cwd)
		if !filepath.IsAbs(trimmedCWD) {
			return "", fmt.Errorf("hook cwd must be absolute")
		}
		candidatePath = filepath.Join(trimmedCWD, trimmedTargetPath)
	} else {
		candidatePath = filepath.Join(repoRoot, trimmedTargetPath)
	}
	absCandidatePath, err := filepath.Abs(filepath.Clean(candidatePath))
	if err != nil {
		return "", err
	}
	return filepath.Clean(absCandidatePath), nil
}

func pathWithinRoot(targetPath string, rootPath string) bool {
	relativePath, err := filepath.Rel(rootPath, targetPath)
	if err != nil {
		return false
	}
	if relativePath == "." {
		return true
	}
	return relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(filepath.Separator))
}

func (server *Server) resolveClaudeCodeHookPolicyPath(req controlapipkg.HookPreValidateRequest, rawTargetPath string, allowedRoots []string, deniedPaths []string) (string, error) {
	candidatePath, err := hookTargetCandidatePath(rawTargetPath, req.CWD, server.repoRoot)
	if err != nil {
		return "", err
	}

	allowedRootsForResolution := allowedRoots
	if len(allowedRootsForResolution) == 0 {
		allowedRootsForResolution = []string{string(filepath.Separator)}
	}

	explanation, err := safety.ExplainSafePath(server.repoRoot, allowedRootsForResolution, deniedPaths, candidatePath)
	if err != nil {
		return explanation.ResolvedAbs, err
	}

	switch req.ToolName {
	case "Read", "Glob", "Grep":
		if _, statErr := os.Stat(explanation.ResolvedAbs); statErr != nil {
			return explanation.ResolvedAbs, fmt.Errorf("target_path_not_resolved: %w", statErr)
		}
	case "Write", "Edit", "MultiEdit":
		usesSymlinkPath, symlinkErr := mutatingHookPathUsesSymlinkPath(server.repoRoot, candidatePath)
		if symlinkErr != nil {
			return explanation.ResolvedAbs, fmt.Errorf("symlink path validation failed: %w", symlinkErr)
		}
		if usesSymlinkPath {
			return explanation.ResolvedAbs, fmt.Errorf("symlink path denied")
		}
	}

	return explanation.ResolvedAbs, nil
}

func mutatingHookPathUsesSymlinkPath(repoRoot string, candidatePath string) (bool, error) {
	inspectionPath := candidatePath
	if _, err := os.Lstat(inspectionPath); err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		inspectionPath = filepath.Dir(inspectionPath)
	}

	startPath, relativePath := symlinkInspectionStart(repoRoot, inspectionPath)
	if relativePath == "." {
		return false, nil
	}

	currentPath := startPath
	for _, pathPart := range strings.Split(relativePath, string(filepath.Separator)) {
		if pathPart == "" || pathPart == "." {
			continue
		}
		if pathPart == ".." {
			return false, fmt.Errorf("path escapes symlink inspection root")
		}
		currentPath = filepath.Join(currentPath, pathPart)
		fileInfo, err := os.Lstat(currentPath)
		if err != nil {
			return false, err
		}
		if fileInfo.Mode()&os.ModeSymlink != 0 {
			return true, nil
		}
	}
	return false, nil
}

func symlinkInspectionStart(repoRoot string, inspectionPath string) (string, string) {
	cleanRepoRoot := filepath.Clean(repoRoot)
	cleanInspectionPath := filepath.Clean(inspectionPath)
	if relativePath, err := filepath.Rel(cleanRepoRoot, cleanInspectionPath); err == nil && relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return cleanRepoRoot, filepath.Clean(relativePath)
	}

	volumeName := filepath.VolumeName(cleanInspectionPath)
	rootPath := volumeName + string(filepath.Separator)
	relativePath, err := filepath.Rel(rootPath, cleanInspectionPath)
	if err != nil {
		return rootPath, "."
	}
	return rootPath, filepath.Clean(relativePath)
}

func hostMatchesDomain(host string, allowedDomain string) bool {
	return host == allowedDomain || strings.HasSuffix(host, "."+allowedDomain)
}
