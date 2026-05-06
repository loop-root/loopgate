package loopgate

import (
	"crypto/subtle"
	"encoding/hex"
	"loopgate/internal/controlruntime"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"time"
)

func (server *Server) currentSessionMACEpochIndex() int64 {
	return controlruntime.SessionMACEpochIndexAt(server.now().UTC())
}

func (server *Server) sessionMACKeyForControlSessionAtEpoch(controlSessionID string, epochIndex int64) string {
	return controlruntime.DerivedSessionMACKeyForControlSessionAtEpoch(server.sessionMACRotationMaster, controlSessionID, epochIndex)
}

func (server *Server) loadOrCreateSessionMACRotationMaster() error {
	master, err := controlruntime.LoadOrCreateSessionMACRotationMaster(server.repoRoot)
	if err != nil {
		return err
	}
	server.sessionMACRotationMaster = master
	return nil
}

func (server *Server) buildSessionMACKeysResponse(controlSessionID string) controlapipkg.SessionMACKeysResponse {
	keys := controlruntime.BuildSessionMACKeys(server.sessionMACRotationMaster, controlSessionID, server.now().UTC())
	return controlapipkg.SessionMACKeysResponse{
		SchemaVersion:         keys.SchemaVersion,
		RotationPeriodSeconds: keys.RotationPeriodSeconds,
		DerivedKeySchema:      keys.DerivedKeySchema,
		ControlSessionID:      keys.ControlSessionID,
		CurrentEpochIndex:     keys.CurrentEpochIndex,
		Previous:              sessionMACKeySlotToWire(keys.Previous),
		Current:               sessionMACKeySlotToWire(keys.Current),
		Next:                  sessionMACKeySlotToWire(keys.Next),
	}
}

func sessionMACKeySlotToWire(slot controlruntime.SessionMACKeySlot) controlapipkg.SessionMACKeySlotInfo {
	return controlapipkg.SessionMACKeySlotInfo{
		Slot:                 slot.Slot,
		EpochIndex:           slot.EpochIndex,
		ValidFromUTC:         slot.ValidFromUTC.Format(time.RFC3339Nano),
		ValidUntilUTC:        slot.ValidUntilUTC.Format(time.RFC3339Nano),
		DerivedSessionMACKey: slot.DerivedSessionMACKey,
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
	candidateMACKeys := controlruntime.CandidateSessionMACKeys(sessionMACRotationMaster, headers.ControlSessionID, server.currentSessionMACEpochIndex())
	return server.verifySignedRequestAgainstMACKeys(request, requestBodyBytes, headers, candidateMACKeys)
}
