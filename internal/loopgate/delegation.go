package loopgate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"loopgate/internal/identifiers"
)

const (
	DelegatedSessionSchemaVersion   = "loopgate.delegated_session.v1"
	DelegatedSessionMessageTypeAuth = "credentials"
	DelegatedSessionRefreshLeadTime = 2 * time.Minute
)

var ErrDelegatedSessionRefreshRequired = errors.New("delegated loopgate credentials require refresh")

type DelegatedSessionState string

const (
	DelegatedSessionStateHealthy         DelegatedSessionState = "healthy"
	DelegatedSessionStateRefreshSoon     DelegatedSessionState = "refresh_soon"
	DelegatedSessionStateRefreshRequired DelegatedSessionState = "refresh_required"
)

type DelegatedSessionEnvelope struct {
	SchemaVersion string                   `json:"schema_version"`
	MessageType   string                   `json:"message_type"`
	SentAtUTC     string                   `json:"sent_at_utc"`
	Credentials   *DelegatedSessionPayload `json:"credentials,omitempty"`
}

type DelegatedSessionPayload struct {
	ControlSessionID string `json:"control_session_id"`
	CapabilityToken  string `json:"capability_token"`
	ApprovalToken    string `json:"approval_token"`
	SessionMACKey    string `json:"session_mac_key"`
	ExpiresAtUTC     string `json:"expires_at_utc"`
}

type DelegatedSessionStreamWriter struct {
	encoder *json.Encoder
	now     func() time.Time
}

type DelegatedSessionStreamReader struct {
	decoder *json.Decoder
}

func NewDelegatedSessionStreamWriter(writer io.Writer) *DelegatedSessionStreamWriter {
	return &DelegatedSessionStreamWriter{
		encoder: json.NewEncoder(writer),
		now:     time.Now,
	}
}

func NewDelegatedSessionStreamReader(reader io.Reader) *DelegatedSessionStreamReader {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	return &DelegatedSessionStreamReader{decoder: decoder}
}

func NewDelegatedSessionEnvelope(delegatedSession DelegatedSessionConfig, now time.Time) (DelegatedSessionEnvelope, error) {
	if err := validateDelegatedSessionConfig(delegatedSession, now); err != nil {
		return DelegatedSessionEnvelope{}, err
	}

	return DelegatedSessionEnvelope{
		SchemaVersion: DelegatedSessionSchemaVersion,
		MessageType:   DelegatedSessionMessageTypeAuth,
		SentAtUTC:     now.UTC().Format(time.RFC3339Nano),
		Credentials: &DelegatedSessionPayload{
			ControlSessionID: delegatedSession.ControlSessionID,
			CapabilityToken:  delegatedSession.CapabilityToken,
			ApprovalToken:    delegatedSession.ApprovalToken,
			SessionMACKey:    delegatedSession.SessionMACKey,
			ExpiresAtUTC:     delegatedSession.ExpiresAt.UTC().Format(time.RFC3339Nano),
		},
	}, nil
}

func (delegatedSessionEnvelope DelegatedSessionEnvelope) Validate() error {
	if strings.TrimSpace(delegatedSessionEnvelope.SchemaVersion) != DelegatedSessionSchemaVersion {
		return fmt.Errorf("unexpected delegated session schema version %q", delegatedSessionEnvelope.SchemaVersion)
	}
	if strings.TrimSpace(delegatedSessionEnvelope.MessageType) != DelegatedSessionMessageTypeAuth {
		return fmt.Errorf("unexpected delegated session message type %q", delegatedSessionEnvelope.MessageType)
	}
	if strings.TrimSpace(delegatedSessionEnvelope.SentAtUTC) == "" {
		return fmt.Errorf("missing delegated session sent_at_utc")
	}
	if _, err := time.Parse(time.RFC3339Nano, delegatedSessionEnvelope.SentAtUTC); err != nil {
		return fmt.Errorf("parse delegated session sent_at_utc: %w", err)
	}
	if delegatedSessionEnvelope.Credentials == nil {
		return fmt.Errorf("missing delegated session credentials")
	}
	return delegatedSessionEnvelope.Credentials.Validate()
}

