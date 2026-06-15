//go:build darwin

package launchbroker

import (
	"errors"
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func DefaultPeerUID(conn *net.UnixConn) (int, error) {
	if conn == nil {
		return 0, errors.New("unix connection is required")
	}

	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("inspect peer credentials: %w", err)
	}

	var uid int
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if err != nil {
			sockErr = err
			return
		}
		uid = int(cred.Uid)
	}); err != nil {
		return 0, fmt.Errorf("inspect peer credentials: %w", err)
	}
	if sockErr != nil {
		return 0, fmt.Errorf("inspect peer credentials: %w", sockErr)
	}
	if uid <= 0 {
		return 0, fmt.Errorf("peer uid must be positive, got %d", uid)
	}
	return uid, nil
}
