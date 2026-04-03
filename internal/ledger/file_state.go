package ledger

import (
	"fmt"
	"os"
	"syscall"
)

type ledgerFileState struct {
	size              int64
	device            uint64
	inode             uint64
	changeTimeSeconds int64
	changeTimeNanos   int64
}

func ledgerFileStateFromFileInfo(fileInfo os.FileInfo) (ledgerFileState, error) {
	statRecord, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !ok || statRecord == nil {
		return ledgerFileState{}, fmt.Errorf("stat record is unavailable")
	}
	changeTimeSeconds, changeTimeNanos := ledgerFileChangeTime(statRecord)
	return ledgerFileState{
		size:              fileInfo.Size(),
		device:            uint64(statRecord.Dev),
		inode:             uint64(statRecord.Ino),
		changeTimeSeconds: changeTimeSeconds,
		changeTimeNanos:   changeTimeNanos,
	}, nil
}
