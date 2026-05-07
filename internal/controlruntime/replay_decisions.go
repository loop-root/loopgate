package controlruntime

import "time"

type ReplayRecordStatus string

const (
	ReplayRecordAccepted  ReplayRecordStatus = "accepted"
	ReplayRecordDuplicate ReplayRecordStatus = "duplicate"
	ReplayRecordSaturated ReplayRecordStatus = "saturated"
)

type UsedToken struct {
	TokenID           string
	ParentTokenID     string
	ControlSessionID  string
	Capability        string
	NormalizedArgHash string
	ConsumedAt        time.Time
}

func RequestReplayKey(controlSessionID string, requestID string) string {
	return controlSessionID + ":" + requestID
}

func RecordSeenRequest(seenRequests map[string]SeenRequest, maxEntries int, controlSessionID string, requestID string, seenAt time.Time) (SeenRequest, ReplayRecordStatus) {
	requestKey := RequestReplayKey(controlSessionID, requestID)
	if _, found := seenRequests[requestKey]; found {
		return SeenRequest{}, ReplayRecordDuplicate
	}
	if len(seenRequests) >= maxEntries {
		return SeenRequest{}, ReplayRecordSaturated
	}
	recordedRequest := SeenRequest{
		ControlSessionID: controlSessionID,
		SeenAt:           seenAt.UTC(),
	}
	seenRequests[requestKey] = recordedRequest
	return recordedRequest, ReplayRecordAccepted
}

func ConsumeUsedToken(usedTokens map[string]UsedToken, tokenID string, parentTokenID string, controlSessionID string, capability string, normalizedArgHash string, consumedAt time.Time) (UsedToken, ReplayRecordStatus) {
	if _, alreadyUsed := usedTokens[tokenID]; alreadyUsed {
		return UsedToken{}, ReplayRecordDuplicate
	}
	consumedToken := UsedToken{
		TokenID:           tokenID,
		ParentTokenID:     parentTokenID,
		ControlSessionID:  controlSessionID,
		Capability:        capability,
		NormalizedArgHash: normalizedArgHash,
		ConsumedAt:        consumedAt.UTC(),
	}
	usedTokens[tokenID] = consumedToken
	return consumedToken, ReplayRecordAccepted
}
