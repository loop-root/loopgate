package loopgate

import (
	"errors"
	"path/filepath"
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

func TestSnapshotNonceReplayStore_RoundTrip(t *testing.T) {
	nowUTC := time.Date(2026, time.April, 16, 12, 0, 0, 0, time.UTC)
	store := snapshotNonceReplayStore{path: filepath.Join(t.TempDir(), "nonce_replay.json")}
	want := map[string]seenRequest{
		"session-a:nonce-a": {
			ControlSessionID: "session-a",
			SeenAt:           nowUTC,
		},
		"session-b:nonce-b": {
			ControlSessionID: "session-b",
			SeenAt:           nowUTC.Add(-10 * time.Minute),
		},
	}

	if err := store.Compact(want); err != nil {
		t.Fatalf("save nonce replay state: %v", err)
	}

	got, err := store.Load(nowUTC)
	if err != nil {
		t.Fatalf("load nonce replay state: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d loaded nonces, got %d", len(want), len(got))
	}
	for nonceKey, wantSeen := range want {
		gotSeen, found := got[nonceKey]
		if !found {
			t.Fatalf("missing nonce key %q", nonceKey)
		}
		if gotSeen.ControlSessionID != wantSeen.ControlSessionID || !gotSeen.SeenAt.Equal(wantSeen.SeenAt) {
			t.Fatalf("nonce %q mismatch: want %#v got %#v", nonceKey, wantSeen, gotSeen)
		}
	}
}

func TestSnapshotNonceReplayStore_LoadPrunesExpiredAndMalformedEntries(t *testing.T) {
	nowUTC := time.Date(2026, time.April, 16, 12, 0, 0, 0, time.UTC)
	storePath := filepath.Join(t.TempDir(), "nonce_replay.json")
	rawState := []byte(`{
  "nonces": {
    "fresh:nonce": {"control_session_id":"fresh","seen_at":"2026-04-16T11:55:00Z"},
    "expired:nonce": {"control_session_id":"expired","seen_at":"2026-04-14T11:55:00Z"},
    "broken:nonce": {"control_session_id":"broken","seen_at":"not-a-time"}
  }
}`)
	if err := atomicWritePrivateJSON(storePath, rawState); err != nil {
		t.Fatalf("write nonce replay state: %v", err)
	}

	store := snapshotNonceReplayStore{path: storePath}
	got, err := store.Load(nowUTC)
	if err != nil {
		t.Fatalf("load nonce replay state: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one surviving nonce, got %#v", got)
	}
	freshSeen, found := got["fresh:nonce"]
	if !found {
		t.Fatalf("expected fresh nonce to survive, got %#v", got)
	}
	if freshSeen.ControlSessionID != "fresh" {
		t.Fatalf("expected fresh control session id, got %#v", freshSeen)
	}
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
	}
	if denial.Status != ResponseStatusError || denial.DenialCode != DenialCodeAuditUnavailable {
		t.Fatalf("expected audit unavailable denial, got %#v", denial)
	}
	if len(server.replayState.seenAuthNonces) != 0 {
		t.Fatalf("expected nonce map rollback after persistence failure, got %#v", server.replayState.seenAuthNonces)
	}
}

func TestAppendOnlyNonceReplayStore_RoundTrip(t *testing.T) {
	nowUTC := time.Date(2026, time.April, 16, 12, 0, 0, 0, time.UTC)
	store := appendOnlyNonceReplayStore{path: filepath.Join(t.TempDir(), "nonce_replay.jsonl")}
	want := map[string]seenRequest{
		"session-a:nonce-a": {
			ControlSessionID: "session-a",
			SeenAt:           nowUTC,
		},
		"session-b:nonce-b": {
			ControlSessionID: "session-b",
			SeenAt:           nowUTC.Add(-15 * time.Minute),
		},
	}

	for nonceKey, seenNonce := range want {
		if err := store.Record(nonceKey, seenNonce); err != nil {
			t.Fatalf("record nonce %q: %v", nonceKey, err)
		}
	}

	got, err := store.Load(nowUTC)
	if err != nil {
		t.Fatalf("load append-only nonce replay store: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d loaded nonces, got %d", len(want), len(got))
	}
	for nonceKey, wantSeen := range want {
		gotSeen, found := got[nonceKey]
		if !found {
			t.Fatalf("missing nonce key %q", nonceKey)
		}
		if gotSeen.ControlSessionID != wantSeen.ControlSessionID || !gotSeen.SeenAt.Equal(wantSeen.SeenAt) {
			t.Fatalf("nonce %q mismatch: want %#v got %#v", nonceKey, wantSeen, gotSeen)
		}
	}
}

