//go:build !darwin && !linux

package loopgate

import (
	"fmt"
	"net"
)

func peerIdentityFromConn(connection net.Conn) (peerIdentity, error) {
	return peerIdentity{}, fmt.Errorf("peer credential lookup is not supported on this platform")
}
