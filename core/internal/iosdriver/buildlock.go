package iosdriver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"

	"dev.atomyx/internal/clock"
)

// acquireBuildLock takes an exclusive advisory lock on the file at
// path, creating it when missing, and returns the function that
// releases it. A lock held elsewhere is retried every
// xctestBuildLockPollMs on clk until ctx ends, so a launch waiting on
// another launch's build is as cancelable as any other wait. The lock
// is tied to the open file description, so it also serializes two
// launches inside one process and is dropped by the operating system if
// the holder exits.
func acquireBuildLock(ctx context.Context, clk clock.Clock, path string) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("iosdriver: open the build lock: %w", err)
	}
	for {
		lockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if lockErr == nil {
			break
		}
		if !errors.Is(lockErr, syscall.EWOULDBLOCK) && !errors.Is(lockErr, syscall.EINTR) {
			_ = f.Close()
			return nil, fmt.Errorf("iosdriver: lock the build directory: %w", lockErr)
		}
		if sleepErr := clk.Sleep(ctx, xctestBuildLockPollMs); sleepErr != nil {
			_ = f.Close()
			return nil, sleepErr
		}
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
