package loopgate

import (
	"encoding/hex"
	"testing"
	"time"
)

func TestSessionMACEpochIndexAt_alignedTo12Hours(t *testing.T) {
	// 1970-01-01 00:00 UTC -> epoch 0
	idx := sessionMACEpochIndexAt(time.Unix(0, 0).UTC())
	if idx != 0 {
		t.Fatalf("epoch 0 start: got %d", idx)
	}
	period := int64(sessionMACEpochDuration / time.Second)
	idx = sessionMACEpochIndexAt(time.Unix(period-1, 0).UTC())
	if idx != 0 {
		t.Fatalf("last second of epoch 0: got %d", idx)
	}
	idx = sessionMACEpochIndexAt(time.Unix(period, 0).UTC())
	if idx != 1 {
		t.Fatalf("first second of epoch 1: got %d", idx)
	}
}

func TestDerivedSessionMACKeyString_deterministic(t *testing.T) {
	master := make([]byte, 32)
	for i := range master {
		master[i] = byte(i)
	}
	epochMat := deriveEpochKeyMaterial(master, 7)
	a := derivedSessionMACKeyString(epochMat, "sess-a")
	b := derivedSessionMACKeyString(epochMat, "sess-a")
	if a != b || len(a) != 64 {
		t.Fatalf("unexpected derived key len=%d a=%q", len(a), a)
	}
	c := derivedSessionMACKeyString(epochMat, "sess-b")
	if a == c {
		t.Fatal("different sessions should derive different keys")
	}
}

func TestRequestSignatureBytesMatchMACKey(t *testing.T) {
	key := derivedSessionMACKeyString(bytesRepeat32(3), "control-1")
	method := "GET"
	path := "/v1/status"
	cs := "control-1"
	ts := "2026-01-02T15:04:05.123456789Z"
	nonce := "abcd"
	body := []byte{}
	sig := signRequest(key, method, path, cs, ts, nonce, body)
	if !requestSignatureBytesMatchMACKey(sig, method, path, cs, ts, nonce, body, key) {
		t.Fatal("expected signature to match")
	}
	if requestSignatureBytesMatchMACKey(sig+"0", method, path, cs, ts, nonce, body, key) {
		t.Fatal("tampered sig should not match")
	}
}

func bytesRepeat32(v byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = v
	}
	return b
}

func TestBuildSessionMACKeysResponse_slots(t *testing.T) {
	server := &Server{
		repoRoot:                 t.TempDir(),
		sessionMACRotationMaster: bytesRepeat32(9),
		now:                      func() time.Time { return time.Unix(43200*5+100, 0).UTC() },
	}
	resp := server.buildSessionMACKeysResponse("abc123")
	if resp.RotationPeriodSeconds != 43200 {
		t.Fatalf("period: %d", resp.RotationPeriodSeconds)
	}
	if resp.CurrentEpochIndex != 5 {
		t.Fatalf("current epoch: %d", resp.CurrentEpochIndex)
	}
	if resp.Previous.EpochIndex != 4 || resp.Next.EpochIndex != 6 {
		t.Fatalf("prev/next epoch: %d %d", resp.Previous.EpochIndex, resp.Next.EpochIndex)
	}
	if len(resp.Current.EpochKeyMaterialHex) != 64 {
		t.Fatalf("epoch key hex: %q", resp.Current.EpochKeyMaterialHex)
	}
	mat, err := hex.DecodeString(resp.Current.EpochKeyMaterialHex)
	if err != nil || len(mat) != 32 {
		t.Fatalf("decode epoch key: %v", err)
	}
	wantDerived := derivedSessionMACKeyString(mat, "abc123")
	if resp.Current.DerivedSessionMACKey != wantDerived {
		t.Fatalf("derived mismatch: %q vs %q", resp.Current.DerivedSessionMACKey, wantDerived)
	}
}
