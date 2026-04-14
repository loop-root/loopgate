package policy

import (
	"morph/internal/config"
)

// Checker evaluates tool calls against the loaded policy.
type Checker struct {
	Policy config.Policy
}

// NewChecker creates a policy checker with the given policy.
func NewChecker(pol config.Policy) *Checker {
	return &Checker{Policy: pol}
}

// CheckResult contains the decision and reason for a policy check.
type CheckResult struct {
	Decision Decision
	Reason   string
}

// OperationType describes what kind of operation a tool performs.
type OperationType = string

const (
	OpRead    OperationType = "read"
	OpWrite   OperationType = "write"
	OpExecute OperationType = "execute"
)

// ToolInfo provides the information needed for policy checking.
// This avoids a dependency on the tools package.
type ToolInfo interface {
	Name() string
	Category() string
	Operation() OperationType
}

// Check evaluates whether a tool is allowed under the current policy.
func (c *Checker) Check(tool ToolInfo) CheckResult {
	category := tool.Category()

	switch category {
	case "filesystem":
		return c.checkFilesystem(tool)
	case "host":
		return c.checkHost(tool)
	case "http":
		return c.checkHTTP()
	case "shell":
		return c.checkShell()
	default:
		return CheckResult{
			Decision: Deny,
			Reason:   "unknown tool category: " + category,
		}
	}
}

func (c *Checker) checkFilesystem(tool ToolInfo) CheckResult {
	fsCfg := c.Policy.Tools.Filesystem
	opType := tool.Operation()

	// Determine access rules based on operation type, not tool name
	switch opType {
	case "write":
		if !fsCfg.WriteEnabled {
			return CheckResult{
				Decision: Deny,
				Reason:   "filesystem writes are disabled by policy",
			}
		}
		if fsCfg.WriteRequiresApproval {
			return CheckResult{
				Decision: NeedsApproval,
				Reason:   "filesystem writes require approval",
			}
		}

	case "read":
		if !fsCfg.ReadEnabled {
			return CheckResult{
				Decision: Deny,
				Reason:   "filesystem reads are disabled by policy",
			}
		}

	case "execute":
		// Filesystem execute operations (if any) default to deny
		return CheckResult{
			Decision: Deny,
			Reason:   "filesystem execute operations are not allowed",
		}

	default:
		// Unknown operation type - deny by default
		return CheckResult{
			Decision: Deny,
			Reason:   "unknown operation type: " + opType,
		}
	}

	return CheckResult{Decision: Allow}
}

func (c *Checker) checkHost(tool ToolInfo) CheckResult {
	fsCfg := c.Policy.Tools.Filesystem
	switch tool.Operation() {
	case OpRead:
		if !fsCfg.ReadEnabled {
			return CheckResult{
				Decision: Deny,
				Reason:   "host folder reads require filesystem read to be enabled by policy",
			}
		}
		return CheckResult{Decision: Allow}
	case OpWrite:
		if !fsCfg.WriteEnabled {
			return CheckResult{
				Decision: Deny,
				Reason:   "host folder writes are disabled by policy",
			}
		}
		// host.plan.apply mutates the real host filesystem — always require explicit approval.
		return CheckResult{
			Decision: NeedsApproval,
			Reason:   "applying a host organization plan requires operator approval",
		}
	default:
		return CheckResult{
			Decision: Deny,
			Reason:   "unknown host tool operation: " + tool.Operation(),
		}
	}
}

func (c *Checker) checkHTTP() CheckResult {
	httpCfg := c.Policy.Tools.HTTP
	if !httpCfg.Enabled {
		return CheckResult{
			Decision: Deny,
			Reason:   "http tools are disabled by policy",
		}
	}
	if httpCfg.RequiresApproval {
		return CheckResult{
			Decision: NeedsApproval,
			Reason:   "http tools require approval",
		}
	}
	return CheckResult{Decision: Allow}
}

func (c *Checker) checkShell() CheckResult {
	sh := c.Policy.Tools.Shell
	if !sh.Enabled {
		return CheckResult{
			Decision: Deny,
			Reason:   "shell tools are disabled by policy",
		}
	}
	if len(sh.AllowedCommands) == 0 {
		return CheckResult{
			Decision: Deny,
			Reason:   "shell allowed_commands must be configured before shell access is allowed",
		}
	}
	if sh.RequiresApproval {
		return CheckResult{
			Decision: NeedsApproval,
			Reason:   "shell commands require operator approval",
		}
	}
	return CheckResult{Decision: Allow}
}
