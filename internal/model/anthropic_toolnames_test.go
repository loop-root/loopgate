package model

import (
	"regexp"
	"testing"
)

var anthropicToolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func TestMessagesAPIToolName_ReplacesDots(t *testing.T) {
	if got := MessagesAPIToolName("host.folder.list"); got != "host_folder_list" {
		t.Fatalf("got %q", got)
	}
	if got := MessagesAPIToolName("fs_list"); got != "fs_list" {
		t.Fatalf("got %q", got)
	}
}

func TestMessagesAPIToolName_StripsInvalidCharacters(t *testing.T) {
	if got := MessagesAPIToolName("host/folder/list"); got != "host_folder_list" {
		t.Fatalf("got %q", got)
	}
	if got := MessagesAPIToolName("tool:foo"); got != "tool_foo" {
		t.Fatalf("got %q", got)
	}
}

func TestMessagesAPIToolName_EmptyInputUsesDeterministicFallback(t *testing.T) {
	a := MessagesAPIToolName("   ")
	b := MessagesAPIToolName("   ")
	if a != b {
		t.Fatalf("expected stable fallback, got %q vs %q", a, b)
	}
	if !anthropicToolNamePattern.MatchString(a) {
		t.Fatalf("fallback %q must match API pattern", a)
	}
}

func TestMessagesAPIToolName_AllNativeAllowlistKeysMatchAPIPattern(t *testing.T) {
	for name := range nativeToolAllowlist {
		api := MessagesAPIToolName(name)
		if !anthropicToolNamePattern.MatchString(api) {
			t.Fatalf("allowlisted tool %q -> API name %q does not match Anthropic pattern", name, api)
		}
	}
}
