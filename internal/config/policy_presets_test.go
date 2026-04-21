package config

import "testing"

func TestResolvePolicyTemplatePreset(t *testing.T) {
	testCases := []struct {
		name                  string
		input                 string
		wantName              string
		wantHTTP              bool
		wantShell             bool
		wantBashApproval      bool
		wantEditApproval      bool
		wantMultiEditApproval bool
		wantWriteApproval     bool
	}{
		{
			name:                  "strict canonical",
			input:                 "strict",
			wantName:              "strict",
			wantHTTP:              false,
			wantShell:             false,
			wantBashApproval:      false,
			wantEditApproval:      true,
			wantMultiEditApproval: true,
			wantWriteApproval:     true,
		},
		{
			name:                  "strict legacy alias",
			input:                 "strict-mvp",
			wantName:              "strict",
			wantHTTP:              false,
			wantShell:             false,
			wantBashApproval:      false,
			wantEditApproval:      true,
			wantMultiEditApproval: true,
			wantWriteApproval:     true,
		},
		{
			name:                  "balanced canonical",
			input:                 "balanced",
			wantName:              "balanced",
			wantHTTP:              false,
			wantShell:             true,
			wantBashApproval:      true,
			wantEditApproval:      false,
			wantMultiEditApproval: false,
			wantWriteApproval:     true,
		},
		{
			name:                  "read-only canonical",
			input:                 "read-only",
			wantName:              "read-only",
			wantHTTP:              false,
			wantShell:             false,
			wantBashApproval:      false,
			wantEditApproval:      false,
			wantMultiEditApproval: false,
			wantWriteApproval:     false,
		},
		{
			name:                  "developer alias",
			input:                 "dev",
			wantName:              "developer",
			wantHTTP:              true,
			wantShell:             true,
			wantBashApproval:      false,
			wantEditApproval:      true,
			wantMultiEditApproval: true,
			wantWriteApproval:     true,
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
			bashPolicy, ok := policy.ClaudeCodeToolPolicy("Bash")
			if !ok {
				t.Fatal("expected Bash tool policy to be present")
			}
			if got := bashPolicy.RequiresApproval != nil && *bashPolicy.RequiresApproval; got != testCase.wantBashApproval {
				t.Fatalf("expected Bash requires_approval=%t, got %t", testCase.wantBashApproval, got)
			}
			editPolicy, ok := policy.ClaudeCodeToolPolicy("Edit")
			if !ok {
				t.Fatal("expected Edit tool policy to be present")
			}
			if got := editPolicy.RequiresApproval != nil && *editPolicy.RequiresApproval; got != testCase.wantEditApproval {
				t.Fatalf("expected Edit requires_approval=%t, got %t", testCase.wantEditApproval, got)
			}
			multiEditPolicy, ok := policy.ClaudeCodeToolPolicy("MultiEdit")
			if !ok {
				t.Fatal("expected MultiEdit tool policy to be present")
			}
			if got := multiEditPolicy.RequiresApproval != nil && *multiEditPolicy.RequiresApproval; got != testCase.wantMultiEditApproval {
				t.Fatalf("expected MultiEdit requires_approval=%t, got %t", testCase.wantMultiEditApproval, got)
			}
			writePolicy, ok := policy.ClaudeCodeToolPolicy("Write")
			if !ok {
				t.Fatal("expected Write tool policy to be present")
			}
			if got := writePolicy.RequiresApproval != nil && *writePolicy.RequiresApproval; got != testCase.wantWriteApproval {
				t.Fatalf("expected Write requires_approval=%t, got %t", testCase.wantWriteApproval, got)
			}
		})
	}
}

func TestResolveSetupPolicyTemplatePreset(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		wantName string
		wantErr  bool
	}{
		{
			name:     "strict supported in setup",
			input:    "strict",
			wantName: "strict",
		},
		{
			name:     "balanced supported in setup",
			input:    "balanced",
			wantName: "balanced",
		},
		{
			name:     "read-only supported in setup",
			input:    "read-only",
			wantName: "read-only",
		},
		{
			name:    "developer excluded from setup",
			input:   "developer",
			wantErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			preset, err := ResolveSetupPolicyTemplatePreset(testCase.input)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("expected ResolveSetupPolicyTemplatePreset(%q) to fail", testCase.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveSetupPolicyTemplatePreset(%q): %v", testCase.input, err)
			}
			if preset.Name != testCase.wantName {
				t.Fatalf("expected canonical name %q, got %q", testCase.wantName, preset.Name)
			}
		})
	}
}

func TestResolvePolicyTemplatePreset_RejectsUnknownPreset(t *testing.T) {
	if _, err := ResolvePolicyTemplatePreset("unknown"); err == nil {
		t.Fatal("expected unknown policy preset to be rejected")
	}
}

