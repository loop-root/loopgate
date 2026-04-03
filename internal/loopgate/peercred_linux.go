//go:build linux

package loopgate

import (
	"fmt"
	"net"
	"os"

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
		ucred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			peerErr = fmt.Errorf("get peer credentials: %w", err)
			return
		}
		peerCreds = peerIdentity{
			UID:  ucred.Uid,
			PID:  int(ucred.Pid),
			EPID: int(ucred.Pid),
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
	exePath, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return "", fmt.Errorf("readlink /proc/%d/exe: %w", pid, err)
	}
	return exePath, nil
}