func (delegatedSessionPayload DelegatedSessionPayload) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("delegated control session id", delegatedSessionPayload.ControlSessionID); err != nil {
		return err
	}
	if strings.TrimSpace(delegatedSessionPayload.CapabilityToken) == "" {
		return fmt.Errorf("missing delegated capability token")
	}
	if strings.TrimSpace(delegatedSessionPayload.ApprovalToken) == "" {
		return fmt.Errorf("missing delegated approval token")
	}
	if strings.TrimSpace(delegatedSessionPayload.SessionMACKey) == "" {
		return fmt.Errorf("missing delegated session mac key")
	}
	if strings.TrimSpace(delegatedSessionPayload.ExpiresAtUTC) == "" {
		return fmt.Errorf("missing delegated expires_at_utc")
	}
	if _, err := time.Parse(time.RFC3339Nano, delegatedSessionPayload.ExpiresAtUTC); err != nil {
		return fmt.Errorf("parse delegated expires_at_utc: %w", err)
	}
	return nil
}

func (delegatedSessionEnvelope DelegatedSessionEnvelope) ToConfig() (DelegatedSessionConfig, error) {
	if err := delegatedSessionEnvelope.Validate(); err != nil {
		return DelegatedSessionConfig{}, err
	}
	expiresAtUTC, err := time.Parse(time.RFC3339Nano, delegatedSessionEnvelope.Credentials.ExpiresAtUTC)
	if err != nil {
		return DelegatedSessionConfig{}, fmt.Errorf("parse delegated credential expiry: %w", err)
	}

	return DelegatedSessionConfig{
		ControlSessionID: delegatedSessionEnvelope.Credentials.ControlSessionID,
		CapabilityToken:  delegatedSessionEnvelope.Credentials.CapabilityToken,
		ApprovalToken:    delegatedSessionEnvelope.Credentials.ApprovalToken,
		SessionMACKey:    delegatedSessionEnvelope.Credentials.SessionMACKey,
		ExpiresAt:        expiresAtUTC.UTC(),
	}, nil
}

func (delegatedSessionStreamWriter *DelegatedSessionStreamWriter) WriteCredentials(delegatedSession DelegatedSessionConfig) error {
	now := time.Now
	if delegatedSessionStreamWriter.now != nil {
		now = delegatedSessionStreamWriter.now
	}
	envelope, err := NewDelegatedSessionEnvelope(delegatedSession, now())
	if err != nil {
		return err
	}
	if err := delegatedSessionStreamWriter.encoder.Encode(envelope); err != nil {
		return fmt.Errorf("encode delegated session envelope: %w", err)
	}
	return nil
}

func (delegatedSessionStreamReader *DelegatedSessionStreamReader) ReadCredentials() (DelegatedSessionConfig, error) {
	var delegatedSessionEnvelope DelegatedSessionEnvelope
	if err := delegatedSessionStreamReader.decoder.Decode(&delegatedSessionEnvelope); err != nil {
		return DelegatedSessionConfig{}, fmt.Errorf("decode delegated session envelope: %w", err)
	}
	return delegatedSessionEnvelope.ToConfig()
}

func EvaluateDelegatedSessionState(now time.Time, expiresAt time.Time) DelegatedSessionState {
	if expiresAt.IsZero() {
		return DelegatedSessionStateRefreshRequired
	}
	nowUTC := now.UTC()
	expiresAtUTC := expiresAt.UTC()
	if !expiresAtUTC.After(nowUTC) {
		return DelegatedSessionStateRefreshRequired
	}
	if expiresAtUTC.Sub(nowUTC) <= DelegatedSessionRefreshLeadTime {
		return DelegatedSessionStateRefreshSoon
	}
	return DelegatedSessionStateHealthy
}

func ShouldRefreshDelegatedSession(now time.Time, expiresAt time.Time) bool {
	return EvaluateDelegatedSessionState(now, expiresAt) != DelegatedSessionStateHealthy
}

func validateDelegatedSessionConfig(delegatedSession DelegatedSessionConfig, now time.Time) error {
	if err := identifiers.ValidateSafeIdentifier("delegated control session id", delegatedSession.ControlSessionID); err != nil {
		return err
	}
	if strings.TrimSpace(delegatedSession.CapabilityToken) == "" {
		return fmt.Errorf("missing delegated capability token")
	}
	if strings.TrimSpace(delegatedSession.ApprovalToken) == "" {
		return fmt.Errorf("missing delegated approval token")
	}
	if strings.TrimSpace(delegatedSession.SessionMACKey) == "" {
		return fmt.Errorf("missing delegated session mac key")
	}
	if delegatedSession.ExpiresAt.IsZero() {
		return fmt.Errorf("missing delegated token expiry")
	}
	if EvaluateDelegatedSessionState(now, delegatedSession.ExpiresAt) == DelegatedSessionStateRefreshRequired {
		return ErrDelegatedSessionRefreshRequired
	}
	return nil
}
