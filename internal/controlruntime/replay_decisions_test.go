package controlruntime

import (
	"testing"
	"time"
)

func TestRecordSeenRequest_AcceptsNewRequest(t *testing.T) {
	seenRequests := make(map[string]SeenRequest)
	nowUTC := time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)

	recordedRequest, status := RecordSeenRequest(seenRequests, 2, "session-a", "request-a", nowUTC)
	if status != ReplayRecordAccepted {
		t.Fatalf("expected accepted status, got %q", status)
	}
	if recordedRequest.ControlSessionID != "session-a" || !recordedRequest.SeenAt.Equal(nowUTC) {
		t.Fatalf("unexpected recorded request: %#v", recordedRequest)
	}
	if _, found := seenRequests[RequestReplayKey("session-a", "request-a")]; !found {
		t.Fatalf("expected request to be stored")
	}
}

func TestRecordSeenRequest_DetectsDuplicate(t *testing.T) {
	seenRequests := make(map[string]SeenRequest)
	nowUTC := time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)

	_, status := RecordSeenRequest(seenRequests, 2, "session-a", "request-a", nowUTC)
	if status != ReplayRecordAccepted {
		t.Fatalf("first record status: %q", status)
	}
	_, status = RecordSeenRequest(seenRequests, 2, "session-a", "request-a", nowUTC.Add(time.Minute))
	if status != ReplayRecordDuplicate {
		t.Fatalf("expected duplicate status, got %q", status)
	}
	if len(seenRequests) != 1 {
		t.Fatalf("duplicate should not add an entry, got %#v", seenRequests)
	}
}

func TestRecordSeenRequest_FailsClosedWhenSaturated(t *testing.T) {
	seenRequests := map[string]SeenRequest{
		RequestReplayKey("session-a", "request-a"): {
			ControlSessionID: "session-a",
			SeenAt:           time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC),
		},
	}

	_, status := RecordSeenRequest(seenRequests, 1, "session-b", "request-b", time.Date(2026, time.May, 6, 10, 1, 0, 0, time.UTC))
	if status != ReplayRecordSaturated {
		t.Fatalf("expected saturated status, got %q", status)
	}
	if len(seenRequests) != 1 {
		t.Fatalf("saturated store should not add an entry, got %#v", seenRequests)
	}
}

func TestConsumeUsedToken_AcceptsFirstUse(t *testing.T) {
	usedTokens := make(map[string]UsedToken)
	nowUTC := time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)

	consumedToken, status := ConsumeUsedToken(usedTokens, "token-a", "parent-a", "session-a", "fs_read", "arg-hash", nowUTC)
	if status != ReplayRecordAccepted {
		t.Fatalf("expected accepted status, got %q", status)
	}
	if consumedToken.TokenID != "token-a" || consumedToken.Capability != "fs_read" || !consumedToken.ConsumedAt.Equal(nowUTC) {
		t.Fatalf("unexpected consumed token: %#v", consumedToken)
	}
	if _, found := usedTokens["token-a"]; !found {
		t.Fatalf("expected token to be stored")
	}
}

func TestConsumeUsedToken_DetectsReuse(t *testing.T) {
	usedTokens := make(map[string]UsedToken)
	nowUTC := time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)

	_, status := ConsumeUsedToken(usedTokens, "token-a", "parent-a", "session-a", "fs_read", "arg-hash", nowUTC)
	if status != ReplayRecordAccepted {
		t.Fatalf("first consume status: %q", status)
	}
	_, status = ConsumeUsedToken(usedTokens, "token-a", "parent-a", "session-a", "fs_read", "arg-hash", nowUTC.Add(time.Minute))
	if status != ReplayRecordDuplicate {
		t.Fatalf("expected duplicate status, got %q", status)
	}
	if len(usedTokens) != 1 {
		t.Fatalf("reuse should not add an entry, got %#v", usedTokens)
	}
}
