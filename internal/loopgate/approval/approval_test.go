package approval

import "testing"

func TestTokenHash_IsStable(t *testing.T) {
	const token = "approval-token"
	first := TokenHash(token)
	second := TokenHash(token)
	if first == "" || first != second {
		t.Fatalf("expected stable token hash, got %q and %q", first, second)
	}
}

func TestRequestBodySHA256_IsStableForEquivalentBodies(t *testing.T) {
	first, err := RequestBodySHA256(map[string]any{
		"capability": "fs_write",
		"arguments": map[string]string{
			"path": "notes.txt",
		},
	})
	if err != nil {
		t.Fatalf("first request hash: %v", err)
	}
	second, err := RequestBodySHA256(map[string]any{
		"capability": "fs_write",
		"arguments": map[string]string{
			"path": "notes.txt",
		},
	})
	if err != nil {
		t.Fatalf("second request hash: %v", err)
	}
	if first == "" || first != second {
		t.Fatalf("expected stable request body hash, got %q and %q", first, second)
	}
}
