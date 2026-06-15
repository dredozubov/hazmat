//go:build !darwin

package launchbroker

import (
	"errors"
	"net"
)

func DefaultPeerUID(conn *net.UnixConn) (int, error) {
	return 0, errors.New("unix peer uid resolution is only implemented on darwin")
}
