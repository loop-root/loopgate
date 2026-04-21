package config

import (
	"fmt"
	"strings"
)

type OperatorOverridePolicy struct {
	Classes map[string]OperatorOverrideClassPolicy `yaml:"classes" json:"classes"`
}

type OperatorOverrideClassPolicy struct {
	MaxDelegation string `yaml:"max_delegation" json:"max_delegation"`
}

const (
	OperatorOverrideDelegationNone       = "none"
	OperatorOverrideDelegationSession    = "session"
	OperatorOverrideDelegationPersistent = "persistent"
)

const (
	OperatorOverrideClassRepoReadSearch   = "repo_read_search"
	OperatorOverrideClassRepoEditSafe     = "repo_edit_safe"
	OperatorOverrideClassRepoWriteSafe    = "repo_write_safe"
	OperatorOverrideClassRepoBashSafe     = "repo_bash_safe"
	OperatorOverrideClassWebAccessTrusted = "web_access_trusted"
)

var supportedOperatorOverrideClasses = map[string]struct{}{
	OperatorOverrideClassRepoReadSearch:   {},
	OperatorOverrideClassRepoEditSafe:     {},
	OperatorOverrideClassRepoWriteSafe:    {},
	OperatorOverrideClassRepoBashSafe:     {},
	OperatorOverrideClassWebAccessTrusted: {},
}

var supportedOperatorOverrideClassList = []string{
	OperatorOverrideClassRepoReadSearch,
	OperatorOverrideClassRepoEditSafe,
	OperatorOverrideClassRepoWriteSafe,
	OperatorOverrideClassRepoBashSafe,
	OperatorOverrideClassWebAccessTrusted,
}

var supportedOperatorOverrideDelegations = map[string]struct{}{
	OperatorOverrideDelegationNone:       {},
	OperatorOverrideDelegationSession:    {},
	OperatorOverrideDelegationPersistent: {},
}

var supportedOperatorOverrideDelegationList = []string{
	OperatorOverrideDelegationNone,
	OperatorOverrideDelegationSession,
	OperatorOverrideDelegationPersistent,
}

var claudeCodeToolOperatorOverrideClasses = map[string]string{
	"Read":      OperatorOverrideClassRepoReadSearch,
	"Glob":      OperatorOverrideClassRepoReadSearch,
	"Grep":      OperatorOverrideClassRepoReadSearch,
	"Edit":      OperatorOverrideClassRepoEditSafe,
	"MultiEdit": OperatorOverrideClassRepoEditSafe,
	"Write":     OperatorOverrideClassRepoWriteSafe,
	"Bash":      OperatorOverrideClassRepoBashSafe,
	"WebFetch":  OperatorOverrideClassWebAccessTrusted,
	"WebSearch": OperatorOverrideClassWebAccessTrusted,
}

func applyOperatorOverridePolicyDefaults(policy *OperatorOverridePolicy) error {
	if policy.Classes == nil {
		policy.Classes = map[string]OperatorOverrideClassPolicy{}
		return nil
	}

	validatedClasses := make(map[string]OperatorOverrideClassPolicy, len(policy.Classes))
	for className, classPolicy := range policy.Classes {
		if _, supported := supportedOperatorOverrideClasses[className]; !supported {
			return fmt.Errorf("operator_overrides.classes contains unsupported class %q", className)
		}
		maxDelegation := strings.TrimSpace(classPolicy.MaxDelegation)
		if maxDelegation == "" {
			maxDelegation = OperatorOverrideDelegationNone
		}
		if _, supported := supportedOperatorOverrideDelegations[maxDelegation]; !supported {
			return fmt.Errorf("operator_overrides.classes.%s.max_delegation must be one of: %s", className, strings.Join(supportedOperatorOverrideDelegationList, ", "))
		}
		classPolicy.MaxDelegation = maxDelegation
		validatedClasses[className] = classPolicy
	}
	policy.Classes = validatedClasses
	return nil
}

func SupportedOperatorOverrideClassNames() []string {
	return append([]string(nil), supportedOperatorOverrideClassList...)
}

func SupportedOperatorOverrideDelegations() []string {
	return append([]string(nil), supportedOperatorOverrideDelegationList...)
}

func ClaudeCodeToolOperatorOverrideClass(toolName string) (string, bool) {
	overrideClass, found := claudeCodeToolOperatorOverrideClasses[strings.TrimSpace(toolName)]
	return overrideClass, found
}

func (p Policy) OperatorOverrideClassPolicy(className string) (OperatorOverrideClassPolicy, bool) {
	classPolicy, found := p.OperatorOverrides.Classes[strings.TrimSpace(className)]
	if !found {
		return OperatorOverrideClassPolicy{MaxDelegation: OperatorOverrideDelegationNone}, false
	}
	if strings.TrimSpace(classPolicy.MaxDelegation) == "" {
		classPolicy.MaxDelegation = OperatorOverrideDelegationNone
	}
	return classPolicy, true
}

func (p Policy) OperatorOverrideMaxDelegation(className string) string {
	classPolicy, found := p.OperatorOverrideClassPolicy(className)
	if !found {
		return OperatorOverrideDelegationNone
	}
	return classPolicy.MaxDelegation
}

func (p Policy) ClaudeCodeToolOperatorOverride(toolName string) (string, string, bool) {
	overrideClass, found := ClaudeCodeToolOperatorOverrideClass(toolName)
	if !found {
		return "", OperatorOverrideDelegationNone, false
	}
	return overrideClass, p.OperatorOverrideMaxDelegation(overrideClass), true
}
