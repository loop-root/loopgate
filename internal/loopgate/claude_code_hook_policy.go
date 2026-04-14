package loopgate

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"morph/internal/config"
	policypkg "morph/internal/policy"
)

func (server *Server) evaluateClaudeCodeHookPolicy(req HookPreValidateRequest, toolDef struct {
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
		return baseResult
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

	return baseResult
}

func (server *Server) applyClaudeCodeHookConstraints(req HookPreValidateRequest, toolPolicy config.ClaudeCodeToolPolicy) (policypkg.CheckResult, bool) {
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

func (server *Server) applyClaudeCodeBashConstraints(req HookPreValidateRequest, toolPolicy config.ClaudeCodeToolPolicy) (policypkg.CheckResult, bool) {
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

	for _, deniedPrefix := range toolPolicy.DeniedCommandPrefixes {
		if strings.HasPrefix(commandText, deniedPrefix) {
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
		if strings.HasPrefix(commandText, allowedPrefix) {
			return policypkg.CheckResult{}, false
		}
	}

	return policypkg.CheckResult{
		Decision: policypkg.Deny,
		Reason:   "bash command does not match any allowed prefix",
	}, true
}

func (server *Server) applyClaudeCodePathConstraints(req HookPreValidateRequest, toolPolicy config.ClaudeCodeToolPolicy) (policypkg.CheckResult, bool) {
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
		resolvedTargetPath := resolveHookTargetPath(targetPath, req.CWD, server.repoRoot)
		if resolvedTargetPath == "" {
			return policypkg.CheckResult{
				Decision: policypkg.Deny,
				Reason:   fmt.Sprintf("%s path policy could not resolve target path", req.ToolName),
			}, true
		}

		if len(allowedRoots) > 0 && !pathMatchesAnyPolicyRoot(resolvedTargetPath, allowedRoots, server.repoRoot) {
			return policypkg.CheckResult{
				Decision: policypkg.Deny,
				Reason:   fmt.Sprintf("%s target %q is outside allowed roots", req.ToolName, resolvedTargetPath),
			}, true
		}

		if pathMatchesAnyPolicyRoot(resolvedTargetPath, deniedPaths, server.repoRoot) {
			return policypkg.CheckResult{
				Decision: policypkg.Deny,
				Reason:   fmt.Sprintf("%s target %q matches denied path policy", req.ToolName, resolvedTargetPath),
			}, true
		}
	}

	return policypkg.CheckResult{}, false
}

func (server *Server) applyClaudeCodeWebFetchConstraints(req HookPreValidateRequest, toolPolicy config.ClaudeCodeToolPolicy) (policypkg.CheckResult, bool) {
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

func hookTargetPaths(req HookPreValidateRequest) ([]string, bool) {
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
	trimmedTargetPath := strings.TrimSpace(rawTargetPath)
	if trimmedTargetPath == "" {
		return ""
	}
	if filepath.IsAbs(trimmedTargetPath) {
		return filepath.Clean(trimmedTargetPath)
	}
	if strings.TrimSpace(cwd) != "" {
		return filepath.Clean(filepath.Join(cwd, trimmedTargetPath))
	}
	return filepath.Clean(filepath.Join(repoRoot, trimmedTargetPath))
}

func pathMatchesAnyPolicyRoot(targetPath string, configuredPaths []string, repoRoot string) bool {
	for _, configuredPath := range configuredPaths {
		resolvedConfiguredPath := resolvePolicyConfiguredPath(configuredPath, repoRoot)
		if resolvedConfiguredPath == "" {
			continue
		}
		if pathWithinRoot(targetPath, resolvedConfiguredPath) {
			return true
		}
	}
	return false
}

func resolvePolicyConfiguredPath(configuredPath string, repoRoot string) string {
	trimmedConfiguredPath := strings.TrimSpace(configuredPath)
	if trimmedConfiguredPath == "" {
		return ""
	}
	if filepath.IsAbs(trimmedConfiguredPath) {
		return filepath.Clean(trimmedConfiguredPath)
	}
	return filepath.Clean(filepath.Join(repoRoot, trimmedConfiguredPath))
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

func hostMatchesDomain(host string, allowedDomain string) bool {
	return host == allowedDomain || strings.HasSuffix(host, "."+allowedDomain)
}
