package loopgate

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRequestSignatureBytesMatchMACKey(t *testing.T) {
	key := "session-mac-key"
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
	wantDerived := server.sessionMACKeyForControlSessionAtEpoch("abc123", resp.CurrentEpochIndex)
	if resp.Current.DerivedSessionMACKey != wantDerived {
		t.Fatalf("derived mismatch: %q vs %q", resp.Current.DerivedSessionMACKey, wantDerived)
	}
}

func TestBuildSessionMACKeysResponse_doesNotExposeEpochKeyMaterial(t *testing.T) {
	server := &Server{
		repoRoot:                 t.TempDir(),
		sessionMACRotationMaster: bytesRepeat32(5),
		now:                      func() time.Time { return time.Unix(43200*2, 0).UTC() },
	}

	responseBytes, err := json.Marshal(server.buildSessionMACKeysResponse("control-session"))
	if err != nil {
		t.Fatalf("marshal session mac response: %v", err)
	}
	if strings.Contains(string(responseBytes), "epoch_key_material_hex") {
		t.Fatalf("response leaked epoch key material field: %s", responseBytes)
	}
}

func bytesRepeat32(v byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = v
	}
	return b
}
