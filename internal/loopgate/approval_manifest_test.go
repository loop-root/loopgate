package loopgate

import "testing"

func TestCloneCapabilityRequest_DeepCopiesArgumentsMap(t *testing.T) {
	sharedArguments := map[string]string{"path": "original.txt", "content": "a"}
	original := CapabilityRequest{
		RequestID:  "req-clone",
		Capability: "fs_write",
		Arguments:  sharedArguments,
	}
	cloned := cloneCapabilityRequest(original)
	sharedArguments["path"] = "mutated.txt"
	if cloned.Arguments["path"] != "original.txt" {
		t.Fatalf("clone should not observe mutations to the original arguments map, got path %q", cloned.Arguments["path"])
	}
	original.Arguments["content"] = "b"
	if cloned.Arguments["content"] != "a" {
		t.Fatalf("clone should not observe mutations via original struct field, got content %q", cloned.Arguments["content"])
	}
}

func TestVerifyPendingApprovalStoredExecutionBody_SkipsWhenNoStoredHash(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pending := pendingApproval{
		Request: CapabilityRequest{
			RequestID:  "r1",
			Capability: "fs_read",
			Arguments:  map[string]string{"path": "."},
		},
		ExecutionBodySHA256: "",
	}
	denial, ok := server.verifyPendingApprovalStoredExecutionBody(pending)
	if !ok || denial.DenialCode != "" {
		t.Fatalf("expected skip when ExecutionBodySHA256 empty, got ok=%v denial=%#v", ok, denial)
	}
}

func TestVerifyPendingApprovalStoredExecutionBody_DetectsMismatch(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	req := CapabilityRequest{
		RequestID:  "r1",
		Capability: "fs_write",
		Arguments:  map[string]string{"path": "a.txt", "content": "x"},
	}
	hash, err := capabilityRequestBodySHA256(req)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	req.Arguments["path"] = "evil.txt"
	pending := pendingApproval{
		Request:             req,
		ExecutionBodySHA256: hash,
	}
	denial, ok := server.verifyPendingApprovalStoredExecutionBody(pending)
	if ok || denial.DenialCode != DenialCodeApprovalExecutionBodyMismatch {
		t.Fatalf("expected execution body mismatch, ok=%v denial=%#v", ok, denial)
	}
}
