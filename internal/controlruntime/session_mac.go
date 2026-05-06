package controlruntime

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const (
	SessionMACEpochDuration = 12 * time.Hour

	sessionMACRotationMasterFile      = "loopgate_session_mac_rotation_master"
	sessionMACRotationMasterByteCount = 32

	SessionMACDerivedKeySchemaV1 = "loopgate-session-mac-v1"
	SessionMACKeysWireSchemaV1   = "loopgate.session_mac_keys.v1"

	sessionMACEpochDomainV1 = "loopgate-session-mac-epoch-v1\x00"
)

type SessionMACKeySlot struct {
	Slot                 string
	EpochIndex           int64
	ValidFromUTC         time.Time
	ValidUntilUTC        time.Time
	DerivedSessionMACKey string
}

type SessionMACKeys struct {
	SchemaVersion         string
	RotationPeriodSeconds int64
	DerivedKeySchema      string
	ControlSessionID      string
	CurrentEpochIndex     int64
	Previous              SessionMACKeySlot
	Current               SessionMACKeySlot
	Next                  SessionMACKeySlot
}

func SessionMACEpochIndexAt(utc time.Time) int64 {
	sec := utc.Unix()
	period := int64(SessionMACEpochDuration / time.Second)
	if sec >= 0 {
		return sec / period
	}
	// Floor division for negative Unix times (pre-1970); not expected in practice.
	return (sec - period + 1) / period
}

func SessionMACEpochWallRange(epochIndex int64) (validFromUTC, validUntilUTC time.Time) {
	period := int64(SessionMACEpochDuration / time.Second)
	startUnix := epochIndex * period
	validFromUTC = time.Unix(startUnix, 0).UTC()
	validUntilUTC = validFromUTC.Add(SessionMACEpochDuration)
	return validFromUTC, validUntilUTC
}

func DeriveEpochKeyMaterial(master []byte, epochIndex int64) []byte {
	if len(master) == 0 {
		return nil
	}
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte(sessionMACEpochDomainV1))
	var idxBuf [8]byte
	binary.BigEndian.PutUint64(idxBuf[:], uint64(epochIndex))
	_, _ = mac.Write(idxBuf[:])
	return mac.Sum(nil)
}

// DerivedSessionMACKeyString returns the 64-character lowercase hex string used as session_mac_key
// on the wire. Callers pass the UTF-8 bytes of this string to HMAC-SHA256 when signing requests.
func DerivedSessionMACKeyString(epochKeyMaterial []byte, controlSessionID string) string {
	if len(epochKeyMaterial) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, epochKeyMaterial)
	_, _ = mac.Write([]byte(controlSessionID))
	return hex.EncodeToString(mac.Sum(nil))
}

func DerivedSessionMACKeyForControlSessionAtEpoch(sessionMACRotationMaster []byte, controlSessionID string, epochIndex int64) string {
	mat := DeriveEpochKeyMaterial(sessionMACRotationMaster, epochIndex)
	return DerivedSessionMACKeyString(mat, controlSessionID)
}

func CandidateSessionMACKeys(sessionMACRotationMaster []byte, controlSessionID string, currentEpochIndex int64) []string {
	candidateMACKeys := make([]string, 0, 3)
	for _, epochIndex := range []int64{currentEpochIndex - 1, currentEpochIndex, currentEpochIndex + 1} {
		candidateMACKeys = append(candidateMACKeys, DerivedSessionMACKeyForControlSessionAtEpoch(sessionMACRotationMaster, controlSessionID, epochIndex))
	}
	return candidateMACKeys
}

func BuildSessionMACKeys(sessionMACRotationMaster []byte, controlSessionID string, nowUTC time.Time) SessionMACKeys {
	cur := SessionMACEpochIndexAt(nowUTC.UTC())
	prevIdx, nextIdx := cur-1, cur+1

	buildSlot := func(slot string, epochIndex int64) SessionMACKeySlot {
		validFrom, validUntil := SessionMACEpochWallRange(epochIndex)
		mat := DeriveEpochKeyMaterial(sessionMACRotationMaster, epochIndex)
		return SessionMACKeySlot{
			Slot:                 slot,
			EpochIndex:           epochIndex,
			ValidFromUTC:         validFrom,
			ValidUntilUTC:        validUntil,
			DerivedSessionMACKey: DerivedSessionMACKeyString(mat, controlSessionID),
		}
	}

	return SessionMACKeys{
		SchemaVersion:         SessionMACKeysWireSchemaV1,
		RotationPeriodSeconds: int64(SessionMACEpochDuration / time.Second),
		DerivedKeySchema:      SessionMACDerivedKeySchemaV1,
		ControlSessionID:      controlSessionID,
		CurrentEpochIndex:     cur,
		Previous:              buildSlot("previous", prevIdx),
		Current:               buildSlot("current", cur),
		Next:                  buildSlot("next", nextIdx),
	}
}

func LoadOrCreateSessionMACRotationMaster(repoRoot string) ([]byte, error) {
	stateDir := filepath.Join(repoRoot, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir runtime state: %w", err)
	}
	path := filepath.Join(stateDir, sessionMACRotationMasterFile)
	existing, err := readPrivateStateFileNoFollow(path)
	if err == nil {
		if len(existing) != sessionMACRotationMasterByteCount {
			return nil, fmt.Errorf("session mac rotation master file %q has wrong size (want %d bytes)", path, sessionMACRotationMasterByteCount)
		}
		return append([]byte(nil), existing...), nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read session mac rotation master: %w", err)
	}
	material := make([]byte, sessionMACRotationMasterByteCount)
	if _, err := rand.Read(material); err != nil {
		return nil, fmt.Errorf("generate session mac rotation master: %w", err)
	}
	if err := createPrivateStateFileExclusive(path, material); err != nil {
		if errors.Is(err, os.ErrExist) {
			existing, readErr := readPrivateStateFileNoFollow(path)
			if readErr != nil {
				return nil, fmt.Errorf("read raced session mac rotation master: %w", readErr)
			}
			if len(existing) != sessionMACRotationMasterByteCount {
				return nil, fmt.Errorf("session mac rotation master file %q has wrong size (want %d bytes)", path, sessionMACRotationMasterByteCount)
			}
			return append([]byte(nil), existing...), nil
		}
		return nil, fmt.Errorf("write session mac rotation master: %w", err)
	}
	return material, nil
}

func readPrivateStateFileNoFollow(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		switch err {
		case unix.ENOENT:
			return nil, os.ErrNotExist
		case unix.ELOOP:
			return nil, fmt.Errorf("state file %q must not be a symlink", path)
		default:
			return nil, fmt.Errorf("open state file: %w", err)
		}
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("wrap state file descriptor")
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat state file: %w", err)
	}
	if fileInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("state file %q must not be a symlink", path)
	}
	return io.ReadAll(file)
}

func createPrivateStateFileExclusive(path string, contents []byte) error {
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		switch err {
		case unix.EEXIST:
			return os.ErrExist
		case unix.ELOOP:
			return fmt.Errorf("state file %q must not be a symlink", path)
		default:
			return fmt.Errorf("open private state file: %w", err)
		}
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("wrap private state file descriptor")
	}
	if _, err := file.Write(contents); err != nil {
		file.Close()
		return fmt.Errorf("write private state file: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync private state file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close private state file: %w", err)
	}
	if stateDir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = stateDir.Sync()
		_ = stateDir.Close()
	}
	return nil
}
