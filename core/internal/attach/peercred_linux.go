package attach

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func platformPeerUID(c *net.UnixConn) (uint32, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("attach: reading peer credentials: %w", err)
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, fmt.Errorf("attach: reading peer credentials: %w", err)
	}
	if credErr != nil {
		return 0, fmt.Errorf("attach: reading peer credentials: %w", credErr)
	}
	return cred.Uid, nil
}
