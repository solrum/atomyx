package process

import (
	"errors"
	"fmt"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// sysctlKinfoProcName is the sysctl that returns the kinfo_proc record
// of one pid.
const sysctlKinfoProcName = "kern.proc.pid"

// sysctlBootTimeName is the sysctl that holds the host boot time.
const sysctlBootTimeName = "kern.boottime"

func inspectProcess(pid int) (ProcessInfo, error) {
	kp, err := unix.SysctlKinfoProc(sysctlKinfoProcName, pid)
	if err != nil {
		// The kernel answers an unknown pid with an empty record, which
		// the binding reports as an I/O error.
		if errors.Is(err, unix.EIO) || errors.Is(err, unix.ESRCH) {
			return ProcessInfo{}, fmt.Errorf("inspect process %d: %w", pid, ErrProcessNotFound)
		}
		return ProcessInfo{}, fmt.Errorf("inspect process %d: %w", pid, err)
	}
	if int(kp.Proc.P_pid) != pid {
		return ProcessInfo{}, fmt.Errorf("inspect process %d: %w", pid, ErrProcessNotFound)
	}
	return ProcessInfo{
		PID:       pid,
		PGID:      int(kp.Eproc.Pgid),
		UID:       int(kp.Eproc.Ucred.Uid),
		Command:   commandBase(cString(kp.Proc.P_comm[:])),
		StartTime: time.Unix(int64(kp.Proc.P_starttime.Sec), int64(kp.Proc.P_starttime.Usec)*int64(time.Microsecond)).UTC(),
	}, nil
}

func cString(raw []byte) string {
	out := make([]byte, 0, len(raw))
	for _, c := range raw {
		if c == 0 {
			break
		}
		out = append(out, c)
	}
	return string(out)
}

func hostBootTime() (time.Time, error) {
	tv, err := unix.SysctlTimeval(sysctlBootTimeName)
	if err != nil {
		return time.Time{}, fmt.Errorf("read the host boot time: %w", err)
	}
	return time.Unix(int64(tv.Sec), int64(tv.Usec)*int64(time.Microsecond)).UTC(), nil
}

func ownProcessGroup() int { return syscall.Getpgrp() }
