//go:build !darwin && !linux

package loopgate

import "fmt"

func processExists(pid int) (bool, error) {
	return false, fmt.Errorf("process liveness probing is unsupported on this platform for pid %d", pid)
}
