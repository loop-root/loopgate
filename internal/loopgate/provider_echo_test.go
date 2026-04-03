package loopgate

import (
	"encoding/json"
	"testing"
)

func TestEchoProviderHappyPath(t *testing.T) {
	raw, err := executeEchoGenerateSummary(map[string]string{
		"input_text": "Hello, world!",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var output EchoProviderOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if output.Summary != "Summary of input (13 chars): Hello, world!" {
		t.Fatalf("unexpected summary: %s", output.Summary)
	}
	if output.InputLength != 13 {
		t.Fatalf("expected input_length 13, got %d", output.InputLength)
	}
	if output.Provider != "echo" {
		t.Fatalf("expected provider 'echo', got %s", output.Provider)
	}
}

func TestEchoProviderMissingInputText(t *testing.T) {
	_, err := executeEchoGenerateSummary(map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing input_text")
	}
}

func TestEchoProviderEmptyInputText(t *testing.T) {
	_, err := executeEchoGenerateSummary(map[string]string{"input_text": ""})
	if err == nil {
		t.Fatal("expected error for empty input_text")
	}
}

func TestEchoProviderOutputDeterministic(t *testing.T) {
	args := map[string]string{"input_text": "determinism test"}
	raw1, err := executeEchoGenerateSummary(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw2, err := executeEchoGenerateSummary(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(raw1) != string(raw2) {
		t.Fatalf("expected deterministic output, got %s and %s", string(raw1), string(raw2))
	}
}
