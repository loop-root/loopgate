package memory

import (
	"fmt"
	"io"
	"os"

	ledgerpkg "loopgate/internal/ledger"
)

func openVerifiedMemoryLedger(ledgerPath string) (*os.File, error) {
	ledgerFileHandle, err := os.Open(ledgerPath)
	if err != nil {
		return nil, err
	}

	if _, _, err := ledgerpkg.ReadVerifiedChainState(ledgerFileHandle, "ledger_sequence"); err != nil {
		_ = ledgerFileHandle.Close()
		return nil, fmt.Errorf("%w: %v", ErrLedgerIntegrity, err)
	}
	if _, err := ledgerFileHandle.Seek(0, io.SeekStart); err != nil {
		_ = ledgerFileHandle.Close()
		return nil, fmt.Errorf("seek verified ledger start: %w", err)
	}
	return ledgerFileHandle, nil
}
