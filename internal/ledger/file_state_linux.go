//go:build linux

package ledger

import "syscall"

func ledgerFileChangeTime(statRecord *syscall.Stat_t) (int64, int64) {
	return statRecord.Ctim.Sec, statRecord.Ctim.Nsec
}
