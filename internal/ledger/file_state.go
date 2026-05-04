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

type FileState struct {
	Size              int64  `json:"size"`
	Device            uint64 `json:"device"`
	Inode             uint64 `json:"inode"`
	ChangeTimeSeconds int64  `json:"change_time_seconds"`
	ChangeTimeNanos   int64  `json:"change_time_nanos"`
}

func ReadFileState(path string) (FileState, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return FileState{}, err
	}
	internalState, err := ledgerFileStateFromFileInfo(fileInfo)
	if err != nil {
		return FileState{}, err
	}
	return FileState{
		Size:              internalState.size,
		Device:            internalState.device,
		Inode:             internalState.inode,
		ChangeTimeSeconds: internalState.changeTimeSeconds,
		ChangeTimeNanos:   internalState.changeTimeNanos,
	}, nil
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
