//go:build !darwin && !linux

package process

import (
	"errors"
	"syscall"
	"time"
)

var errInspectUnsupported = errors.New("process inspection is not supported on this platform")

func inspectProcess(int) (ProcessInfo, error) { return ProcessInfo{}, errInspectUnsupported }

func hostBootTime() (time.Time, error) { return time.Time{}, errInspectUnsupported }

func ownProcessGroup() int { return syscall.Getpgrp() }
