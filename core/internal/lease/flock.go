package lease

import (
	"errors"
	"os"
	"syscall"
)

// flock applies how to f's descriptor. It goes through SyscallConn so
// the descriptor is never switched to blocking mode, and it retries
// when a signal interrupts the call.
func flock(f *os.File, how int) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var callErr error
	ctrlErr := rc.Control(func(fd uintptr) {
		for {
			callErr = syscall.Flock(int(fd), how)
			if !errors.Is(callErr, syscall.EINTR) {
				return
			}
		}
	})
	if ctrlErr != nil {
		return ctrlErr
	}
	return callErr
}
