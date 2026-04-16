package loopgate

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestLoadOrCreateSessionMACRotationMaster_CreatesPrivateMasterFile(t *testing.T) {
	repoRoot := t.TempDir()
	server := &Server{
		repoRoot: repoRoot,
		now:      time.Now,
	}
	if err := server.loadOrCreateSessionMACRotationMaster(); err != nil {
		t.Fatalf("load/create session mac rotation master: %v", err)
	}
	if len(server.sessionMACRotationMaster) != sessionMACRotationMasterByteCount {
		t.Fatalf("unexpected master length: %d", len(server.sessionMACRotationMaster))
	}

	masterPath := filepath.Join(repoRoot, "runtime", "state", sessionMACRotationMasterFile)
	fileInfo, err := os.Stat(masterPath)
	if err != nil {
		t.Fatalf("stat session mac rotation master: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("expected session mac rotation master permissions 0600, got %o", fileInfo.Mode().Perm())
	}

	reloadedServer := &Server{
		repoRoot: repoRoot,
		now:      time.Now,
	}
	if err := reloadedServer.loadOrCreateSessionMACRotationMaster(); err != nil {
		t.Fatalf("reload session mac rotation master: %v", err)
	}
	if hex.EncodeToString(reloadedServer.sessionMACRotationMaster) != hex.EncodeToString(server.sessionMACRotationMaster) {
		t.Fatal("expected reloaded master to match original")
	}
}

func TestLoadOrCreateSessionMACRotationMaster_RejectsSymlink(t *testing.T) {
	repoRoot := t.TempDir()
	stateDir := filepath.Join(repoRoot, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}
	targetPath := filepath.Join(t.TempDir(), "outside-master")
	if err := os.WriteFile(targetPath, bytesRepeat32(7), 0o600); err != nil {
		t.Fatalf("write outside master: %v", err)
	}
	masterPath := filepath.Join(stateDir, sessionMACRotationMasterFile)
	if err := os.Symlink(targetPath, masterPath); err != nil {
		t.Fatalf("symlink session mac rotation master: %v", err)
	}

	server := &Server{
		repoRoot: repoRoot,
		now:      time.Now,
	}
	err := server.loadOrCreateSessionMACRotationMaster()
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}
