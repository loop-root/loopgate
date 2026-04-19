package config

import "testing"

func TestResolvePolicyTemplatePreset(t *testing.T) {
	testCases := []struct {
		name      string
		input     string
		wantName  string
		wantHTTP  bool
		wantShell bool
	}{
		{
			name:      "strict canonical",
			input:     "strict",
			wantName:  "strict",
			wantHTTP:  false,
			wantShell: false,
		},
		{
			name:      "strict legacy alias",
			input:     "strict-mvp",
			wantName:  "strict",
			wantHTTP:  false,
			wantShell: false,
		},
		{
			name:      "balanced canonical",
			input:     "balanced",
			wantName:  "balanced",
			wantHTTP:  false,
			wantShell: true,
		},
		{
			name:      "developer alias",
			input:     "dev",
			wantName:  "developer",
			wantHTTP:  true,
			wantShell: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			preset, err := ResolvePolicyTemplatePreset(testCase.input)
			if err != nil {
				t.Fatalf("ResolvePolicyTemplatePreset(%q): %v", testCase.input, err)
			}
			if preset.Name != testCase.wantName {
				t.Fatalf("expected canonical name %q, got %q", testCase.wantName, preset.Name)
			}
			policy, err := ParsePolicyDocument([]byte(preset.TemplateYAML))
			if err != nil {
				t.Fatalf("ParsePolicyDocument(%q): %v", testCase.input, err)
			}
			if policy.Tools.HTTP.Enabled != testCase.wantHTTP {
				t.Fatalf("expected HTTP enabled=%t, got %t", testCase.wantHTTP, policy.Tools.HTTP.Enabled)
			}
			if policy.Tools.Shell.Enabled != testCase.wantShell {
				t.Fatalf("expected shell enabled=%t, got %t", testCase.wantShell, policy.Tools.Shell.Enabled)
			}
		})
	}
}

func TestResolvePolicyTemplatePreset_RejectsUnknownPreset(t *testing.T) {
	if _, err := ResolvePolicyTemplatePreset("unknown"); err == nil {
		t.Fatal("expected unknown policy preset to be rejected")
	}
}
