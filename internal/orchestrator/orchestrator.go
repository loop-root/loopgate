package orchestrator

import (
	"context"
	"fmt"
	"time"

	"loopgate/internal/policy"
	"loopgate/internal/tools"
)

// Approver handles user approval for gated operations.
type Approver interface {
	// RequestApproval prompts the user to approve a tool call.
	// Returns true if approved, false if denied.
	RequestApproval(call ToolCall, reason string) (approved bool, err error)
}

// Logger records orchestrator events.
type Logger interface {
	LogToolCall(call ToolCall, decision policy.Decision, reason string)
	LogToolResult(call ToolCall, result ToolResult)
}

// Orchestrator coordinates tool execution with policy enforcement.
type Orchestrator struct {
	Registry    *tools.Registry
	Checker     *policy.Checker
	Approver    Approver
	Logger      Logger
	Parser      *Parser
	RateLimiter *RateLimiter
}

// Config holds orchestrator configuration options.
type Config struct {
	MaxCallsPerTurn    int
	MaxCallsPerSession int
	CooldownBetween    time.Duration
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		MaxCallsPerTurn:    10,
		MaxCallsPerSession: 500,
		CooldownBetween:    0,
	}
}

// New creates an orchestrator with the given components and default config.
func New(registry *tools.Registry, checker *policy.Checker, approver Approver, logger Logger) *Orchestrator {
	return NewWithConfig(registry, checker, approver, logger, DefaultConfig())
}

// NewWithConfig creates an orchestrator with the given components and config.
func NewWithConfig(registry *tools.Registry, checker *policy.Checker, approver Approver, logger Logger, cfg Config) *Orchestrator {
	parser := NewParser()
	parser.Registry = registry
	return &Orchestrator{
		Registry:    registry,
		Checker:     checker,
		Approver:    approver,
		Logger:      logger,
		Parser:      parser,
		RateLimiter: NewRateLimiter(cfg.MaxCallsPerTurn, cfg.MaxCallsPerSession, cfg.CooldownBetween),
	}
}

// ProcessModelOutput parses and executes tool calls from model output.
// Returns tool results, any non-tool text, and any parse errors encountered.
func (o *Orchestrator) ProcessModelOutput(ctx context.Context, rawModelOutput string) ([]ToolResult, string, []error) {
	parsedOutput := o.Parser.Parse(rawModelOutput)

	if len(parsedOutput.Calls) == 0 {
		return nil, parsedOutput.Text, parsedOutput.ParseErrs
	}

	toolResults := o.ProcessToolCalls(ctx, parsedOutput.Calls)
	return toolResults, parsedOutput.Text, parsedOutput.ParseErrs
}

// ProcessToolCalls executes a batch of tool calls with policy checks.
// This represents a single "turn" for rate limiting purposes.
func (o *Orchestrator) ProcessToolCalls(ctx context.Context, calls []ToolCall) []ToolResult {
	// Reset per-turn counter at start of new batch
	if o.RateLimiter != nil {
		o.RateLimiter.NewTurn()
	}

	results := make([]ToolResult, 0, len(calls))

	for _, call := range calls {
		result := o.processOne(ctx, call)
		results = append(results, result)

		// If we hit rate limits, stop processing remaining calls
		if result.Status == StatusDenied && result.Reason != "" {
			if o.RateLimiter != nil {
				check := o.RateLimiter.Check()
				if !check.Allowed {
					// Rate limited - add denial results for remaining calls
					for _, remaining := range calls[len(results):] {
						results = append(results, ToolResult{
							CallID: remaining.ID,
							Status: StatusDenied,
							Reason: check.Reason,
						})
					}
					break
				}
			}
		}
	}

	return results
}

func (o *Orchestrator) processOne(ctx context.Context, call ToolCall) ToolResult {
	// 0. Check rate limits first
	if o.RateLimiter != nil {
		check := o.RateLimiter.Check()
		if !check.Allowed {
			result := ToolResult{
				CallID: call.ID,
				Status: StatusDenied,
				Reason: check.Reason,
			}
			o.logCall(call, policy.Deny, "rate_limit: "+check.Reason)
			return result
		}
	}

	// 1. Look up tool
	tool := o.Registry.Get(call.Name)
	if tool == nil {
		result := ToolResult{
			CallID: call.ID,
			Status: StatusDenied,
			Reason: fmt.Sprintf("unknown tool: %s", call.Name),
		}
		o.logCall(call, policy.Deny, result.Reason)
		return result
	}

	// 2. Validate arguments
	if err := tool.Schema().Validate(call.Args); err != nil {
		result := ToolResult{
			CallID: call.ID,
			Status: StatusError,
			Reason: err.Error(),
		}
		o.logCall(call, policy.Deny, result.Reason)
		return result
	}

	// 3. Check policy (tool implements policy.ToolInfo via Name() and Category())
	check := o.Checker.Check(tool)
	o.logCall(call, check.Decision, check.Reason)

	switch check.Decision {
	case policy.Deny:
		return ToolResult{
			CallID: call.ID,
			Status: StatusDenied,
			Reason: check.Reason,
		}

	case policy.NeedsApproval:
		approved, err := o.requestApproval(call, check.Reason)
		if err != nil {
			return ToolResult{
				CallID: call.ID,
				Status: StatusError,
				Reason: fmt.Sprintf("approval error: %v", err),
			}
		}
		if !approved {
			return ToolResult{
				CallID: call.ID,
				Status: StatusDenied,
				Reason: "user denied approval",
			}
		}
		// Fall through to execute

	case policy.Allow:
		// Continue to execute
	}

	// 4. Record the call for rate limiting (before execution)
	if o.RateLimiter != nil {
		o.RateLimiter.RecordCall()
	}

	// 5. Execute with timeout
	result := o.execute(ctx, tool, call)
	o.logResult(call, result)
	return result
}

func (o *Orchestrator) execute(ctx context.Context, tool tools.Tool, call ToolCall) ToolResult {
	// Add a reasonable timeout if none exists
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	output, err := tool.Execute(ctx, call.Args)
	if err != nil {
		return ToolResult{
			CallID: call.ID,
			Status: StatusError,
			Output: err.Error(),
		}
	}

	return ToolResult{
		CallID: call.ID,
		Status: StatusSuccess,
		Output: output,
	}
}

func (o *Orchestrator) requestApproval(call ToolCall, reason string) (bool, error) {
	if o.Approver == nil {
		// No approver configured, deny by default
		return false, nil
	}
	return o.Approver.RequestApproval(call, reason)
}

func (o *Orchestrator) logCall(call ToolCall, decision policy.Decision, reason string) {
	if o.Logger != nil {
		o.Logger.LogToolCall(call, decision, reason)
	}
}

func (o *Orchestrator) logResult(call ToolCall, result ToolResult) {
	if o.Logger != nil {
		o.Logger.LogToolResult(call, result)
	}
}

// ResetSession resets session-level state (for /reset command).
func (o *Orchestrator) ResetSession() {
	if o.RateLimiter != nil {
		o.RateLimiter.Reset()
	}
}

// Stats returns current rate limiter statistics.
func (o *Orchestrator) Stats() (turnCalls, sessionCalls int) {
	if o.RateLimiter != nil {
		return o.RateLimiter.Stats()
	}
	return 0, 0
}
