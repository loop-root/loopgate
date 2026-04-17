//go:build unix

package loopgate

import (
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
)

// unixSocketUmaskMu serializes temporary process-umask changes during Unix
// socket creation. Umask is process-global, so parallel test servers or
// concurrent startup attempts must not stomp each other's create-time mask.
var unixSocketUmaskMu sync.Mutex

func listenPrivateUnixSocket(socketPath string) (net.Listener, error) {
	unixSocketUmaskMu.Lock()
	previousUmask := syscall.Umask(0o077)
	listener, listenErr := net.Listen("unix", socketPath)
	if listenErr != nil {
		_ = syscall.Umask(previousUmask)
		unixSocketUmaskMu.Unlock()
		return nil, listenErr
	}
	chmodErr := os.Chmod(socketPath, 0o600)
	_ = syscall.Umask(previousUmask)
	unixSocketUmaskMu.Unlock()
	if chmodErr != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("chmod socket: %w", chmodErr)
	}
	return listener, nil
}
