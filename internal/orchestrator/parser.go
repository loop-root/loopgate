package orchestrator

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"loopgate/internal/tools"
)

var reservedMorphCommandNames = map[string]struct{}{
	"help":        {},
	"exit":        {},
	"quit":        {},
	"reset":       {},
	"pwd":         {},
	"ls":          {},
	"cat":         {},
	"write":       {},
	"policy":      {},
	"debug":       {},
	"setup":       {},
	"agent":       {},
	"model":       {},
	"persona":     {},
	"settings":    {},
	"network":     {},
	"connections": {},
	"site":        {},
	"sandbox":     {},
	"quarantine":  {},
	"config":      {},
	"tools":       {},
	"memory":      {},
}

// ParseResult contains parsed tool calls and any remaining text.
type ParseResult struct {
	Calls     []ToolCall
	Text      string // Non-tool-call text from the model
	ParseErrs []error
}

// Parser extracts tool calls from model output.
type Parser struct {
	// Registry enables invoke_capability expansion on the XML fallback path; optional.
	Registry *tools.Registry
}

// NewParser creates a new parser.
func NewParser() *Parser {
	return &Parser{}
}

// Parse extracts tool calls from model output.
// Tool calls are expected in JSON format within <tool_call> tags:
//
//	<tool_call>
//	{"name": "fs_read", "args": {"path": "foo.txt"}}
//	</tool_call>
//
// Returns parsed calls, remaining text, and any parse errors (non-fatal).
func (p *Parser) Parse(modelOutput string) ParseResult {
	result := ParseResult{}
	remaining := modelOutput

	// Find all <tool_call>...</tool_call> blocks
	for {
		start := strings.Index(remaining, "<tool_call>")
		if start == -1 {
			break
		}

		end := strings.Index(remaining[start:], "</tool_call>")
		if end == -1 {
			// Unclosed tag - treat rest as text
			result.ParseErrs = append(result.ParseErrs,
				fmt.Errorf("unclosed <tool_call> tag at position %d", start))
			break
		}
		end += start // Adjust to absolute position

		// Extract content between tags
		tagLen := len("<tool_call>")
		content := strings.TrimSpace(remaining[start+tagLen : end])

		// Parse the JSON
		call, err := p.parseCallJSON(content)
		if err != nil {
			result.ParseErrs = append(result.ParseErrs, err)
		} else {
			expandedCall, expandErr := ExpandInvokeCapabilityToolCall(call, p.Registry)
			if expandErr != nil {
				result.ParseErrs = append(result.ParseErrs, expandErr)
			} else {
				result.Calls = append(result.Calls, expandedCall)
			}
		}

		// Remove this block from remaining text
		endTagLen := len("</tool_call>")
		remaining = remaining[:start] + remaining[end+endTagLen:]
	}

	// Tool results are runtime-generated only. If the model emits them,
	// strip them from user-visible text and record a parse warning.
	for {
		start := strings.Index(remaining, "<tool_result>")
		if start == -1 {
			break
		}

		end := strings.Index(remaining[start:], "</tool_result>")
		if end == -1 {
			result.ParseErrs = append(result.ParseErrs,
				fmt.Errorf("unclosed <tool_result> tag at position %d", start))
			remaining = remaining[:start]
			break
		}
		end += start

		result.ParseErrs = append(result.ParseErrs,
			fmt.Errorf("model emitted reserved <tool_result> block at position %d", start))

		endTagLen := len("</tool_result>")
		remaining = remaining[:start] + remaining[end+endTagLen:]
	}

	result.Text = strings.TrimSpace(remaining)
	return result
}

// parseCallJSON parses a single tool call from JSON.
func (p *Parser) parseCallJSON(content string) (ToolCall, error) {
	var raw struct {
		Name      string            `json:"name"`
		ID        string            `json:"id"`
		Args      map[string]string `json:"args"`
		Arguments map[string]string `json:"arguments"`
	}

	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return ToolCall{}, fmt.Errorf("invalid tool call JSON: %w", err)
	}

	if raw.Name == "" {
		return ToolCall{}, fmt.Errorf("tool call missing 'name' field")
	}
	if err := validateToolCallName(raw.Name); err != nil {
		return ToolCall{}, err
	}

	// Generate ID if not provided
	callID := raw.ID
	if callID == "" {
		callID = generateCallID()
	}

	// Ensure args map exists. Accept "arguments" as an alias for "args" so
	// model output shaped like OpenAI tool JSON still parses in XML tool_call blocks.
	args := raw.Args
	if len(args) == 0 && len(raw.Arguments) > 0 {
		args = raw.Arguments
	}
	if args == nil {
		args = make(map[string]string)
	}

	return ToolCall{
		ID:   callID,
		Name: raw.Name,
		Args: args,
	}, nil
}

func validateToolCallName(rawName string) error {
	normalizedName := strings.TrimSpace(strings.ToLower(rawName))
	if normalizedName == "" {
		return fmt.Errorf("tool call missing 'name' field")
	}
	if strings.HasPrefix(normalizedName, "/") {
		return fmt.Errorf("local Morph command %q is not a Loopgate tool call", rawName)
	}
	if _, reserved := reservedMorphCommandNames[normalizedName]; reserved {
		return fmt.Errorf("local Morph command %q is not a Loopgate tool call", rawName)
	}
	return nil
}

// generateCallID creates a random call ID.
func generateCallID() string {
	randBytes := make([]byte, 4)
	_, _ = rand.Read(randBytes)
	return "call_" + hex.EncodeToString(randBytes)
}

// FormatResults formats tool results for inclusion in the next model prompt.
func FormatResults(results []ToolResult) string {
	if len(results) == 0 {
		return ""
	}

	var builder strings.Builder
	for _, result := range results {
		builder.WriteString("<tool_result>\n")
		builder.WriteString(fmt.Sprintf(`{"call_id": %q, "status": %q`,
			result.CallID, result.Status))

		if result.Output != "" {
			// Escape the output for JSON
			outputJSON, _ := json.Marshal(result.Output)
			builder.WriteString(fmt.Sprintf(`, "output": %s`, outputJSON))
		}
		if result.Reason != "" {
			builder.WriteString(fmt.Sprintf(`, "reason": %q`, result.Reason))
		}

		builder.WriteString("}\n</tool_result>\n")
	}
	return builder.String()
}
