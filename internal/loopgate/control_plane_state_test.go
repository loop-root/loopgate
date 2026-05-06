package loopgate

import (
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"testing"
	"time"
)

type failingNonceReplayStore struct {
	saveErr error
}

func (store failingNonceReplayStore) Load(nowUTC time.Time) (map[string]seenRequest, error) {
	return map[string]seenRequest{}, nil
}

func (store failingNonceReplayStore) Record(nonceKey string, seenNonce seenRequest) error {
	return store.saveErr
}

func (store failingNonceReplayStore) Compact(seenAuthNonces map[string]seenRequest) error {
	return store.saveErr
}

func TestRecordAuthNonce_RollsBackWhenReplayPersistenceFails(t *testing.T) {
	server := &Server{
		now: func() time.Time { return time.Date(2026, time.April, 16, 12, 0, 0, 0, time.UTC) },
		replayState: replayControlState{
			seenAuthNonces: make(map[string]seenRequest),
		},
		maxAuthNonceReplayEntries: defaultMaxAuthNonceReplayEntries,
		nonceReplayStore:          failingNonceReplayStore{saveErr: errors.New("persist failed")},
	}

	denial := server.recordAuthNonce("session-a", "nonce-a")
	if denial == nil {
		t.Fatal("expected persistence failure denial")
		return
	}
	if denial.Status != controlapipkg.ResponseStatusError || denial.DenialCode != controlapipkg.DenialCodeAuditUnavailable {
		t.Fatalf("expected audit unavailable denial, got %#v", denial)
	}
	if len(server.replayState.seenAuthNonces) != 0 {
		t.Fatalf("expected nonce map rollback after persistence failure, got %#v", server.replayState.seenAuthNonces)
	}
}
