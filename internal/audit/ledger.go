package audit

import (
	"fmt"

	"loopgate/internal/ledger"
)

type Class string

const (
	ClassMustPersist Class = "must_persist"
	ClassWarnOnly    Class = "warn_only"
)

type WarningReporter func(error)

type LedgerWriter struct {
	appendEvent   func(string, ledger.Event) error
	reportWarning WarningReporter
}

func NewLedgerWriter(
	appendEvent func(string, ledger.Event) error,
	reportWarning WarningReporter,
) LedgerWriter {
	if appendEvent == nil {
		appendEvent = ledger.Append
	}
	return LedgerWriter{
		appendEvent:   appendEvent,
		reportWarning: reportWarning,
	}
}

func (writer LedgerWriter) Record(path string, class Class, ledgerEvent ledger.Event) error {
	if err := writer.appendEvent(path, ledgerEvent); err != nil {
		wrappedError := fmt.Errorf("append %s ledger event %q: %w", class, ledgerEvent.Type, err)
		if class == ClassWarnOnly {
			if writer.reportWarning != nil {
				writer.reportWarning(wrappedError)
			}
			return nil
		}
		return wrappedError
	}
	return nil
}

func RecordMustPersist(path string, ledgerEvent ledger.Event) error {
	return NewLedgerWriter(nil, nil).Record(path, ClassMustPersist, ledgerEvent)
}

func RecordWarnOnly(path string, ledgerEvent ledger.Event, reportWarning WarningReporter) {
	_ = NewLedgerWriter(nil, reportWarning).Record(path, ClassWarnOnly, ledgerEvent)
}
