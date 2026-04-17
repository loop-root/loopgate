package approval

import (
	"testing"
	"time"

	protocolpkg "loopgate/internal/loopgate/protocol"
)

func TestBackfillPendingApprovalManifest_PopulatesManifestAndBodyHash(t *testing.T) {
	approvalRecord := PendingApproval{
		ID: "approval-1",
		Request: protocolpkg.CapabilityRequest{
			RequestID:  "req-1",
			Capability: "fs_write",
			Arguments:  map[string]string{"path": "notes.txt", "content": "hello"},
		},
		ExpiresAt: time.Date(2026, time.April, 16, 12, 0, 0, 0, time.UTC),
	}
	records := map[string]PendingApproval{
		approvalRecord.ID: approvalRecord,
	}

	backfilled := BackfillPendingApprovalManifest(records, approvalRecord.ID, approvalRecord)
	if backfilled.ApprovalManifestSHA256 == "" {
		t.Fatalf("expected approval manifest sha256, got %#v", backfilled)
	}
	if backfilled.ExecutionBodySHA256 == "" {
		t.Fatalf("expected execution body sha256, got %#v", backfilled)
	}
	if stored := records[approvalRecord.ID]; stored.ApprovalManifestSHA256 == "" || stored.ExecutionBodySHA256 == "" {
		t.Fatalf("expected stored record to be updated, got %#v", stored)
	}
}
