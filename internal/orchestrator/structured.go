package orchestrator

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"loopgate/internal/model"
	"loopgate/internal/tools"
)

// ToolCallValidationError captures a validation failure for a specific
// tool-use block, preserving the block ID so the caller can return a
// properly correlated error result to the model.
type ToolCallValidationError struct {
	BlockID   string // Provider-assigned tool-use block ID
	BlockName string // Tool name the model attempted
	Err       error  // Validation failure reason
}

func (e ToolCallValidationError) Error() string {
	return e.Err.Error()
}

// ExtractStructuredCalls validates and converts structured tool-use blocks
// from the model provider's native API into ToolCall values suitable for
// the existing Loopgate capability request path.
//
// Each tool-use block is validated against:
//   - The tool name must exist in the provided registry (fail closed on unknown)
//   - The tool name must not be a reserved Morph command
//   - Required arguments must be present
//   - Argument values must pass the tool's schema validation
//
// This function does NOT authorize anything. It only validates that the
// model's structured request is well-formed before passing it to the
// existing CapabilityRequest path where Loopgate enforces policy.
//
// Validation failures are returned as ToolCallValidationError values so
// callers can correlate each error with the originating block ID and
// return structured error results to the model.
func ExtractStructuredCalls(blocks []model.ToolUseBlock, registry *tools.Registry) ([]ToolCall, []ToolCallValidationError) {
	if len(blocks) == 0 {
		return nil, nil
	}

	calls := make([]ToolCall, 0, len(blocks))
	var errs []ToolCallValidationError

	for _, block := range blocks {
		call, err := validateStructuredBlock(block, registry)
		if err != nil {
			errs = append(errs, ToolCallValidationError{
				BlockID:   block.ID,
				BlockName: block.Name,
				Err:       err,
			})
			continue
		}
		calls = append(calls, call)
	}

	return calls, errs
}

func validateStructuredBlock(block model.ToolUseBlock, registry *tools.Registry) (ToolCall, error) {
	// Reject empty or whitespace-only names.
	name := strings.TrimSpace(block.Name)
	if name == "" {
		return ToolCall{}, fmt.Errorf("structured tool-use block has empty name")
	}

	name = canonicalStructuredToolName(name, registry)

	// Reject reserved Morph command names — same check as the XML parser.
	if err := validateToolCallName(name); err != nil {
		return ToolCall{}, fmt.Errorf("structured tool-use block rejected: %w", err)
	}

	if name == "invoke_capability" {
		return expandInvokeCapabilityBlock(block, registry)
	}

	// Reject unknown tools — fail closed.
	tool := registry.Get(name)
	if tool == nil {
		return ToolCall{}, fmt.Errorf("structured tool-use block rejected: unknown tool %q", name)
	}

	// Validate input against the tool's schema (required args, length limits).
	args := block.Input
	if args == nil {
		args = make(map[string]string)
	}
	if err := tool.Schema().Validate(args); err != nil {
		return ToolCall{}, fmt.Errorf("structured tool-use block %q: invalid input: %w", name, err)
	}

	// Use the provider-assigned ID if present; generate one otherwise.
	callID := strings.TrimSpace(block.ID)
	if callID == "" {
		callID = generateCallID()
	}

	return ToolCall{
		ID:   callID,
		Name: name,
		Args: args,
	}, nil
}

func expandInvokeCapabilityBlock(block model.ToolUseBlock, registry *tools.Registry) (ToolCall, error) {
	dispatch := registry.Get("invoke_capability")
	if dispatch == nil {
		return ToolCall{}, fmt.Errorf("structured tool-use block rejected: invoke_capability not registered")
	}
	args := block.Input
	if args == nil {
		args = make(map[string]string)
	}
	if err := dispatch.Schema().Validate(args); err != nil {
		return ToolCall{}, fmt.Errorf("structured tool-use block %q: invalid input: %w", "invoke_capability", err)
	}
	innerName := strings.TrimSpace(args["capability"])
	if innerName == "" {
		return ToolCall{}, fmt.Errorf("structured tool-use block %q: missing capability", "invoke_capability")
	}
	if innerName == "invoke_capability" {
		return ToolCall{}, fmt.Errorf("structured tool-use block %q: nested invoke_capability is not allowed", "invoke_capability")
	}
	if err := validateToolCallName(innerName); err != nil {
		return ToolCall{}, fmt.Errorf("structured tool-use block rejected: %w", err)
	}
	innerTool := registry.Get(innerName)
	if innerTool == nil {
		return ToolCall{}, fmt.Errorf("structured tool-use block rejected: unknown tool %q", innerName)
	}
	innerArgs, err := parseArgumentsJSONString(strings.TrimSpace(args["arguments_json"]))
	if err != nil {
		return ToolCall{}, fmt.Errorf("structured tool-use block %q: arguments_json: %w", "invoke_capability", err)
	}
	if err := innerTool.Schema().Validate(innerArgs); err != nil {
		return ToolCall{}, fmt.Errorf("structured tool-use block %q: invalid input for %q: %w", "invoke_capability", innerName, err)
	}
	callID := strings.TrimSpace(block.ID)
	if callID == "" {
		callID = generateCallID()
	}
	return ToolCall{ID: callID, Name: innerName, Args: innerArgs}, nil
}

// ExpandInvokeCapabilityToolCall applies the same invoke_capability expansion as the
// structured tool-use path to an XML-parsed ToolCall when a registry is available.
func ExpandInvokeCapabilityToolCall(call ToolCall, registry *tools.Registry) (ToolCall, error) {
	if registry == nil || strings.TrimSpace(call.Name) != "invoke_capability" {
		return call, nil
	}
	block := model.ToolUseBlock{ID: call.ID, Name: "invoke_capability", Input: call.Args}
	return expandInvokeCapabilityBlock(block, registry)
}

func parseArgumentsJSONString(jsonStr string) (map[string]string, error) {
	if strings.TrimSpace(jsonStr) == "" {
		return map[string]string{}, nil
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		switch typed := v.(type) {
		case string:
			out[key] = typed
		case float64:
			out[key] = strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			out[key] = strconv.FormatBool(typed)
		case nil:
			out[key] = ""
		default:
			encoded, err := json.Marshal(typed)
			if err != nil {
				return nil, err
			}
			out[key] = string(encoded)
		}
	}
	return out, nil
}

// openAIToolNameAliases maps common mis-names from OpenAI-compatible models
// (including Kimi) to registered capability names. Moonshot docs recommend
// function names using [A-Za-z0-9_-] only; models sometimes rewrite or
// flatten dotted capability names.
//
// Only aliases that resolve to a tool present in the registry are applied.
var openAIToolNameAliases = map[string]string{
	"list":               "fs_list",
	"host_folder_list":   "host.folder.list",
	"host_folder_read":   "host.folder.read",
	"host_organize_plan": "host.organize.plan",
	"host_plan_apply":    "host.plan.apply",
}

func canonicalStructuredToolName(attempted string, registry *tools.Registry) string {
	if registry.Get(attempted) != nil {
		return attempted
	}
	key := strings.ToLower(strings.TrimSpace(attempted))
	if canonical, ok := openAIToolNameAliases[key]; ok {
		if registry.Get(canonical) != nil {
			return canonical
		}
	}
	// Anthropic (and some gateways) emit dotted capability names with '.' → '_'.
	// e.g. host_folder_list → host.folder.list when the registry only registers the latter.
	dotted := strings.ReplaceAll(attempted, "_", ".")
	if dotted != attempted && registry.Get(dotted) != nil {
		return dotted
	}
	return attempted
}
