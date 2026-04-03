package audit

import (
	"errors"
	"strings"
	"testing"

	"morph/internal/ledger"
)

func TestLedgerWriter_MustPersistReturnsAppendFailure(t *testing.T) {
	expectedError := errors.New("disk full")
	writer := NewLedgerWriter(func(string, ledger.Event) error {
		return expectedError
	}, nil)

	err := writer.Record("ignored", ClassMustPersist, ledger.Event{Type: "capability.executed"})
	if err == nil {
		t.Fatal("expected must-persist audit failure to be returned")
	}
	if !strings.Contains(err.Error(), "capability.executed") {
		t.Fatalf("expected event type context in error, got %v", err)
	}
}

func TestLedgerWriter_WarnOnlyReportsButDoesNotReturnFailure(t *testing.T) {
	expectedError := errors.New("append failed")
	var reportedError error
	writer := NewLedgerWriter(func(string, ledger.Event) error {
		return expectedError
	}, func(err error) {
		reportedError = err
	})

	err := writer.Record("ignored", ClassWarnOnly, ledger.Event{Type: "warning.state_save_failed"})
	if err != nil {
		t.Fatalf("expected warn-only audit failure to be suppressed, got %v", err)
	}
	if reportedError == nil {
		t.Fatal("expected warn-only audit failure to be reported")
	}
	if !strings.Contains(reportedError.Error(), "warning.state_save_failed") {
		t.Fatalf("expected event type context in reported warning, got %v", reportedError)
	}
}
