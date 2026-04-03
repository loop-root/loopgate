package loopgate

import (
	"encoding/json"
	"fmt"
)

// executeEchoGenerateSummary runs the echo.generate_summary capability.
// This is a local fake provider — no external dependencies, no network calls.
// Returns canonical JSON bytes (via EchoProviderOutput) so the caller can
// store them as json.RawMessage without re-serialization.
func executeEchoGenerateSummary(arguments map[string]string) (json.RawMessage, error) {
	inputText, ok := arguments["input_text"]
	if !ok {
		return nil, fmt.Errorf("missing required argument: input_text")
	}
	if inputText == "" {
		return nil, fmt.Errorf("input_text must not be empty")
	}

	output := EchoProviderOutput{
		Summary:     fmt.Sprintf("Summary of input (%d chars): %s", len(inputText), inputText),
		InputLength: len(inputText),
		Provider:    "echo",
	}

	outputBytes, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("marshal echo provider output: %w", err)
	}
	return json.RawMessage(outputBytes), nil
}