func TestBalancedPreset_DeniesSensitivePathsForReadAndEdit(t *testing.T) {
	preset, err := ResolvePolicyTemplatePreset("balanced")
	if err != nil {
		t.Fatalf("ResolvePolicyTemplatePreset: %v", err)
	}
	policy, err := ParsePolicyDocument([]byte(preset.TemplateYAML))
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}

	readPolicy, ok := policy.ClaudeCodeToolPolicy("Read")
	if !ok {
		t.Fatal("expected Read tool policy")
	}
	editPolicy, ok := policy.ClaudeCodeToolPolicy("Edit")
	if !ok {
		t.Fatal("expected Edit tool policy")
	}

	for _, deniedPath := range []string{".git", "persona", "runtime/state", "core/policy"} {
		if !containsString(readPolicy.DeniedPaths, deniedPath) {
			t.Fatalf("expected Read denied_paths to contain %q; got %#v", deniedPath, readPolicy.DeniedPaths)
		}
		if !containsString(editPolicy.DeniedPaths, deniedPath) {
			t.Fatalf("expected Edit denied_paths to contain %q; got %#v", deniedPath, editPolicy.DeniedPaths)
		}
	}
}

func TestReadOnlyPreset_DisablesWritesAndShell(t *testing.T) {
	preset, err := ResolvePolicyTemplatePreset("read-only")
	if err != nil {
		t.Fatalf("ResolvePolicyTemplatePreset: %v", err)
	}
	policy, err := ParsePolicyDocument([]byte(preset.TemplateYAML))
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}

	if policy.Tools.Filesystem.WriteEnabled {
		t.Fatal("expected read-only preset to disable filesystem writes")
	}
	if policy.Tools.Shell.Enabled {
		t.Fatal("expected read-only preset to disable shell")
	}

	for _, toolName := range []string{"Write", "Edit", "MultiEdit"} {
		toolPolicy, ok := policy.ClaudeCodeToolPolicy(toolName)
		if !ok {
			t.Fatalf("expected %s tool policy", toolName)
		}
		if toolPolicy.Enabled == nil || *toolPolicy.Enabled {
			t.Fatalf("expected %s to be disabled in read-only preset, got %#v", toolName, toolPolicy.Enabled)
		}
	}
}

func TestDetectSetupPolicyTemplatePresetName(t *testing.T) {
	testCases := []struct {
		name       string
		presetName string
		want       string
		mutate     func(*Policy)
	}{
		{
			name:       "balanced",
			presetName: "balanced",
			want:       "balanced",
		},
		{
			name:       "strict",
			presetName: "strict",
			want:       "strict",
		},
		{
			name:       "read-only",
			presetName: "read-only",
			want:       "read-only",
		},
		{
			name:       "custom",
			presetName: "balanced",
			want:       "custom",
			mutate: func(policy *Policy) {
				policy.Tools.HTTP.TimeoutSeconds = 42
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			preset, err := ResolvePolicyTemplatePreset(testCase.presetName)
			if err != nil {
				t.Fatalf("ResolvePolicyTemplatePreset: %v", err)
			}
			policy, err := ParsePolicyDocument([]byte(preset.TemplateYAML))
			if err != nil {
				t.Fatalf("ParsePolicyDocument: %v", err)
			}
			if testCase.mutate != nil {
				testCase.mutate(&policy)
			}
			if got := DetectSetupPolicyTemplatePresetName(policy); got != testCase.want {
				t.Fatalf("DetectSetupPolicyTemplatePresetName() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestPolicyTemplatePresets_OperatorOverrideDelegationDefaults(t *testing.T) {
	testCases := []struct {
		presetName     string
		className      string
		wantDelegation string
	}{
		{
			presetName:     "strict",
			className:      OperatorOverrideClassRepoEditSafe,
			wantDelegation: OperatorOverrideDelegationSession,
		},
		{
			presetName:     "balanced",
			className:      OperatorOverrideClassRepoEditSafe,
			wantDelegation: OperatorOverrideDelegationPersistent,
		},
		{
			presetName:     "balanced",
			className:      OperatorOverrideClassRepoWriteSafe,
			wantDelegation: OperatorOverrideDelegationSession,
		},
		{
			presetName:     "read-only",
			className:      OperatorOverrideClassRepoEditSafe,
			wantDelegation: OperatorOverrideDelegationNone,
		},
		{
			presetName:     "developer",
			className:      OperatorOverrideClassRepoBashSafe,
			wantDelegation: OperatorOverrideDelegationPersistent,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.presetName+"_"+testCase.className, func(t *testing.T) {
			preset, err := ResolvePolicyTemplatePreset(testCase.presetName)
			if err != nil {
				t.Fatalf("ResolvePolicyTemplatePreset(%q): %v", testCase.presetName, err)
			}
			policy, err := ParsePolicyDocument([]byte(preset.TemplateYAML))
			if err != nil {
				t.Fatalf("ParsePolicyDocument(%q): %v", testCase.presetName, err)
			}
			if got := policy.OperatorOverrideMaxDelegation(testCase.className); got != testCase.wantDelegation {
				t.Fatalf("expected %s max delegation %q, got %q", testCase.className, testCase.wantDelegation, got)
			}
		})
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
