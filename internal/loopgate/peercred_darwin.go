//go:build darwin

package loopgate

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func peerIdentityFromConn(connection net.Conn) (peerIdentity, error) {
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return peerIdentity{}, fmt.Errorf("unexpected connection type %T", connection)
	}

	rawConnection, err := unixConnection.SyscallConn()
	if err != nil {
		return peerIdentity{}, fmt.Errorf("syscall conn: %w", err)
	}

	var peerCreds peerIdentity
	var peerErr error
	controlErr := rawConnection.Control(func(fd uintptr) {
		xucred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if err != nil {
			peerErr = fmt.Errorf("get peer credentials: %w", err)
			return
		}

		peerPID, err := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		if err != nil {
			peerErr = fmt.Errorf("get peer pid: %w", err)
			return
		}

		peerEPID, err := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEEREPID)
		if err != nil {
			peerErr = fmt.Errorf("get peer effective pid: %w", err)
			return
		}

		peerCreds = peerIdentity{
			UID:  xucred.Uid,
			PID:  peerPID,
			EPID: peerEPID,
		}
	})
	if controlErr != nil {
		return peerIdentity{}, fmt.Errorf("read peer credentials: %w", controlErr)
	}
	if peerErr != nil {
		return peerIdentity{}, peerErr
	}
	return peerCreds, nil
}

func resolveExecutablePath(pid int) (string, error) {
	// Use PROC_PIDPATHINFO via SysctlRaw with kern.procargs2 alternative:
	// We use the safer approach of reading /proc equivalent via the
	// proc_pidpath syscall wrapper available in x/sys.
	//
	// Fallback: use the KERN_PROCARGS2 sysctl to extract the executable path.
	const maxPathLen = 4096
	rawArgs, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", fmt.Errorf("sysctl kern.procargs2 pid=%d: %w", pid, err)
	}
	// kern.procargs2 layout: [4-byte argc] [executable path \0] [args...]
	if len(rawArgs) < 4 {
		return "", fmt.Errorf("kern.procargs2 result too short for pid %d", pid)
	}
	// Skip argc (4 bytes), then read the null-terminated executable path.
	pathBytes := rawArgs[4:]
	nullIdx := -1
	for i, b := range pathBytes {
		if b == 0 {
			nullIdx = i
			break
		}
		if i >= maxPathLen {
			break
		}
	}
	if nullIdx <= 0 {
		return "", fmt.Errorf("cannot extract executable path from kern.procargs2 for pid %d", pid)
	}
	return string(pathBytes[:nullIdx]), nil
}
