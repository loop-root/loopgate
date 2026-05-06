package controlruntime

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
	idx := SessionMACEpochIndexAt(time.Unix(0, 0).UTC())
	if idx != 0 {
		t.Fatalf("epoch 0 start: got %d", idx)
	}
	period := int64(SessionMACEpochDuration / time.Second)
	idx = SessionMACEpochIndexAt(time.Unix(period-1, 0).UTC())
	if idx != 0 {
		t.Fatalf("last second of epoch 0: got %d", idx)
	}
	idx = SessionMACEpochIndexAt(time.Unix(period, 0).UTC())
	if idx != 1 {
		t.Fatalf("first second of epoch 1: got %d", idx)
	}
}

func TestDerivedSessionMACKeyString_deterministic(t *testing.T) {
	master := make([]byte, 32)
	for i := range master {
		master[i] = byte(i)
	}
	epochMat := DeriveEpochKeyMaterial(master, 7)
	a := DerivedSessionMACKeyString(epochMat, "sess-a")
	b := DerivedSessionMACKeyString(epochMat, "sess-a")
	if a != b || len(a) != 64 {
		t.Fatalf("unexpected derived key len=%d a=%q", len(a), a)
	}
	c := DerivedSessionMACKeyString(epochMat, "sess-b")
	if a == c {
		t.Fatal("different sessions should derive different keys")
	}
}

func TestBuildSessionMACKeys_slots(t *testing.T) {
	resp := BuildSessionMACKeys(bytesRepeat32(9), "abc123", time.Unix(43200*5+100, 0).UTC())
	if resp.RotationPeriodSeconds != 43200 {
		t.Fatalf("period: %d", resp.RotationPeriodSeconds)
	}
	if resp.CurrentEpochIndex != 5 {
		t.Fatalf("current epoch: %d", resp.CurrentEpochIndex)
	}
	if resp.Previous.EpochIndex != 4 || resp.Next.EpochIndex != 6 {
		t.Fatalf("prev/next epoch: %d %d", resp.Previous.EpochIndex, resp.Next.EpochIndex)
	}
	wantDerived := DerivedSessionMACKeyForControlSessionAtEpoch(bytesRepeat32(9), "abc123", resp.CurrentEpochIndex)
	if resp.Current.DerivedSessionMACKey != wantDerived {
		t.Fatalf("derived mismatch: %q vs %q", resp.Current.DerivedSessionMACKey, wantDerived)
	}
}

func TestBuildSessionMACKeys_doesNotExposeEpochKeyMaterial(t *testing.T) {
	responseBytes, err := json.Marshal(BuildSessionMACKeys(bytesRepeat32(5), "control-session", time.Unix(43200*2, 0).UTC()))
	if err != nil {
		t.Fatalf("marshal session mac response: %v", err)
	}
	if strings.Contains(string(responseBytes), "epoch_key_material_hex") {
		t.Fatalf("response leaked epoch key material field: %s", responseBytes)
	}
}

func TestLoadOrCreateSessionMACRotationMaster_CreatesPrivateMasterFile(t *testing.T) {
	repoRoot := t.TempDir()
	master, err := LoadOrCreateSessionMACRotationMaster(repoRoot)
	if err != nil {
		t.Fatalf("load/create session mac rotation master: %v", err)
	}
	if len(master) != sessionMACRotationMasterByteCount {
		t.Fatalf("unexpected master length: %d", len(master))
	}

	masterPath := filepath.Join(repoRoot, "runtime", "state", sessionMACRotationMasterFile)
	fileInfo, err := os.Stat(masterPath)
	if err != nil {
		t.Fatalf("stat session mac rotation master: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("expected session mac rotation master permissions 0600, got %o", fileInfo.Mode().Perm())
	}

	reloadedMaster, err := LoadOrCreateSessionMACRotationMaster(repoRoot)
	if err != nil {
		t.Fatalf("reload session mac rotation master: %v", err)
	}
	if hex.EncodeToString(reloadedMaster) != hex.EncodeToString(master) {
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

	_, err := LoadOrCreateSessionMACRotationMaster(repoRoot)
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func bytesRepeat32(v byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = v
	}
	return b
}
