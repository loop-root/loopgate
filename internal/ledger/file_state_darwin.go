//go:build darwin

package ledger

import "syscall"

func ledgerFileChangeTime(statRecord *syscall.Stat_t) (int64, int64) {
	return statRecord.Ctimespec.Sec, statRecord.Ctimespec.Nsec
}
