package loopgate

import (
	"strings"
	"testing"

	"loopgate/internal/config"
)

func TestAuditIntegrityModeMessage_NilServer(t *testing.T) {
	var s *Server
	if got := s.AuditIntegrityModeMessage(); got != "" {
		t.Errorf("nil server: want empty string, got %q", got)
	}
}

func TestAuditIntegrityModeMessage_HMACDisabled(t *testing.T) {
	s := &Server{
		runtimeConfig: config.RuntimeConfig{},
	}
	// HMACCheckpoint.Enabled defaults to false
	got := s.AuditIntegrityModeMessage()
	if !strings.Contains(got, "hash-chain only") {
		t.Errorf("disabled HMAC: want message containing %q, got %q", "hash-chain only", got)
	}
	if strings.Contains(got, "HMAC checkpoints (every") {
		t.Errorf("disabled HMAC: message should not mention checkpoint interval, got %q", got)
	}
}

func TestAuditIntegrityModeMessage_HMACEnabled_DefaultInterval(t *testing.T) {
	s := &Server{
		runtimeConfig: config.RuntimeConfig{},
	}
	s.runtimeConfig.Logging.AuditLedger.HMACCheckpoint.Enabled = true
	// IntervalEvents zero should default to 256 in the message
	got := s.AuditIntegrityModeMessage()
	if !strings.Contains(got, "HMAC checkpoints") {
		t.Errorf("enabled HMAC: want message containing %q, got %q", "HMAC checkpoints", got)
	}
	if !strings.Contains(got, "256") {
		t.Errorf("enabled HMAC with zero interval: want default 256 in message, got %q", got)
	}
}

func TestAuditIntegrityModeMessage_HMACEnabled_ExplicitInterval(t *testing.T) {
	s := &Server{
		runtimeConfig: config.RuntimeConfig{},
	}
	s.runtimeConfig.Logging.AuditLedger.HMACCheckpoint.Enabled = true
	s.runtimeConfig.Logging.AuditLedger.HMACCheckpoint.IntervalEvents = 512
	got := s.AuditIntegrityModeMessage()
	if !strings.Contains(got, "HMAC checkpoints") {
		t.Errorf("enabled HMAC: want message containing %q, got %q", "HMAC checkpoints", got)
	}
	if !strings.Contains(got, "512") {
		t.Errorf("enabled HMAC with interval 512: want 512 in message, got %q", got)
	}
	if strings.Contains(got, "hash-chain only") {
		t.Errorf("enabled HMAC: message should not say hash-chain only, got %q", got)
	}
}