func TestAppendOnlyNonceReplayStore_LoadToleratesTruncatedTail(t *testing.T) {
	nowUTC := time.Date(2026, time.April, 16, 12, 0, 0, 0, time.UTC)
	storePath := filepath.Join(t.TempDir(), "nonce_replay.jsonl")
	rawLog := []byte("{\"nonce_key\":\"fresh:nonce\",\"control_session_id\":\"fresh\",\"seen_at\":\"2026-04-16T11:55:00Z\"}\n{\"nonce_key\":\"truncated")
	if err := atomicWritePrivateJSON(storePath, rawLog); err != nil {
		t.Fatalf("write append-only nonce replay log: %v", err)
	}

	store := appendOnlyNonceReplayStore{path: storePath}
	got, err := store.Load(nowUTC)
	if err != nil {
		t.Fatalf("load append-only nonce replay log: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one surviving nonce after truncated tail, got %#v", got)
	}
	if _, found := got["fresh:nonce"]; !found {
		t.Fatalf("expected fresh nonce to survive, got %#v", got)
	}
}

func TestAppendOnlyNonceReplayStore_LoadRejectsMalformedMiddleRecord(t *testing.T) {
	nowUTC := time.Date(2026, time.April, 16, 12, 0, 0, 0, time.UTC)
	storePath := filepath.Join(t.TempDir(), "nonce_replay.jsonl")
	rawLog := []byte("{\"nonce_key\":\"first:nonce\",\"control_session_id\":\"first\",\"seen_at\":\"2026-04-16T11:55:00Z\"}\nnot-json\n{\"nonce_key\":\"last:nonce\",\"control_session_id\":\"last\",\"seen_at\":\"2026-04-16T11:58:00Z\"}\n")
	if err := atomicWritePrivateJSON(storePath, rawLog); err != nil {
		t.Fatalf("write append-only nonce replay log: %v", err)
	}

	store := appendOnlyNonceReplayStore{path: storePath}
	_, err := store.Load(nowUTC)
	if err == nil {
		t.Fatal("expected malformed middle record error")
	}
}

func TestAppendOnlyNonceReplayStore_LoadsLegacySnapshotWhenLogMissing(t *testing.T) {
	nowUTC := time.Date(2026, time.April, 16, 12, 0, 0, 0, time.UTC)
	baseDir := t.TempDir()
	legacyPath := filepath.Join(baseDir, "nonce_replay.json")
	if err := (snapshotNonceReplayStore{path: legacyPath}).Compact(map[string]seenRequest{
		"legacy-session:legacy-nonce": {
			ControlSessionID: "legacy-session",
			SeenAt:           nowUTC.Add(-5 * time.Minute),
		},
	}); err != nil {
		t.Fatalf("write legacy snapshot nonce replay state: %v", err)
	}

	store := appendOnlyNonceReplayStore{
		path:               filepath.Join(baseDir, "nonce_replay.jsonl"),
		legacySnapshotPath: legacyPath,
	}
	got, err := store.Load(nowUTC)
	if err != nil {
		t.Fatalf("load legacy nonce replay snapshot: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one legacy nonce entry, got %#v", got)
	}
	if _, found := got["legacy-session:legacy-nonce"]; !found {
		t.Fatalf("expected legacy nonce to load, got %#v", got)
	}
}
