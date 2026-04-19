package loopgate

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const (
	sessionMACEpochDuration = 12 * time.Hour

	sessionMACRotationMasterFile      = "loopgate_session_mac_rotation_master"
	sessionMACRotationMasterByteCount = 32

	sessionMACDerivedKeySchemaV1 = "loopgate-session-mac-v1"
	sessionMACKeysWireSchemaV1   = "loopgate.session_mac_keys.v1"

	sessionMACEpochDomainV1 = "loopgate-session-mac-epoch-v1\x00"
)

func sessionMACEpochIndexAt(utc time.Time) int64 {
	sec := utc.Unix()
	period := int64(sessionMACEpochDuration / time.Second)
	if sec >= 0 {
		return sec / period
	}
	// Floor division for negative Unix times (pre-1970); not expected in practice.
	return (sec - period + 1) / period
}

func sessionMACEpochWallRange(epochIndex int64) (validFromUTC, validUntilUTC time.Time) {
	period := int64(sessionMACEpochDuration / time.Second)
	startUnix := epochIndex * period
	validFromUTC = time.Unix(startUnix, 0).UTC()
	validUntilUTC = validFromUTC.Add(sessionMACEpochDuration)
	return validFromUTC, validUntilUTC
}

func deriveEpochKeyMaterial(master []byte, epochIndex int64) []byte {
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

// derivedSessionMACKeyString returns the 64-character lowercase hex string used as session_mac_key
// on the wire (UTF-8 bytes of that string are passed to HMAC-SHA256 in signRequest).
func derivedSessionMACKeyString(epochKeyMaterial []byte, controlSessionID string) string {
	if len(epochKeyMaterial) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, epochKeyMaterial)
	_, _ = mac.Write([]byte(controlSessionID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (server *Server) currentSessionMACEpochIndex() int64 {
	return sessionMACEpochIndexAt(server.now().UTC())
}

func derivedSessionMACKeyForControlSessionAtEpoch(sessionMACRotationMaster []byte, controlSessionID string, epochIndex int64) string {
	mat := deriveEpochKeyMaterial(sessionMACRotationMaster, epochIndex)
	return derivedSessionMACKeyString(mat, controlSessionID)
}

func (server *Server) sessionMACKeyForControlSessionAtEpoch(controlSessionID string, epochIndex int64) string {
	return derivedSessionMACKeyForControlSessionAtEpoch(server.sessionMACRotationMaster, controlSessionID, epochIndex)
}

func (server *Server) loadOrCreateSessionMACRotationMaster() error {
	stateDir := filepath.Join(server.repoRoot, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("mkdir runtime state: %w", err)
	}
	path := filepath.Join(stateDir, sessionMACRotationMasterFile)
	existing, err := readPrivateStateFileNoFollow(path)
	if err == nil {
		if len(existing) != sessionMACRotationMasterByteCount {
			return fmt.Errorf("session mac rotation master file %q has wrong size (want %d bytes)", path, sessionMACRotationMasterByteCount)
		}
		server.sessionMACRotationMaster = append([]byte(nil), existing...)
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("read session mac rotation master: %w", err)
	}
	material := make([]byte, sessionMACRotationMasterByteCount)
	if _, err := rand.Read(material); err != nil {
		return fmt.Errorf("generate session mac rotation master: %w", err)
	}
	if err := createPrivateStateFileExclusive(path, material); err != nil {
		if errors.Is(err, os.ErrExist) {
			existing, readErr := readPrivateStateFileNoFollow(path)
			if readErr != nil {
				return fmt.Errorf("read raced session mac rotation master: %w", readErr)
			}
			if len(existing) != sessionMACRotationMasterByteCount {
				return fmt.Errorf("session mac rotation master file %q has wrong size (want %d bytes)", path, sessionMACRotationMasterByteCount)
			}
			server.sessionMACRotationMaster = append([]byte(nil), existing...)
			return nil
		}
		return fmt.Errorf("write session mac rotation master: %w", err)
	}
	server.sessionMACRotationMaster = material
	return nil
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

func (server *Server) buildSessionMACKeysResponse(controlSessionID string) controlapipkg.SessionMACKeysResponse {
	cur := server.currentSessionMACEpochIndex()
	prevIdx, nextIdx := cur-1, cur+1

	buildSlot := func(slot string, epochIndex int64) controlapipkg.SessionMACKeySlotInfo {
		validFrom, validUntil := sessionMACEpochWallRange(epochIndex)
		mat := deriveEpochKeyMaterial(server.sessionMACRotationMaster, epochIndex)
		return controlapipkg.SessionMACKeySlotInfo{
			Slot:                 slot,
			EpochIndex:           epochIndex,
			ValidFromUTC:         validFrom.Format(time.RFC3339Nano),
			ValidUntilUTC:        validUntil.Format(time.RFC3339Nano),
			DerivedSessionMACKey: derivedSessionMACKeyString(mat, controlSessionID),
		}
	}

	return controlapipkg.SessionMACKeysResponse{
		SchemaVersion:         sessionMACKeysWireSchemaV1,
		RotationPeriodSeconds: int64(sessionMACEpochDuration / time.Second),
		DerivedKeySchema:      sessionMACDerivedKeySchemaV1,
		ControlSessionID:      controlSessionID,
		CurrentEpochIndex:     cur,
		Previous:              buildSlot("previous", prevIdx),
		Current:               buildSlot("current", cur),
		Next:                  buildSlot("next", nextIdx),
	}
}

func requestSignatureBytesMatchMACKey(requestSignatureHex string, method string, path string, controlSessionID string, requestTimestamp string, requestNonce string, requestBodyBytes []byte, sessionMACKey string) bool {
	decodedRequestSignature, err := hex.DecodeString(requestSignatureHex)
	if err != nil {
		return false
	}
	expectedHex := signRequest(sessionMACKey, method, path, controlSessionID, requestTimestamp, requestNonce, requestBodyBytes)
	decodedExpectedSignature, err := hex.DecodeString(expectedHex)
	if err != nil {
		return false
	}
	if len(decodedRequestSignature) != len(decodedExpectedSignature) {
		return false
	}
	return subtle.ConstantTimeCompare(decodedExpectedSignature, decodedRequestSignature) == 1
}

// verifySignedRequestAgainstRotatingSessionMAC tries HMAC keys derived from previous, current, and next
// 12-hour epochs so clients can cross rotation boundaries without immediately refreshing session_mac_key.
// boundControlSessionID must match X-Loopgate-Control-Session and is used for both derivation and signing_payload.
func (server *Server) verifySignedRequestAgainstRotatingSessionMAC(request *http.Request, requestBodyBytes []byte, headers signedControlPlaneHeaders, sessionMACRotationMaster []byte) (controlapipkg.CapabilityResponse, bool) {
	if len(sessionMACRotationMaster) == 0 {
		return controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "session mac rotation is not initialized",
			DenialCode:   controlapipkg.DenialCodeRequestSignatureInvalid,
		}, false
	}
	cur := server.currentSessionMACEpochIndex()
	candidateMACKeys := make([]string, 0, 3)
	for _, epochIndex := range []int64{cur - 1, cur, cur + 1} {
		candidateMACKeys = append(candidateMACKeys, derivedSessionMACKeyForControlSessionAtEpoch(sessionMACRotationMaster, headers.ControlSessionID, epochIndex))
	}
	return server.verifySignedRequestAgainstMACKeys(request, requestBodyBytes, headers, candidateMACKeys)
}
