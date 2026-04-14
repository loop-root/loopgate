//go:build darwin || linux

package loopgate

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func processExists(pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("invalid pid %d", pid)
	}
	err := unix.Kill(pid, 0)
	switch err {
	case nil:
		return true, nil
	case unix.ESRCH:
		return false, nil
	case unix.EPERM:
		// EPERM means the process exists but this process cannot signal it.
		return true, nil
	default:
		return false, fmt.Errorf("probe process liveness for pid %d: %w", pid, err)
	}
}
